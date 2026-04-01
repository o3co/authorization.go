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

//go:build !integration

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
)

// --- NewOPAEndpoint constructor tests ---

// TestNewOPAEndpoint_EmptyURL_ReturnsError verifies that an empty baseURL returns an error.
func TestNewOPAEndpoint_EmptyURL_ReturnsError(t *testing.T) {
	_, err := NewOPAEndpoint("", "authz/allow")
	if err == nil {
		t.Error("expected error for empty URL")
	}
}

// TestNewOPAEndpoint_EmptyPolicyPath_ReturnsError verifies that an empty policyPath returns an error.
func TestNewOPAEndpoint_EmptyPolicyPath_ReturnsError(t *testing.T) {
	_, err := NewOPAEndpoint("http://localhost:8181", "")
	if err == nil {
		t.Error("expected error for empty policy path")
	}
}

// TestNewOPAEndpoint_ValidURL_ConstructsCorrectEndpoint verifies that the evaluate URL is
// constructed as {baseURL}/v1/data/{policyPath}.
func TestNewOPAEndpoint_ValidURL_ConstructsCorrectEndpoint(t *testing.T) {
	ep, err := NewOPAEndpoint("http://localhost:8181", "authz/allow")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	o := ep.(*restOPAEndpoint)
	want := "http://localhost:8181/v1/data/authz/allow"
	if o.evaluateURL != want {
		t.Errorf("evaluateURL = %q, want %q", o.evaluateURL, want)
	}
}

// TestNewOPAEndpoint_TrailingSlash_NormalizedCorrectly verifies that trailing slashes in baseURL
// do not produce double slashes in the evaluate URL.
func TestNewOPAEndpoint_TrailingSlash_NormalizedCorrectly(t *testing.T) {
	ep, err := NewOPAEndpoint("http://localhost:8181/", "authz/allow")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	o := ep.(*restOPAEndpoint)
	want := "http://localhost:8181/v1/data/authz/allow"
	if o.evaluateURL != want {
		t.Errorf("evaluateURL = %q, want %q", o.evaluateURL, want)
	}
}

// TestNewOPAEndpoint_NoScheme_DefaultsToHTTP verifies that a baseURL without a scheme gets
// http:// prepended automatically.
func TestNewOPAEndpoint_NoScheme_DefaultsToHTTP(t *testing.T) {
	ep, err := NewOPAEndpoint("localhost:8181", "authz/allow")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	o := ep.(*restOPAEndpoint)
	want := "http://localhost:8181/v1/data/authz/allow"
	if o.evaluateURL != want {
		t.Errorf("evaluateURL = %q, want %q", o.evaluateURL, want)
	}
}

// TestNewOPAEndpoint_LeadingSlashInPolicyPath_Normalized verifies that a leading slash in
// policyPath is stripped so the URL is not duplicated.
func TestNewOPAEndpoint_LeadingSlashInPolicyPath_Normalized(t *testing.T) {
	ep, err := NewOPAEndpoint("http://localhost:8181", "/authz/allow")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	o := ep.(*restOPAEndpoint)
	want := "http://localhost:8181/v1/data/authz/allow"
	if o.evaluateURL != want {
		t.Errorf("evaluateURL = %q, want %q", o.evaluateURL, want)
	}
}

// --- WithOPATimeout option tests ---

// TestWithOPATimeout_Zero_Panics verifies that a zero timeout panics.
func TestWithOPATimeout_Zero_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for zero timeout")
		}
	}()
	WithOPATimeout(0)
}

// TestWithOPATimeout_Valid_SetsClientTimeout verifies that a valid timeout is applied to the
// HTTP client.
func TestWithOPATimeout_Valid_SetsClientTimeout(t *testing.T) {
	ep, err := NewOPAEndpoint("http://localhost:8181", "authz/allow", WithOPATimeout(5*time.Second))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	o := ep.(*restOPAEndpoint)
	if o.httpClient.Timeout != 5*time.Second {
		t.Errorf("timeout = %v, want %v", o.httpClient.Timeout, 5*time.Second)
	}
}

// --- WithOPAMaxResponseBodySize option tests ---

// TestWithOPAMaxResponseBodySize_Zero_Panics verifies that a zero body size limit panics.
func TestWithOPAMaxResponseBodySize_Zero_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for zero body size")
		}
	}()
	WithOPAMaxResponseBodySize(0)
}

// --- OPAEndpoint.Verify tests ---

func newTestOPAEndpoint(t *testing.T, serverURL string) VerifierEndpoint {
	t.Helper()
	ep, err := NewOPAEndpoint(serverURL, "authz/allow")
	if err != nil {
		t.Fatalf("failed to create OPA endpoint: %v", err)
	}
	return ep
}

func opaServerTrue() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		result := true
		resp := opaResponse{Result: &result}
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func opaServerFalse() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		result := false
		resp := opaResponse{Result: &result}
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func opaServerAbsent() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// No "result" field — OPA undefined semantics
		_, _ = w.Write([]byte(`{}`))
	}))
}

// TestOPAVerify_ResultTrue_ReturnsNil verifies that {"result": true} maps to nil (allow).
func TestOPAVerify_ResultTrue_ReturnsNil(t *testing.T) {
	server := opaServerTrue()
	defer server.Close()

	ep := newTestOPAEndpoint(t, server.URL)
	if err := ep.Verify(ctxWithBearerToken("token123"), "posts", "read"); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

// TestOPAVerify_ResultFalse_ReturnsPermissionDenied verifies that {"result": false} maps to
// codes.PermissionDenied.
func TestOPAVerify_ResultFalse_ReturnsPermissionDenied(t *testing.T) {
	server := opaServerFalse()
	defer server.Close()

	ep := newTestOPAEndpoint(t, server.URL)
	err := ep.Verify(ctxWithBearerToken("token123"), "posts", "delete")
	assertGRPCCode(t, err, codes.PermissionDenied)
}

// TestOPAVerify_ResultAbsent_ReturnsPermissionDenied verifies that {} (OPA undefined) maps to
// codes.PermissionDenied.
func TestOPAVerify_ResultAbsent_ReturnsPermissionDenied(t *testing.T) {
	server := opaServerAbsent()
	defer server.Close()

	ep := newTestOPAEndpoint(t, server.URL)
	err := ep.Verify(ctxWithBearerToken("token123"), "posts", "read")
	assertGRPCCode(t, err, codes.PermissionDenied)
}

// TestOPAVerify_ServerError_ReturnsInternal verifies that an HTTP 500 from OPA maps to
// codes.Internal.
func TestOPAVerify_ServerError_ReturnsInternal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	ep := newTestOPAEndpoint(t, server.URL)
	err := ep.Verify(ctxWithBearerToken("token123"), "posts", "read")
	assertGRPCCode(t, err, codes.Internal)
}

// TestOPAVerify_NoToken_ReturnsUnauthenticated verifies that missing gRPC metadata returns
// codes.Unauthenticated without calling the OPA server.
func TestOPAVerify_NoToken_ReturnsUnauthenticated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("OPA server should not be called when no token is present")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ep := newTestOPAEndpoint(t, server.URL)
	err := ep.Verify(context.Background(), "posts", "read")
	assertGRPCCode(t, err, codes.Unauthenticated)
}

// TestOPAVerify_RequestBody_ContainsInputFields verifies that the request body sent to OPA
// contains input.resource, input.action, and input.token.
func TestOPAVerify_RequestBody_ContainsInputFields(t *testing.T) {
	var captured opaRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &captured)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		result := true
		resp := opaResponse{Result: &result}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	ep := newTestOPAEndpoint(t, server.URL)
	if err := ep.Verify(ctxWithBearerToken("my-token"), "orders/42", "write"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if captured.Input.Resource != "orders/42" {
		t.Errorf("input.resource = %q, want %q", captured.Input.Resource, "orders/42")
	}
	if captured.Input.Action != "write" {
		t.Errorf("input.action = %q, want %q", captured.Input.Action, "write")
	}
	if captured.Input.Token != "my-token" {
		t.Errorf("input.token = %q, want %q", captured.Input.Token, "my-token")
	}
}

// TestOPAVerify_WithRequestIDHeaderKey verifies that a custom header key is used
// when WithOPARequestIDHeaderKey is set.
func TestOPAVerify_WithRequestIDHeaderKey(t *testing.T) {
	var capturedHeader string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeader = r.Header.Get("X-Correlation-Id")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		result := true
		resp := opaResponse{Result: &result}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	ep, err := NewOPAEndpoint(server.URL, "authz/allow",
		WithOPARequestIDHeaderKey("X-Correlation-Id"),
		WithOPARequestIDFunc(func(ctx context.Context) string {
			return "opa-custom-789"
		}),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := ep.Verify(ctxWithBearerToken("token"), "posts", "read"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedHeader != "opa-custom-789" {
		t.Errorf("X-Correlation-Id = %q, want %q", capturedHeader, "opa-custom-789")
	}
}

// TestOPAVerify_WithRequestIDFunc verifies that a custom function is used to extract the request ID.
func TestOPAVerify_WithRequestIDFunc(t *testing.T) {
	var capturedRequestID string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRequestID = r.Header.Get("x-request-id")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		result := true
		resp := opaResponse{Result: &result}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	ep, err := NewOPAEndpoint(server.URL, "authz/allow", WithOPARequestIDFunc(func(ctx context.Context) string {
		return "opa-func-injected-id"
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := ep.Verify(ctxWithBearerToken("token"), "posts", "read"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedRequestID != "opa-func-injected-id" {
		t.Errorf("x-request-id = %q, want %q", capturedRequestID, "opa-func-injected-id")
	}
}

// TestOPAVerify_WithRequestIDHeaderKey_Empty_DisablesForwarding verifies that
// an empty header key disables request ID forwarding to OPA.
func TestOPAVerify_WithRequestIDHeaderKey_Empty_DisablesForwarding(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if v := r.Header.Get("x-request-id"); v != "" {
			t.Errorf("x-request-id should not be set, got %q", v)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		result := true
		resp := opaResponse{Result: &result}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	ep, err := NewOPAEndpoint(server.URL, "authz/allow",
		WithOPARequestIDHeaderKey(""),
		WithOPARequestIDFunc(func(ctx context.Context) string {
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
