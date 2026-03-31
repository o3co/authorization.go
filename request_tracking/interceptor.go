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

package requesttracking

import (
	"context"
	"log/slog"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// config holds interceptor configuration.
type config struct {
	logLevel    slog.Level
	metadataKey string
}

// Option configures the request tracking interceptor.
type Option func(*config)

// WithLogLevel sets the log level. Default is slog.LevelError.
func WithLogLevel(level slog.Level) Option {
	return func(c *config) {
		c.logLevel = level
	}
}

// WithMetadataKey sets the gRPC metadata key to extract the request ID from.
// Default is "x-request-id". The key is normalized to lowercase because gRPC
// metadata keys are case-insensitive and stored in lowercase.
func WithMetadataKey(key string) Option {
	return func(c *config) {
		c.metadataKey = strings.ToLower(key)
	}
}

// extractOrGenerateRequestID extracts the request ID from the specified gRPC metadata key.
// If not found or empty, generates a new one.
func extractOrGenerateRequestID(ctx context.Context, metadataKey string) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if ok {
		if values := md[metadataKey]; len(values) > 0 && values[0] != "" {
			return values[0]
		}
	}
	return generateRequestID()
}

// requestIDStream wraps grpc.ServerStream to propagate the enriched context
// (with x-request-id) to the handler.
type requestIDStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *requestIDStream) Context() context.Context { return s.ctx }

// applyOptions applies functional options and returns the configured logger and metadata key.
func applyOptions(opts []Option) (*slog.Logger, string) {
	cfg := &config{logLevel: slog.LevelError, metadataKey: "x-request-id"}
	for _, opt := range opts {
		opt(cfg)
	}
	return newLogger(cfg.logLevel), cfg.metadataKey
}

// Interceptor returns a gRPC UnaryServerInterceptor that extracts or generates
// a request ID from incoming metadata and stores it in the context via WithRequestID.
func Interceptor(opts ...Option) grpc.UnaryServerInterceptor {
	log, metadataKey := applyOptions(opts)

	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		requestID := RequestIDFromContext(ctx)
		if requestID == "" {
			requestID = extractOrGenerateRequestID(ctx, metadataKey)
			ctx = WithRequestID(ctx, requestID)
		}

		log.Debug("request tracking", "method", info.FullMethod, "request_id", requestID)

		return handler(ctx, req)
	}
}

// StreamInterceptor returns a gRPC StreamServerInterceptor that extracts or generates
// a request ID from incoming metadata at stream establishment and propagates it
// via a wrapped ServerStream.
func StreamInterceptor(opts ...Option) grpc.StreamServerInterceptor {
	log, metadataKey := applyOptions(opts)

	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx := ss.Context()
		requestID := RequestIDFromContext(ctx)
		if requestID == "" {
			requestID = extractOrGenerateRequestID(ctx, metadataKey)
			ctx = WithRequestID(ctx, requestID)
		}

		log.Debug("request tracking (stream)", "method", info.FullMethod, "request_id", requestID)

		return handler(srv, &requestIDStream{ServerStream: ss, ctx: ctx})
	}
}
