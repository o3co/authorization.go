# Contributing to grpc.authz

Thank you for your interest in contributing!

## How to Contribute

### Reporting Issues

Use [GitHub Issues](https://github.com/o3co/grpc.authz/issues) to report bugs or request features. Please search existing issues before opening a new one.

### Submitting Pull Requests

1. Fork the repository and create a branch from `develop`.
2. Make your changes with tests.
3. Ensure all tests pass (see below).
4. Open a pull request against `develop`.

## Development Setup

This repository contains two independent Go modules. Work in each module directory separately.

```bash
# protobuf_policy_option module
cd protobuf_policy_option
go test ./...

# policy_verification module
cd policy_verification
go test ./...
```

### Regenerating Protobuf Files

If you modify `protobuf_policy_option/schema/policy.proto`, regenerate the Go bindings:

```bash
cd protobuf_policy_option
make generate
```

Required tools: [protoc 34.0](https://github.com/protocolbuffers/protobuf/releases), [protoc-gen-go v1.36.10](https://pkg.go.dev/google.golang.org/protobuf/cmd/protoc-gen-go), [protoc-gen-go-grpc v1.6.0](https://pkg.go.dev/google.golang.org/grpc/cmd/protoc-gen-go-grpc).

## Code Style

- Follow standard Go conventions (`gofmt`, `go vet`).
- Keep public APIs minimal and well-documented.
- Write tests for all new behavior.

## License

By contributing, you agree that your contributions will be licensed under the [Apache License 2.0](LICENSE).
