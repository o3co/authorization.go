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
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// --- Unary Interceptor ---

// metadata の x-request-id を handler の context から取得できることを確認する。
func TestInterceptor_ExtractsFromMetadata(t *testing.T) {
	interceptor := Interceptor()

	md := metadata.Pairs("x-request-id", "existing-id-abc")
	ctx := metadata.NewIncomingContext(context.Background(), md)

	var capturedID string
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		capturedID = RequestIDFromContext(ctx)
		return nil, nil
	}

	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}
	_, err := interceptor(ctx, nil, info, handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedID != "existing-id-abc" {
		t.Errorf("RequestIDFromContext() = %q, want %q", capturedID, "existing-id-abc")
	}
}

// metadata に x-request-id がない場合は自動生成した ID が context に入ることを確認する。
func TestInterceptor_GeneratesWhenAbsent(t *testing.T) {
	interceptor := Interceptor()

	var capturedID string
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		capturedID = RequestIDFromContext(ctx)
		return nil, nil
	}

	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}
	_, err := interceptor(context.Background(), nil, info, handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedID == "" {
		t.Error("expected a generated request ID in context, got empty string")
	}
}

// x-request-id が空文字の場合は新しい ID を生成することを確認する。
func TestInterceptor_GeneratesWhenEmpty(t *testing.T) {
	interceptor := Interceptor()

	md := metadata.Pairs("x-request-id", "")
	ctx := metadata.NewIncomingContext(context.Background(), md)

	var capturedID string
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		capturedID = RequestIDFromContext(ctx)
		return nil, nil
	}

	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}
	_, err := interceptor(ctx, nil, info, handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedID == "" {
		t.Error("expected a generated request ID, got empty string")
	}
}

// handler の返り値がそのまま返されることを確認する（パススルー動作）。
func TestInterceptor_PassesThrough(t *testing.T) {
	interceptor := Interceptor()

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "response", nil
	}

	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}
	resp, err := interceptor(context.Background(), "request", info, handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != "response" {
		t.Errorf("resp = %v, want %q", resp, "response")
	}
}

// --- Stream Interceptor ---

// mockServerStream implements grpc.ServerStream for testing.
type mockServerStream struct {
	ctx context.Context
}

func (m *mockServerStream) Context() context.Context     { return m.ctx }
func (m *mockServerStream) RecvMsg(interface{}) error     { return nil }
func (m *mockServerStream) SendMsg(interface{}) error     { return nil }
func (m *mockServerStream) SetHeader(metadata.MD) error   { return nil }
func (m *mockServerStream) SendHeader(metadata.MD) error  { return nil }
func (m *mockServerStream) SetTrailer(metadata.MD)        {}

// Stream handler が受け取る context に x-request-id が含まれることを確認する。
func TestStreamInterceptor_PropagatesContext(t *testing.T) {
	interceptor := StreamInterceptor()

	md := metadata.Pairs("x-request-id", "stream-id-xyz")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	ss := &mockServerStream{ctx: ctx}

	var capturedID string
	handler := func(srv interface{}, stream grpc.ServerStream) error {
		capturedID = RequestIDFromContext(stream.Context())
		return nil
	}

	info := &grpc.StreamServerInfo{FullMethod: "/test.Service/StreamMethod"}
	err := interceptor(nil, ss, info, handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedID != "stream-id-xyz" {
		t.Errorf("RequestIDFromContext() = %q, want %q", capturedID, "stream-id-xyz")
	}
}

// metadata がない場合は自動生成した ID が stream context に入ることを確認する。
func TestStreamInterceptor_GeneratesWhenAbsent(t *testing.T) {
	interceptor := StreamInterceptor()

	ss := &mockServerStream{ctx: context.Background()}

	var capturedID string
	handler := func(srv interface{}, stream grpc.ServerStream) error {
		capturedID = RequestIDFromContext(stream.Context())
		return nil
	}

	info := &grpc.StreamServerInfo{FullMethod: "/test.Service/StreamMethod"}
	err := interceptor(nil, ss, info, handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedID == "" {
		t.Error("expected a generated request ID in stream context, got empty string")
	}
}

// handler の返り値がそのまま返されることを確認する（パススルー動作）。
func TestStreamInterceptor_PassesThrough(t *testing.T) {
	interceptor := StreamInterceptor()
	ss := &mockServerStream{ctx: context.Background()}

	handlerCalled := false
	handler := func(srv interface{}, stream grpc.ServerStream) error {
		handlerCalled = true
		return nil
	}

	info := &grpc.StreamServerInfo{FullMethod: "/test.Service/StreamMethod"}
	err := interceptor(nil, ss, info, handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !handlerCalled {
		t.Error("handler was not called")
	}
}
