# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

---

## [Unreleased]

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

[Unreleased]: https://github.com/o3co/grpc.authz/compare/HEAD...HEAD
