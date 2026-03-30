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

// Package policyoption provides a gRPC server interceptor that reads access
// policy declarations from .proto method options and injects the resolved
// policy into the request context.
//
// It is the first half of a two-module pipeline. The second half,
// [policy_verification], reads the policy from context and calls an external
// authorization server.
//
// Annotate your gRPC methods in .proto:
//
//	rpc GetPost(GetPostRequest) returns (GetPostResponse) {
//	    option (o3co.authz.v1.policy) = {
//	        resource: "posts/{id}"
//	        action:   "read"
//	        field_mappings: [{placeholder: "id", field: "id"}]
//	    };
//	}
//
// Then register the interceptors in your server:
//
//	grpc.NewServer(
//	    grpc.ChainUnaryInterceptor(
//	        policyoption.Interceptor(),
//	        policyverification.Interceptor(verifier),
//	    ),
//	)
//
// Note: field_mappings are not supported for streaming RPCs. Use a static
// resource string for streaming methods.
package policyoption
