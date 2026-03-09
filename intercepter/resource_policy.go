package intercepter

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"unicode/utf8"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"

	pb "github.com/o3co/authorization.go/generated/schema"
)

type ctxKey string

const ctxKeyPolicy ctxKey = "policy"

type Policy struct {
	Resource string
	Action   string
}

func WithPolicy(ctx context.Context, resource, action string) context.Context {
	return context.WithValue(ctx, ctxKeyPolicy, &Policy{
		Resource: resource,
		Action:   action,
	})
}

func WithPolicyMetadata(ctx context.Context, pm *Policy) context.Context {
	return context.WithValue(ctx, ctxKeyPolicy, pm)
}

func PolicyFromContext(ctx context.Context) (*Policy, bool) {
	v := ctx.Value(ctxKeyPolicy)

	if v == nil {
		return nil, false
	}

	pm, ok := v.(*Policy)

	return pm, ok
}

// RPCMethod は gRPC のフルメソッド名を分解した構造体
type RPCMethod struct {
	Service string
	Method  string
}

// ResourcePolicyInterceptor protobufオプションとリクエストからリソースを解決
func ResourcePolicyInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
	log.Printf("[resourcePolicyInterceptor] processing method: %s", info.FullMethod)

	// protobufからpermissionを取得
	policy, err := GetMethodPolicy(info.FullMethod)

	if err != nil {
		log.Printf("[resourcePolicyInterceptor] failed to get method policy: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to get method policy: %v", err)
	}

	if policy == nil {
		// 権限定義なし、継続
		return handler(ctx, req)
	}

	log.Printf("[resourcePolicyInterceptor] policy: %+v", policy)
	// リソース解決処理
	resolvedResource, err := resolveResourceFromRequest(policy, req)

	// リソース解決に失敗した場合はInternalServerError
	if err != nil {
		return nil, status.Errorf(codes.Internal, "resource resolved failed: %v", err)
	}

	ctx = WithPolicy(ctx, resolvedResource.Resource, resolvedResource.Action)

	return handler(ctx, req)
}

// フルメソッド名からサービスとメソッドを分解して取得する関数
func parseFullMethodName(fullMethodName string) (RPCMethod, error) {
	// fullMethodName は gRPC のフルメソッド名で、
	// 形式は "/<package>.<Service>/<Method>" です。
	// 例: "/sample.v1.SampleService/SearchSamples"

	// 空文字または先頭が '/' でない場合は形式が不正
	if len(fullMethodName) == 0 || fullMethodName[0] != '/' {
		return RPCMethod{}, fmt.Errorf("invalid full method format: %s", fullMethodName)
	}

	// 先頭の '/' を除去して実際の文字列部分を取り出す
	// 例: "sample.v1.SampleService/SearchSamples"
	method := fullMethodName[1:]

	// 最後の '/' を探し、サービス名とメソッド名を分割する
	// パッケージやサービス名に '/' は含まれない前提のため、最後の '/' を使う
	lastSlash := strings.LastIndex(method, "/")
	if lastSlash == -1 {
		return RPCMethod{}, fmt.Errorf("invalid full method format: %s", fullMethodName)
	}

	// 最後の '/' の 左側がサービス名、右側がメソッド名
	// serviceName: "sample.v1.SampleService"
	// methodName:  "SearchSamples"
	serviceName := method[:lastSlash]
	methodName := method[lastSlash+1:]

	// 分割した値を構造体で返す
	return RPCMethod{Service: serviceName, Method: methodName}, nil
}

// GetMethodPolicy protobufメソッドから権限情報を取得
func GetMethodPolicy(fullMethodName string) (*pb.Policy, error) {
	// "/sample.v1.SampleService/SearchSamples" -> RPCMethod{Service: "sample.v1.SampleService", Method: "SearchSamples"}
	mm, err := parseFullMethodName(fullMethodName)

	if err != nil {
		return nil, err
	}

	serviceName, methodName := mm.Service, mm.Method

	//.protoファイルで定義されたサービス（例：SampleService）のメタ情報を保持するオブジェクトです。
	var serviceDesc protoreflect.ServiceDescriptor

	log.Printf("[GetMethodPolicy] 探しているサービス名: %s", serviceName)

	//Protocol Buffersの.protoファイル1つ分のメタ情報を表すインターフェース
	//1つの.protoファイル（例：sample.proto）全体の情報を保持するオブジェクト
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		log.Printf("[GetMethodPolicy] .protoファイルをチェック中: %s", fd.Path())

		// 各ファイルで定義されているサービス一覧を取得
		// Services{{Name: ServerReflection, Methods: [{Name: ServerReflectionInfo, Input: grpc.reflection.v1.ServerReflectionRequest, Output: grpc.reflection.v1.ServerReflectionResponse, IsStreamingClient: true, IsStreamingServer: true}]}}
		services := fd.Services()

		log.Printf("[GetMethodPolicy] このファイル内のサービス数: %d", services.Len())

		for i := 0; i < services.Len(); i++ {
			// i番目のサービスを取得
			svc := services.Get(i)

			svcName := string(svc.FullName())

			log.Printf("[GetMethodPolicy] 発見したサービス [%d]: %s", i, svcName)

			// サービス名が一致するかチェック
			if svcName == serviceName {
				log.Printf("[GetMethodPolicy] 🎯 サービスが見つかりました！ %s", svcName)

				serviceDesc = svc
				return false // 見つかったので停止
			}
		}

		log.Printf("[GetMethodPolicy] このファイルには目的のサービスがありません、次へ...")
		return true // 継続
	})

	if serviceDesc == nil {
		log.Printf("[GetMethodPolicy] サービスディスクリプターが見つかりませんでした")
		return nil, fmt.Errorf("service descriptor not found for %s", serviceName)
	}

	log.Printf("[GetMethodPolicy] サービスディスクリプター: %s", serviceDesc.FullName())
	// メソッドディスクリプターを取得
	methodDesc := serviceDesc.Methods().ByName(protoreflect.Name(methodName))

	if methodDesc == nil {
		log.Printf("[GetMethodPolicy] メソッドディスクリプターが見つかりませんでした: %s", methodName)
		return nil, fmt.Errorf("method %s not found in service %s", methodName, serviceName)
	}

	log.Printf("[GetMethodPolicy] メソッドディスクリプター: %s", methodDesc.FullName())

	// メソッドオプションを取得
	opts := methodDesc.Options()

	if opts == nil {
		return nil, nil // オプションが設定されていない
	}

	methodOptions, ok := opts.(*descriptorpb.MethodOptions)

	if !ok {
		log.Printf("[GetMethodPolicy] 内部エラー: 予期しないメソッドオプション型: %T", opts)
		return nil, fmt.Errorf("internal error: unexpected method options type %T", opts)
	}

	log.Printf("[GetMethodPolicy] メソッドオプション: %v", methodOptions)
	// カスタムpermissionオプションを抽出
	// 拡張が存在するかチェック
	if proto.HasExtension(methodOptions, pb.E_Policy) {
		//拡張の値を取得
		ext := proto.GetExtension(methodOptions, pb.E_Policy)

		// 型アサーションでPermission型に変換
		if permission, ok := ext.(*pb.Policy); ok {
			return permission, nil
		}
	}

	return nil, nil // permissionオプションが設定されていない
}

// resolveResourceFromRequest リソースを解決 (policy とリクエストからプレースホルダを置換)
func resolveResourceFromRequest(policy *pb.Policy, req interface{}) (*Policy, error) {
	// permissionがnilの場合は解決できないのでnilを返す
	if policy == nil {
		return nil, nil
	}

	resource := policy.Resource

	log.Printf("[resolveResourceFromRequest] original resource template: %s", resource)

	// resource_fieldsでテンプレート置換
	for _, field := range policy.FieldMappings {
		// プレースホルダーの形式は "<field_name>" とする
		placeholder := fmt.Sprintf("<%s>", field.Placeholder)

		//リソーステンプレートに該当するプレースホルダーが含まれているかチェック
		if strings.Contains(resource, placeholder) {
			var value string

			value, err := extractFieldFromRequest(req, field.RequestField)

			if err != nil {
				log.Printf("[resolveResourceFromRequest] failed to extract field %s: %v", field.RequestField, err)

				return nil, fmt.Errorf("failed to extract field %s: %v", field.RequestField, err)
			}

			resource = strings.Replace(resource, placeholder, value, -1)
		}
	}

	log.Printf("[resolveResourceFromRequest] final resource: %s", resource)

	return &Policy{
		Resource: resource,
		Action:   policy.Action,
	}, nil
}

// extractFieldFromRequest リクエストからフィールド値を抽出（リフレクション使用）
func extractFieldFromRequest(req interface{}, fieldPath string) (string, error) {
	log.Printf("[extractFieldFromRequest] attempting to extract field: %s from request type: %T", fieldPath, req)

	// まずprotobufの反射APIで安全に取得を試みる（生成されたメッセージで確実に動作）
	if pm, ok := req.(proto.Message); ok {
		m := pm.ProtoReflect()
		// proto側のフィールド名で検索（例: "id"）
		fd := m.Descriptor().Fields().ByName(protoreflect.Name(fieldPath))

		if fd == nil {
			return "", fmt.Errorf("field %s not found in request", fieldPath)
		}

		if !m.Has(fd) {
			return "", fmt.Errorf("field %s is not set in request", fieldPath)
		}

		val := m.Get(fd)

		// list/mapは未対応
		if fd.IsList() || fd.IsMap() {
			log.Printf("[extractFieldFromRequest] field %s is list/map, unsupported", fieldPath)
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
		case protoreflect.Uint32Kind, protoreflect.Uint64Kind:
			return fmt.Sprintf("%d", val.Uint()), nil
		case protoreflect.BoolKind:
			return fmt.Sprintf("%v", val.Bool()), nil
		default:
			log.Printf("[extractFieldFromRequest] unsupported proto field kind %s for %s", fd.Kind(), fieldPath)
			return "", fmt.Errorf("unsupported proto field kind %s for %s", fd.Kind(), fieldPath)
		}
	}

	return "", fmt.Errorf("field %s not found in request", fieldPath)
}
