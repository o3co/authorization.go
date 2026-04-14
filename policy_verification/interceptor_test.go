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

package policyverification

import (
	"context"
	"regexp"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	policyoption "github.com/o3co/grpc.authz/protobuf_policy_option"
	rt "github.com/o3co/grpc.authz/request_tracking"
	"github.com/o3co/grpc.authz/policy_verification/endpointtest"
)

// chainInterceptors is a helper that builds an interceptor chain and invokes the handler.
func chainInterceptors(ctx context.Context, req interface{}, fullMethod string, handler grpc.UnaryHandler, interceptors ...grpc.UnaryServerInterceptor) (interface{}, error) {
	info := &grpc.UnaryServerInfo{FullMethod: fullMethod}
	h := handler
	for i := len(interceptors) - 1; i >= 0; i-- {
		idx := i
		next := h
		h = func(ctx context.Context, req interface{}) (interface{}, error) {
			return interceptors[idx](ctx, req, info, next)
		}
	}
	return h(ctx, req)
}

// --- generateRequestID ---

// Verify that the generated ID conforms to the "YYYYMMDDHHmmss_<hex>" format.
func TestGenerateRequestID_Format(t *testing.T) {
	id := generateRequestID()
	pattern := regexp.MustCompile(`^\d{14}_[0-9a-f]+$`)
	if !pattern.MatchString(id) {
		t.Errorf("id %q does not match expected pattern YYYYMMDDHHmmss_<hex>", id)
	}
}

// Verify that 100 consecutively generated IDs are all unique.
func TestGenerateRequestID_Uniqueness(t *testing.T) {
	seen := make(map[string]struct{})
	for range 100 {
		id := generateRequestID()
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate request ID generated: %q", id)
		}
		seen[id] = struct{}{}
	}
}

// --- extractOrGenerateRequestID ---

// Verify that when x-request-id exists in gRPC incoming metadata, its value is returned as-is.
func TestExtractOrGenerateRequestID_UsesExistingID(t *testing.T) {
	md := metadata.Pairs("x-request-id", "existing-id-123")
	ctx := metadata.NewIncomingContext(context.Background(), md)

	got := extractOrGenerateRequestID(ctx)
	if got != "existing-id-123" {
		t.Errorf("got %q, want %q", got, "existing-id-123")
	}
}

// Verify that a new ID is generated when no metadata is present.
func TestExtractOrGenerateRequestID_GeneratesWhenNoMetadata(t *testing.T) {
	ctx := context.Background()
	got := extractOrGenerateRequestID(ctx)
	if got == "" {
		t.Error("expected non-empty generated ID when no metadata")
	}
}

// Verify that when x-request-id is an empty string it is not treated as an existing value and a new ID is generated.
func TestExtractOrGenerateRequestID_GeneratesWhenIDIsEmpty(t *testing.T) {
	md := metadata.Pairs("x-request-id", "")
	ctx := metadata.NewIncomingContext(context.Background(), md)

	got := extractOrGenerateRequestID(ctx)
	if got == "" {
		t.Error("expected non-empty generated ID when x-request-id is empty string")
	}
}

// --- Interceptor ---

// Verify that passing nil as verifierEndpoint panics as a misconfiguration.
func TestInterceptor_NilEndpoint_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil verifierEndpoint")
		}
	}()
	Interceptor(nil)
}

// Verify that when called without going through protobuf_policy_option.Interceptor,
// an Internal error is returned as an interceptor chain misconfiguration.
func TestInterceptor_PolicyOptionInterceptorNotRan_ReturnsInternal(t *testing.T) {
	ep := endpointtest.Func(func(ctx context.Context, resource, action string) error {
		t.Error("Verify should not be called")
		return nil
	})

	pvInterceptor := Interceptor(ep)
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		t.Error("handler should not be called")
		return nil, nil
	}

	ctx := context.Background()
	info := &grpc.UnaryServerInfo{FullMethod: "/some.Service/SomeMethod"}
	_, err := pvInterceptor(ctx, nil, info, handler)

	if err == nil {
		t.Fatal("expected error when policy_option interceptor has not run")
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got %T: %v", err, err)
	}
	if st.Code() != codes.Internal {
		t.Errorf("code = %v, want %v", st.Code(), codes.Internal)
	}
}

// Verify that for a method with no policy definition (not registered in the proto registry),
// Verify is not called and the handler is invoked directly.
func TestInterceptor_NoPolicy_CallsHandler(t *testing.T) {
	ep := endpointtest.Func(func(ctx context.Context, resource, action string) error {
		t.Error("Verify should not be called when method has no policy")
		return nil
	})

	called := false
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		called = true
		return "ok", nil
	}

	ctx := context.Background()
	// unregistered method → policyoption.Interceptor calls handler with no policy
	resp, err := chainInterceptors(
		ctx, nil, "/unknown.Service/UnknownMethod", handler,
		policyoption.Interceptor(),
		Interceptor(ep),
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != "ok" {
		t.Errorf("unexpected response: %v", resp)
	}
	if !called {
		t.Error("handler was not called")
	}
}

// Verify that the x-request-id from metadata is written to the context inside the interceptor
// and can be retrieved from the context passed to the handler.
func TestInterceptor_RequestID_PropagatedToContext(t *testing.T) {
	ep := endpointtest.Allow()

	var capturedCtx context.Context
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		capturedCtx = ctx
		return nil, nil
	}

	ctx := endpointtest.CtxWithRequestID(context.Background(), "test-request-id-xyz")

	_, err := chainInterceptors(
		ctx, nil, "/unknown.Service/UnknownMethod", handler,
		policyoption.Interceptor(),
		Interceptor(ep),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedCtx == nil {
		t.Fatal("handler was not called")
	}

	gotID := rt.RequestIDFromContext(capturedCtx)
	if gotID != "test-request-id-xyz" {
		t.Errorf("x-request-id in context = %q, want %q", gotID, "test-request-id-xyz")
	}
}

// Verify that even when x-request-id is absent from metadata,
// the auto-generated ID is written to the context passed to the handler.
func TestInterceptor_RequestID_GeneratedWhenAbsent(t *testing.T) {
	ep := endpointtest.Allow()

	var capturedCtx context.Context
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		capturedCtx = ctx
		return nil, nil
	}

	ctx := context.Background() // no metadata

	_, err := chainInterceptors(
		ctx, nil, "/unknown.Service/UnknownMethod", handler,
		policyoption.Interceptor(),
		Interceptor(ep),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedCtx == nil {
		t.Fatal("handler was not called")
	}

	gotID := rt.RequestIDFromContext(capturedCtx)
	if gotID == "" {
		t.Error("expected a generated request ID in context, got empty string")
	}
}

// When request_tracking.Interceptor has already run and set a request ID in the
// context, policy_verification should use the existing ID instead of generating a new one.
func TestInterceptor_UsesExistingContextID(t *testing.T) {
	ep := endpointtest.Allow()

	var capturedID string
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		capturedID = rt.RequestIDFromContext(ctx)
		return nil, nil
	}

	// Simulate request_tracking having already run by pre-setting the context value.
	ctx := rt.WithRequestID(context.Background(), "pre-existing-id")

	_, err := chainInterceptors(
		ctx, nil, "/unknown.Service/UnknownMethod", handler,
		policyoption.Interceptor(),
		Interceptor(ep),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedID != "pre-existing-id" {
		t.Errorf("RequestIDFromContext() = %q, want %q", capturedID, "pre-existing-id")
	}
}
