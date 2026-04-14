// Copyright 2026 1o1 Co. Ltd.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package policyoption

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"unicode/utf8"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"

	pb "github.com/o3co/grpc.authz/protobuf_policy_option/schema"
)

type ctxKey string

const (
	ctxKeyPolicy         ctxKey = "o3:policy"
	ctxKeyInterceptorRan ctxKey = "o3:interceptor_ran"
)

type Policy struct {
	Resource string
	Action   string
}

func withPolicy(ctx context.Context, resource, action string) context.Context {
	return context.WithValue(ctx, ctxKeyPolicy, &Policy{
		Resource: resource,
		Action:   action,
	})
}

// markInterceptorRan records in ctx that the Interceptor has run (internal use).
func markInterceptorRan(ctx context.Context) context.Context {
	return context.WithValue(ctx, ctxKeyInterceptorRan, true)
}

// InterceptorRanFromContext returns whether protobuf_policy_option.Interceptor has already run.
// Used by policy_verification.Interceptor to detect interceptor chain misconfiguration.
func InterceptorRanFromContext(ctx context.Context) bool {
	return ctx.Value(ctxKeyInterceptorRan) != nil
}

func PolicyFromContext(ctx context.Context) (*Policy, bool) {
	v := ctx.Value(ctxKeyPolicy)

	if v == nil {
		return nil, false
	}

	pm, ok := v.(*Policy)

	return pm, ok
}

// config holds interceptor configuration.
type config struct {
	logLevel slog.Level
}

// Option configures the interceptor.
type Option func(*config)

// WithLogLevel sets the log level. Default when unspecified is slog.LevelError.
func WithLogLevel(level slog.Level) Option {
	return func(c *config) {
		c.logLevel = level
	}
}

// rpcMethod holds the decomposed parts of a gRPC full method name.
type rpcMethod struct {
	Service string
	Method  string
}

// Interceptor resolves resources from protobuf options and the request.
func Interceptor(opts ...Option) grpc.UnaryServerInterceptor {
	cfg := &config{logLevel: slog.LevelError}
	for _, opt := range opts {
		opt(cfg)
	}
	log := newLogger(cfg.logLevel)

	var cache sync.Map // per-interceptor-instance cache (prevents contamination between tests)

	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		log.Debug("processing method", "method", info.FullMethod)

		// Retrieve permissions from protobuf.
		policy, err := getMethodPolicy(&cache, log, info.FullMethod)

		if err != nil {
			log.Error("failed to get method policy", "method", info.FullMethod, "error", err)
			return nil, status.Errorf(codes.Internal, "failed to get method policy: %v", err)
		}

		// Always mark the interceptor as ran (so policy_verification can detect chain misconfiguration).
		ctx = markInterceptorRan(ctx)

		if policy == nil {
			// No permission defined; continue to next handler.
			return handler(ctx, req)
		}

		log.Debug("policy resolved", "resource", policy.Resource, "action", policy.Action)

		// Resolve resource from the request.
		resolvedResource, err := resolveResourceFromRequest(log, policy, req)

		// If resource resolution fails, return InternalServerError.
		if err != nil {
			return nil, status.Errorf(codes.Internal, "resource resolved failed: %v", err)
		}

		ctx = withPolicy(ctx, resolvedResource.Resource, resolvedResource.Action)

		return handler(ctx, req)
	}
}

// parseFullMethodName parses a gRPC full method name into its service and method components.
func parseFullMethodName(fullMethodName string) (rpcMethod, error) {
	// fullMethodName is a gRPC full method name in the format
	// "/<package>.<Service>/<Method>".
	// Example: "/sample.v1.SampleService/SearchSamples"

	// Empty string or missing leading '/' is an invalid format.
	if len(fullMethodName) == 0 || fullMethodName[0] != '/' {
		return rpcMethod{}, fmt.Errorf("invalid full method format: %s", fullMethodName)
	}

	// Strip the leading '/' to get the actual string portion.
	// Example: "sample.v1.SampleService/SearchSamples"
	method := fullMethodName[1:]

	// Find the last '/' to split the service name from the method name.
	// Packages and service names cannot contain '/', so we use the last '/'.
	lastSlash := strings.LastIndex(method, "/")
	if lastSlash == -1 {
		return rpcMethod{}, fmt.Errorf("invalid full method format: %s", fullMethodName)
	}

	// Everything to the left of the last '/' is the service name; everything to the right is the method name.
	// serviceName: "sample.v1.SampleService"
	// methodName:  "SearchSamples"
	serviceName := method[:lastSlash]
	methodName := method[lastSlash+1:]

	// Return the parsed values as a struct.
	return rpcMethod{Service: serviceName, Method: methodName}, nil
}

// cachedPolicy is a wrapper stored in the method policy cache.
// policy == nil means "no policy" and is distinguished from a cache miss.
// Holding err allows error results to be cached, preventing repeated scans.
type cachedPolicy struct {
	policy *pb.Policy
	err    error
}

// getMethodPolicy retrieves permission information from a protobuf method.
func getMethodPolicy(cache *sync.Map, log *slog.Logger, fullMethodName string) (*pb.Policy, error) {
	// Check cache (no scan needed from second call onwards).
	if v, ok := cache.Load(fullMethodName); ok {
		c := v.(*cachedPolicy)
		return c.policy, c.err
	}

	policy, err := lookupMethodPolicy(log, fullMethodName)

	// Store in cache including errors (prevents repeated scans).
	cache.Store(fullMethodName, &cachedPolicy{policy: policy, err: err})
	return policy, err
}

// lookupMethodPolicy scans GlobalFiles to resolve the policy.
// Called by getMethodPolicy on the first lookup only.
func lookupMethodPolicy(log *slog.Logger, fullMethodName string) (*pb.Policy, error) {
	mm, err := parseFullMethodName(fullMethodName)
	if err != nil {
		return nil, err
	}
	serviceName, methodName := mm.Service, mm.Method

	var serviceDesc protoreflect.ServiceDescriptor
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		services := fd.Services()
		for i := 0; i < services.Len(); i++ {
			svc := services.Get(i)
			if string(svc.FullName()) == serviceName {
				serviceDesc = svc
				return false // found; stop scanning
			}
		}
		return true // continue to next file
	})

	if serviceDesc == nil {
		return nil, nil
	}

	methodDesc := serviceDesc.Methods().ByName(protoreflect.Name(methodName))
	if methodDesc == nil {
		return nil, nil
	}

	opts := methodDesc.Options()
	if opts == nil {
		return nil, nil
	}

	methodOptions, ok := opts.(*descriptorpb.MethodOptions)
	if !ok {
		return nil, fmt.Errorf("internal error: unexpected method options type %T", opts)
	}

	if proto.HasExtension(methodOptions, pb.E_Policy) {
		ext := proto.GetExtension(methodOptions, pb.E_Policy)
		if permission, ok := ext.(*pb.Policy); ok {
			log.Debug("policy found", "method", fullMethodName, "resource", permission.Resource, "action", permission.Action)
			return permission, nil
		}
	}

	return nil, nil
}

// resolveResourceFromRequest resolves the resource by replacing placeholders using the policy and request.
func resolveResourceFromRequest(log *slog.Logger, policy *pb.Policy, req interface{}) (*Policy, error) {
	if policy.Resource == "" {
		return nil, fmt.Errorf("policy resource must not be empty")
	}
	if policy.Action == "" {
		return nil, fmt.Errorf("policy action must not be empty")
	}

	resource := policy.Resource

	log.Debug("resolving resource template", "template", resource)

	// Template substitution via resource_fields.
	for _, field := range policy.FieldMappings {
		if field.Placeholder == "" || field.RequestField == "" {
			return nil, fmt.Errorf("invalid field mapping: placeholder and request_field must not be empty")
		}

		// Placeholder format is "<field_name>".
		placeholder := fmt.Sprintf("<%s>", field.Placeholder)

		// Check whether the resource template contains the relevant placeholder.
		if strings.Contains(resource, placeholder) {
			value, err := extractFieldFromRequest(log, req, field.RequestField)

			if err != nil {
				log.Error("failed to extract field", "field", field.RequestField, "error", err)

				return nil, fmt.Errorf("failed to extract field %s: %v", field.RequestField, err)
			}

			resource = strings.ReplaceAll(resource, placeholder, value)
		}
	}

	log.Debug("resolved resource", "resource", resource)

	return &Policy{
		Resource: resource,
		Action:   policy.Action,
	}, nil
}

// hasFieldMappings returns true if the policy has any field_mappings defined.
// Streaming RPCs do not support field_mappings.
func hasFieldMappings(policy *pb.Policy) bool {
	return len(policy.FieldMappings) > 0
}

// fieldMappingsNotSupportedError returns the standard codes.Internal error
// used when a streaming RPC method has field_mappings defined.
func fieldMappingsNotSupportedError() error {
	return status.Error(codes.Internal,
		"field_mappings are not supported for streaming RPCs; use a static resource string")
}

// StreamInterceptor resolves policy from proto method options and injects it into
// the stream context. Must be chained before policy_verification.StreamInterceptor.
//
// field_mappings are not supported for streaming RPCs. If a method's policy
// defines field_mappings, the stream is rejected with codes.Internal.
func StreamInterceptor(opts ...Option) grpc.StreamServerInterceptor {
	cfg := &config{logLevel: slog.LevelError}
	for _, opt := range opts {
		opt(cfg)
	}
	log := newLogger(cfg.logLevel)

	var cache sync.Map

	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		log.Debug("processing stream method", "method", info.FullMethod)

		policy, err := getMethodPolicy(&cache, log, info.FullMethod)
		if err != nil {
			log.Error("failed to get method policy", "method", info.FullMethod, "error", err)
			return status.Errorf(codes.Internal, "failed to get method policy: %v", err)
		}

		// Always mark interceptor as ran so policy_verification.StreamInterceptor
		// can detect misconfiguration.
		wrapped := &contextServerStream{ServerStream: ss}
		wrapped.ctx = markInterceptorRan(ss.Context())

		if policy == nil {
			return handler(srv, wrapped)
		}

		if hasFieldMappings(policy) {
			log.Error("field_mappings are not supported for streaming RPCs", "method", info.FullMethod)
			return fieldMappingsNotSupportedError()
		}

		log.Debug("policy resolved for stream", "resource", policy.Resource, "action", policy.Action)
		wrapped.ctx = withPolicy(wrapped.ctx, policy.Resource, policy.Action)

		return handler(srv, wrapped)
	}
}

// contextServerStream wraps grpc.ServerStream to override Context().
type contextServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *contextServerStream) Context() context.Context { return s.ctx }

// extractFieldFromRequest extracts a field value from the request using reflection.
func extractFieldFromRequest(log *slog.Logger, req interface{}, fieldPath string) (string, error) {
	log.Debug("extracting field from request", "field", fieldPath, "type", fmt.Sprintf("%T", req))

	// First, attempt a safe retrieval via the protobuf reflection API (reliable for generated messages).
	if pm, ok := req.(proto.Message); ok {
		m := pm.ProtoReflect()
		// Search by the proto-side field name (e.g., "id").
		fd := m.Descriptor().Fields().ByName(protoreflect.Name(fieldPath))

		if fd == nil {
			return "", fmt.Errorf("field %s not found in request", fieldPath)
		}

		val := m.Get(fd)

		// list/map fields are not supported.
		if fd.IsList() || fd.IsMap() {
			log.Error("unsupported field type: list/map not supported", "field", fieldPath)
			return "", fmt.Errorf("field %s is list/map, unsupported", fieldPath)
		}

		switch fd.Kind() {
		case protoreflect.StringKind:
			return val.String(), nil
		case protoreflect.BytesKind:
			b := val.Bytes()
			// If valid UTF-8, return as a string directly.
			if utf8.Valid(b) {
				return string(b), nil
			}
			// For binary data, return hex-encoded.
			return hex.EncodeToString(b), nil
		case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind,
			protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
			return fmt.Sprintf("%d", val.Int()), nil
		case protoreflect.Uint32Kind, protoreflect.Uint64Kind,
			protoreflect.Fixed32Kind, protoreflect.Fixed64Kind:
			return fmt.Sprintf("%d", val.Uint()), nil
		case protoreflect.BoolKind:
			return fmt.Sprintf("%v", val.Bool()), nil
		default:
			log.Error("unsupported proto field kind", "kind", fd.Kind(), "field", fieldPath)
			return "", fmt.Errorf("unsupported proto field kind %s for %s", fd.Kind(), fieldPath)
		}
	}

	return "", fmt.Errorf("request does not implement proto.Message (got %T)", req)
}
