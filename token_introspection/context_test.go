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

package tokenintrospection

import (
	"context"
	"testing"
	"time"
)

// Verify that WithResult stores a result that can be retrieved with ResultFromContext.
func TestWithResult_RoundTrip(t *testing.T) {
	result := &IntrospectionResult{
		Subject:   "user-123",
		Scopes:    []string{"read", "write"},
		ExpiresAt: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
		Claims:    map[string]any{"iss": "auth.provider"},
	}
	ctx := WithResult(context.Background(), result)
	got := ResultFromContext(ctx)
	if got == nil {
		t.Fatal("expected non-nil result")
	}
	if got.Subject != "user-123" {
		t.Errorf("Subject = %q, want %q", got.Subject, "user-123")
	}
	if len(got.Scopes) != 2 || got.Scopes[0] != "read" {
		t.Errorf("Scopes = %v, want [read write]", got.Scopes)
	}
}

// Verify that ResultFromContext returns nil when no result is stored.
func TestResultFromContext_Nil(t *testing.T) {
	got := ResultFromContext(context.Background())
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}
