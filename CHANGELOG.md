# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

---

## [Unreleased]

---

## [0.2.1] - 2026-03-30

### Added

- `NewStaticEndpoint` — local rule evaluation without external service, supports exact match, wildcard (`*`), and prefix wildcards (`posts/*`)
- Example functions for pkg.go.dev documentation (`ExampleNewRESTEndpoint`, `ExampleNewOPAEndpoint`, `ExampleNewCedarAgentEndpoint`, `ExampleNewStaticEndpoint`, `ExampleAllow`, `ExampleDeny`, `ExampleFunc`)
- README.ja.md updated with v0.2.0 content (OPA, Cedar, Static backends)
- Language toggle links between README.md and README.ja.md

### Fixed

- Add Apache 2.0 license headers to integration and example test files
- Document `crypto/rand.Read` error suppression safety (Go 1.20+ guarantee)
- Fix docker-compose healthchecks: use host-side curl instead of in-container tools (OPA scratch image has no shell)
- Make OPA platform configurable via `OPA_PLATFORM` env var (default `linux/amd64`)
- Add nil check to `WithCedarAgentPrincipalResolver` (panics on nil, matching other option constructors)
- Add missing `time` and `context` imports in README code examples

---

## [0.2.0] - 2026-03-30

### Added

- `NewOPAEndpoint` — Open Policy Agent REST adapter (`POST /v1/data/{path}`)
- `NewCedarAgentEndpoint` — permitio/cedar-agent REST adapter (`POST /v1/is_authorized`) with configurable entity prefixes and principal resolver
- `NewStaticEndpoint` — local rule evaluation without external service, supports exact match, wildcard (`*`), and prefix wildcards (`posts/*`)
- Docker-based integration tests for OPA and Cedar agent (`//go:build integration`)
- Example functions for pkg.go.dev documentation
- README "Alternative backends" section with usage examples for all built-in adapters

---

## [0.1.0] - 2026-03-19

### Added

- `endpointtest` package: test utilities for services using `policy_verification` (`Allow`, `Deny`, `Func`, `CtxWithBearerToken`, `CtxWithRequestID`, `AssertGRPCCode`)
- `StreamInterceptor` for both `protobuf_policy_option` and `policy_verification` modules — per-`RecvMsg` authorization with no caching
- `field_mappings` are rejected at stream establishment with `codes.Internal` (streaming RPCs require static resource strings)
- English README with full documentation: purpose, how it works, interceptor chain, proto option reference, quick start, streaming, testing, authorization server contract
- Japanese README (`README.ja.md`) and per-module Japanese READMEs
- GitHub Actions CI for both modules
- `CONTRIBUTING.md`, `SECURITY.md`, `CODE_OF_CONDUCT.md`

### Changed

- `x-request-id` in streaming RPCs now uses a stable ID computed once at stream establishment (not regenerated per `RecvMsg`)

---

[Unreleased]: https://github.com/o3co/grpc.authz/compare/policy_verification/v0.2.1...HEAD
[0.2.1]: https://github.com/o3co/grpc.authz/compare/policy_verification/v0.2.0...policy_verification/v0.2.1
[0.2.0]: https://github.com/o3co/grpc.authz/compare/policy_verification/v0.1.0...policy_verification/v0.2.0
[0.1.0]: https://github.com/o3co/grpc.authz/releases/tag/policy_verification/v0.1.0
