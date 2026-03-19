# grpc.authz

`grpc.authz` is a gRPC authorization middleware library for Go. It lets you declare access policy (resource + action) directly in `.proto` method options, then enforce it automatically via interceptors — no hand-written auth checks scattered across your handlers.

## Why

When authorization policy lives in the code, it drifts away from the API contract. Reviews miss it, refactors break it, and every new RPC needs a boilerplate check. By co-locating policy with the method definition in `.proto`, the rules are visible in the same place the API is designed, easy to audit in code review, and automatically enforced at runtime.

`grpc.authz` is intentionally lightweight. Teams already using protobuf don't need the full weight of OPA or Casbin — they just need a clean place to declare "who can do what to which resource" and a reliable way to enforce it.

## How it works

```text
gRPC request
     │
     ▼
┌─────────────────────────────────────────┐
│  protobuf_policy_option.Interceptor     │  reads (o3.policy) option from .proto,
│                                         │  resolves field_mappings from request,
│                                         │  injects Policy{Resource, Action} into ctx
└──────────────────┬──────────────────────┘
                   │ ctx carries resolved policy
                   ▼
┌─────────────────────────────────────────┐
│  policy_verification.Interceptor        │  reads policy from ctx,
│                                         │  POSTs to authorization server /verify,
│                                         │  maps HTTP status → gRPC status code
└──────────────────┬──────────────────────┘
                   │
                   ▼
             handler (your code)
```

The two modules are independent Go modules with a deliberate split of responsibilities:

| Module | Responsibility |
| --- | --- |
| `protobuf_policy_option` | Reads the `(o3.policy)` method option from the proto registry, resolves `<placeholder>` tokens using request fields, and stores the result in `context.Context` |
| `policy_verification` | Reads the resolved policy from context, calls an external authorization server via `POST /verify`, and translates the HTTP response into the appropriate gRPC status code |

Keeping them separate means you can swap the authorization backend (e.g. a gRPC verifier instead of REST) without touching the policy declaration layer, and you can unit-test each concern in isolation.

## Interceptor chain

The two interceptors **must** be chained in this exact order:

```text
[1] protobuf_policy_option.Interceptor   →  resolves policy into ctx
[2] policy_verification.Interceptor      →  reads policy from ctx and verifies
```

If the order is reversed or `protobuf_policy_option.Interceptor` is missing entirely, `policy_verification.Interceptor` will return `codes.Internal` on every request with the message `protobuf_policy_option.Interceptor is not registered in the interceptor chain`.

```go
import (
    "log/slog"
    "time"
    policyoption       "github.com/o3co/grpc.authz/protobuf_policy_option"
    policyverification "github.com/o3co/grpc.authz/policy_verification"
    pvendpoint         "github.com/o3co/grpc.authz/policy_verification/endpoint"
)

verifier, err := pvendpoint.NewRESTEndpoint(
    "http://auth-service/",
    pvendpoint.WithTimeout(5 * time.Second),
    pvendpoint.WithLogLevel(slog.LevelError),
)
if err != nil { /* handle */ }

grpc.NewServer(
    grpc.ChainUnaryInterceptor(
        policyoption.Interceptor(
            policyoption.WithLogLevel(slog.LevelError),
        ),
        policyverification.Interceptor(verifier,
            policyverification.WithLogLevel(slog.LevelError),
        ),
    ),
    grpc.ChainStreamInterceptor(
        policyoption.StreamInterceptor(
            policyoption.WithLogLevel(slog.LevelError),
        ),
        policyverification.StreamInterceptor(verifier,
            policyverification.WithLogLevel(slog.LevelError),
        ),
    ),
)
```

## Proto option reference

Declare the `(o3.policy)` option on any method that requires authorization:

```proto
syntax = "proto3";

import "policy.proto";  // provides the (o3.policy) extension

service PostService {

  // Static resource — no field extraction needed
  rpc ListPosts(ListPostsRequest) returns (ListPostsResponse) {
    option (o3.policy) = {
      resource: "posts"   // literal resource identifier sent to /verify
      action: "list"      // action string sent to /verify
    };
  }

  // Dynamic resource — placeholder resolved from request field
  rpc GetPost(GetPostRequest) returns (GetPostResponse) {
    option (o3.policy) = {
      resource: "posts/<id>"          // <id> is replaced at runtime
      action: "read"
      field_mappings: [
        { placeholder: "id", request_field: "id" }
        // placeholder: name used in the resource template (without angle brackets)
        // request_field: proto field name on the request message
      ]
    };
  }
}
```

`field_mappings` supports scalar proto field types: `string`, `bytes`, `int32/64`, `uint32/64`, `bool`. `repeated` fields, `map` fields, and nested messages are not supported.

## Quick start (Unary)

```go
package main

import (
    "log"
    "log/slog"
    "net"
    "time"

    "google.golang.org/grpc"

    policyoption       "github.com/o3co/grpc.authz/protobuf_policy_option"
    policyverification "github.com/o3co/grpc.authz/policy_verification"
    pvendpoint         "github.com/o3co/grpc.authz/policy_verification/endpoint"

    // your generated proto package
    postv1 "example.com/myapp/gen/post/v1"
)

func main() {
    verifier, err := pvendpoint.NewRESTEndpoint(
        "http://auth-service/",
        pvendpoint.WithTimeout(5 * time.Second),
    )
    if err != nil {
        log.Fatalf("failed to create verifier: %v", err)
    }

    srv := grpc.NewServer(
        grpc.ChainUnaryInterceptor(
            policyoption.Interceptor(
                policyoption.WithLogLevel(slog.LevelWarn),
            ),
            policyverification.Interceptor(verifier),
        ),
    )

    postv1.RegisterPostServiceServer(srv, &postServiceServer{})

    lis, err := net.Listen("tcp", ":50051")
    if err != nil {
        log.Fatalf("listen: %v", err)
    }
    log.Fatal(srv.Serve(lis))
}
```

Methods without an `(o3.policy)` option are passed through without any authorization check.

## Streaming RPC

Use `StreamInterceptor` alongside `Interceptor` in the same chain order:

```go
grpc.ChainStreamInterceptor(
    policyoption.StreamInterceptor(),
    policyverification.StreamInterceptor(verifier),
)
```

For streaming RPCs, authorization is checked on **every `RecvMsg` call**, not just at stream open. This means a token that is revoked mid-stream will be rejected on the next message, rather than allowing the entire stream to complete with a stale credential.

**`field_mappings` are not supported for streaming RPCs.** The request message is not available when the stream is established, so resource templates with placeholders cannot be resolved. If a streaming method's proto option includes `field_mappings`, the interceptor returns `codes.Internal`. Use a static resource string instead:

```proto
rpc WatchPosts(WatchPostsRequest) returns (stream Post) {
  option (o3.policy) = {
    resource: "posts"   // static — no field_mappings
    action: "watch"
  };
}
```

## Testing your service

The `endpointtest` package provides mock `VerifierEndpoint` implementations for use in tests. Import it only from test files.

```go
import "github.com/o3co/grpc.authz/policy_verification/endpointtest"
```

```go
// Always allow — use to test the happy path
verifier := endpointtest.Allow()

// Always deny with codes.PermissionDenied — use to test access denied behavior
verifier := endpointtest.Deny()

// Custom logic — inspect resource and action in tests
verifier := endpointtest.Func(func(ctx context.Context, resource, action string) error {
    if resource == "posts/123" && action == "read" {
        return nil
    }
    return status.Error(codes.PermissionDenied, "access denied")
})
```

Helper functions for building test contexts:

```go
// Inject "Authorization: Bearer <token>" into gRPC incoming metadata
ctx = endpointtest.CtxWithBearerToken(ctx, "my-token")

// Inject a known x-request-id for deterministic test assertions
ctx = endpointtest.CtxWithRequestID(ctx, "test-request-id")
```

Assert the gRPC status code of an error:

```go
// Asserts err has code codes.PermissionDenied
endpointtest.AssertGRPCCode(t, err, codes.PermissionDenied)
```

## Authorization server contract

The `policy_verification` module sends a single HTTP request per RPC call (and per `RecvMsg` for streaming):

```http
POST /verify
Content-Type: application/json
Authorization: Bearer <token forwarded from gRPC incoming metadata>
x-request-id: 20260318120530_a1b2c3d4e5f6...

{"resource": "posts/123", "action": "read"}
```

Header forwarding rules:

- `Authorization`: required. Forwarded verbatim from gRPC `authorization` metadata. If absent, the interceptor returns `codes.Unauthenticated` before sending the request.
- `x-request-id`: forwarded if present in gRPC metadata. If absent, a new ID is generated in the format `YYYYMMDDHHmmss_<uuid-v4>`.

Response → gRPC status code mapping:

| HTTP response | gRPC status code |
| --- | --- |
| `2xx` | `codes.OK` (request proceeds) |
| `401` | `codes.Unauthenticated` |
| `403` | `codes.PermissionDenied` |
| Any other | `codes.Internal` |

The authorization server's response body is never forwarded to the gRPC client. It is logged at error level (up to 1 KB) for debugging.

## License

Apache 2.0
