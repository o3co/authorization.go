// Copyright 2026 o3co Inc.
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
	"log/slog"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"

	policyoption "github.com/o3co/grpc.authz/protobuf_policy_option"
	"github.com/o3co/grpc.authz/policy_verification/endpoint"
	"github.com/o3co/grpc.authz/policy_verification/endpointtest"
)

// mockServerStream implements grpc.ServerStream for testing.
type mockServerStream struct {
	ctx       context.Context
	recvErr   error // error to return from RecvMsg
	recvCalls int   // how many times RecvMsg was called
}

func (m *mockServerStream) Context() context.Context     { return m.ctx }
func (m *mockServerStream) RecvMsg(interface{}) error    { m.recvCalls++; return m.recvErr }
func (m *mockServerStream) SendMsg(interface{}) error    { return nil }
func (m *mockServerStream) SetHeader(metadata.MD) error  { return nil }
func (m *mockServerStream) SendHeader(metadata.MD) error { return nil }
func (m *mockServerStream) SetTrailer(metadata.MD)       {}

// chainStreamInterceptors builds a stream handler chain and invokes it.
func chainStreamInterceptors(ss grpc.ServerStream, fullMethod string, handler grpc.StreamHandler, interceptors ...grpc.StreamServerInterceptor) error {
	info := &grpc.StreamServerInfo{FullMethod: fullMethod}
	h := handler
	for i := len(interceptors) - 1; i >= 0; i-- {
		idx := i
		next := h
		h = func(srv interface{}, stream grpc.ServerStream) error {
			return interceptors[idx](srv, stream, info, next)
		}
	}
	return h(nil, ss)
}

// TestStreamInterceptor_NilEndpoint_Panics verifies that passing nil verifierEndpoint
// panics immediately at interceptor construction.
func TestStreamInterceptor_NilEndpoint_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil verifierEndpoint")
		}
	}()
	StreamInterceptor(nil)
}

// TestStreamInterceptor_PolicyOptionInterceptorNotRan_ReturnsInternal verifies
// that the misconfiguration guard fires when policyoption.StreamInterceptor is absent.
func TestStreamInterceptor_PolicyOptionInterceptorNotRan_ReturnsInternal(t *testing.T) {
	ss := &mockServerStream{ctx: context.Background()}
	handler := func(srv interface{}, stream grpc.ServerStream) error {
		t.Error("handler should not be called")
		return nil
	}

	pvInterceptor := StreamInterceptor(endpointtest.Allow())
	info := &grpc.StreamServerInfo{FullMethod: "/unknown.Service/UnknownMethod"}
	err := pvInterceptor(nil, ss, info, handler)

	endpointtest.AssertGRPCCode(t, err, codes.Internal)
}

// TestStreamInterceptor_NoPolicy_CallsHandler verifies that when the method has
// no policy, the handler is called without invoking Verify.
func TestStreamInterceptor_NoPolicy_CallsHandler(t *testing.T) {
	called := false
	handler := func(srv interface{}, stream grpc.ServerStream) error {
		called = true
		return nil
	}

	ss := &mockServerStream{ctx: context.Background()}
	err := chainStreamInterceptors(ss, "/unknown.Service/UnknownMethod", handler,
		policyoption.StreamInterceptor(),
		StreamInterceptor(endpointtest.Func(func(_ context.Context, _, _ string) error {
			t.Error("Verify should not be called for no-policy method")
			return nil
		})),
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("handler was not called")
	}
}

// TestAuthServerStream_RecvMsg_CallsVerify verifies that RecvMsg on the wrapped
// stream calls Verify each time, with no caching.
// Tests authServerStream directly (package-internal type) to avoid needing a
// registered proto method for context setup.
func TestAuthServerStream_RecvMsg_CallsVerify(t *testing.T) {
	verifyCalls := 0
	ep := endpointtest.Func(func(_ context.Context, _, _ string) error {
		verifyCalls++
		return nil
	})

	underlying := &mockServerStream{ctx: context.Background()}
	wrapped := &authServerStream{
		ServerStream: underlying,
		resource:     "posts",
		action:       "read",
		verifier:     ep,
		log:          newLogger(slog.LevelError),
	}

	_ = wrapped.RecvMsg(nil)
	if verifyCalls != 1 {
		t.Errorf("Verify called %d times after first RecvMsg, want 1", verifyCalls)
	}

	// Second RecvMsg must also call Verify — no caching.
	_ = wrapped.RecvMsg(nil)
	if verifyCalls != 2 {
		t.Errorf("Verify called %d times after second RecvMsg, want 2", verifyCalls)
	}
}

// TestAuthServerStream_RecvMsg_DeniedReturnsError verifies that when Verify denies,
// RecvMsg returns the error without calling the underlying RecvMsg.
func TestAuthServerStream_RecvMsg_DeniedReturnsError(t *testing.T) {
	underlying := &mockServerStream{ctx: context.Background()}
	wrapped := &authServerStream{
		ServerStream: underlying,
		resource:     "posts",
		action:       "delete",
		verifier:     endpointtest.Deny(),
		log:          newLogger(slog.LevelError),
	}

	err := wrapped.RecvMsg(nil)
	endpointtest.AssertGRPCCode(t, err, codes.PermissionDenied)

	// Underlying RecvMsg must NOT have been called.
	if underlying.recvCalls != 0 {
		t.Errorf("underlying RecvMsg called %d times, want 0", underlying.recvCalls)
	}
}

// TestAuthServerStream_RecvMsg_UsesStreamContext verifies that Verify receives
// the context from the underlying stream (evaluated at call time).
func TestAuthServerStream_RecvMsg_UsesStreamContext(t *testing.T) {
	var capturedRequestID string
	ep := endpointtest.Func(func(ctx context.Context, _, _ string) error {
		capturedRequestID = endpoint.RequestIDFromContext(ctx)
		return nil
	})

	baseCtx := endpointtest.CtxWithRequestID(context.Background(), "stream-req-id")
	underlying := &mockServerStream{ctx: baseCtx}
	wrapped := &authServerStream{
		ServerStream: underlying,
		resource:     "posts",
		action:       "read",
		verifier:     ep,
		log:          newLogger(slog.LevelError),
	}

	_ = wrapped.RecvMsg(nil)

	if capturedRequestID != "stream-req-id" {
		t.Errorf("requestID in Verify = %q, want %q", capturedRequestID, "stream-req-id")
	}
}
