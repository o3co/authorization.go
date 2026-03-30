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

	rt "github.com/o3co/grpc.authz/request_tracking"
)

// VerifierEndpoint is the interface for policy verification endpoints.
type VerifierEndpoint interface {
	Verify(ctx context.Context, resource, action string) error
}

// WithRequestID stores the request ID in the context.
//
// Deprecated: Use requesttracking.WithRequestID directly.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return rt.WithRequestID(ctx, requestID)
}

// RequestIDFromContext retrieves the request ID from the context.
//
// Deprecated: Use requesttracking.RequestIDFromContext directly.
func RequestIDFromContext(ctx context.Context) string {
	return rt.RequestIDFromContext(ctx)
}
