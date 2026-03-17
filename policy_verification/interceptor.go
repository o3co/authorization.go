package interceptor

import (
	"context"
	"log"

	client "github.com/o3co/authorization.go/policy_verification/client"
	policy "github.com/o3co/authorization.go/protobuf_policy_option"

	"google.golang.org/grpc"
)

// Interceptor 認可チェックを行うインターセプター
func Interceptor(verifierClient client.VerifierClient) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		log.Printf("[interceptor] processing method: %s", info.FullMethod)

		// contextから解決済みポリシーメタデータを取得
		policyData, ok := policy.PolicyFromContext(ctx)

		if !ok {
			// リソース情報がない場合は認可チェックできないので処理継続
			return handler(ctx, req)
		}

		resource := policyData.Resource
		action := policyData.Action

		// ログでリソース情報を表示
		log.Printf("[authorizationInterceptor] Resource: %s", resource)
		log.Printf("[authorizationInterceptor] Action: %s", action)

		// 認可チェック実行（クライアントはstatusエラーを返す設計）
		if err := verifierClient.Verify(ctx, resource, action); err != nil {
			log.Printf("[authorizationInterceptor] authorization check failed: %v", err)

			return nil, err
		}

		log.Printf("[authorizationInterceptor] authorization check passed for resource: %s, action: %s", resource, action)

		return handler(ctx, req)
	}
}
