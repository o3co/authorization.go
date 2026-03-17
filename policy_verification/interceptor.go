package policyverification

import (
	"context"
	"log/slog"

	client "github.com/o3co/authorization.go/policy_verification/client"
	policy "github.com/o3co/authorization.go/protobuf_policy_option"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

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

// Interceptor 認可チェックを行うインターセプター
func Interceptor(verifierClient client.VerifierClient, opts ...Option) grpc.UnaryServerInterceptor {
	if verifierClient == nil {
		panic("verifierClient must not be nil")
	}

	cfg := &config{logLevel: slog.LevelError}
	for _, opt := range opts {
		opt(cfg)
	}
	log := newLogger(cfg.logLevel)

	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		log.Debug("processing method", "method", info.FullMethod)

		// protobuf_policy_option.Interceptor が実行済みかチェック
		// 未登録の場合はチェーン設定ミスとして Internal エラーを返す
		if !policy.InterceptorRanFromContext(ctx) {
			log.Error("interceptor chain misconfiguration: protobuf_policy_option.Interceptor is not registered")
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

		log.Debug("verifying authorization", "resource", resource, "action", action)

		// 認可チェック実行（クライアントはstatusエラーを返す設計）
		if err := verifierClient.Verify(ctx, resource, action); err != nil {
			log.Error("authorization check failed", "resource", resource, "action", action, "error", err)

			return nil, err
		}

		log.Debug("authorization check passed", "resource", resource, "action", action)

		return handler(ctx, req)
	}
}
