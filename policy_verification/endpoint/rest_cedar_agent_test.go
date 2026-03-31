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
	"google.golang.org/grpc/metadata"
)

// --- NewCedarAgentEndpoint constructor tests ---

// TestNewCedarAgentEndpoint_EmptyURL_ReturnsError verifies that an empty baseURL returns an error.
func TestNewCedarAgentEndpoint_EmptyURL_ReturnsError(t *testing.T) {
	_, err := NewCedarAgentEndpoint("")
	if err == nil {
		t.Error("expected error for empty URL")
	}
}

// TestNewCedarAgentEndpoint_ValidURL_ConstructsCorrectEndpoint verifies that the authorize URL
// is constructed as {baseURL}/v1/is_authorized.
func TestNewCedarAgentEndpoint_ValidURL_ConstructsCorrectEndpoint(t *testing.T) {
	ep, err := NewCedarAgentEndpoint("http://localhost:8180")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c := ep.(*restCedarAgentEndpoint)
	want := "http://localhost:8180/v1/is_authorized"
	if c.authorizeURL != want {
		t.Errorf("authorizeURL = %q, want %q", c.authorizeURL, want)
	}
}

// TestNewCedarAgentEndpoint_TrailingSlash_NormalizedCorrectly verifies that trailing slashes in
// baseURL do not produce double slashes in the authorize URL.
func TestNewCedarAgentEndpoint_TrailingSlash_NormalizedCorrectly(t *testing.T) {
	ep, err := NewCedarAgentEndpoint("http://localhost:8180/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c := ep.(*restCedarAgentEndpoint)
	want := "http://localhost:8180/v1/is_authorized"
	if c.authorizeURL != want {
		t.Errorf("authorizeURL = %q, want %q", c.authorizeURL, want)
	}
}

// TestNewCedarAgentEndpoint_NoScheme_DefaultsToHTTP verifies that a baseURL without a scheme gets
// http:// prepended automatically.
func TestNewCedarAgentEndpoint_NoScheme_DefaultsToHTTP(t *testing.T) {
	ep, err := NewCedarAgentEndpoint("localhost:8180")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c := ep.(*restCedarAgentEndpoint)
	want := "http://localhost:8180/v1/is_authorized"
	if c.authorizeURL != want {
		t.Errorf("authorizeURL = %q, want %q", c.authorizeURL, want)
	}
}

// TestNewCedarAgentEndpoint_CustomPrefixes verifies that entity type prefixes are stored correctly.
func TestNewCedarAgentEndpoint_CustomPrefixes(t *testing.T) {
	ep, err := NewCedarAgentEndpoint("http://localhost:8180",
		WithCedarAgentPrincipalPrefix("Account"),
		WithCedarAgentActionPrefix("Op"),
		WithCedarAgentResourcePrefix("Doc"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c := ep.(*restCedarAgentEndpoint)
	if c.principalPrefix != "Account" {
		t.Errorf("principalPrefix = %q, want %q", c.principalPrefix, "Account")
	}
	if c.actionPrefix != "Op" {
		t.Errorf("actionPrefix = %q, want %q", c.actionPrefix, "Op")
	}
	if c.resourcePrefix != "Doc" {
		t.Errorf("resourcePrefix = %q, want %q", c.resourcePrefix, "Doc")
	}
}

// --- WithCedarAgentTimeout option tests ---

// TestWithCedarAgentTimeout_Zero_Panics verifies that a zero timeout panics.
func TestWithCedarAgentTimeout_Zero_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for zero timeout")
		}
	}()
	WithCedarAgentTimeout(0)
}

// TestWithCedarAgentTimeout_Valid_SetsClientTimeout verifies that a valid timeout is applied to
// the HTTP client.
func TestWithCedarAgentTimeout_Valid_SetsClientTimeout(t *testing.T) {
	ep, err := NewCedarAgentEndpoint("http://localhost:8180", WithCedarAgentTimeout(5*time.Second))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c := ep.(*restCedarAgentEndpoint)
	if c.httpClient.Timeout != 5*time.Second {
		t.Errorf("timeout = %v, want %v", c.httpClient.Timeout, 5*time.Second)
	}
}

// --- WithCedarAgentMaxResponseBodySize option tests ---

// TestWithCedarAgentMaxResponseBodySize_Zero_Panics verifies that a zero body size limit panics.
func TestWithCedarAgentMaxResponseBodySize_Zero_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for zero body size")
		}
	}()
	WithCedarAgentMaxResponseBodySize(0)
}

// TestWithCedarAgentPrincipalResolver_Nil_Panics verifies that a nil resolver panics.
func TestWithCedarAgentPrincipalResolver_Nil_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil resolver")
		}
	}()
	WithCedarAgentPrincipalResolver(nil)
}

// --- CedarAgentEndpoint.Verify tests ---

func newTestCedarAgentEndpoint(t *testing.T, serverURL string, opts ...CedarAgentOption) VerifierEndpoint {
	t.Helper()
	ep, err := NewCedarAgentEndpoint(serverURL, opts...)
	if err != nil {
		t.Fatalf("failed to create Cedar agent endpoint: %v", err)
	}
	return ep
}

func cedarAgentServerAllow() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"decision":"Allow"}`))
	}))
}

func cedarAgentServerDeny() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"decision":"Deny"}`))
	}))
}

// TestCedarVerify_Allow_ReturnsNil verifies that {"decision":"Allow"} maps to nil.
func TestCedarVerify_Allow_ReturnsNil(t *testing.T) {
	server := cedarAgentServerAllow()
	defer server.Close()

	ep := newTestCedarAgentEndpoint(t, server.URL)
	if err := ep.Verify(ctxWithBearerToken("token123"), "posts", "read"); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

// TestCedarVerify_Deny_ReturnsPermissionDenied verifies that {"decision":"Deny"} maps to
// codes.PermissionDenied.
func TestCedarVerify_Deny_ReturnsPermissionDenied(t *testing.T) {
	server := cedarAgentServerDeny()
	defer server.Close()

	ep := newTestCedarAgentEndpoint(t, server.URL)
	err := ep.Verify(ctxWithBearerToken("token123"), "posts", "delete")
	assertGRPCCode(t, err, codes.PermissionDenied)
}

// TestCedarVerify_ServerError_ReturnsInternal verifies that an HTTP 500 from the Cedar agent
// maps to codes.Internal.
func TestCedarVerify_ServerError_ReturnsInternal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	ep := newTestCedarAgentEndpoint(t, server.URL)
	err := ep.Verify(ctxWithBearerToken("token123"), "posts", "read")
	assertGRPCCode(t, err, codes.Internal)
}

// TestCedarVerify_NoToken_ReturnsUnauthenticated verifies that missing gRPC metadata returns
// codes.Unauthenticated without calling the Cedar agent server.
func TestCedarVerify_NoToken_ReturnsUnauthenticated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Cedar agent server should not be called when no token is present")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ep := newTestCedarAgentEndpoint(t, server.URL)
	err := ep.Verify(context.Background(), "posts", "read")
	assertGRPCCode(t, err, codes.Unauthenticated)
}

// TestCedarVerify_RequestBody_ContainsCedarEntityUIDs verifies that the request body sent to
// the Cedar agent contains principal, action, and resource as Cedar entity UIDs.
func TestCedarVerify_RequestBody_ContainsCedarEntityUIDs(t *testing.T) {
	var captured cedarAgentRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &captured)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"decision":"Allow"}`))
	}))
	defer server.Close()

	ep := newTestCedarAgentEndpoint(t, server.URL)
	if err := ep.Verify(ctxWithBearerToken("my-token"), "posts/123", "read"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantPrincipal := `User::"my-token"`
	wantAction := `Action::"read"`
	wantResource := `Resource::"posts/123"`

	if captured.Principal != wantPrincipal {
		t.Errorf("principal = %q, want %q", captured.Principal, wantPrincipal)
	}
	if captured.Action != wantAction {
		t.Errorf("action = %q, want %q", captured.Action, wantAction)
	}
	if captured.Resource != wantResource {
		t.Errorf("resource = %q, want %q", captured.Resource, wantResource)
	}
}

// TestCedarVerify_CustomPrefixes_UsedInEntityUIDs verifies that custom entity type prefixes
// are used when formatting entity UIDs.
func TestCedarVerify_CustomPrefixes_UsedInEntityUIDs(t *testing.T) {
	var captured cedarAgentRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &captured)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"decision":"Allow"}`))
	}))
	defer server.Close()

	ep := newTestCedarAgentEndpoint(t, server.URL,
		WithCedarAgentPrincipalPrefix("Account"),
		WithCedarAgentActionPrefix("Op"),
		WithCedarAgentResourcePrefix("Doc"),
	)
	if err := ep.Verify(ctxWithBearerToken("my-token"), "docs/456", "write"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantPrincipal := `Account::"my-token"`
	wantAction := `Op::"write"`
	wantResource := `Doc::"docs/456"`

	if captured.Principal != wantPrincipal {
		t.Errorf("principal = %q, want %q", captured.Principal, wantPrincipal)
	}
	if captured.Action != wantAction {
		t.Errorf("action = %q, want %q", captured.Action, wantAction)
	}
	if captured.Resource != wantResource {
		t.Errorf("resource = %q, want %q", captured.Resource, wantResource)
	}
}

// TestCedarVerify_CustomPrincipalResolver_UsesResolvedID verifies that a custom principal
// resolver is applied to the token value before formatting the entity UID.
func TestCedarVerify_CustomPrincipalResolver_UsesResolvedID(t *testing.T) {
	var captured cedarAgentRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &captured)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"decision":"Allow"}`))
	}))
	defer server.Close()

	resolver := func(_ context.Context, token string) string {
		return "resolved-" + token
	}

	ep := newTestCedarAgentEndpoint(t, server.URL, WithCedarAgentPrincipalResolver(resolver))
	if err := ep.Verify(ctxWithBearerToken("raw-token"), "posts", "read"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantPrincipal := `User::"resolved-raw-token"`
	if captured.Principal != wantPrincipal {
		t.Errorf("principal = %q, want %q", captured.Principal, wantPrincipal)
	}
}

// TestCedarVerify_WithRequestIDHeaderKey verifies that a custom header key is used
// when WithCedarAgentRequestIDHeaderKey is set.
func TestCedarVerify_WithRequestIDHeaderKey(t *testing.T) {
	var capturedHeader string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeader = r.Header.Get("X-Trace-Id")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"decision":"Allow"}`))
	}))
	defer server.Close()

	ep := newTestCedarAgentEndpoint(t, server.URL, WithCedarAgentRequestIDHeaderKey("X-Trace-Id"))

	md := metadata.Pairs("authorization", "Bearer token")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	ctx = WithRequestID(ctx, "cedar-custom-456")

	if err := ep.Verify(ctx, "posts", "read"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedHeader != "cedar-custom-456" {
		t.Errorf("X-Trace-Id = %q, want %q", capturedHeader, "cedar-custom-456")
	}
}

// TestCedarVerify_WithRequestIDHeaderKey_Empty_DisablesForwarding verifies that
// an empty header key disables request ID forwarding to Cedar agent.
func TestCedarVerify_WithRequestIDHeaderKey_Empty_DisablesForwarding(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if v := r.Header.Get("x-request-id"); v != "" {
			t.Errorf("x-request-id should not be set, got %q", v)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"decision":"Allow"}`))
	}))
	defer server.Close()

	ep := newTestCedarAgentEndpoint(t, server.URL, WithCedarAgentRequestIDHeaderKey(""))

	ctx := ctxWithBearerToken("token")
	ctx = WithRequestID(ctx, "should-not-forward")

	if err := ep.Verify(ctx, "posts", "read"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
