//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/o3co/grpc.authz/policy_verification/endpoint"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const opaBaseURL = "http://localhost:8181"

func ctxWithBearerToken(token string) context.Context {
	md := metadata.Pairs("authorization", "Bearer "+token)
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

func TestOPAIntegration_AllowRead(t *testing.T) {
	ep, err := endpoint.NewOPAEndpoint(opaBaseURL, "authz/allow")
	if err != nil {
		t.Fatalf("failed to create OPA endpoint: %v", err)
	}
	if err := ep.Verify(ctxWithBearerToken("test-token"), "posts/123", "read"); err != nil {
		t.Errorf("expected allow, got %v", err)
	}
}

func TestOPAIntegration_DenyWrite(t *testing.T) {
	ep, err := endpoint.NewOPAEndpoint(opaBaseURL, "authz/allow")
	if err != nil {
		t.Fatalf("failed to create OPA endpoint: %v", err)
	}
	err = ep.Verify(ctxWithBearerToken("test-token"), "posts/123", "write")
	assertGRPCCode(t, err, codes.PermissionDenied)
}

func TestOPAIntegration_DenyUnknownResource(t *testing.T) {
	ep, err := endpoint.NewOPAEndpoint(opaBaseURL, "authz/allow")
	if err != nil {
		t.Fatalf("failed to create OPA endpoint: %v", err)
	}
	err = ep.Verify(ctxWithBearerToken("test-token"), "users/1", "read")
	assertGRPCCode(t, err, codes.PermissionDenied)
}

func TestOPAIntegration_NoToken(t *testing.T) {
	ep, err := endpoint.NewOPAEndpoint(opaBaseURL, "authz/allow")
	if err != nil {
		t.Fatalf("failed to create OPA endpoint: %v", err)
	}
	err = ep.Verify(context.Background(), "posts/123", "read")
	assertGRPCCode(t, err, codes.Unauthenticated)
}
