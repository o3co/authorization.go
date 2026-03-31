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

// Package tokenintrospection provides gRPC server interceptors for credential
// validation via pluggable introspection backends.
//
// The interceptor dispatches by authentication scheme (Bearer, Basic, etc.) and
// supports multiple strategies per scheme with fallthrough evaluation. An optional
// interceptor-level cache avoids redundant backend calls.
//
// Usage:
//
//	rfc7662, _ := tokenintrospection.NewRFC7662Introspector(
//	    "http://auth.provider:3000/oauth/introspect",
//	)
//
//	grpc.NewServer(
//	    grpc.ChainUnaryInterceptor(
//	        tokenintrospection.Interceptor(
//	            tokenintrospection.WithIntrospector(rfc7662),
//	            tokenintrospection.WithCache(tokenintrospection.NewInMemoryCache(ctx, 30 * time.Second)),
//	        ),
//	    ),
//	)
//
// Retrieve the result in handlers:
//
//	result := tokenintrospection.ResultFromContext(ctx)
package tokenintrospection
