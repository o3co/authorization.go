//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/o3co/grpc.authz/policy_verification/endpoint"
	"google.golang.org/grpc/codes"
)

const cedarBaseURL = "http://localhost:8180"

func TestCedarIntegration_AllowRead(t *testing.T) {
	ep, err := endpoint.NewCedarAgentEndpoint(cedarBaseURL,
		endpoint.WithCedarAgentPrincipalResolver(func(_ context.Context, _ string) string {
			return "test-user"
		}),
	)
	if err != nil {
		t.Fatalf("failed to create Cedar endpoint: %v", err)
	}
	if err := ep.Verify(ctxWithBearerToken("test-token"), "posts/123", "read"); err != nil {
		t.Errorf("expected allow, got %v", err)
	}
}

func TestCedarIntegration_DenyWrite(t *testing.T) {
	ep, err := endpoint.NewCedarAgentEndpoint(cedarBaseURL,
		endpoint.WithCedarAgentPrincipalResolver(func(_ context.Context, _ string) string {
			return "test-user"
		}),
	)
	if err != nil {
		t.Fatalf("failed to create Cedar endpoint: %v", err)
	}
	err = ep.Verify(ctxWithBearerToken("test-token"), "posts/123", "write")
	assertGRPCCode(t, err, codes.PermissionDenied)
}

func TestCedarIntegration_DenyUnknownResource(t *testing.T) {
	ep, err := endpoint.NewCedarAgentEndpoint(cedarBaseURL,
		endpoint.WithCedarAgentPrincipalResolver(func(_ context.Context, _ string) string {
			return "test-user"
		}),
	)
	if err != nil {
		t.Fatalf("failed to create Cedar endpoint: %v", err)
	}
	err = ep.Verify(ctxWithBearerToken("test-token"), "users/1", "read")
	assertGRPCCode(t, err, codes.PermissionDenied)
}

func TestCedarIntegration_NoToken(t *testing.T) {
	ep, err := endpoint.NewCedarAgentEndpoint(cedarBaseURL)
	if err != nil {
		t.Fatalf("failed to create Cedar endpoint: %v", err)
	}
	err = ep.Verify(context.Background(), "posts/123", "read")
	assertGRPCCode(t, err, codes.Unauthenticated)
}
