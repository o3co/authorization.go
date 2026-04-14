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

package endpoint

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// --- NewRESTEndpoint ---

// Verify that passing an empty string returns an error.
func TestNewRESTEndpoint_EmptyURL_ReturnsError(t *testing.T) {
	_, err := NewRESTEndpoint("")
	if err == nil {
		t.Error("expected error for empty URL")
	}
}

// Verify that passing a whitespace-only string returns an error (becomes empty after TrimSpace).
func TestNewRESTEndpoint_WhitespaceOnlyURL_ReturnsError(t *testing.T) {
	_, err := NewRESTEndpoint("   ")
	if err == nil {
		t.Error("expected error for whitespace-only URL")
	}
}

// Verify that "/verify" is appended when a valid URL is provided.
func TestNewRESTEndpoint_ValidURL_AppendsVerifyPath(t *testing.T) {
	ep, err := NewRESTEndpoint("http://localhost:8080")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := ep.(*restPolicyVerifierEndpoint)
	if r.verifyURL != "http://localhost:8080/verify" {
		t.Errorf("verifyURL = %q, want %q", r.verifyURL, "http://localhost:8080/verify")
	}
}

// Verify that "/verify" is appended correctly even when the URL has a trailing "/" (no double slash).
func TestNewRESTEndpoint_TrailingSlash_NormalizedCorrectly(t *testing.T) {
	ep, err := NewRESTEndpoint("http://localhost:8080/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := ep.(*restPolicyVerifierEndpoint)
	if r.verifyURL != "http://localhost:8080/verify" {
		t.Errorf("verifyURL = %q, want %q", r.verifyURL, "http://localhost:8080/verify")
	}
}

// Verify that "/verify" is appended to a URL that includes a base path.
func TestNewRESTEndpoint_WithBasePath_AppendsVerify(t *testing.T) {
	ep, err := NewRESTEndpoint("http://localhost:8080/api/v1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := ep.(*restPolicyVerifierEndpoint)
	if r.verifyURL != "http://localhost:8080/api/v1/verify" {
		t.Errorf("verifyURL = %q, want %q", r.verifyURL, "http://localhost:8080/api/v1/verify")
	}
}

// Verify that a URL without a scheme is automatically prefixed with "http://".
func TestNewRESTEndpoint_NoScheme_DefaultsToHTTP(t *testing.T) {
	ep, err := NewRESTEndpoint("localhost:8080")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := ep.(*restPolicyVerifierEndpoint)
	if r.verifyURL != "http://localhost:8080/verify" {
		t.Errorf("verifyURL = %q, want %q", r.verifyURL, "http://localhost:8080/verify")
	}
}

// --- WithTimeout ---

// Verify that a zero timeout panics as an invalid configuration.
func TestWithTimeout_Zero_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for zero timeout")
		}
	}()
	WithTimeout(0)
}

// Verify that a negative timeout panics as an invalid configuration.
func TestWithTimeout_Negative_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for negative timeout")
		}
	}()
	WithTimeout(-1 * time.Second)
}

// Verify that a valid timeout value is set on the HTTP client.
func TestWithTimeout_Valid_SetsClientTimeout(t *testing.T) {
	ep, err := NewRESTEndpoint("http://localhost", WithTimeout(5*time.Second))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := ep.(*restPolicyVerifierEndpoint)
	if r.httpClient.Timeout != 5*time.Second {
		t.Errorf("timeout = %v, want %v", r.httpClient.Timeout, 5*time.Second)
	}
}

// --- WithMaxResponseBodySize ---

// Verify that a zero body size limit panics as an invalid configuration.
func TestWithMaxResponseBodySize_Zero_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for zero body size")
		}
	}()
	WithMaxResponseBodySize(0)
}

// Verify that a negative body size limit panics as an invalid configuration.
func TestWithMaxResponseBodySize_Negative_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for negative body size")
		}
	}()
	WithMaxResponseBodySize(-1)
}

// Verify that a valid body size limit is stored in the field.
func TestWithMaxResponseBodySize_Valid_SetsField(t *testing.T) {
	ep, err := NewRESTEndpoint("http://localhost", WithMaxResponseBodySize(512))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := ep.(*restPolicyVerifierEndpoint)
	if r.maxResponseBodySize != 512 {
		t.Errorf("maxResponseBodySize = %d, want 512", r.maxResponseBodySize)
	}
}

// --- getToken ---

// Verify that a token cannot be retrieved from a context with no gRPC incoming metadata, returning an error.
func TestGetToken_NoMetadata_ReturnsError(t *testing.T) {
	ctx := context.Background()
	_, err := getToken(ctx)
	if err == nil {
		t.Error("expected error when no metadata in context")
	}
}

// Verify that an error is returned when the authorization header is absent.
func TestGetToken_NoAuthorizationHeader_ReturnsError(t *testing.T) {
	md := metadata.Pairs("other-header", "value")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	_, err := getToken(ctx)
	if err == nil {
		t.Error("expected error when authorization header is missing")
	}
}

// Verify that a single-word value (not in "Bearer <token>" format) returns an error as an invalid format.
func TestGetToken_SingleWordValue_ReturnsError(t *testing.T) {
	md := metadata.Pairs("authorization", "onlyone")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	_, err := getToken(ctx)
	if err == nil {
		t.Error("expected error for invalid authorization header format")
	}
}

// Verify that TokenType and Value are correctly parsed from a "Bearer <token>" value.
func TestGetToken_BearerToken_ReturnsCorrectFields(t *testing.T) {
	md := metadata.Pairs("authorization", "Bearer my-token-value")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	tok, err := getToken(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok.TokenType != "Bearer" {
		t.Errorf("TokenType = %q, want %q", tok.TokenType, "Bearer")
	}
	if tok.Value != "my-token-value" {
		t.Errorf("Value = %q, want %q", tok.Value, "my-token-value")
	}
}

// --- Verify (integration tests using httptest) ---

func newTestEndpoint(t *testing.T, serverURL string) VerifierEndpoint {
	t.Helper()
	ep, err := NewRESTEndpoint(serverURL)
	if err != nil {
		t.Fatalf("failed to create endpoint: %v", err)
	}
	return ep
}

func ctxWithBearerToken(token string) context.Context {
	md := metadata.Pairs("authorization", "Bearer "+token)
	return metadata.NewIncomingContext(context.Background(), md)
}

// Verify that nil is returned when the authorization service responds with 200.
func TestVerify_200_ReturnsNil(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ep := newTestEndpoint(t, server.URL)
	if err := ep.Verify(ctxWithBearerToken("token123"), "posts", "read"); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

// Verify that codes.PermissionDenied is returned when the authorization service responds with 403.
func TestVerify_403_ReturnsPermissionDenied(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	ep := newTestEndpoint(t, server.URL)
	err := ep.Verify(ctxWithBearerToken("token123"), "posts", "delete")
	assertGRPCCode(t, err, codes.PermissionDenied)
}

// Verify that codes.Unauthenticated is returned when the authorization service responds with 401.
func TestVerify_401_ReturnsUnauthenticated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	ep := newTestEndpoint(t, server.URL)
	err := ep.Verify(ctxWithBearerToken("expired-token"), "posts", "read")
	assertGRPCCode(t, err, codes.Unauthenticated)
}

// Verify that codes.Internal is returned when the authorization service responds with 500.
func TestVerify_500_ReturnsInternal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	ep := newTestEndpoint(t, server.URL)
	err := ep.Verify(ctxWithBearerToken("token123"), "posts", "read")
	assertGRPCCode(t, err, codes.Internal)
}

// Verify that when no token is present in gRPC metadata,
// codes.Unauthenticated is returned before sending an HTTP request to the server.
func TestVerify_NoToken_ReturnsUnauthenticatedWithoutCallingServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("server should not be called when no token is present")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ep := newTestEndpoint(t, server.URL)
	err := ep.Verify(context.Background(), "posts", "read")
	assertGRPCCode(t, err, codes.Unauthenticated)
}

// Verify that Authorization, x-request-id, and Content-Type headers
// are correctly set on the request to the authorization service.
func TestVerify_RequestHeaders_SetCorrectly(t *testing.T) {
	var capturedAuth, capturedRequestID, capturedContentType string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		capturedRequestID = r.Header.Get("x-request-id")
		capturedContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ep, err := NewRESTEndpoint(server.URL, WithRequestIDFunc(func(ctx context.Context) string {
		return "req-id-456"
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	md := metadata.Pairs("authorization", "Bearer my-token")
	ctx := metadata.NewIncomingContext(context.Background(), md)

	if err := ep.Verify(ctx, "posts", "read"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedAuth != "Bearer my-token" {
		t.Errorf("Authorization = %q, want %q", capturedAuth, "Bearer my-token")
	}
	if capturedRequestID != "req-id-456" {
		t.Errorf("x-request-id = %q, want %q", capturedRequestID, "req-id-456")
	}
	if capturedContentType != "application/json" {
		t.Errorf("Content-Type = %q, want %q", capturedContentType, "application/json")
	}
}

// Verify that when x-request-id is absent, the x-request-id header is not added
// to the request to the authorization service (do not forward a header that does not exist).
func TestVerify_NoRequestID_HeaderNotForwarded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if v := r.Header.Get("x-request-id"); v != "" {
			t.Errorf("x-request-id header should not be set, got %q", v)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ep := newTestEndpoint(t, server.URL)
	if err := ep.Verify(ctxWithBearerToken("token"), "posts", "read"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Verify that codes.Internal is returned when the caller's context times out.
func TestVerify_ContextTimeout_ReturnsInternal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Wait longer than the client timeout (50ms),
		// then exit via r.Context() cancellation or fallback timer.
		select {
		case <-r.Context().Done():
		case <-time.After(500 * time.Millisecond):
		}
	}))
	defer server.Close()

	ep := newTestEndpoint(t, server.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	md := metadata.Pairs("authorization", "Bearer token")
	ctx = metadata.NewIncomingContext(ctx, md)

	err := ep.Verify(ctx, "posts", "read")
	assertGRPCCode(t, err, codes.Internal)
}

// Verify that the request body to the authorization service contains resource and action as JSON.
func TestVerify_RequestBody_ContainsResourceAndAction(t *testing.T) {
	var body map[string]string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ep := newTestEndpoint(t, server.URL)
	if err := ep.Verify(ctxWithBearerToken("token"), "posts/123", "delete"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if body["resource"] != "posts/123" {
		t.Errorf("body[\"resource\"] = %q, want %q", body["resource"], "posts/123")
	}
	if body["action"] != "delete" {
		t.Errorf("body[\"action\"] = %q, want %q", body["action"], "delete")
	}
}

// Verify that the Accept header is set to "application/json".
func TestVerify_RequestHeader_AcceptIsJSON(t *testing.T) {
	var capturedAccept string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAccept = r.Header.Get("Accept")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ep := newTestEndpoint(t, server.URL)
	if err := ep.Verify(ctxWithBearerToken("token"), "posts", "read"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedAccept != "application/json" {
		t.Errorf("Accept = %q, want %q", capturedAccept, "application/json")
	}
}

// Verify that nil is returned for non-200 2xx statuses (201, 204) as authorization success.
func TestVerify_2xx_ReturnsNil(t *testing.T) {
	for _, statusCode := range []int{201, 204} {
		statusCode := statusCode
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(statusCode)
			}))
			defer server.Close()

			ep := newTestEndpoint(t, server.URL)
			if err := ep.Verify(ctxWithBearerToken("token"), "posts", "read"); err != nil {
				t.Errorf("expected nil for %d, got %v", statusCode, err)
			}
		})
	}
}

// Verify that non-auth-related 4xx statuses (400, 404, 422) return codes.Internal
// as an unexpected error from the authorization service.
func TestVerify_4xx_NotAuthRelated_ReturnsInternal(t *testing.T) {
	for _, statusCode := range []int{400, 404, 422} {
		statusCode := statusCode
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(statusCode)
			}))
			defer server.Close()

			ep := newTestEndpoint(t, server.URL)
			err := ep.Verify(ctxWithBearerToken("token"), "posts", "read")
			assertGRPCCode(t, err, codes.Internal)
		})
	}
}

// Verify that WithRequestIDHeaderKey changes the forwarded header key.
func TestVerify_WithRequestIDHeaderKey(t *testing.T) {
	var capturedHeader string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeader = r.Header.Get("X-Correlation-Id")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ep, err := NewRESTEndpoint(server.URL,
		WithRequestIDHeaderKey("X-Correlation-Id"),
		WithRequestIDFunc(func(ctx context.Context) string {
			return "req-custom-789"
		}),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := ep.Verify(ctxWithBearerToken("my-token"), "posts", "read"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedHeader != "req-custom-789" {
		t.Errorf("X-Correlation-Id = %q, want %q", capturedHeader, "req-custom-789")
	}
}

// Verify that empty requestIDHeaderKey disables forwarding.
func TestVerify_WithRequestIDHeaderKey_Empty_DisablesForwarding(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if v := r.Header.Get("x-request-id"); v != "" {
			t.Errorf("x-request-id should not be set, got %q", v)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ep, err := NewRESTEndpoint(server.URL,
		WithRequestIDHeaderKey(""),
		WithRequestIDFunc(func(ctx context.Context) string {
			return "should-not-forward"
		}),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := ep.Verify(ctxWithBearerToken("token"), "posts", "read"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestVerify_WithRequestIDFunc verifies that a custom function is used to extract the request ID.
func TestVerify_WithRequestIDFunc(t *testing.T) {
	var capturedRequestID string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRequestID = r.Header.Get("x-request-id")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ep, err := NewRESTEndpoint(server.URL, WithRequestIDFunc(func(ctx context.Context) string {
		return "func-injected-id"
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := ep.Verify(ctxWithBearerToken("token"), "posts", "read"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedRequestID != "func-injected-id" {
		t.Errorf("x-request-id = %q, want %q", capturedRequestID, "func-injected-id")
	}
}

// assertGRPCCode verifies that err is a gRPC status error with the expected code.
func assertGRPCCode(t *testing.T, err error, wantCode codes.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error with code %v, got nil", wantCode)
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got %T: %v", err, err)
	}
	if st.Code() != wantCode {
		t.Errorf("code = %v, want %v", st.Code(), wantCode)
	}
}
