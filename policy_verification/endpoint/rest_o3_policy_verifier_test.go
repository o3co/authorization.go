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

// 空文字を渡した場合はエラーを返すことを確認する。
func TestNewRESTEndpoint_EmptyURL_ReturnsError(t *testing.T) {
	_, err := NewRESTEndpoint("")
	if err == nil {
		t.Error("expected error for empty URL")
	}
}

// スペースのみの文字列を渡した場合はエラーを返すことを確認する（TrimSpace 後に空になる）。
func TestNewRESTEndpoint_WhitespaceOnlyURL_ReturnsError(t *testing.T) {
	_, err := NewRESTEndpoint("   ")
	if err == nil {
		t.Error("expected error for whitespace-only URL")
	}
}

// 正常な URL を渡した場合に "/verify" が末尾に付加されることを確認する。
func TestNewRESTEndpoint_ValidURL_AppendsVerifyPath(t *testing.T) {
	ep, err := NewRESTEndpoint("http://localhost:8080")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := ep.(*restO3PolicyVerifierEndpoint)
	if r.verifyURL != "http://localhost:8080/verify" {
		t.Errorf("verifyURL = %q, want %q", r.verifyURL, "http://localhost:8080/verify")
	}
}

// 末尾に "/" がある URL でも "/verify" が正しく付加されることを確認する（二重スラッシュにならない）。
func TestNewRESTEndpoint_TrailingSlash_NormalizedCorrectly(t *testing.T) {
	ep, err := NewRESTEndpoint("http://localhost:8080/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := ep.(*restO3PolicyVerifierEndpoint)
	if r.verifyURL != "http://localhost:8080/verify" {
		t.Errorf("verifyURL = %q, want %q", r.verifyURL, "http://localhost:8080/verify")
	}
}

// ベースパスが含まれる URL の末尾に "/verify" が付加されることを確認する。
func TestNewRESTEndpoint_WithBasePath_AppendsVerify(t *testing.T) {
	ep, err := NewRESTEndpoint("http://localhost:8080/api/v1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := ep.(*restO3PolicyVerifierEndpoint)
	if r.verifyURL != "http://localhost:8080/api/v1/verify" {
		t.Errorf("verifyURL = %q, want %q", r.verifyURL, "http://localhost:8080/api/v1/verify")
	}
}

// スキームなしの URL は自動的に "http://" を補完することを確認する。
func TestNewRESTEndpoint_NoScheme_DefaultsToHTTP(t *testing.T) {
	ep, err := NewRESTEndpoint("localhost:8080")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := ep.(*restO3PolicyVerifierEndpoint)
	if r.verifyURL != "http://localhost:8080/verify" {
		t.Errorf("verifyURL = %q, want %q", r.verifyURL, "http://localhost:8080/verify")
	}
}

// --- WithTimeout ---

// ゼロのタイムアウトは不正な設定として panic することを確認する。
func TestWithTimeout_Zero_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for zero timeout")
		}
	}()
	WithTimeout(0)
}

// 負のタイムアウトは不正な設定として panic することを確認する。
func TestWithTimeout_Negative_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for negative timeout")
		}
	}()
	WithTimeout(-1 * time.Second)
}

// 正常なタイムアウト値が HTTP クライアントに設定されることを確認する。
func TestWithTimeout_Valid_SetsClientTimeout(t *testing.T) {
	ep, err := NewRESTEndpoint("http://localhost", WithTimeout(5*time.Second))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := ep.(*restO3PolicyVerifierEndpoint)
	if r.httpClient.Timeout != 5*time.Second {
		t.Errorf("timeout = %v, want %v", r.httpClient.Timeout, 5*time.Second)
	}
}

// --- WithMaxResponseBodySize ---

// ゼロのボディサイズ上限は不正な設定として panic することを確認する。
func TestWithMaxResponseBodySize_Zero_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for zero body size")
		}
	}()
	WithMaxResponseBodySize(0)
}

// 負のボディサイズ上限は不正な設定として panic することを確認する。
func TestWithMaxResponseBodySize_Negative_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for negative body size")
		}
	}()
	WithMaxResponseBodySize(-1)
}

// 正常なボディサイズ上限がフィールドに設定されることを確認する。
func TestWithMaxResponseBodySize_Valid_SetsField(t *testing.T) {
	ep, err := NewRESTEndpoint("http://localhost", WithMaxResponseBodySize(512))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := ep.(*restO3PolicyVerifierEndpoint)
	if r.maxResponseBodySize != 512 {
		t.Errorf("maxResponseBodySize = %d, want 512", r.maxResponseBodySize)
	}
}

// --- getToken ---

// gRPC incoming metadata がない context からはトークンを取得できずエラーになることを確認する。
func TestGetToken_NoMetadata_ReturnsError(t *testing.T) {
	ctx := context.Background()
	_, err := getToken(ctx)
	if err == nil {
		t.Error("expected error when no metadata in context")
	}
}

// authorization ヘッダーが存在しない場合はエラーになることを確認する。
func TestGetToken_NoAuthorizationHeader_ReturnsError(t *testing.T) {
	md := metadata.Pairs("other-header", "value")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	_, err := getToken(ctx)
	if err == nil {
		t.Error("expected error when authorization header is missing")
	}
}

// "Bearer <token>" 形式ではなく単語が1つだけの値はフォーマット不正としてエラーになることを確認する。
func TestGetToken_SingleWordValue_ReturnsError(t *testing.T) {
	md := metadata.Pairs("authorization", "onlyone")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	_, err := getToken(ctx)
	if err == nil {
		t.Error("expected error for invalid authorization header format")
	}
}

// "Bearer <token>" 形式の値から TokenType と Value が正しく分解されることを確認する。
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

// --- Verify (httptest を使った結合テスト) ---

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

// 認可サービスが 200 を返した場合は nil を返すことを確認する。
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

// 認可サービスが 403 を返した場合は codes.PermissionDenied を返すことを確認する。
func TestVerify_403_ReturnsPermissionDenied(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	ep := newTestEndpoint(t, server.URL)
	err := ep.Verify(ctxWithBearerToken("token123"), "posts", "delete")
	assertGRPCCode(t, err, codes.PermissionDenied)
}

// 認可サービスが 401 を返した場合は codes.Unauthenticated を返すことを確認する。
func TestVerify_401_ReturnsUnauthenticated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	ep := newTestEndpoint(t, server.URL)
	err := ep.Verify(ctxWithBearerToken("expired-token"), "posts", "read")
	assertGRPCCode(t, err, codes.Unauthenticated)
}

// 認可サービスが 500 を返した場合は codes.Internal を返すことを確認する。
func TestVerify_500_ReturnsInternal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	ep := newTestEndpoint(t, server.URL)
	err := ep.Verify(ctxWithBearerToken("token123"), "posts", "read")
	assertGRPCCode(t, err, codes.Internal)
}

// gRPC metadata にトークンがない場合はサーバーに HTTP リクエストを送る前に
// codes.Unauthenticated を返すことを確認する。
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

// Authorization / x-request-id / Content-Type ヘッダーが
// 認可サービスへのリクエストに正しく設定されることを確認する。
func TestVerify_RequestHeaders_SetCorrectly(t *testing.T) {
	var capturedAuth, capturedRequestID, capturedContentType string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		capturedRequestID = r.Header.Get("x-request-id")
		capturedContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ep := newTestEndpoint(t, server.URL)
	md := metadata.Pairs("authorization", "Bearer my-token")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	ctx = WithRequestID(ctx, "req-id-456")

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

// x-request-id がない場合は認可サービスへのリクエストに x-request-id ヘッダーを
// 付加しないことを確認する（存在しないヘッダーを転送しない）。
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

// 呼び出し元の context がタイムアウトした場合は codes.Internal を返すことを確認する。
func TestVerify_ContextTimeout_ReturnsInternal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// クライアントタイムアウト (50ms) より長く待機し、
		// r.Context() キャンセルまたはフォールバックタイマーで抜ける
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

// 認可サービスへのリクエストボディに resource と action が JSON で含まれることを確認する。
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

// Accept ヘッダーが "application/json" に設定されることを確認する。
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

// 200 以外の 2xx ステータス（201, 204）でも認可成功として nil を返すことを確認する。
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

// 401/403 以外の 4xx ステータス（400, 404, 422）は認可サービス側の予期しないエラーとして
// codes.Internal を返すことを確認する。
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

	ep, err := NewRESTEndpoint(server.URL, WithRequestIDHeaderKey("X-Correlation-Id"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	md := metadata.Pairs("authorization", "Bearer my-token")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	ctx = WithRequestID(ctx, "req-custom-789")

	if err := ep.Verify(ctx, "posts", "read"); err != nil {
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

	ep, err := NewRESTEndpoint(server.URL, WithRequestIDHeaderKey(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := ctxWithBearerToken("token")
	ctx = WithRequestID(ctx, "should-not-forward")

	if err := ep.Verify(ctx, "posts", "read"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// assertGRPCCode は err が gRPC ステータスエラーであり、期待するコードを持つことを検証する
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
