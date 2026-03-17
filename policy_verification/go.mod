module github.com/o3co/grpc.authz/policy_verification

go 1.25.5

replace github.com/o3co/grpc.authz/protobuf_policy_option => ../protobuf_policy_option

require (
	github.com/o3co/grpc.authz/protobuf_policy_option v0.0.0
	google.golang.org/grpc v1.79.2
)

require (
	golang.org/x/net v0.48.0 // indirect
	golang.org/x/sys v0.39.0 // indirect
	golang.org/x/text v0.32.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251202230838-ff82c1b0f217 // indirect
	google.golang.org/protobuf v1.36.10 // indirect
)
