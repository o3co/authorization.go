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

// Package requesttracking provides gRPC server interceptors that manage
// x-request-id for distributed request tracing.
//
// The interceptors extract x-request-id from incoming gRPC metadata or
// generate a new one if absent, and store it in the context for downstream
// use. This module is independent of the authorization pipeline and can be
// used standalone.
//
// Usage:
//
//	grpc.NewServer(
//	    grpc.ChainUnaryInterceptor(
//	        requesttracking.Interceptor(),
//	    ),
//	    grpc.ChainStreamInterceptor(
//	        requesttracking.StreamInterceptor(),
//	    ),
//	)
//
// Retrieve the request ID in handlers or downstream interceptors:
//
//	id := requesttracking.RequestIDFromContext(ctx)
package requesttracking
