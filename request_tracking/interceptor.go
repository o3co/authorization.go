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

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// config holds interceptor configuration.
type config struct {
	logLevel slog.Level
}

// Option configures the request tracking interceptor.
type Option func(*config)

// WithLogLevel sets the log level. Default is slog.LevelError.
func WithLogLevel(level slog.Level) Option {
	return func(c *config) {
		c.logLevel = level
	}
}

// extractOrGenerateRequestID extracts x-request-id from incoming gRPC metadata.
// If not found or empty, generates a new one.
func extractOrGenerateRequestID(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if ok {
		if values := md["x-request-id"]; len(values) > 0 && values[0] != "" {
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

// applyOptions applies functional options and returns the configured logger.
func applyOptions(opts []Option) *slog.Logger {
	cfg := &config{logLevel: slog.LevelError}
	for _, opt := range opts {
		opt(cfg)
	}
	return newLogger(cfg.logLevel)
}

// Interceptor returns a gRPC UnaryServerInterceptor that extracts or generates
// x-request-id from incoming metadata and stores it in the context via WithRequestID.
func Interceptor(opts ...Option) grpc.UnaryServerInterceptor {
	log := applyOptions(opts)

	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		requestID := extractOrGenerateRequestID(ctx)
		ctx = WithRequestID(ctx, requestID)

		log.Debug("request tracking", "method", info.FullMethod, "x-request-id", requestID)

		return handler(ctx, req)
	}
}

// StreamInterceptor returns a gRPC StreamServerInterceptor that extracts or generates
// x-request-id from incoming metadata at stream establishment and propagates it
// via a wrapped ServerStream.
func StreamInterceptor(opts ...Option) grpc.StreamServerInterceptor {
	log := applyOptions(opts)

	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx := ss.Context()
		requestID := extractOrGenerateRequestID(ctx)
		ctx = WithRequestID(ctx, requestID)

		log.Debug("request tracking (stream)", "method", info.FullMethod, "x-request-id", requestID)

		return handler(srv, &requestIDStream{ServerStream: ss, ctx: ctx})
	}
}
