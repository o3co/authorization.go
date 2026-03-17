// Copyright 2026 o3co Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package policyverification

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"time"

	client "github.com/o3co/grpc.authz/policy_verification/client"
	policy "github.com/o3co/grpc.authz/protobuf_policy_option"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// generateRequestID x-request-id を生成する。
// フォーマット: YYYYMMDDHHmmss_<uuid-v4-no-dashes>
func generateRequestID() string {
	now := time.Now().UTC()
	timestamp := now.Format("20060102150405")

	var b [16]byte
	_, _ = rand.Read(b[:])
	// UUID v4: version bits
	b[6] = (b[6] & 0x0f) | 0x40
	// UUID v4: variant bits
	b[8] = (b[8] & 0x3f) | 0x80

	return fmt.Sprintf("%s_%x", timestamp, b)
}

// extractOrGenerateRequestID incoming metadata から x-request-id を取得し、
// 存在しない場合は新たに生成して返す。
func extractOrGenerateRequestID(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if ok {
		if values := md["x-request-id"]; len(values) > 0 && values[0] != "" {
			return values[0]
		}
	}
	return generateRequestID()
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
		requestID := extractOrGenerateRequestID(ctx)
		ctx = client.WithRequestID(ctx, requestID)

		log.Debug("processing method", "method", info.FullMethod, "x-request-id", requestID)

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
