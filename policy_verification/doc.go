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

// Package policyverification provides a gRPC server interceptor that enforces
// authorization by retrieving the access policy from the request context and
// calling an external authorization server to verify access.
//
// It is the second half of a two-module pipeline:
//
//  1. [protobuf_policy_option] resolves the policy declared in .proto method
//     options and injects it into the context.
//  2. This package reads that policy and calls the authorization server.
//
// The interceptors must be chained in this order:
//
//	grpc.NewServer(
//	    grpc.ChainUnaryInterceptor(
//	        policyoption.Interceptor(),
//	        policyverification.Interceptor(verifier),
//	    ),
//	    grpc.ChainStreamInterceptor(
//	        policyoption.StreamInterceptor(),
//	        policyverification.StreamInterceptor(verifier),
//	    ),
//	)
//
// For testing, use the [endpointtest] sub-package which provides mock
// VerifierEndpoint implementations.
package policyverification
