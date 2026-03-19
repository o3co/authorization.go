# policy_verification

`policy_verification` is a gRPC server interceptor module that reads the resolved authorization policy from `context.Context` and enforces it by calling an external authorization server. It is one of two modules that make up `grpc.authz`; it handles the enforcement side, while `protobuf_policy_option` handles the policy declaration and resolution side.

## Public API

### Interceptor

```go
func Interceptor(verifierEndpoint endpoint.VerifierEndpoint, opts ...Option) grpc.UnaryServerInterceptor
```

Returns a unary server interceptor that, for each RPC call:

1. Extracts or generates an `x-request-id` and stores it in context.
2. Checks that `protobuf_policy_option.Interceptor` ran (returns `codes.Internal` if not).
3. Reads the resolved policy from context. If no policy is set (method has no `(o3.policy)` option), the request passes through.
4. Calls `verifierEndpoint.Verify(ctx, resource, action)` and returns its error directly if non-nil.

### StreamInterceptor

```go
func StreamInterceptor(verifierEndpoint endpoint.VerifierEndpoint, opts ...Option) grpc.StreamServerInterceptor
```

Returns a stream server interceptor with the same chain guard and pass-through logic as `Interceptor`. When a policy is present, it wraps the stream so that `Verify` is called on **every `RecvMsg`** — not just at stream open.

### WithLogLevel

```go
func WithLogLevel(level slog.Level) Option
```

Sets the log level for the interceptor. Default: `slog.LevelError`.

### endpoint package

```go
import "github.com/o3co/grpc.authz/policy_verification/endpoint"
```

| Symbol | Description |
| --- | --- |
| `VerifierEndpoint` | Interface with a single method: `Verify(ctx context.Context, resource, action string) error` |
| `NewRESTEndpoint(baseURL string, opts ...Option) (VerifierEndpoint, error)` | Creates a REST implementation that POSTs to `<baseURL>/verify` |
| `WithTimeout(d time.Duration) Option` | HTTP client timeout (default: 10s) |
| `WithLogLevel(level slog.Level) Option` | Log level for the endpoint (default: `slog.LevelError`) |
| `WithMaxResponseBodySize(size int64) Option` | Max bytes to read from the response body (default: 1 MB) |
| `RequestIDFromContext(ctx) string` | Retrieves the x-request-id from context |

## endpointtest package

The `endpointtest` package provides mock `VerifierEndpoint` implementations for use in tests. Import it only from test files.

```go
import "github.com/o3co/grpc.authz/policy_verification/endpointtest"
```

| Function | Description |
| --- | --- |
| `Allow() VerifierEndpoint` | Always returns nil (allow) |
| `Deny() VerifierEndpoint` | Always returns `codes.PermissionDenied` |
| `Func(fn) VerifierEndpoint` | Calls `fn` on each `Verify` call |
| `CtxWithBearerToken(ctx, token) context.Context` | Injects `Authorization: Bearer <token>` into gRPC incoming metadata |
| `CtxWithRequestID(ctx, id) context.Context` | Injects `x-request-id` into gRPC incoming metadata |
| `AssertGRPCCode(t, err, code)` | Asserts `err` is a gRPC status error with the given code |

## Usage example

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
    pvendpoint.WithMaxResponseBodySize(512 * 1024),
    pvendpoint.WithLogLevel(slog.LevelWarn),
)
if err != nil { /* handle */ }

grpc.NewServer(
    grpc.ChainUnaryInterceptor(
        policyoption.Interceptor(),              // must come first
        policyverification.Interceptor(verifier),
    ),
    grpc.ChainStreamInterceptor(
        policyoption.StreamInterceptor(),        // must come first
        policyverification.StreamInterceptor(verifier),
    ),
)
```

To use a custom authorization backend, implement `VerifierEndpoint`:

```go
import "github.com/o3co/grpc.authz/policy_verification/endpoint"

type myVerifier struct{}

func (v *myVerifier) Verify(ctx context.Context, resource, action string) error {
    // custom authorization logic
    return nil
}
```

See root README for full setup and proto option reference.
