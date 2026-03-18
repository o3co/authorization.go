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

package endpoint

import "context"

// VerifierEndpoint はポリシー検証を行うエンドポイントのインターフェース。
// 実装は REST、gRPC など複数想定されるが、現状は REST のみ。
type VerifierEndpoint interface {
	Verify(ctx context.Context, resource, action string) error
}

type contextKey struct{}

// WithRequestID x-request-id を context に保存する（interceptor から呼び出す）。
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, contextKey{}, requestID)
}

// RequestIDFromContext context から x-request-id を取得する。
func RequestIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(contextKey{}).(string)
	return v
}
