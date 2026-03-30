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

package endpointtest_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/o3co/grpc.authz/policy_verification/endpointtest"
)

func TestAllow_ReturnsNil(t *testing.T) {
	ep := endpointtest.Allow()
	if err := ep.Verify(context.Background(), "res", "act"); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestDeny_ReturnsPermissionDenied(t *testing.T) {
	ep := endpointtest.Deny()
	endpointtest.AssertGRPCCode(t, ep.Verify(context.Background(), "res", "act"), codes.PermissionDenied)
}

func TestFunc_CallsProvidedFunction(t *testing.T) {
	called := false
	ep := endpointtest.Func(func(_ context.Context, resource, action string) error {
		called = true
		if resource != "posts" || action != "read" {
			t.Errorf("resource=%q action=%q, want posts/read", resource, action)
		}
		return nil
	})
	_ = ep.Verify(context.Background(), "posts", "read")
	if !called {
		t.Error("fn was not called")
	}
}

func TestCtxWithBearerToken_InjectsAuthHeader(t *testing.T) {
	ctx := endpointtest.CtxWithBearerToken(context.Background(), "my-token")
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		t.Fatal("no metadata in context")
	}
	vals := md["authorization"]
	if len(vals) == 0 || vals[0] != "Bearer my-token" {
		t.Errorf("authorization = %v, want [Bearer my-token]", vals)
	}
}

func TestCtxWithRequestID_InjectsHeader(t *testing.T) {
	ctx := endpointtest.CtxWithRequestID(context.Background(), "req-123")
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		t.Fatal("no metadata in context")
	}
	vals := md["x-request-id"]
	if len(vals) == 0 || vals[0] != "req-123" {
		t.Errorf("x-request-id = %v, want [req-123]", vals)
	}
}

func TestCtxWithBearerTokenAndRequestID_BothPreserved(t *testing.T) {
	ctx := endpointtest.CtxWithBearerToken(context.Background(), "my-token")
	ctx = endpointtest.CtxWithRequestID(ctx, "req-123")
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		t.Fatal("no metadata in context")
	}
	if vals := md["authorization"]; len(vals) == 0 || vals[0] != "Bearer my-token" {
		t.Errorf("authorization = %v, want [Bearer my-token]", vals)
	}
	if vals := md["x-request-id"]; len(vals) == 0 || vals[0] != "req-123" {
		t.Errorf("x-request-id = %v, want [req-123]", vals)
	}
}

func TestAssertGRPCCode_MatchingCode_Passes(t *testing.T) {
	fakeT := &testing.T{}
	err := status.Error(codes.NotFound, "not found")
	endpointtest.AssertGRPCCode(fakeT, err, codes.NotFound)
	if fakeT.Failed() {
		t.Error("AssertGRPCCode should not fail for matching code")
	}
}

// fakeT is a minimal testing.TB implementation for testing that AssertGRPCCode
// calls t.Fatalf when it should. It records the failure message without calling
// runtime.Goexit (by design), so the outer test can inspect the result.
// The early-return guards prevent execution continuation after a failure from
// masking the intended failure path with additional Errorf calls.
type fakeT struct {
	testing.TB
	failed bool
	msg    string
}

func (f *fakeT) Helper() {}

func (f *fakeT) Fatalf(format string, args ...interface{}) {
	if f.failed {
		return
	}
	f.failed = true
	f.msg = fmt.Sprintf(format, args...)
}

func (f *fakeT) Errorf(format string, args ...interface{}) {
	if f.failed {
		return
	}
	f.failed = true
	f.msg = fmt.Sprintf(format, args...)
}

func TestAssertGRPCCode_NilError_Fails(t *testing.T) {
	ft := &fakeT{}
	endpointtest.AssertGRPCCode(ft, nil, codes.PermissionDenied)
	if !ft.failed {
		t.Fatal("expected AssertGRPCCode to call Fatalf for nil error, but it did not")
	}
}

func TestAssertGRPCCode_NonGRPCError_Fails(t *testing.T) {
	ft := &fakeT{}
	endpointtest.AssertGRPCCode(ft, errors.New("plain error"), codes.PermissionDenied)
	if !ft.failed {
		t.Fatal("expected AssertGRPCCode to call Fatalf for non-gRPC error, but it did not")
	}
}

