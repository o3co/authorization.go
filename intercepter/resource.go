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

	pb "github.com/o3co/authorization.go/generated/go/schema"
)

// ResolvedPermission 解決済み権限情報
type ResolvedPolicy struct {
	Resource string // "sample:01KF6PF398G9PZK7JE075ZDM5S" (置換済み)
	Action   string // "read"
}

// Backward compatibility alias for the old misspelled type name.
// TODO: Consider deprecating ResolvedPlicy in favor of ResolvedPolicy.
type ResolvedPlicy = ResolvedPolicy
// RPCMethod は gRPC のフルメソッド名を分解した構造体
type RPCMethod struct {
	Service string
	Method  string
}

// ResourceResolverInterceptor protobufオプションとリクエストからリソースを解決
func ResourceResolverInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
	log.Printf("[resourceResolver] processing method: %s", info.FullMethod)

	// protobufからpermissionを取得
	permission, err := GetMethodPermission(info.FullMethod)

	if err != nil {
		log.Printf("[resourceResolver] failed to get method permission: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to get method permission: %v", err)
	}

	if permission == nil {
		// 権限定義なし、継続
		return handler(ctx, req)
	}

	log.Printf("[resourceResolver] permission: %+v", permission)

	// リソース解決処理
	resolvedResource, err := resolveResourceFromRequest(permission, req)

	// リソース解決に失敗した場合はInternalServerError
	if err != nil {
		return nil, status.Errorf(codes.Internal, "resource resolved failed: %v", err)
	}

	ctx = context.WithValue(ctx, "resource", resolvedResource.Resource)
	ctx = context.WithValue(ctx, "action", resolvedResource.Action)

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

// GetMethodPermission protobufメソッドから権限情報を取得
func GetMethodPermission(fullMethodName string) (*pb.Policy, error) {
	// "/sample.v1.SampleService/SearchSamples" -> RPCMethod{Service: "sample.v1.SampleService", Method: "SearchSamples"}
	mm, err := parseFullMethodName(fullMethodName)

	if err != nil {
		return nil, err
	}

	serviceName, methodName := mm.Service, mm.Method

	//.protoファイルで定義されたサービス（例：SampleService）のメタ情報を保持するオブジェクトです。
	var serviceDesc protoreflect.ServiceDescriptor

	log.Printf("[GetMethodPermission] 探しているサービス名: %s", serviceName)

	//Protocol Buffersの.protoファイル1つ分のメタ情報を表すインターフェース
	//1つの.protoファイル（例：sample.proto）全体の情報を保持するオブジェクト
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		log.Printf("[GetMethodPermission] .protoファイルをチェック中: %s", fd.Path())

		// 各ファイルで定義されているサービス一覧を取得
		// Services{{Name: ServerReflection, Methods: [{Name: ServerReflectionInfo, Input: grpc.reflection.v1.ServerReflectionRequest, Output: grpc.reflection.v1.ServerReflectionResponse, IsStreamingClient: true, IsStreamingServer: true}]}}
		services := fd.Services()

		log.Printf("[GetMethodPermission] このファイル内のサービス数: %d", services.Len())

		for i := 0; i < services.Len(); i++ {
			// i番目のサービスを取得
			svc := services.Get(i)

			svcName := string(svc.FullName())

			log.Printf("[GetMethodPermission] 発見したサービス [%d]: %s", i, svcName)

			// サービス名が一致するかチェック
			if svcName == serviceName {
				log.Printf("[GetMethodPermission] 🎯 サービスが見つかりました！ %s", svcName)

				serviceDesc = svc
				return false // 見つかったので停止
			}
		}

		log.Printf("[GetMethodPermission] このファイルには目的のサービスがありません、次へ...")
		return true // 継続
	})

	if serviceDesc == nil {
		log.Printf("[GetMethodPermission] サービスディスクリプターが見つかりませんでした")
		return nil, fmt.Errorf("service descriptor not found for %s", serviceName)
	}

	log.Printf("[GetMethodPermission] サービスディスクリプター: %s", serviceDesc.FullName())

	// メソッドディスクリプターを取得
	methodDesc := serviceDesc.Methods().ByName(protoreflect.Name(methodName))

	if methodDesc == nil {
		log.Printf("[GetMethodPermission] メソッドディスクリプターが見つかりませんでした: %s", methodName)
		return nil, fmt.Errorf("method %s not found in service %s", methodName, serviceName)
	}

	log.Printf("[GetMethodPermission] メソッドディスクリプター: %s", methodDesc.FullName())

	// メソッドオプションを取得
	opts := methodDesc.Options()
	if opts == nil {
		return nil, nil // オプションが設定されていない
	}

	methodOptions, ok := opts.(*descriptorpb.MethodOptions)
	if !ok {
		log.Printf("[GetMethodPermission] 内部エラー: 予期しないメソッドオプション型: %T", opts)
		return nil, fmt.Errorf("internal error: unexpected method options type %T", opts)
	}

	log.Printf("[GetMethodPermission] メソッドオプション: %v", methodOptions)
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

// resolveResourceFromRequest リソースを解決 (permission とリクエストからプレースホルダを置換)
func resolveResourceFromRequest(permission *pb.Policy, req interface{}) (*ResolvedPlicy, error) {
	// permissionがnilの場合は解決できないのでnilを返す
	if permission == nil {
		return nil, nil
	}

	resource := permission.Resource

	log.Printf("[resolveResourceFromRequest] original resource template: %s", resource)

	// resource_fieldsでテンプレート置換
	for _, field := range permission.FieldMappings {
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

	return &ResolvedPlicy{
		Resource: resource,
		Action:   permission.Action,
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

		if fd.HasOptionalKeyword() {
			return "", fmt.Errorf("field %s is optional, value may not be set", fieldPath)
		}

		if fd != nil {
			val := m.Get(fd)

			if !m.Has(fd) {
				return "", fmt.Errorf("field %s is not set in request", fieldPath)
			}

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
	}

	return "", fmt.Errorf("field %s not found in request", fieldPath)
}