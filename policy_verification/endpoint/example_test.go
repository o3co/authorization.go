package endpoint_test

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/o3co/grpc.authz/policy_verification/endpoint"
)

func ExampleNewRESTEndpoint() {
	verifier, err := endpoint.NewRESTEndpoint(
		"http://auth-service:8080",
		endpoint.WithTimeout(5*time.Second),
		endpoint.WithLogLevel(slog.LevelWarn),
	)
	if err != nil {
		panic(err)
	}

	// Use verifier with policy_verification.Interceptor:
	//   grpc.ChainUnaryInterceptor(
	//       policyoption.Interceptor(),
	//       policyverification.Interceptor(verifier),
	//   )
	_ = verifier
}

func ExampleNewOPAEndpoint() {
	verifier, err := endpoint.NewOPAEndpoint(
		"http://opa:8181",  // OPA server URL
		"authz/allow",      // Rego package/rule path
		endpoint.WithOPATimeout(5*time.Second),
		endpoint.WithOPALogLevel(slog.LevelWarn),
	)
	if err != nil {
		panic(err)
	}

	// OPA receives: {"input": {"resource": "...", "action": "...", "token": "..."}}
	// Write a Rego policy that evaluates `allow` to true or false.
	_ = verifier
}

func ExampleNewCedarAgentEndpoint() {
	verifier, err := endpoint.NewCedarAgentEndpoint(
		"http://cedar-agent:8180",
		endpoint.WithCedarAgentTimeout(5*time.Second),
		endpoint.WithCedarAgentPrincipalPrefix("User"),
		endpoint.WithCedarAgentResourcePrefix("Resource"),
		endpoint.WithCedarAgentPrincipalResolver(func(ctx context.Context, token string) string {
			// Extract subject from JWT, or return token as-is.
			return token
		}),
	)
	if err != nil {
		panic(err)
	}

	// Cedar agent receives entity UIDs:
	//   principal: User::"<subject>", action: Action::"<action>", resource: Resource::"<resource>"
	_ = verifier
}

func ExampleNewCedarAgentEndpoint_customPrefixes() {
	verifier, err := endpoint.NewCedarAgentEndpoint(
		"http://cedar-agent:8180",
		endpoint.WithCedarAgentPrincipalPrefix("Account"),
		endpoint.WithCedarAgentActionPrefix("Operation"),
		endpoint.WithCedarAgentResourcePrefix("Document"),
	)
	if err != nil {
		panic(err)
	}

	// Produces entity UIDs like:
	//   Account::"user-1", Operation::"read", Document::"doc/42"
	fmt.Println("verifier created")
	_ = verifier
	// Output: verifier created
}
