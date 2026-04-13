# protobuf_policy_option

`protobuf_policy_option` is a gRPC server interceptor module that reads the `(policy.v1.policy)` custom method option from the protobuf registry, resolves `<placeholder>` tokens in the resource string using fields from the incoming request, and stores the result in `context.Context` for downstream interceptors to consume. It is one of two modules that make up `grpc.authz`; it handles the policy declaration and resolution side, while `policy_verification` handles the enforcement side.

## Public API

### Interceptor

```go
func Interceptor(opts ...Option) grpc.UnaryServerInterceptor
```

Returns a unary server interceptor that, for each RPC call:

1. Looks up the `(policy.v1.policy)` method option from the proto registry (result is cached per interceptor instance after the first call).
2. Marks itself as ran in context so `policy_verification.Interceptor` can detect misconfiguration.
3. If no policy option is found, passes through to the next handler.
4. Resolves any `<placeholder>` tokens in the resource string using `field_mappings` and request fields.
5. Stores the resolved `Policy{Resource, Action}` in context.

### StreamInterceptor

```go
func StreamInterceptor(opts ...Option) grpc.StreamServerInterceptor
```

Returns a stream server interceptor with the same policy lookup and context injection logic as `Interceptor`. `field_mappings` are **not supported** for streaming RPCs — if a method's option includes `field_mappings`, the stream is rejected with `codes.Internal`. Use a static resource string for streaming methods.

### WithLogLevel

```go
func WithLogLevel(level slog.Level) Option
```

Sets the log level for the interceptor. Default: `slog.LevelError`.

### Context helpers

```go
func PolicyFromContext(ctx context.Context) (*Policy, bool)
```

Returns the resolved policy stored by `Interceptor`, and a boolean indicating whether one was present. Use this in custom downstream interceptors or middleware that need to inspect the resolved resource and action.

```go
func InterceptorRanFromContext(ctx context.Context) bool
```

Returns true if `protobuf_policy_option.Interceptor` (or `StreamInterceptor`) has already run in this context. Used internally by `policy_verification` to detect interceptor chain misconfiguration.

### Policy type

```go
type Policy struct {
    Resource string
    Action   string
}
```

## Proto setup

Import `policy.proto` (from the `schema` sub-package) in your `.proto` files to access the `(policy.v1.policy)` method option extension:

```proto
syntax = "proto3";

import "policy.proto";

service ItemService {
  rpc GetItem(GetItemRequest) returns (GetItemResponse) {
    option (policy.v1.policy) = {
      resource: "items/<id>"
      action:   "read"
      field_mappings: [
        { placeholder: "id", request_field: "id" }
      ]
    };
  }
}
```

The option is defined in the `policy.v1` protobuf package with field number `50000`.

## field_mappings explanation

`field_mappings` maps placeholder names in the resource template to proto field names on the request message.

| field_mappings field | Meaning |
| --- | --- |
| `placeholder` | The name used in the resource template, written as `<name>` (e.g. `"id"` matches `<id>`) |
| `request_field` | The proto field name on the request message to extract the value from |

Supported scalar field types: `string`, `bytes`, `int32/64`, `uint32/64`, `bool`. `repeated` fields, `map` fields, and nested message types are not supported. If an unsupported type or missing field is encountered, the interceptor returns `codes.Internal`.

Example: for `resource: "items/<id>"` with `field_mappings: [{ placeholder: "id", request_field: "id" }]`, if the request has `id = "42"`, the resolved resource is `"items/42"`.

## Usage example

```go
import (
    "log/slog"
    policyoption "github.com/o3co/grpc.authz/protobuf_policy_option"
)

grpc.NewServer(
    grpc.ChainUnaryInterceptor(
        policyoption.Interceptor(               // must come before policy_verification
            policyoption.WithLogLevel(slog.LevelWarn),
        ),
        // ... other interceptors
    ),
    grpc.ChainStreamInterceptor(
        policyoption.StreamInterceptor(         // must come before policy_verification
            policyoption.WithLogLevel(slog.LevelWarn),
        ),
        // ... other interceptors
    ),
)
```

Reading the resolved policy in a custom interceptor:

```go
import policyoption "github.com/o3co/grpc.authz/protobuf_policy_option"

policy, ok := policyoption.PolicyFromContext(ctx)
if ok {
    fmt.Println(policy.Resource, policy.Action)
}
```

See root README for full setup and interceptor chain requirements.
