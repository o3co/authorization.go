module github.com/o3co/grpc.authz/tests/e2e

go 1.25.5

require (
	github.com/o3co/grpc.authz/policy_verification v0.0.0
	github.com/o3co/grpc.authz/protobuf_policy_option v0.1.0
	github.com/o3co/grpc.authz/request_tracking v0.1.0
	github.com/o3co/grpc.authz/token_introspection v0.0.0
	google.golang.org/grpc v1.79.3
	google.golang.org/protobuf v1.36.10
)

require (
	golang.org/x/net v0.48.0 // indirect
	golang.org/x/sys v0.39.0 // indirect
	golang.org/x/text v0.32.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251202230838-ff82c1b0f217 // indirect
)

replace (
	github.com/o3co/grpc.authz/policy_verification => ../../policy_verification
	github.com/o3co/grpc.authz/protobuf_policy_option => ../../protobuf_policy_option
	github.com/o3co/grpc.authz/request_tracking => ../../request_tracking
	github.com/o3co/grpc.authz/token_introspection => ../../token_introspection
)
