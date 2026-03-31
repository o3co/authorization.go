# token_introspection

[![Go Reference](https://pkg.go.dev/badge/github.com/o3co/grpc.authz/token_introspection.svg)](https://pkg.go.dev/github.com/o3co/grpc.authz/token_introspection)

[日本語](README.ja.md)

gRPC server interceptors for credential validation via pluggable introspection backends. Dispatches by authentication scheme (Bearer, Basic, etc.) with strategy chain evaluation and optional caching.

## Install

```bash
go get github.com/o3co/grpc.authz/token_introspection
```

## Usage

### Basic: Bearer + RFC 7662

```go
import ti "github.com/o3co/grpc.authz/token_introspection"

rfc7662, _ := ti.NewRFC7662Introspector(
    "http://auth.provider:3000/oauth/introspect",
)

grpc.NewServer(
    grpc.ChainUnaryInterceptor(
        ti.Interceptor(
            ti.WithIntrospector(rfc7662),
        ),
    ),
    grpc.ChainStreamInterceptor(
        ti.StreamInterceptor(
            ti.WithIntrospector(rfc7662),
        ),
    ),
)
```

### With cache

```go
grpc.NewServer(
    grpc.ChainUnaryInterceptor(
        ti.Interceptor(
            ti.WithIntrospector(rfc7662),
            ti.WithCache(ti.NewInMemoryCache(30 * time.Second)),
        ),
    ),
)
```

Cache key is `SHA-256(scheme + ":" + credential)`. All schemes share one cache instance. Only successful results are cached; errors are not.

### Access claims in handlers

```go
func (s *server) GetPost(ctx context.Context, req *pb.GetPostRequest) (*pb.Post, error) {
    result := ti.ResultFromContext(ctx)
    if result != nil {
        log.Info("user", "subject", result.Subject, "scopes", result.Scopes)
    }
    // ...
}
```

### Forward request ID to introspection endpoint

```go
import rt "github.com/o3co/grpc.authz/request_tracking"

rfc7662, _ := ti.NewRFC7662Introspector(
    "http://auth.provider:3000/oauth/introspect",
    ti.WithRequestIDFunc(rt.RequestIDFromContext),
)
```

### Multiple strategies (same scheme)

```go
rfc7662, _ := ti.NewRFC7662Introspector(url)
jwtLocal := NewJWTIntrospector(publicKey) // future

grpc.NewServer(
    grpc.ChainUnaryInterceptor(
        ti.Interceptor(
            ti.WithIntrospector(rfc7662),   // bearer: 1st
            ti.WithIntrospector(jwtLocal),  // bearer: 2nd (fallthrough)
        ),
    ),
)
```

Strategy chain evaluation: `codes.Unauthenticated` → try next; any other error → abort; success → return.

### With the authorization pipeline

```go
grpc.NewServer(
    grpc.ChainUnaryInterceptor(
        rt.Interceptor(),                        // [1] request tracking (opt-in)
        ti.Interceptor(                          // [2] credential validation (opt-in)
            ti.WithIntrospector(rfc7662),
            ti.WithCache(ti.NewInMemoryCache(30 * time.Second)),
        ),
        policyoption.Interceptor(),              // [3] policy resolution
        policyverification.Interceptor(verifier), // [4] authorization check
    ),
)
```

### Public API (no auth required)

Requests without an `Authorization` header pass through without error. The handler receives `nil` from `ResultFromContext`. This supports mixed public/private APIs.

## License

Apache License 2.0
