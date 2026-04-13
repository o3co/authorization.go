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

package e2e_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	testpb "github.com/o3co/grpc.authz/tests/e2e/proto"
	tokenintrospection "github.com/o3co/grpc.authz/token_introspection"
	policyoption "github.com/o3co/grpc.authz/protobuf_policy_option"
	policyverification "github.com/o3co/grpc.authz/policy_verification"
	"github.com/o3co/grpc.authz/policy_verification/endpoint"
	rt "github.com/o3co/grpc.authz/request_tracking"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// --- Test service implementation ---

type testServiceImpl struct {
	testpb.UnimplementedTestServiceServer
}

func (s *testServiceImpl) GetResource(_ context.Context, req *testpb.GetResourceRequest) (*testpb.GetResourceResponse, error) {
	return &testpb.GetResourceResponse{Id: req.Id, Name: "test-resource"}, nil
}

func (s *testServiceImpl) CreateResource(_ context.Context, req *testpb.CreateResourceRequest) (*testpb.CreateResourceResponse, error) {
	return &testpb.CreateResourceResponse{Id: "new-id"}, nil
}

func (s *testServiceImpl) HealthCheck(_ context.Context, _ *testpb.HealthCheckRequest) (*testpb.HealthCheckResponse, error) {
	return &testpb.HealthCheckResponse{Status: "ok"}, nil
}

// --- Test harness ---

// serverSetup holds state captured by mock servers during a test run.
type serverSetup struct {
	introspectServer *httptest.Server
	verifyServer     *httptest.Server

	mu                    sync.Mutex
	introspectRequestIDs  []string
	verifyRequestIDs      []string
}

func (s *serverSetup) close() {
	s.introspectServer.Close()
	s.verifyServer.Close()
}

// mockIntrospectServer returns a server that accepts any token matching validToken
// as active (with the given scope), and returns active=false for anything else.
func newServerSetup(validToken, scope string) *serverSetup {
	ss := &serverSetup{}

	ss.introspectServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rid := r.Header.Get("x-request-id"); rid != "" {
			ss.mu.Lock()
			ss.introspectRequestIDs = append(ss.introspectRequestIDs, rid)
			ss.mu.Unlock()
		}

		body, _ := io.ReadAll(r.Body)
		params := string(body)

		active := strings.Contains(params, "token="+validToken)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if active {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"active": true,
				"sub":    "did:dplaas:org:test",
				"scope":  scope,
			})
		} else {
			_ = json.NewEncoder(w).Encode(map[string]any{"active": false})
		}
	}))

	ss.verifyServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rid := r.Header.Get("x-request-id"); rid != "" {
			ss.mu.Lock()
			ss.verifyRequestIDs = append(ss.verifyRequestIDs, rid)
			ss.mu.Unlock()
		}

		// Read body to get resource/action
		var reqBody map[string]string
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &reqBody)

		resource := reqBody["resource"]
		action := reqBody["action"]

		// Determine required scope from action
		// action "read" → scope "read:resource", action "write" → scope "write:resource"
		authHeader := r.Header.Get("Authorization")
		// scope is embedded in the auth header as a placeholder — in real life the
		// policy-verifier would validate the JWT. For this mock we derive the
		// allowed actions from the Authorization header value which carries the
		// introspected scope via context (forwarded by the interceptor chain).
		// Simpler: we store the expected scope in the verify server closure.
		// Because the verify endpoint only receives the raw Bearer token (not the
		// introspection result), we need to infer permission from the token itself.
		// In the real scenario the policy-verifier calls introspect again or validates JWT.
		// For this mock: we check if the token is the validToken, and whether the
		// requested action is within the granted scope.
		_ = authHeader // used below via validToken check

		// Extract token from Authorization header
		parts := strings.Fields(authHeader)
		if len(parts) != 2 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		token := parts[1]

		if token != validToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		// Check if the action is allowed for this token's scope
		scopeParts := strings.Fields(scope)
		allowed := false
		for _, s := range scopeParts {
			// scope format: "action:resource" or just "action"
			if s == action+":"+resource || s == action {
				allowed = true
				break
			}
		}

		if allowed {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusForbidden)
		}
	}))

	return ss
}

// buildGRPCServer creates an in-process gRPC server with the full interceptor chain
// and returns a connected client.
func buildGRPCServer(t *testing.T, ss *serverSetup) (testpb.TestServiceClient, func()) {
	t.Helper()

	// Build introspector — forward x-request-id from context to the introspect endpoint
	introspector, err := tokenintrospection.NewRFC7662Introspector(
		ss.introspectServer.URL,
		tokenintrospection.WithSelfIntrospect(),
		tokenintrospection.WithRequestIDFunc(rt.RequestIDFromContext),
	)
	if err != nil {
		t.Fatalf("failed to create introspector: %v", err)
	}

	// Build verify endpoint — pass x-request-id from context
	verifyEP, err := endpoint.NewRESTEndpoint(
		ss.verifyServer.URL,
		endpoint.WithRequestIDFunc(rt.RequestIDFromContext),
	)
	if err != nil {
		t.Fatalf("failed to create verify endpoint: %v", err)
	}

	// Build interceptor chain
	srv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			rt.Interceptor(),
			tokenintrospection.Interceptor(
				tokenintrospection.WithIntrospector(introspector),
			),
			policyoption.Interceptor(),
			policyverification.Interceptor(verifyEP),
		),
	)
	testpb.RegisterTestServiceServer(srv, &testServiceImpl{})

	// Use a plain TCP listener on a random port (avoids bufconn import complexity)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	go func() { _ = srv.Serve(lis) }()

	conn, err := grpc.NewClient(
		lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}

	client := testpb.NewTestServiceClient(conn)

	cleanup := func() {
		_ = conn.Close()
		srv.GracefulStop()
	}
	return client, cleanup
}

// bearerCtx returns a context carrying a Bearer token in gRPC metadata.
func bearerCtx(token string) context.Context {
	md := metadata.Pairs("authorization", "Bearer "+token)
	return metadata.NewOutgoingContext(context.Background(), md)
}

// --- Tests ---

// TestE2E_Allow verifies the happy path: valid token + correct action → handler responds.
func TestE2E_Allow(t *testing.T) {
	const validToken = "valid-token-abc"
	ss := newServerSetup(validToken, "read:resource")
	defer ss.close()

	client, cleanup := buildGRPCServer(t, ss)
	defer cleanup()

	resp, err := client.GetResource(bearerCtx(validToken), &testpb.GetResourceRequest{Id: "42"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Id != "42" || resp.Name != "test-resource" {
		t.Errorf("unexpected response: %+v", resp)
	}
}

// TestE2E_Deny verifies that a token with insufficient scope results in PermissionDenied.
// The token has read:resource scope but CreateResource requires write:resource.
func TestE2E_Deny(t *testing.T) {
	const validToken = "read-only-token"
	ss := newServerSetup(validToken, "read:resource")
	defer ss.close()

	client, cleanup := buildGRPCServer(t, ss)
	defer cleanup()

	_, err := client.CreateResource(bearerCtx(validToken), &testpb.CreateResourceRequest{Name: "new"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got %T: %v", err, err)
	}
	if st.Code() != codes.PermissionDenied {
		t.Errorf("code = %v, want %v", st.Code(), codes.PermissionDenied)
	}
}

// TestE2E_Unauthenticated verifies that an invalid token results in Unauthenticated.
func TestE2E_Unauthenticated(t *testing.T) {
	const validToken = "valid-token-xyz"
	ss := newServerSetup(validToken, "read:resource write:resource")
	defer ss.close()

	client, cleanup := buildGRPCServer(t, ss)
	defer cleanup()

	// Send a token that is not the validToken → introspect returns active=false → Unauthenticated
	_, err := client.GetResource(bearerCtx("invalid-token"), &testpb.GetResourceRequest{Id: "1"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got %T: %v", err, err)
	}
	if st.Code() != codes.Unauthenticated {
		t.Errorf("code = %v, want %v", st.Code(), codes.Unauthenticated)
	}
}

// TestE2E_NoPolicyOption verifies that HealthCheck (no policy annotation) passes through
// without calling the verify server.
func TestE2E_NoPolicyOption(t *testing.T) {
	const validToken = "any-token"
	ss := newServerSetup(validToken, "read:resource")

	verifyCallCount := 0
	// Override verify server to detect unexpected calls
	originalVerify := ss.verifyServer
	originalVerify.Close()

	ss.verifyServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		verifyCallCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer ss.close()

	client, cleanup := buildGRPCServer(t, ss)
	defer cleanup()

	// HealthCheck has no policy option — should pass through without calling /verify
	// No token needed (token_introspection passes through when no Authorization header)
	resp, err := client.HealthCheck(context.Background(), &testpb.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != "ok" {
		t.Errorf("status = %q, want %q", resp.Status, "ok")
	}
	if verifyCallCount != 0 {
		t.Errorf("verify server called %d times, want 0", verifyCallCount)
	}
}

// TestE2E_RequestIDForwarded verifies that x-request-id is forwarded to both
// /introspect and /verify endpoints.
func TestE2E_RequestIDForwarded(t *testing.T) {
	const validToken = "fwd-token"
	ss := newServerSetup(validToken, "read:resource")
	defer ss.close()

	client, cleanup := buildGRPCServer(t, ss)
	defer cleanup()

	// Send a request with a specific x-request-id in gRPC metadata
	const requestID = "test-request-id-e2e"
	md := metadata.Pairs(
		"authorization", "Bearer "+validToken,
		"x-request-id", requestID,
	)
	ctx := metadata.NewOutgoingContext(context.Background(), md)

	_, err := client.GetResource(ctx, &testpb.GetResourceRequest{Id: "99"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the request ID was forwarded to both services
	ss.mu.Lock()
	introspectIDs := append([]string(nil), ss.introspectRequestIDs...)
	verifyIDs := append([]string(nil), ss.verifyRequestIDs...)
	ss.mu.Unlock()

	if len(introspectIDs) == 0 {
		t.Error("x-request-id was not captured at introspect server (none received)")
	} else if introspectIDs[0] != requestID {
		t.Errorf("introspect x-request-id = %q, want %q", introspectIDs[0], requestID)
	}

	if len(verifyIDs) == 0 {
		t.Error("x-request-id was not captured at verify server (none received)")
	} else if verifyIDs[0] != requestID {
		t.Errorf("verify x-request-id = %q, want %q", verifyIDs[0], requestID)
	}
}
