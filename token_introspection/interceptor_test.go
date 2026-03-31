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

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// mockIntrospector is a test helper that implements Introspector.
type mockIntrospector struct {
	scheme string
	fn     func(ctx context.Context, credential string) (*IntrospectionResult, error)
}

func (m *mockIntrospector) Scheme() string { return m.scheme }
func (m *mockIntrospector) Introspect(ctx context.Context, credential string) (*IntrospectionResult, error) {
	return m.fn(ctx, credential)
}

func newMockIntrospector(scheme string, fn func(ctx context.Context, credential string) (*IntrospectionResult, error)) *mockIntrospector {
	return &mockIntrospector{scheme: scheme, fn: fn}
}

func allowIntrospector(scheme string) *mockIntrospector {
	return newMockIntrospector(scheme, func(_ context.Context, _ string) (*IntrospectionResult, error) {
		return &IntrospectionResult{Subject: "test-user", Scopes: []string{"read"}}, nil
	})
}

func denyIntrospector(scheme string) *mockIntrospector {
	return newMockIntrospector(scheme, func(_ context.Context, _ string) (*IntrospectionResult, error) {
		return nil, status.Error(codes.Unauthenticated, "invalid credential")
	})
}

func internalErrorIntrospector(scheme string) *mockIntrospector {
	return newMockIntrospector(scheme, func(_ context.Context, _ string) (*IntrospectionResult, error) {
		return nil, status.Error(codes.Internal, "backend down")
	})
}

func ctxWithAuth(value string) context.Context {
	md := metadata.Pairs("authorization", value)
	return metadata.NewIncomingContext(context.Background(), md)
}

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

// --- Unary Interceptor ---

// Verify that a valid Bearer token stores the result in context.
func TestInterceptor_ValidBearer_StoresResult(t *testing.T) {
	interceptor := Interceptor(WithIntrospector(allowIntrospector("bearer")))

	var capturedResult *IntrospectionResult
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		capturedResult = ResultFromContext(ctx)
		return nil, nil
	}

	ctx := ctxWithAuth("Bearer my-jwt-token")
	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}
	_, err := interceptor(ctx, nil, info, handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedResult == nil {
		t.Fatal("expected result in context")
	}
	if capturedResult.Subject != "test-user" {
		t.Errorf("Subject = %q, want %q", capturedResult.Subject, "test-user")
	}
}

// Verify that an invalid Bearer token returns Unauthenticated.
func TestInterceptor_InvalidBearer_ReturnsUnauthenticated(t *testing.T) {
	interceptor := Interceptor(WithIntrospector(denyIntrospector("bearer")))

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		t.Error("handler should not be called")
		return nil, nil
	}

	ctx := ctxWithAuth("Bearer invalid-token")
	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}
	_, err := interceptor(ctx, nil, info, handler)
	assertGRPCCode(t, err, codes.Unauthenticated)
}

// Verify that missing Authorization passes through without result.
func TestInterceptor_NoAuth_PassesThrough(t *testing.T) {
	interceptor := Interceptor(WithIntrospector(allowIntrospector("bearer")))

	called := false
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		called = true
		if ResultFromContext(ctx) != nil {
			t.Error("expected no result in context for unauthenticated request")
		}
		return "ok", nil
	}

	ctx := context.Background()
	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}
	resp, err := interceptor(ctx, nil, info, handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("handler was not called")
	}
	if resp != "ok" {
		t.Errorf("resp = %v, want %q", resp, "ok")
	}
}

// Verify that an unsupported scheme returns Unauthenticated.
func TestInterceptor_UnsupportedScheme_ReturnsUnauthenticated(t *testing.T) {
	interceptor := Interceptor(WithIntrospector(allowIntrospector("bearer")))

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		t.Error("handler should not be called")
		return nil, nil
	}

	ctx := ctxWithAuth("ApiKey some-key")
	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}
	_, err := interceptor(ctx, nil, info, handler)
	assertGRPCCode(t, err, codes.Unauthenticated)
}

// Verify that scheme matching is case-insensitive.
func TestInterceptor_SchemeIsCaseInsensitive(t *testing.T) {
	interceptor := Interceptor(WithIntrospector(allowIntrospector("bearer")))

	var capturedResult *IntrospectionResult
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		capturedResult = ResultFromContext(ctx)
		return nil, nil
	}

	ctx := ctxWithAuth("BEARER my-token")
	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}
	_, err := interceptor(ctx, nil, info, handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedResult == nil {
		t.Fatal("expected result in context")
	}
}

// Verify strategy chain: first Unauthenticated, second succeeds.
func TestInterceptor_MultipleStrategySameScheme(t *testing.T) {
	first := denyIntrospector("bearer")
	second := allowIntrospector("bearer")
	interceptor := Interceptor(WithIntrospector(first), WithIntrospector(second))

	var capturedResult *IntrospectionResult
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		capturedResult = ResultFromContext(ctx)
		return nil, nil
	}

	ctx := ctxWithAuth("Bearer token")
	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}
	_, err := interceptor(ctx, nil, info, handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedResult == nil {
		t.Fatal("expected result from second strategy")
	}
}

// Verify that Internal error in chain aborts evaluation.
func TestInterceptor_ChainInternalAborts(t *testing.T) {
	first := internalErrorIntrospector("bearer")
	second := allowIntrospector("bearer")
	interceptor := Interceptor(WithIntrospector(first), WithIntrospector(second))

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		t.Error("handler should not be called")
		return nil, nil
	}

	ctx := ctxWithAuth("Bearer token")
	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}
	_, err := interceptor(ctx, nil, info, handler)
	assertGRPCCode(t, err, codes.Internal)
}

// Verify all strategies Unauthenticated returns Unauthenticated.
func TestInterceptor_AllStrategiesUnauthenticated(t *testing.T) {
	first := denyIntrospector("bearer")
	second := denyIntrospector("bearer")
	interceptor := Interceptor(WithIntrospector(first), WithIntrospector(second))

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		t.Error("handler should not be called")
		return nil, nil
	}

	ctx := ctxWithAuth("Bearer token")
	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}
	_, err := interceptor(ctx, nil, info, handler)
	assertGRPCCode(t, err, codes.Unauthenticated)
}

// Verify cache hit skips introspector call.
func TestInterceptor_CacheHit(t *testing.T) {
	callCount := 0
	introspector := newMockIntrospector("bearer", func(_ context.Context, _ string) (*IntrospectionResult, error) {
		callCount++
		return &IntrospectionResult{Subject: "cached-user"}, nil
	})

	cache := NewInMemoryCache(1 * time.Minute)
	interceptor := Interceptor(WithIntrospector(introspector), WithCache(cache))

	handler := func(ctx context.Context, req interface{}) (interface{}, error) { return nil, nil }
	ctx := ctxWithAuth("Bearer same-token")
	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}

	// First call: cache miss, introspector called
	_, _ = interceptor(ctx, nil, info, handler)
	if callCount != 1 {
		t.Fatalf("expected 1 introspector call, got %d", callCount)
	}

	// Second call: cache hit, introspector not called
	_, _ = interceptor(ctx, nil, info, handler)
	if callCount != 1 {
		t.Errorf("expected introspector not called on cache hit, got %d calls", callCount)
	}
}

// Verify cache miss calls introspector and stores result.
func TestInterceptor_CacheMiss(t *testing.T) {
	callCount := 0
	introspector := newMockIntrospector("bearer", func(_ context.Context, _ string) (*IntrospectionResult, error) {
		callCount++
		return &IntrospectionResult{Subject: "user"}, nil
	})

	cache := NewInMemoryCache(1 * time.Minute)
	interceptor := Interceptor(WithIntrospector(introspector), WithCache(cache))

	handler := func(ctx context.Context, req interface{}) (interface{}, error) { return nil, nil }
	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}

	// Different tokens = different cache keys
	_, _ = interceptor(ctxWithAuth("Bearer token-a"), nil, info, handler)
	_, _ = interceptor(ctxWithAuth("Bearer token-b"), nil, info, handler)
	if callCount != 2 {
		t.Errorf("expected 2 introspector calls for different tokens, got %d", callCount)
	}
}

// Verify errors are not cached.
func TestInterceptor_CacheErrorNotCached(t *testing.T) {
	callCount := 0
	introspector := newMockIntrospector("bearer", func(_ context.Context, _ string) (*IntrospectionResult, error) {
		callCount++
		return nil, status.Error(codes.Unauthenticated, "invalid")
	})

	cache := NewInMemoryCache(1 * time.Minute)
	interceptor := Interceptor(WithIntrospector(introspector), WithCache(cache))

	handler := func(ctx context.Context, req interface{}) (interface{}, error) { return nil, nil }
	ctx := ctxWithAuth("Bearer bad-token")
	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}

	_, _ = interceptor(ctx, nil, info, handler)
	_, _ = interceptor(ctx, nil, info, handler)
	if callCount != 2 {
		t.Errorf("expected introspector called each time for errors, got %d", callCount)
	}
}

// Verify handler receives enriched context.
func TestInterceptor_PassesThrough(t *testing.T) {
	interceptor := Interceptor(WithIntrospector(allowIntrospector("bearer")))

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "response", nil
	}

	ctx := ctxWithAuth("Bearer token")
	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}
	resp, err := interceptor(ctx, nil, info, handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != "response" {
		t.Errorf("resp = %v, want %q", resp, "response")
	}
}

// --- Stream Interceptor ---

type mockServerStream struct {
	ctx context.Context
}

func (m *mockServerStream) Context() context.Context     { return m.ctx }
func (m *mockServerStream) RecvMsg(interface{}) error     { return nil }
func (m *mockServerStream) SendMsg(interface{}) error     { return nil }
func (m *mockServerStream) SetHeader(metadata.MD) error   { return nil }
func (m *mockServerStream) SendHeader(metadata.MD) error  { return nil }
func (m *mockServerStream) SetTrailer(metadata.MD)        {}

// Verify stream with valid token.
func TestStreamInterceptor_ValidToken(t *testing.T) {
	interceptor := StreamInterceptor(WithIntrospector(allowIntrospector("bearer")))

	md := metadata.Pairs("authorization", "Bearer valid-token")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	ss := &mockServerStream{ctx: ctx}

	var capturedResult *IntrospectionResult
	handler := func(srv interface{}, stream grpc.ServerStream) error {
		capturedResult = ResultFromContext(stream.Context())
		return nil
	}

	info := &grpc.StreamServerInfo{FullMethod: "/test.Service/Stream"}
	err := interceptor(nil, ss, info, handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedResult == nil {
		t.Fatal("expected result in stream context")
	}
}

// Verify stream with invalid token.
func TestStreamInterceptor_InvalidToken(t *testing.T) {
	interceptor := StreamInterceptor(WithIntrospector(denyIntrospector("bearer")))

	md := metadata.Pairs("authorization", "Bearer invalid")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	ss := &mockServerStream{ctx: ctx}

	handler := func(srv interface{}, stream grpc.ServerStream) error {
		t.Error("handler should not be called")
		return nil
	}

	info := &grpc.StreamServerInfo{FullMethod: "/test.Service/Stream"}
	err := interceptor(nil, ss, info, handler)
	assertGRPCCode(t, err, codes.Unauthenticated)
}

// Verify stream with no auth passes through.
func TestStreamInterceptor_NoAuth_PassesThrough(t *testing.T) {
	interceptor := StreamInterceptor(WithIntrospector(allowIntrospector("bearer")))

	ss := &mockServerStream{ctx: context.Background()}

	called := false
	handler := func(srv interface{}, stream grpc.ServerStream) error {
		called = true
		if ResultFromContext(stream.Context()) != nil {
			t.Error("expected no result in context")
		}
		return nil
	}

	info := &grpc.StreamServerInfo{FullMethod: "/test.Service/Stream"}
	err := interceptor(nil, ss, info, handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("handler was not called")
	}
}
