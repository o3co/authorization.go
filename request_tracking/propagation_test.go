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
	"net/http"
	"testing"
)

// Verify that the request ID is set in HTTP headers when it exists in context.
func TestSetRequestIDHeader_SetsWhenPresent(t *testing.T) {
	ctx := WithRequestID(context.Background(), "req-id-456")
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://example.com", nil)

	ok := SetRequestIDHeader(ctx, req)
	if !ok {
		t.Error("SetRequestIDHeader() = false, want true")
	}
	if got := req.Header.Get("x-request-id"); got != "req-id-456" {
		t.Errorf("x-request-id header = %q, want %q", got, "req-id-456")
	}
}

// Verify that false is returned and no header is set when request ID is absent from context.
func TestSetRequestIDHeader_SkipsWhenAbsent(t *testing.T) {
	ctx := context.Background()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://example.com", nil)

	ok := SetRequestIDHeader(ctx, req)
	if ok {
		t.Error("SetRequestIDHeader() = true, want false")
	}
	if got := req.Header.Get("x-request-id"); got != "" {
		t.Errorf("x-request-id header = %q, want empty", got)
	}
}

// Verify that an empty request ID is not set in the header.
func TestSetRequestIDHeader_SkipsWhenEmpty(t *testing.T) {
	ctx := WithRequestID(context.Background(), "")
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://example.com", nil)

	ok := SetRequestIDHeader(ctx, req)
	if ok {
		t.Error("SetRequestIDHeader() = true, want false")
	}
}
