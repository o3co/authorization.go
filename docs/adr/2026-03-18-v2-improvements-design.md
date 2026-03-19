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

### Scope

`endpointtest` is part of the `policy_verification` module (`policy_verification/endpointtest/`). It is intended for users of the `policy_verification` module. Users of `protobuf_policy_option` alone do not need it. The package must be documented as test-only with a package-level doc comment (`// Package endpointtest provides test utilities...`). It must not carry a build constraint since `_test.go` files in other packages need to import it.

### Design

Add `policy_verification/endpointtest/mock.go`:

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

### Tests

`endpointtest` itself has a `mock_test.go` with basic usage tests covering `Allow`, `Deny`, `Func`, `CtxWithBearerToken`, `CtxWithRequestID`, and `AssertGRPCCode`.

### Impact on Existing Tests

- `interceptor_test.go`: remove local `mockVerifierEndpoint`, replace with `endpointtest.Allow()` / `endpointtest.Func(...)`; replace local `chainInterceptors` helper context setup with `endpointtest.CtxWithBearerToken`
- `rest_o3_policy_verifier_test.go`: replace local `assertGRPCCode` and `ctxWithBearerToken` with `endpointtest.AssertGRPCCode` and `endpointtest.CtxWithBearerToken`; `httptest.NewServer` setup remains (o3-specific HTTP behavior)

---

## Change 2: Streaming RPC Support

### Motivation

Only `grpc.UnaryServerInterceptor` is provided. gRPC services using server/client/bidi streaming RPCs cannot use this library.

### Security Model

Authorization must be checked **per message on `RecvMsg`**, with no caching. Rationale:

- A stream is one logical operation but may last longer than a token's TTL
- If a user's permissions are revoked mid-stream, the next `RecvMsg` must reject them
- Caching would introduce a window where revoked permissions continue to grant access
- Token expiry during `SendMsg` (between RecvMsg calls) is a session/connection management concern, not an authorization concern — out of scope for this library
- `MaxConnectionAge` is a deployment-level control that the library cannot rely on

**Server-streaming RPC note:** For server-streaming RPCs (`rpc Foo(Request) returns (stream Response)`), the client sends exactly one message. The gRPC framework calls `RecvMsg` once before invoking the handler. This means the per-message check on `RecvMsg` reduces to a single check at stream establishment — functionally equivalent to the unary check. This is acceptable: there is no ongoing client-driven messaging to re-authorize, and the server's send-side is out of scope.

### `field_mappings` Restriction

Dynamic resource resolution from message fields (`field_mappings`) is not supported for streaming RPCs. Streaming RPCs must use static resource strings (e.g., `resource: "posts"`) only.

**Detection:** The check happens at **stream establishment** in `protobuf_policy_option.StreamInterceptor`, before the handler is invoked. If a method's policy contains one or more `field_mappings`, the interceptor returns immediately with `codes.Internal` and the error message `"field_mappings are not supported for streaming RPCs; use a static resource string"`. The stream never starts and `policy_verification.StreamInterceptor` is never invoked in this path.

**Cache:** `StreamInterceptor` uses its own `sync.Map` cache, separate from the `Interceptor` (unary) cache. Each interceptor instance maintains its own cache to prevent test pollution, matching the existing unary behavior.

### Implementation

**`protobuf_policy_option.StreamInterceptor`:**

```go
func StreamInterceptor(opts ...Option) grpc.StreamServerInterceptor
```

- At stream establishment: resolves policy from proto method options (same `getMethodPolicy` logic as unary, own `sync.Map` cache)
- If policy has `field_mappings`: return `codes.Internal` with message `"field_mappings are not supported for streaming RPCs; use a static resource string"`
- Marks interceptor as ran in context (same as unary)
- If no policy: pass through to handler

**`policy_verification.StreamInterceptor`:**

```go
func StreamInterceptor(verifierEndpoint endpoint.VerifierEndpoint, opts ...Option) grpc.StreamServerInterceptor
```

- Panics on nil `verifierEndpoint` (same behavior as unary `Interceptor`)
- Checks `policy.InterceptorRanFromContext` at stream establishment; returns `codes.Internal` if not ran (same misconfiguration guard as unary)
- Wraps `grpc.ServerStream` to intercept `RecvMsg`; calls `verifierEndpoint.Verify` using `stream.Context()` (evaluated at call time to respect cancellation and token expiry) before each message

```go
type authServerStream struct {
    grpc.ServerStream
    resource string
    action   string
    verifier endpoint.VerifierEndpoint
    log      *slog.Logger
}

func (s *authServerStream) RecvMsg(m interface{}) error {
    if err := s.verifier.Verify(s.ServerStream.Context(), s.resource, s.action); err != nil {
        s.log.Error("authorization check failed on RecvMsg", "resource", s.resource, "action", s.action, "error", err)
        return err
    }
    return s.ServerStream.RecvMsg(m)
}
```

Note: `s.ServerStream.Context()` is called inline on each `RecvMsg`, not captured at construction, so cancellation and deadline propagation work correctly for long-lived streams.

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

### Test Coverage

Both `protobuf_policy_option` and `policy_verification` gain `stream_interceptor_test.go` files covering:

- Nil endpoint panics (policy_verification)
- Misconfiguration guard (policy_option not ran)
- Static resource authorized / denied
- `field_mappings` present → `codes.Internal`
- No policy → handler called

---

## Change 3: README Restructure

### Background

`README.md` is written entirely in Japanese, lacks sufficient depth, and does not explain the project's purpose or design rationale. The existing content will be fully rewritten from scratch.

### Structure

```text
README.md       ← English (primary, full rewrite)
README.ja.md    ← Japanese translation of the new README.md
```

Both files are written fresh. The old `README.md` is replaced entirely; `README.ja.md` is created as a full Japanese translation of the new English version.

**README.md sections:**

1. **What it is** — 2–3 sentence summary: a gRPC authorization middleware that lets you declare access policy in `.proto` files and enforce it automatically via interceptors
2. **Why** — motivation: co-locating policy with API definition (DRY, reviewable), lightweight alternative to OPA/Casbin for teams already using protobuf
3. **How it works** — conceptual explanation of the two-module pipeline: `protobuf_policy_option` resolves policy from proto options into context; `policy_verification` reads context and calls the authorization server; why two modules instead of one
4. **Interceptor chain** — required order, what happens if misconfigured, code example
5. **Proto option reference** — how to annotate a method: `resource`, `action`, `field_mappings` with placeholder syntax, a complete `.proto` example
6. **Quick start (Unary)** — minimal working Go example from server setup to first authorized call
7. **Streaming RPC** — how `StreamServerInterceptor` works, the per-`RecvMsg` security model, `field_mappings` limitation, code example
8. **Testing your service** — how to use `endpointtest` to test gRPC handlers without a real authorization server; `Allow()`, `Deny()`, `Func()` examples
9. **Authorization server contract** — what the o3 REST endpoint expects (`POST /verify`, request/response shape, status code mapping to gRPC codes)
10. **License**

Per-module READMEs (`policy_verification/README.md`, `protobuf_policy_option/README.md`) also fully rewritten in English with `.ja.md` counterparts added.

---

## Commit Plan

| # | Commit | Contents |
|---|---|---|
| 1 | `feat: add endpointtest package and refactor tests` | New `endpointtest` package with `Allow`, `Deny`, `Func`, `CtxWithBearerToken`, `CtxWithRequestID`, `AssertGRPCCode` + `mock_test.go`; rewrite `interceptor_test.go` and `rest_o3_policy_verifier_test.go` |
| 2 | `feat: add StreamInterceptor to both modules` | `StreamInterceptor` in `protobuf_policy_option` and `policy_verification`; per-message auth on `RecvMsg`; `field_mappings` guard at stream establishment; `stream_interceptor_test.go` for both modules |
| 3 | `docs: rewrite README in English, add README.ja.md` | Full rewrite of `README.md` (English) with purpose, how it works, proto reference, quick start, streaming, testing, auth server contract; `README.ja.md` (Japanese translation); per-module READMEs updated |

---

## Out of Scope

- OPA or other `VerifierEndpoint` adapter implementations (separate future work)
- Dynamic resource resolution (`field_mappings`) for streaming RPCs
- Per-`SendMsg` authorization checks
- Cache layer for streaming authorization
