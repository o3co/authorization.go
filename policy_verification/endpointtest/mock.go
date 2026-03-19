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

// Package endpointtest provides test utilities for services using the
// policy_verification module. Import this package only from test files.
package endpointtest

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/o3co/grpc.authz/policy_verification/endpoint"
)

type mockVerifier struct {
	fn func(ctx context.Context, resource, action string) error
}

func (m *mockVerifier) Verify(ctx context.Context, resource, action string) error {
	return m.fn(ctx, resource, action)
}

// Allow returns a VerifierEndpoint that always grants authorization.
func Allow() endpoint.VerifierEndpoint {
	return &mockVerifier{fn: func(_ context.Context, _, _ string) error { return nil }}
}

// Deny returns a VerifierEndpoint that always denies with codes.PermissionDenied.
func Deny() endpoint.VerifierEndpoint {
	return &mockVerifier{fn: func(_ context.Context, _, _ string) error {
		return status.Error(codes.PermissionDenied, "access denied")
	}}
}

// Func returns a VerifierEndpoint that calls fn on each Verify call.
func Func(fn func(ctx context.Context, resource, action string) error) endpoint.VerifierEndpoint {
	return &mockVerifier{fn: fn}
}

// CtxWithBearerToken injects "Authorization: Bearer <token>" into gRPC incoming metadata.
// Existing incoming metadata on ctx is preserved.
func CtxWithBearerToken(ctx context.Context, token string) context.Context {
	existing, _ := metadata.FromIncomingContext(ctx)
	md := metadata.Join(existing, metadata.Pairs("authorization", "Bearer "+token))
	return metadata.NewIncomingContext(ctx, md)
}

// CtxWithRequestID injects x-request-id into gRPC incoming metadata.
// Existing incoming metadata on ctx is preserved.
func CtxWithRequestID(ctx context.Context, id string) context.Context {
	existing, _ := metadata.FromIncomingContext(ctx)
	md := metadata.Join(existing, metadata.Pairs("x-request-id", id))
	return metadata.NewIncomingContext(ctx, md)
}

// AssertGRPCCode asserts that err is a gRPC status error with the expected code.
// Do not use with codes.OK — use `if err != nil { t.Fatalf(...) }` instead.
func AssertGRPCCode(t *testing.T, err error, wantCode codes.Code) {
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
