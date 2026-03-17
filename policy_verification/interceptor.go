package interceptor

import (
	"context"
	"log"

	client "github.com/o3co/authorization.go/policy_verification/client"
	policy "github.com/o3co/authorization.go/protobuf_policy_option"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Interceptor 認可チェックを行うインターセプター
func Interceptor(verifierClient client.VerifierClient) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		log.Printf("[interceptor] processing method: %s", info.FullMethod)

		// protobuf_policy_option.Interceptor が実行済みかチェック
		// 未登録の場合はチェーン設定ミスとして Internal エラーを返す
		if !policy.InterceptorRanFromContext(ctx) {
			return nil, status.Error(codes.Internal,
				"protobuf_policy_option.Interceptor is not registered in the interceptor chain")
		}

		// contextから解決済みポリシーメタデータを取得
		policyData, ok := policy.PolicyFromContext(ctx)

		if !ok {
			// Interceptor は実行済みだが、このメソッドにポリシー定義がない（認可不要）
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
