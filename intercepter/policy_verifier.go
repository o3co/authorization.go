package intercepter

import (
	"context"
	"log"

	client "github.com/o3co/authorization.go/client"

	"google.golang.org/grpc"
)

// PolicyVerifierInterceptor 認可チェックを行うインターセプター
func PolicyVerifierInterceptor(permissionVerifierClient client.PermissionVerifierClient) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		log.Printf("[policyVerifierInterceptor] processing method: %s", info.FullMethod)

		// contextから解決済みポリシーメタデータを取得
		policy, ok := PolicyFromContext(ctx)

		if !ok {
			// リソース情報がない場合は認可チェックできないので処理継続
			return handler(ctx, req)
		}

		resource := policy.Resource
		action := policy.Action

		// ログでリソース情報を表示
		log.Printf("[authorizationInterceptor] Resource: %s", resource)
		log.Printf("[authorizationInterceptor] Action: %s", action)

		// 認可チェック実行（クライアントはstatusエラーを返す設計）
		if err := permissionVerifierClient.Verify(ctx, resource, action); err != nil {
			log.Printf("[authorizationInterceptor] authorization check failed: %v", err)

			return nil, err
		}

		log.Printf("[authorizationInterceptor] authorization check passed for resource: %s, action: %s", resource, action)

		return handler(ctx, req)
	}
}
