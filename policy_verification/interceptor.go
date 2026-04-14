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

package policyverification

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"time"

	"github.com/o3co/grpc.authz/policy_verification/endpoint"
	rt "github.com/o3co/grpc.authz/request_tracking"
	policy "github.com/o3co/grpc.authz/protobuf_policy_option"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// generateRequestID generates an x-request-id.
// Format: YYYYMMDDHHmmss_<uuid-v4-no-dashes>
func generateRequestID() string {
	now := time.Now().UTC()
	timestamp := now.Format("20060102150405")

	var b [16]byte
	// crypto/rand.Read always returns len(b) and nil error on supported platforms (Go 1.20+).
	// It panics only if the OS random source is unavailable, which is unrecoverable.
	_, _ = rand.Read(b[:])
	// UUID v4: version bits
	b[6] = (b[6] & 0x0f) | 0x40
	// UUID v4: variant bits
	b[8] = (b[8] & 0x3f) | 0x80

	return fmt.Sprintf("%s_%x", timestamp, b)
}

// extractOrGenerateRequestID retrieves x-request-id from incoming metadata,
// or generates a new one if not present.
func extractOrGenerateRequestID(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if ok {
		if values := md["x-request-id"]; len(values) > 0 && values[0] != "" {
			return values[0]
		}
	}
	return generateRequestID()
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

// requestIDStream wraps grpc.ServerStream to propagate the enriched context
// (with x-request-id) to the handler even when no policy is defined.
type requestIDStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *requestIDStream) Context() context.Context { return s.ctx }

// authServerStream wraps grpc.ServerStream to call Verify before each RecvMsg.
// ctx holds the enriched context (with a stable x-request-id) computed once at
// stream establishment, so all RecvMsg calls share the same request ID.
type authServerStream struct {
	grpc.ServerStream
	ctx      context.Context
	resource string
	action   string
	verifier endpoint.VerifierEndpoint
	log      *slog.Logger
}

// Context returns the enriched context with the stable x-request-id. Because
// ctx is derived from the underlying stream context, cancellation and deadline
// propagation work correctly for long-lived streams.
func (s *authServerStream) Context() context.Context { return s.ctx }

// RecvMsg checks authorization before delegating to the underlying RecvMsg.
func (s *authServerStream) RecvMsg(m interface{}) error {
	if err := s.verifier.Verify(s.ctx, s.resource, s.action); err != nil {
		s.log.Error("authorization check failed on RecvMsg",
			"resource", s.resource, "action", s.action, "error", err)
		return err
	}
	return s.ServerStream.RecvMsg(m)
}

// StreamInterceptor returns a gRPC StreamServerInterceptor that checks authorization
// on every RecvMsg call. It must be chained after policyoption.StreamInterceptor.
func StreamInterceptor(verifierEndpoint endpoint.VerifierEndpoint, opts ...Option) grpc.StreamServerInterceptor {
	if verifierEndpoint == nil {
		panic("verifierEndpoint must not be nil")
	}

	cfg := &config{logLevel: slog.LevelError}
	for _, opt := range opts {
		opt(cfg)
	}
	log := newLogger(cfg.logLevel)

	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx := ss.Context()
		requestID := rt.RequestIDFromContext(ctx)
		if requestID == "" {
			requestID = extractOrGenerateRequestID(ctx)
			ctx = rt.WithRequestID(ctx, requestID)
		}

		log.Debug("processing stream method", "method", info.FullMethod, "x-request-id", requestID)

		if !policy.InterceptorRanFromContext(ctx) {
			log.Error("interceptor chain misconfiguration: protobuf_policy_option.StreamInterceptor is not registered")
			return status.Error(codes.Internal,
				"protobuf_policy_option.Interceptor is not registered in the interceptor chain")
		}

		policyData, ok := policy.PolicyFromContext(ctx)
		if !ok {
			// No policy defined for this method — pass through, but still propagate
			// the enriched context (with x-request-id) via requestIDStream.
			return handler(srv, &requestIDStream{ServerStream: ss, ctx: ctx})
		}

		log.Debug("verifying stream authorization", "resource", policyData.Resource, "action", policyData.Action)

		wrapped := &authServerStream{
			ServerStream: ss,
			ctx:          ctx, // enriched context with stable x-request-id from stream establishment
			resource:     policyData.Resource,
			action:       policyData.Action,
			verifier:     verifierEndpoint,
			log:          log,
		}
		return handler(srv, wrapped)
	}
}

// Interceptor returns a gRPC UnaryServerInterceptor that performs authorization checks.
func Interceptor(verifierEndpoint endpoint.VerifierEndpoint, opts ...Option) grpc.UnaryServerInterceptor {
	if verifierEndpoint == nil {
		panic("verifierEndpoint must not be nil")
	}

	cfg := &config{logLevel: slog.LevelError}
	for _, opt := range opts {
		opt(cfg)
	}
	log := newLogger(cfg.logLevel)

	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		requestID := rt.RequestIDFromContext(ctx)
		if requestID == "" {
			requestID = extractOrGenerateRequestID(ctx)
			ctx = rt.WithRequestID(ctx, requestID)
		}

		log.Debug("processing method", "method", info.FullMethod, "x-request-id", requestID)

		// Check whether protobuf_policy_option.Interceptor has run.
		// If not registered, treat it as an interceptor chain misconfiguration and return an Internal error.
		if !policy.InterceptorRanFromContext(ctx) {
			log.Error("interceptor chain misconfiguration: protobuf_policy_option.Interceptor is not registered")
			return nil, status.Error(codes.Internal,
				"protobuf_policy_option.Interceptor is not registered in the interceptor chain")
		}

		// Get the resolved policy metadata from context.
		policyData, ok := policy.PolicyFromContext(ctx)
		if !ok {
			// Interceptor has already run, but no policy is defined for this method (no authorization required).
			return handler(ctx, req)
		}

		resource := policyData.Resource
		action := policyData.Action

		log.Debug("verifying authorization", "resource", resource, "action", action)

		// Execute authorization check (the endpoint is designed to return a status error).
		if err := verifierEndpoint.Verify(ctx, resource, action); err != nil {
			log.Error("authorization check failed", "resource", resource, "action", action, "error", err)
			return nil, err
		}

		log.Debug("authorization check passed", "resource", resource, "action", action)

		return handler(ctx, req)
	}
}
