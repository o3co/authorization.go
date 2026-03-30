// Copyright 2026 1o1 Co. Ltd.
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

package requesttracking

import (
	"context"
	"testing"
)

// WithRequestID で保存した値を RequestIDFromContext で取得できることを確認する。
func TestWithRequestID_RoundTrip(t *testing.T) {
	ctx := WithRequestID(context.Background(), "test-id-123")
	got := RequestIDFromContext(ctx)
	if got != "test-id-123" {
		t.Errorf("RequestIDFromContext() = %q, want %q", got, "test-id-123")
	}
}

// context に RequestID が未設定の場合は空文字を返すことを確認する。
func TestRequestIDFromContext_Empty(t *testing.T) {
	ctx := context.Background()
	got := RequestIDFromContext(ctx)
	if got != "" {
		t.Errorf("RequestIDFromContext() = %q, want empty string", got)
	}
}

// 上書きした場合は最新の値が取得されることを確認する。
func TestWithRequestID_Overwrite(t *testing.T) {
	ctx := WithRequestID(context.Background(), "first")
	ctx = WithRequestID(ctx, "second")
	got := RequestIDFromContext(ctx)
	if got != "second" {
		t.Errorf("RequestIDFromContext() = %q, want %q", got, "second")
	}
}
