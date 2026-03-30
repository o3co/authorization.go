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
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// StaticRule defines an allow rule for a resource/action pair.
// Both Resource and Action support three matching modes:
//
//   - Exact match: "posts" matches only "posts"
//   - Wildcard: "*" matches anything
//   - Prefix wildcard: "posts/*" matches "posts/123", "posts/123/comments/456", etc.
type StaticRule struct {
	Resource string
	Action   string
}

type staticEndpoint struct {
	rules []StaticRule
}

// NewStaticEndpoint creates a VerifierEndpoint that evaluates rules locally
// without calling an external service. If any rule matches the requested
// resource and action, access is allowed. If no rules match, access is denied.
//
// This is useful for simple deployments, development, and as a starting point
// before introducing an external policy engine like OPA or Cedar.
func NewStaticEndpoint(rules []StaticRule) VerifierEndpoint {
	r := make([]StaticRule, len(rules))
	copy(r, rules)
	return &staticEndpoint{rules: r}
}

// matchPattern checks if value matches pattern.
// Supports exact match, "*" (match all), and "prefix/*" (prefix match).
func matchPattern(pattern, value string) bool {
	if pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(value, prefix)
	}
	return pattern == value
}

// Verify checks the resource and action against the static rules.
func (e *staticEndpoint) Verify(ctx context.Context, resource, action string) error {
	_, err := getToken(ctx)
	if err != nil {
		return status.Errorf(codes.Unauthenticated, "failed to get authorization token: %v", err)
	}

	for _, rule := range e.rules {
		if matchPattern(rule.Resource, resource) && matchPattern(rule.Action, action) {
			return nil
		}
	}

	return status.Error(codes.PermissionDenied, "access denied")
}
