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

package tokenintrospection

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type config struct {
	logLevel    slog.Level
	metadataKey string
	// scheme (lowercase) → ordered list of introspectors
	schemes map[string][]Introspector
	cache   Cache
}

// Option configures the token introspection interceptor.
type Option func(*config)

// WithLogLevel sets the log level. Default is slog.LevelError.
func WithLogLevel(level slog.Level) Option {
	return func(c *config) {
		c.logLevel = level
	}
}

// WithMetadataKey sets the gRPC metadata key to extract the credential from.
// Default is "authorization".
func WithMetadataKey(key string) Option {
	return func(c *config) {
		c.metadataKey = strings.ToLower(key)
	}
}

// WithIntrospector registers an Introspector. The scheme is taken from
// Introspector.Scheme(). Calling this multiple times for the same scheme
// creates a strategy chain evaluated in registration order.
func WithIntrospector(introspector Introspector) Option {
	return func(c *config) {
		scheme := strings.ToLower(introspector.Scheme())
		c.schemes[scheme] = append(c.schemes[scheme], introspector)
	}
}

// WithCache sets the interceptor-level cache. Cache key is SHA-256(scheme + ":" + credential).
// Shared across all schemes and introspectors.
func WithCache(cache Cache) Option {
	return func(c *config) {
		c.cache = cache
	}
}

func buildConfig(opts []Option) (*config, *slog.Logger) {
	cfg := &config{
		logLevel:    slog.LevelError,
		metadataKey: "authorization",
		schemes:     make(map[string][]Introspector),
	}
	for _, opt := range opts {
		opt(cfg)
	}
	return cfg, newLogger(cfg.logLevel)
}

// cacheKey computes the cache key for a scheme + credential pair.
func cacheKey(scheme, credential string) string {
	h := sha256.Sum256([]byte(scheme + ":" + credential))
	return fmt.Sprintf("%x", h)
}

// parseAuthorization splits "Scheme credential" into (scheme, credential).
// Returns empty strings if the format is invalid.
func parseAuthorization(value string) (scheme, credential string) {
	parts := strings.SplitN(value, " ", 2)
	if len(parts) != 2 || parts[1] == "" {
		return "", ""
	}
	return strings.ToLower(parts[0]), parts[1]
}

// introspect runs the scheme dispatch, cache check, and strategy chain evaluation.
func introspect(ctx context.Context, cfg *config, log *slog.Logger) (context.Context, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ctx, nil // no metadata = pass through
	}

	values := md[cfg.metadataKey]
	if len(values) == 0 || values[0] == "" {
		return ctx, nil // no authorization = pass through
	}

	scheme, credential := parseAuthorization(values[0])
	if scheme == "" || credential == "" {
		return ctx, status.Error(codes.Unauthenticated, "malformed authorization header")
	}

	// Cache check
	if cfg.cache != nil {
		key := cacheKey(scheme, credential)
		if result, hit := cfg.cache.Get(key); hit {
			log.Debug("cache hit", "scheme", scheme)
			return WithResult(ctx, result), nil
		}
	}

	// Find introspectors for this scheme
	introspectors, ok := cfg.schemes[scheme]
	if !ok || len(introspectors) == 0 {
		return ctx, status.Errorf(codes.Unauthenticated, "unsupported authentication scheme: %s", scheme)
	}

	// Strategy chain evaluation
	var lastErr error
	for _, i := range introspectors {
		result, err := i.Introspect(ctx, credential)
		if err == nil {
			// Success — cache and return
			if cfg.cache != nil {
				cfg.cache.Set(cacheKey(scheme, credential), result)
			}
			log.Debug("introspection succeeded", "scheme", scheme, "subject", result.Subject)
			return WithResult(ctx, result), nil
		}

		lastErr = err
		st, ok := status.FromError(err)
		if !ok || st.Code() != codes.Unauthenticated {
			// Non-Unauthenticated error (e.g., Internal) → abort chain
			log.Error("introspection failed (aborting chain)", "scheme", scheme, "error", err)
			return ctx, err
		}
		// Unauthenticated → try next strategy
		log.Debug("strategy unauthenticated, trying next", "scheme", scheme)
	}

	// All strategies returned Unauthenticated
	return ctx, lastErr
}

// wrappedStream wraps grpc.ServerStream with an enriched context.
type wrappedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *wrappedStream) Context() context.Context { return s.ctx }

// Interceptor returns a gRPC UnaryServerInterceptor.
func Interceptor(opts ...Option) grpc.UnaryServerInterceptor {
	cfg, log := buildConfig(opts)

	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		ctx, err := introspect(ctx, cfg, log)
		if err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

// StreamInterceptor returns a gRPC StreamServerInterceptor.
func StreamInterceptor(opts ...Option) grpc.StreamServerInterceptor {
	cfg, log := buildConfig(opts)

	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx, err := introspect(ss.Context(), cfg, log)
		if err != nil {
			return err
		}
		return handler(srv, &wrappedStream{ServerStream: ss, ctx: ctx})
	}
}
