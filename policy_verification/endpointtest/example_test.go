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
