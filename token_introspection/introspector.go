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
	"time"
)

// Introspector validates a credential and returns the introspection result.
// The credential value does not include the scheme prefix (e.g., for Bearer
// it receives the raw JWT string, not "Bearer <jwt>").
//
// Scheme returns the authentication scheme this introspector handles (lowercase,
// e.g., "bearer", "basic"). The interceptor uses this to match against the
// Authorization header's scheme.
//
// Introspect returns gRPC status errors:
//   - codes.Unauthenticated: credential is invalid (chain moves to next strategy)
//   - codes.Internal: backend failure (chain aborts)
type Introspector interface {
	Scheme() string
	Introspect(ctx context.Context, credential string) (*IntrospectionResult, error)
}

// IntrospectionResult holds validated credential claims.
type IntrospectionResult struct {
	Subject   string         // sub — the authenticated user/entity
	Scopes    []string       // granted scopes
	ExpiresAt time.Time      // expiration (zero value if not set)
	TokenType string         // token_type — e.g. "at+jwt", "rt+jwt"
	Claims    map[string]any // additional claims (aud, iss, azp, etc.)
}
