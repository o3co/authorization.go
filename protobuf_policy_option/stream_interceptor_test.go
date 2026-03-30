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

package policyoption

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "github.com/o3co/grpc.authz/protobuf_policy_option/schema"
)

// mockServerStream implements grpc.ServerStream for testing.
type mockServerStream struct {
	ctx context.Context
}

func (m *mockServerStream) Context() context.Context         { return m.ctx }
func (m *mockServerStream) RecvMsg(interface{}) error        { return nil }
func (m *mockServerStream) SendMsg(interface{}) error        { return nil }
func (m *mockServerStream) SetHeader(metadata.MD) error      { return nil }
func (m *mockServerStream) SendHeader(metadata.MD) error     { return nil }
func (m *mockServerStream) SetTrailer(metadata.MD)           {}

func newMockStream(ctx context.Context) grpc.ServerStream {
	return &mockServerStream{ctx: ctx}
}

// TestStreamInterceptor_NoPolicy_CallsHandler verifies that a method with
// no proto policy registration passes through to the handler unchanged.
func TestStreamInterceptor_NoPolicy_CallsHandler(t *testing.T) {
	called := false
	handler := func(srv interface{}, stream grpc.ServerStream) error {
		called = true
		return nil
	}

	interceptor := StreamInterceptor()
	info := &grpc.StreamServerInfo{FullMethod: "/unknown.Service/UnknownMethod"}
	err := interceptor(nil, newMockStream(context.Background()), info, handler)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("handler was not called")
	}
}

// TestStreamInterceptor_NoPolicy_MarksInterceptorRan verifies that the
// interceptor marks itself as ran in the context even when there is no policy.
func TestStreamInterceptor_NoPolicy_MarksInterceptorRan(t *testing.T) {
	var capturedCtx context.Context
	handler := func(srv interface{}, stream grpc.ServerStream) error {
		capturedCtx = stream.Context()
		return nil
	}

	interceptor := StreamInterceptor()
	info := &grpc.StreamServerInfo{FullMethod: "/unknown.Service/UnknownMethod"}
	_ = interceptor(nil, newMockStream(context.Background()), info, handler)

	if capturedCtx == nil {
		t.Fatal("handler was not called")
	}
	if !InterceptorRanFromContext(capturedCtx) {
		t.Error("interceptor should mark itself as ran in context")
	}
}

// TestHasFieldMappings verifies the field_mappings detection helper.
func TestHasFieldMappings(t *testing.T) {
	noMappings := &pb.Policy{Resource: "posts", Action: "read"}
	if hasFieldMappings(noMappings) {
		t.Error("expected false for policy with no field_mappings")
	}

	withMappings := buildPolicyWithFieldMappings()
	if !hasFieldMappings(withMappings) {
		t.Error("expected true for policy with field_mappings")
	}
}

// TestFieldMappingsNotSupportedError verifies the error sentinel.
func TestFieldMappingsNotSupportedError(t *testing.T) {
	err := fieldMappingsNotSupportedError()
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got %T", err)
	}
	if st.Code() != codes.Internal {
		t.Errorf("code = %v, want %v", st.Code(), codes.Internal)
	}
	const wantMsg = "field_mappings are not supported for streaming RPCs; use a static resource string"
	if st.Message() != wantMsg {
		t.Errorf("message = %q, want %q", st.Message(), wantMsg)
	}
}

func buildPolicyWithFieldMappings() *pb.Policy {
	return &pb.Policy{
		Resource:      "posts/<id>",
		Action:        "read",
		FieldMappings: []*pb.FieldMapping{{Placeholder: "id", RequestField: "id"}},
	}
}
