# Design: authorization.go v2 Improvements

**Date:** 2026-03-18
**Status:** Approved
**Scope:** Three independent improvements to the `authorization.go` library

---

## Background

`authorization.go` is a gRPC authorization middleware library for Go. It provides two independent modules:

- `protobuf_policy_option` — reads policy definitions from `.proto` method options and injects resolved policy into `context.Context`
- `policy_verification` — retrieves policy from context and calls an external authorization server to verify access

The current state has three gaps identified through OSS value analysis:

1. No test utilities for library users
2. No streaming RPC support
3. Documentation is Japanese-only, limiting OSS adoption

---

## Change 1: `endpointtest` Package

### Problem

Library users need to write tests for their own gRPC services that use this library. Currently they must implement `endpoint.VerifierEndpoint` themselves in every test file. `interceptor_test.go` already has a local `mockVerifierEndpoint` struct that duplicates this pattern.

### Design

Add `policy_verification/endpointtest/mock.go` — a testing utility package providing:

**Mock verifier constructors:**

```go
// Always returns nil (authorization granted)
func Allow() endpoint.VerifierEndpoint

// Always returns codes.PermissionDenied
func Deny() endpoint.VerifierEndpoint

// Custom logic
func Func(fn func(ctx context.Context, resource, action string) error) endpoint.VerifierEndpoint
```

**gRPC context helpers:**

```go
// Injects "Authorization: Bearer <token>" into gRPC incoming metadata
func CtxWithBearerToken(ctx context.Context, token string) context.Context

// Injects x-request-id into gRPC incoming metadata
func CtxWithRequestID(ctx context.Context, id string) context.Context
```

**Test assertion helper:**

```go
// Asserts err is a gRPC status error with the expected code
func AssertGRPCCode(t *testing.T, err error, wantCode codes.Code)
```

### Impact on Existing Tests

- `interceptor_test.go`: remove local `mockVerifierEndpoint`, replace with `endpointtest.Allow()` / `endpointtest.Func(...)`
- `rest_o3_policy_verifier_test.go`: replace local `assertGRPCCode` and `ctxWithBearerToken` helpers with `endpointtest` equivalents; `httptest.NewServer` setup remains (o3-specific HTTP behavior)

---

## Change 2: Streaming RPC Support

### Problem

Only `grpc.UnaryServerInterceptor` is provided. gRPC services using server/client/bidi streaming RPCs cannot use this library.

### Security Model

Authorization must be checked **per message on `RecvMsg`**, with no caching. Rationale:

- A stream is one logical operation but may last longer than a token's TTL
- If a user's permissions are revoked mid-stream, the next `RecvMsg` must reject them
- Caching would introduce a window where revoked permissions continue to grant access
- Token expiry during `SendMsg` (between RecvMsg calls) is a session/connection management concern, not an authorization concern — out of scope for this library
- `MaxConnectionAge` is a deployment-level control that the library cannot rely on

### Design

Add `StreamInterceptor` to both modules, following the same Option pattern as `Interceptor`.

**`protobuf_policy_option.StreamInterceptor`:**

```go
func StreamInterceptor(opts ...Option) grpc.StreamServerInterceptor
```

- Resolves policy from proto method options at stream establishment (same logic as Unary)
- Marks interceptor as ran in context (same as Unary)
- **`field_mappings` (dynamic resource resolution from message fields) is not supported for streaming** — streaming RPCs must use static resource strings only
- If a method has `field_mappings` defined and is called via streaming, return `codes.Internal`

**`policy_verification.StreamInterceptor`:**

```go
func StreamInterceptor(verifierEndpoint endpoint.VerifierEndpoint, opts ...Option) grpc.StreamServerInterceptor
```

- Wraps `grpc.ServerStream` to intercept `RecvMsg`
- Calls `verifierEndpoint.Verify` before each `RecvMsg` completes
- `SendMsg` is not intercepted (token expiry during send is out of scope)

```go
type authServerStream struct {
    grpc.ServerStream
    verifyFn func() error
}

func (s *authServerStream) RecvMsg(m interface{}) error {
    if err := s.verifyFn(); err != nil {
        return err
    }
    return s.ServerStream.RecvMsg(m)
}
```

**Usage:**

```go
grpc.NewServer(
    grpc.ChainUnaryInterceptor(
        policyoption.Interceptor(),
        policyverification.Interceptor(verifier),
    ),
    grpc.ChainStreamInterceptor(
        policyoption.StreamInterceptor(),
        policyverification.StreamInterceptor(verifier),
    ),
)
```

---

## Change 3: README Restructure

### Problem

`README.md` is written entirely in Japanese, limiting OSS adoption. Additionally, the new streaming and testing features need documentation.

### Design

```
README.md       ← English (primary, new)
README.ja.md    ← Japanese (existing content migrated + updated)
```

**README.md sections:**

1. What it is (1–2 sentence summary)
2. Module structure (directory tree + responsibility table)
3. Interceptor chain order and rationale
4. Usage example (Unary)
5. Streaming RPC support (new)
6. Testing utilities (`endpointtest`)
7. License

**README.ja.md:** Japanese translation of the new README.md, replacing the current README.md content.

Per-module READMEs (`policy_verification/README.md`, etc.) updated to match if they exist.

---

## Commit Plan

| # | Commit | Contents |
|---|---|---|
| 1 | `feat: add endpointtest package and refactor tests` | New `endpointtest` package; rewrite `interceptor_test.go` and `rest_o3_policy_verifier_test.go` |
| 2 | `feat: add StreamInterceptor to both modules` | `StreamInterceptor` in `protobuf_policy_option` and `policy_verification`; per-message auth on RecvMsg |
| 3 | `docs: add English README and Japanese README.ja.md` | New `README.md` (English); `README.ja.md` (Japanese) |

---

## Out of Scope

- OPA or other `VerifierEndpoint` adapter implementations (separate future work)
- Dynamic resource resolution (`field_mappings`) for streaming RPCs
- Per-`SendMsg` authorization checks
- Cache layer for streaming authorization
