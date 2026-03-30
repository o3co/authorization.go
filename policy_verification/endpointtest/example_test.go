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

package endpointtest_test

import (
	"context"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/o3co/grpc.authz/policy_verification/endpointtest"
)

func ExampleAllow() {
	verifier := endpointtest.Allow()

	ctx := endpointtest.CtxWithBearerToken(context.Background(), "test-token")
	err := verifier.Verify(ctx, "posts/123", "read")
	fmt.Println(err)
	// Output: <nil>
}

func ExampleDeny() {
	verifier := endpointtest.Deny()

	ctx := endpointtest.CtxWithBearerToken(context.Background(), "test-token")
	err := verifier.Verify(ctx, "posts/123", "read")
	st, _ := status.FromError(err)
	fmt.Println(st.Code())
	// Output: PermissionDenied
}

func ExampleFunc() {
	verifier := endpointtest.Func(func(ctx context.Context, resource, action string) error {
		if resource == "posts/123" && action == "read" {
			return nil
		}
		return status.Error(codes.PermissionDenied, "access denied")
	})

	ctx := endpointtest.CtxWithBearerToken(context.Background(), "test-token")

	err := verifier.Verify(ctx, "posts/123", "read")
	fmt.Println("read posts/123:", err)

	err = verifier.Verify(ctx, "posts/123", "delete")
	st, _ := status.FromError(err)
	fmt.Println("delete posts/123:", st.Code())
	// Output:
	// read posts/123: <nil>
	// delete posts/123: PermissionDenied
}
