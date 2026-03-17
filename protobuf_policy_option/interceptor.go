package policyoption

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"unicode/utf8"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"

	pb "github.com/o3co/authorization.go/protobuf_policy_option/schema"
)

type ctxKey string

const (
	ctxKeyPolicy         ctxKey = "o3:policy"
	ctxKeyInterceptorRan ctxKey = "o3:interceptor_ran"
)

type Policy struct {
	Resource string
	Action   string
}

func withPolicy(ctx context.Context, resource, action string) context.Context {
	return context.WithValue(ctx, ctxKeyPolicy, &Policy{
		Resource: resource,
		Action:   action,
	})
}

// markInterceptorRan Interceptor が実行されたことを ctx に記録する（内部用）
func markInterceptorRan(ctx context.Context) context.Context {
	return context.WithValue(ctx, ctxKeyInterceptorRan, true)
}

// InterceptorRanFromContext protobuf_policy_option.Interceptor が実行済みかどうかを返す。
// policy_verification.Interceptor でチェーンの設定ミスを検出するために使用する。
func InterceptorRanFromContext(ctx context.Context) bool {
	return ctx.Value(ctxKeyInterceptorRan) != nil
}

func PolicyFromContext(ctx context.Context) (*Policy, bool) {
	v := ctx.Value(ctxKeyPolicy)

	if v == nil {
		return nil, false
	}

	pm, ok := v.(*Policy)

	return pm, ok
}

// config はインターセプターの設定
type config struct {
	logLevel slog.Level
}

// Option はインターセプターの設定オプション
type Option func(*config)

// WithLogLevel ログレベルを指定する。未指定時のデフォルトは slog.LevelError。
func WithLogLevel(level slog.Level) Option {
	return func(c *config) {
		c.logLevel = level
	}
}

// rpcMethod は gRPC のフルメソッド名を分解した構造体
type rpcMethod struct {
	Service string
	Method  string
}

// Interceptor protobufオプションとリクエストからリソースを解決
func Interceptor(opts ...Option) grpc.UnaryServerInterceptor {
	cfg := &config{logLevel: slog.LevelError}
	for _, opt := range opts {
		opt(cfg)
	}
	log := newLogger(cfg.logLevel)

	var cache sync.Map // インターセプターインスタンスごとのキャッシュ（テスト間の汚染を防ぐ）

	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		log.Debug("processing method", "method", info.FullMethod)

		// protobufからpermissionを取得
		policy, err := getMethodPolicy(&cache, log, info.FullMethod)

		if err != nil {
			log.Error("failed to get method policy", "method", info.FullMethod, "error", err)
			return nil, status.Errorf(codes.Internal, "failed to get method policy: %v", err)
		}

		// Interceptor が実行されたことを常にマーク（policy_verification 側でチェーン設定ミスを検出するため）
		ctx = markInterceptorRan(ctx)

		if policy == nil {
			// 権限定義なし、継続
			return handler(ctx, req)
		}

		log.Debug("policy resolved", "resource", policy.Resource, "action", policy.Action)

		// リソース解決処理
		resolvedResource, err := resolveResourceFromRequest(log, policy, req)

		// リソース解決に失敗した場合はInternalServerError
		if err != nil {
			return nil, status.Errorf(codes.Internal, "resource resolved failed: %v", err)
		}

		ctx = withPolicy(ctx, resolvedResource.Resource, resolvedResource.Action)

		return handler(ctx, req)
	}
}

// フルメソッド名からサービスとメソッドを分解して取得する関数
func parseFullMethodName(fullMethodName string) (rpcMethod, error) {
	// fullMethodName は gRPC のフルメソッド名で、
	// 形式は "/<package>.<Service>/<Method>" です。
	// 例: "/sample.v1.SampleService/SearchSamples"

	// 空文字または先頭が '/' でない場合は形式が不正
	if len(fullMethodName) == 0 || fullMethodName[0] != '/' {
		return rpcMethod{}, fmt.Errorf("invalid full method format: %s", fullMethodName)
	}

	// 先頭の '/' を除去して実際の文字列部分を取り出す
	// 例: "sample.v1.SampleService/SearchSamples"
	method := fullMethodName[1:]

	// 最後の '/' を探し、サービス名とメソッド名を分割する
	// パッケージやサービス名に '/' は含まれない前提のため、最後の '/' を使う
	lastSlash := strings.LastIndex(method, "/")
	if lastSlash == -1 {
		return rpcMethod{}, fmt.Errorf("invalid full method format: %s", fullMethodName)
	}

	// 最後の '/' の 左側がサービス名、右側がメソッド名
	// serviceName: "sample.v1.SampleService"
	// methodName:  "SearchSamples"
	serviceName := method[:lastSlash]
	methodName := method[lastSlash+1:]

	// 分割した値を構造体で返す
	return rpcMethod{Service: serviceName, Method: methodName}, nil
}

// cachedPolicy は methodPolicyCache に格納するラッパー。
// policy == nil は「ポリシーなし」を意味し、キャッシュ未登録と区別する。
// err を持たせることでエラー結果もキャッシュし、再スキャンを防ぐ。
type cachedPolicy struct {
	policy *pb.Policy
	err    error
}

// getMethodPolicy protobufメソッドから権限情報を取得
func getMethodPolicy(cache *sync.Map, log *slog.Logger, fullMethodName string) (*pb.Policy, error) {
	// キャッシュヒット確認（2回目以降はスキャン不要）
	if v, ok := cache.Load(fullMethodName); ok {
		c := v.(*cachedPolicy)
		return c.policy, c.err
	}

	policy, err := lookupMethodPolicy(log, fullMethodName)

	// エラーも含めてキャッシュに登録（再スキャン防止）
	cache.Store(fullMethodName, &cachedPolicy{policy: policy, err: err})
	return policy, err
}

// lookupMethodPolicy GlobalFiles をスキャンしてポリシーを解決する。
// getMethodPolicy から初回のみ呼ばれる。
func lookupMethodPolicy(log *slog.Logger, fullMethodName string) (*pb.Policy, error) {
	mm, err := parseFullMethodName(fullMethodName)
	if err != nil {
		return nil, err
	}
	serviceName, methodName := mm.Service, mm.Method

	var serviceDesc protoreflect.ServiceDescriptor
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		services := fd.Services()
		for i := 0; i < services.Len(); i++ {
			svc := services.Get(i)
			if string(svc.FullName()) == serviceName {
				serviceDesc = svc
				return false // 発見したので停止
			}
		}
		return true // 次のファイルへ
	})

	if serviceDesc == nil {
		return nil, nil
	}

	methodDesc := serviceDesc.Methods().ByName(protoreflect.Name(methodName))
	if methodDesc == nil {
		return nil, nil
	}

	opts := methodDesc.Options()
	if opts == nil {
		return nil, nil
	}

	methodOptions, ok := opts.(*descriptorpb.MethodOptions)
	if !ok {
		return nil, fmt.Errorf("internal error: unexpected method options type %T", opts)
	}

	if proto.HasExtension(methodOptions, pb.E_Policy) {
		ext := proto.GetExtension(methodOptions, pb.E_Policy)
		if permission, ok := ext.(*pb.Policy); ok {
			log.Debug("policy found", "method", fullMethodName, "resource", permission.Resource, "action", permission.Action)
			return permission, nil
		}
	}

	return nil, nil
}

// resolveResourceFromRequest リソースを解決 (policy とリクエストからプレースホルダを置換)
func resolveResourceFromRequest(log *slog.Logger, policy *pb.Policy, req interface{}) (*Policy, error) {
	if policy.Resource == "" {
		return nil, fmt.Errorf("policy resource must not be empty")
	}
	if policy.Action == "" {
		return nil, fmt.Errorf("policy action must not be empty")
	}

	resource := policy.Resource

	log.Debug("resolving resource template", "template", resource)

	// resource_fieldsでテンプレート置換
	for _, field := range policy.FieldMappings {
		if field.Placeholder == "" || field.RequestField == "" {
			return nil, fmt.Errorf("invalid field mapping: placeholder and request_field must not be empty")
		}

		// プレースホルダーの形式は "<field_name>" とする
		placeholder := fmt.Sprintf("<%s>", field.Placeholder)

		//リソーステンプレートに該当するプレースホルダーが含まれているかチェック
		if strings.Contains(resource, placeholder) {
			value, err := extractFieldFromRequest(log, req, field.RequestField)

			if err != nil {
				log.Error("failed to extract field", "field", field.RequestField, "error", err)

				return nil, fmt.Errorf("failed to extract field %s: %v", field.RequestField, err)
			}

			resource = strings.ReplaceAll(resource, placeholder, value)
		}
	}

	log.Debug("resolved resource", "resource", resource)

	return &Policy{
		Resource: resource,
		Action:   policy.Action,
	}, nil
}

// extractFieldFromRequest リクエストからフィールド値を抽出（リフレクション使用）
func extractFieldFromRequest(log *slog.Logger, req interface{}, fieldPath string) (string, error) {
	log.Debug("extracting field from request", "field", fieldPath, "type", fmt.Sprintf("%T", req))

	// まずprotobufの反射APIで安全に取得を試みる（生成されたメッセージで確実に動作）
	if pm, ok := req.(proto.Message); ok {
		m := pm.ProtoReflect()
		// proto側のフィールド名で検索（例: "id"）
		fd := m.Descriptor().Fields().ByName(protoreflect.Name(fieldPath))

		if fd == nil {
			return "", fmt.Errorf("field %s not found in request", fieldPath)
		}

		val := m.Get(fd)

		// list/mapは未対応
		if fd.IsList() || fd.IsMap() {
			log.Error("unsupported field type: list/map not supported", "field", fieldPath)
			return "", fmt.Errorf("field %s is list/map, unsupported", fieldPath)
		}

		switch fd.Kind() {
		case protoreflect.StringKind:
			return val.String(), nil
		case protoreflect.BytesKind:
			b := val.Bytes()
			// UTF-8 の場合はそのまま文字列で返す
			if utf8.Valid(b) {
				return string(b), nil
			}
			// バイナリの場合は hex エンコードして返す
			return hex.EncodeToString(b), nil
		case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind,
			protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
			return fmt.Sprintf("%d", val.Int()), nil
		case protoreflect.Uint32Kind, protoreflect.Uint64Kind,
			protoreflect.Fixed32Kind, protoreflect.Fixed64Kind:
			return fmt.Sprintf("%d", val.Uint()), nil
		case protoreflect.BoolKind:
			return fmt.Sprintf("%v", val.Bool()), nil
		default:
			log.Error("unsupported proto field kind", "kind", fd.Kind(), "field", fieldPath)
			return "", fmt.Errorf("unsupported proto field kind %s for %s", fd.Kind(), fieldPath)
		}
	}

	return "", fmt.Errorf("request does not implement proto.Message (got %T)", req)
}
