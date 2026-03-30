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
