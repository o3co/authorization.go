# request_tracking

[![Go Reference](https://pkg.go.dev/badge/github.com/o3co/grpc.authz/request_tracking.svg)](https://pkg.go.dev/github.com/o3co/grpc.authz/request_tracking)

[日本語](README.ja.md)

gRPC server interceptors for `x-request-id` management. Extracts the request ID from incoming metadata or generates a new one, and stores it in the context for downstream use.

## Install

```bash
go get github.com/o3co/grpc.authz/request_tracking
```

## Usage

### Interceptors

```go
import rt "github.com/o3co/grpc.authz/request_tracking"

grpc.NewServer(
    grpc.ChainUnaryInterceptor(
        rt.Interceptor(),
    ),
    grpc.ChainStreamInterceptor(
        rt.StreamInterceptor(),
    ),
)
```

### Retrieve request ID in handlers

```go
func (s *server) GetPost(ctx context.Context, req *pb.GetPostRequest) (*pb.Post, error) {
    requestID := rt.RequestIDFromContext(ctx)
    // ...
}
```

### Retrieve request ID for HTTP propagation

When calling downstream HTTP services, retrieve the ID from context and set the header yourself:

```go
if id := rt.RequestIDFromContext(ctx); id != "" {
    httpReq.Header.Set("x-request-id", id)
}
```

### Structured logging

Wrap any `slog.Handler` with `NewRequestIDHandler` to automatically include the request ID in every log record:

```go
handler := rt.NewRequestIDHandler(slog.NewTextHandler(os.Stderr, nil))
slog.SetDefault(slog.New(handler))

// then anywhere:
slog.InfoContext(ctx, "handling request")
// → time=... level=INFO msg="handling request" x-request-id=abc123
```

Use `WithAttributeKey` to change the log attribute key:

```go
handler := rt.NewRequestIDHandler(base, rt.WithAttributeKey("trace-id"))
```

### With policy_verification

When used together with the authorization pipeline, add it as the first interceptor:

```go
grpc.NewServer(
    grpc.ChainUnaryInterceptor(
        rt.Interceptor(),                        // request tracking (opt-in)
        policyoption.Interceptor(),              // policy resolution
        policyverification.Interceptor(verifier), // authorization check
    ),
)
```

## Request ID Format

Generated IDs follow the format `YYYYMMDDHHmmss_<uuid-v4-hex>` (e.g., `20260331120000_550e8400e29b41d4a716446655440000`).

If the client (or an upstream proxy) provides `x-request-id` in gRPC metadata, the interceptor uses it as-is without generating a new one.

## License

Apache License 2.0
