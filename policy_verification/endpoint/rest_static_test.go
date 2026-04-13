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

package endpoint

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
)

// --- NewStaticEndpoint ---

func TestNewStaticEndpoint_NilRules_ReturnsEndpoint(t *testing.T) {
	ep := NewStaticEndpoint(nil)
	if ep == nil {
		t.Error("expected non-nil endpoint")
	}
}

func TestNewStaticEndpoint_EmptyRules_ReturnsEndpoint(t *testing.T) {
	ep := NewStaticEndpoint([]StaticRule{})
	if ep == nil {
		t.Error("expected non-nil endpoint")
	}
}

// --- Verify: exact match ---

func TestStaticVerify_ExactMatch_Allow(t *testing.T) {
	ep := NewStaticEndpoint([]StaticRule{
		{Resource: "posts", Action: "list"},
	})
	ctx := ctxWithBearerToken("token")
	if err := ep.Verify(ctx, "posts", "list"); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestStaticVerify_ExactMatch_DenyWrongAction(t *testing.T) {
	ep := NewStaticEndpoint([]StaticRule{
		{Resource: "posts", Action: "list"},
	})
	ctx := ctxWithBearerToken("token")
	err := ep.Verify(ctx, "posts", "delete")
	assertGRPCCode(t, err, codes.PermissionDenied)
}

func TestStaticVerify_ExactMatch_DenyWrongResource(t *testing.T) {
	ep := NewStaticEndpoint([]StaticRule{
		{Resource: "posts", Action: "list"},
	})
	ctx := ctxWithBearerToken("token")
	err := ep.Verify(ctx, "users", "list")
	assertGRPCCode(t, err, codes.PermissionDenied)
}

// --- Verify: wildcard ---

func TestStaticVerify_WildcardResource_MatchesAny(t *testing.T) {
	ep := NewStaticEndpoint([]StaticRule{
		{Resource: "*", Action: "read"},
	})
	ctx := ctxWithBearerToken("token")
	if err := ep.Verify(ctx, "anything", "read"); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestStaticVerify_WildcardAction_MatchesAny(t *testing.T) {
	ep := NewStaticEndpoint([]StaticRule{
		{Resource: "posts", Action: "*"},
	})
	ctx := ctxWithBearerToken("token")
	if err := ep.Verify(ctx, "posts", "delete"); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestStaticVerify_WildcardBoth_MatchesAll(t *testing.T) {
	ep := NewStaticEndpoint([]StaticRule{
		{Resource: "*", Action: "*"},
	})
	ctx := ctxWithBearerToken("token")
	if err := ep.Verify(ctx, "anything", "whatever"); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

// --- Verify: prefix wildcard ---

func TestStaticVerify_PrefixWildcard_MatchesSubpath(t *testing.T) {
	ep := NewStaticEndpoint([]StaticRule{
		{Resource: "posts/*", Action: "read"},
	})
	ctx := ctxWithBearerToken("token")
	if err := ep.Verify(ctx, "posts/123", "read"); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestStaticVerify_PrefixWildcard_NoMatchExact(t *testing.T) {
	ep := NewStaticEndpoint([]StaticRule{
		{Resource: "posts/*", Action: "read"},
	})
	ctx := ctxWithBearerToken("token")
	// "posts" does not match "posts/*" — the wildcard requires something after "posts/"
	err := ep.Verify(ctx, "posts", "read")
	assertGRPCCode(t, err, codes.PermissionDenied)
}

func TestStaticVerify_PrefixWildcard_MatchesDeepSubpath(t *testing.T) {
	ep := NewStaticEndpoint([]StaticRule{
		{Resource: "posts/*", Action: "read"},
	})
	ctx := ctxWithBearerToken("token")
	if err := ep.Verify(ctx, "posts/123/comments/456", "read"); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestStaticVerify_ActionPrefixWildcard_Matches(t *testing.T) {
	ep := NewStaticEndpoint([]StaticRule{
		{Resource: "posts", Action: "admin:*"},
	})
	ctx := ctxWithBearerToken("token")
	if err := ep.Verify(ctx, "posts", "admin:delete"); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

// --- Verify: multiple rules ---

func TestStaticVerify_MultipleRules_FirstMatch(t *testing.T) {
	ep := NewStaticEndpoint([]StaticRule{
		{Resource: "posts", Action: "list"},
		{Resource: "posts/*", Action: "read"},
		{Resource: "users", Action: "list"},
	})
	ctx := ctxWithBearerToken("token")
	if err := ep.Verify(ctx, "posts/123", "read"); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
	if err := ep.Verify(ctx, "users", "list"); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestStaticVerify_MultipleRules_NoneMatch(t *testing.T) {
	ep := NewStaticEndpoint([]StaticRule{
		{Resource: "posts", Action: "list"},
		{Resource: "posts/*", Action: "read"},
	})
	ctx := ctxWithBearerToken("token")
	err := ep.Verify(ctx, "users", "delete")
	assertGRPCCode(t, err, codes.PermissionDenied)
}

// --- Verify: no token ---

func TestStaticVerify_NoToken_ReturnsUnauthenticated(t *testing.T) {
	ep := NewStaticEndpoint([]StaticRule{
		{Resource: "*", Action: "*"},
	})
	err := ep.Verify(context.Background(), "posts", "read")
	assertGRPCCode(t, err, codes.Unauthenticated)
}

// --- Verify: empty rules ---

func TestStaticVerify_NoRules_DeniesEverything(t *testing.T) {
	ep := NewStaticEndpoint(nil)
	ctx := ctxWithBearerToken("token")
	err := ep.Verify(ctx, "posts", "read")
	assertGRPCCode(t, err, codes.PermissionDenied)
}

// --- matchPattern ---

func TestMatchPattern_ExactMatch(t *testing.T) {
	if !matchPattern("posts", "posts") {
		t.Error("expected match")
	}
}

func TestMatchPattern_ExactNoMatch(t *testing.T) {
	if matchPattern("posts", "users") {
		t.Error("expected no match")
	}
}

func TestMatchPattern_Star(t *testing.T) {
	if !matchPattern("*", "anything") {
		t.Error("expected match")
	}
}

func TestMatchPattern_PrefixWildcard(t *testing.T) {
	if !matchPattern("posts/*", "posts/123") {
		t.Error("expected match")
	}
}

func TestMatchPattern_PrefixWildcard_NoMatch(t *testing.T) {
	if matchPattern("posts/*", "users/123") {
		t.Error("expected no match")
	}
}

// helper to create context with bearer token using metadata directly
// (ctxWithBearerToken is already available in rest_policy_verifier_test.go)
func ctxWithMetadataBearerToken(token string) context.Context {
	md := metadata.Pairs("authorization", "Bearer "+token)
	return metadata.NewIncomingContext(context.Background(), md)
}
