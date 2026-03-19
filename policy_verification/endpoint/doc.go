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

// Package endpoint defines the VerifierEndpoint interface and provides a
// REST-based implementation for the o3 authorization server.
//
// To use a custom authorization backend, implement the VerifierEndpoint
// interface and pass it to [policyverification.Interceptor]:
//
//	type myVerifier struct{}
//
//	func (v *myVerifier) Verify(ctx context.Context, resource, action string) error {
//	    // call your authorization backend
//	}
//
//	grpc.NewServer(
//	    grpc.ChainUnaryInterceptor(
//	        policyoption.Interceptor(),
//	        policyverification.Interceptor(&myVerifier{}),
//	    ),
//	)
package endpoint
