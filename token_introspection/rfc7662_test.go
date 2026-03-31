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
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
)

// Verify Scheme() returns "bearer".
func TestRFC7662_Scheme(t *testing.T) {
	ep, err := NewRFC7662Introspector("http://localhost/oauth/introspect")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ep.Scheme() != "bearer" {
		t.Errorf("Scheme() = %q, want %q", ep.Scheme(), "bearer")
	}
}

// Verify active=true returns parsed IntrospectionResult.
func TestRFC7662_ActiveTrue_ReturnsResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"active": true,
			"sub":    "user-42",
			"scopes": []string{"read", "write"},
			"exp":    1735689600, // 2025-01-01T00:00:00Z
			"iss":    "auth.provider",
		})
	}))
	defer server.Close()

	ep, _ := NewRFC7662Introspector(server.URL)
	result, err := ep.Introspect(context.Background(), "my-jwt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Subject != "user-42" {
		t.Errorf("Subject = %q, want %q", result.Subject, "user-42")
	}
	if len(result.Scopes) != 2 || result.Scopes[0] != "read" {
		t.Errorf("Scopes = %v, want [read write]", result.Scopes)
	}
	if result.ExpiresAt.IsZero() {
		t.Error("expected non-zero ExpiresAt")
	}
	if result.Claims["iss"] != "auth.provider" {
		t.Errorf("Claims[iss] = %v, want %q", result.Claims["iss"], "auth.provider")
	}
}

// Verify active=false returns Unauthenticated.
func TestRFC7662_ActiveFalse_ReturnsUnauthenticated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"active": false})
	}))
	defer server.Close()

	ep, _ := NewRFC7662Introspector(server.URL)
	_, err := ep.Introspect(context.Background(), "expired-token")
	assertGRPCCode(t, err, codes.Unauthenticated)
}

// Verify 401 returns Unauthenticated.
func TestRFC7662_401_ReturnsUnauthenticated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	ep, _ := NewRFC7662Introspector(server.URL)
	_, err := ep.Introspect(context.Background(), "bad-token")
	assertGRPCCode(t, err, codes.Unauthenticated)
}

// Verify 500 returns Internal.
func TestRFC7662_500_ReturnsInternal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	ep, _ := NewRFC7662Introspector(server.URL)
	_, err := ep.Introspect(context.Background(), "token")
	assertGRPCCode(t, err, codes.Internal)
}

// Verify claim mapping: sub, scopes, exp go to struct fields; rest to Claims.
func TestRFC7662_ClaimMapping(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"active":  true,
			"sub":     "user-99",
			"scopes":  []string{"admin"},
			"exp":     1735689600,
			"aud":     "my-app",
			"client":  map[string]any{"id": "client-1"},
		})
	}))
	defer server.Close()

	ep, _ := NewRFC7662Introspector(server.URL)
	result, err := ep.Introspect(context.Background(), "token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Structured fields
	if result.Subject != "user-99" {
		t.Errorf("Subject = %q, want %q", result.Subject, "user-99")
	}
	wantExp := time.Unix(1735689600, 0).UTC()
	if !result.ExpiresAt.Equal(wantExp) {
		t.Errorf("ExpiresAt = %v, want %v", result.ExpiresAt, wantExp)
	}

	// Claims should contain aud, client but NOT sub, scopes, exp, active
	if _, ok := result.Claims["sub"]; ok {
		t.Error("Claims should not contain 'sub' (promoted to Subject)")
	}
	if _, ok := result.Claims["scopes"]; ok {
		t.Error("Claims should not contain 'scopes' (promoted to Scopes)")
	}
	if _, ok := result.Claims["exp"]; ok {
		t.Error("Claims should not contain 'exp' (promoted to ExpiresAt)")
	}
	if _, ok := result.Claims["active"]; ok {
		t.Error("Claims should not contain 'active'")
	}
	if result.Claims["aud"] != "my-app" {
		t.Errorf("Claims[aud] = %v, want %q", result.Claims["aud"], "my-app")
	}
}

// Verify request ID is forwarded when WithRequestIDFunc is set.
func TestRFC7662_RequestIDForwarded(t *testing.T) {
	var capturedHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeader = r.Header.Get("x-request-id")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"active": true, "sub": "u"})
	}))
	defer server.Close()

	ep, _ := NewRFC7662Introspector(server.URL,
		WithRequestIDFunc(func(_ context.Context) string { return "req-id-abc" }),
	)
	_, err := ep.Introspect(context.Background(), "token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedHeader != "req-id-abc" {
		t.Errorf("x-request-id = %q, want %q", capturedHeader, "req-id-abc")
	}
}

// Verify no request ID header when func is not set.
func TestRFC7662_RequestIDNotSet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if v := r.Header.Get("x-request-id"); v != "" {
			t.Errorf("x-request-id should not be set, got %q", v)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"active": true, "sub": "u"})
	}))
	defer server.Close()

	ep, _ := NewRFC7662Introspector(server.URL)
	_, _ = ep.Introspect(context.Background(), "token")
}

// Verify custom request ID header key.
func TestRFC7662_WithRequestIDHeaderKey(t *testing.T) {
	var capturedHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeader = r.Header.Get("X-Trace-Id")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"active": true, "sub": "u"})
	}))
	defer server.Close()

	ep, _ := NewRFC7662Introspector(server.URL,
		WithRequestIDFunc(func(_ context.Context) string { return "trace-xyz" }),
		WithRequestIDHeaderKey("X-Trace-Id"),
	)
	_, err := ep.Introspect(context.Background(), "token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedHeader != "trace-xyz" {
		t.Errorf("X-Trace-Id = %q, want %q", capturedHeader, "trace-xyz")
	}
}

// Verify the request body contains the token.
func TestRFC7662_RequestBody(t *testing.T) {
	var body map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"active": true, "sub": "u"})
	}))
	defer server.Close()

	ep, _ := NewRFC7662Introspector(server.URL)
	_, _ = ep.Introspect(context.Background(), "my-jwt-token")

	if body["token"] != "my-jwt-token" {
		t.Errorf("body[token] = %q, want %q", body["token"], "my-jwt-token")
	}
}

// Verify Authorization: Bearer <token> is set on the request.
func TestRFC7662_AuthorizationHeader(t *testing.T) {
	var capturedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"active": true, "sub": "u"})
	}))
	defer server.Close()

	ep, _ := NewRFC7662Introspector(server.URL)
	_, _ = ep.Introspect(context.Background(), "my-jwt-token")

	if capturedAuth != "Bearer my-jwt-token" {
		t.Errorf("Authorization = %q, want %q", capturedAuth, "Bearer my-jwt-token")
	}
}

// Verify RFC 7662 "scope" (space-separated string) is parsed into Scopes.
func TestRFC7662_ScopeString(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"active": true,
			"sub":    "user-1",
			"scope":  "read write admin",
		})
	}))
	defer server.Close()

	ep, _ := NewRFC7662Introspector(server.URL)
	result, err := ep.Introspect(context.Background(), "token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Scopes) != 3 || result.Scopes[0] != "read" || result.Scopes[1] != "write" || result.Scopes[2] != "admin" {
		t.Errorf("Scopes = %v, want [read write admin]", result.Scopes)
	}
	if _, ok := result.Claims["scope"]; ok {
		t.Error("Claims should not contain 'scope' (promoted to Scopes)")
	}
}

// Verify "scopes" (array) takes precedence over "scope" (string) when both are present.
func TestRFC7662_ScopesArrayPrecedence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"active": true,
			"sub":    "user-1",
			"scope":  "read",
			"scopes": []string{"write", "admin"},
		})
	}))
	defer server.Close()

	ep, _ := NewRFC7662Introspector(server.URL)
	result, err := ep.Introspect(context.Background(), "token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Scopes) != 2 || result.Scopes[0] != "write" || result.Scopes[1] != "admin" {
		t.Errorf("Scopes = %v, want [write admin] (scopes array should take precedence)", result.Scopes)
	}
}

// Verify empty URL returns error.
func TestNewRFC7662Introspector_EmptyURL_ReturnsError(t *testing.T) {
	_, err := NewRFC7662Introspector("")
	if err == nil {
		t.Error("expected error for empty URL")
	}
}
