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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	defaultTimeout             = 10 * time.Second
	defaultMaxResponseBodySize = 1024 * 1024 // 1MB
)

type rfc7662Config struct {
	timeout             time.Duration
	maxResponseBodySize int64
	logger              *slog.Logger
	requestIDFunc       func(context.Context) string
	requestIDHeaderKey  string
}

// RFC7662Option configures the RFC 7662 introspection backend.
type RFC7662Option func(*rfc7662Config)

// WithTimeout sets the HTTP client timeout. Default is 10s.
func WithTimeout(d time.Duration) RFC7662Option {
	if d <= 0 {
		panic(fmt.Sprintf("timeout must be positive, got %v", d))
	}
	return func(c *rfc7662Config) {
		c.timeout = d
	}
}

// WithRequestIDFunc sets a function to extract a request ID from the context
// for forwarding to the introspection endpoint as a header.
func WithRequestIDFunc(fn func(context.Context) string) RFC7662Option {
	return func(c *rfc7662Config) {
		c.requestIDFunc = fn
	}
}

// WithRequestIDHeaderKey sets the HTTP header key for the request ID.
// Default is "x-request-id". Only used when WithRequestIDFunc is also set.
func WithRequestIDHeaderKey(key string) RFC7662Option {
	return func(c *rfc7662Config) {
		c.requestIDHeaderKey = key
	}
}

// WithMaxResponseBodySize sets the max bytes to read from the response.
func WithMaxResponseBodySize(size int64) RFC7662Option {
	if size <= 0 {
		panic(fmt.Sprintf("maxResponseBodySize must be positive, got %d", size))
	}
	return func(c *rfc7662Config) {
		c.maxResponseBodySize = size
	}
}

// WithRFC7662LogLevel sets the log level for the RFC 7662 backend.
func WithRFC7662LogLevel(level slog.Level) RFC7662Option {
	return func(c *rfc7662Config) {
		c.logger = newLogger(level)
	}
}

type rfc7662Introspector struct {
	httpClient          *http.Client
	introspectURL       string
	maxResponseBodySize int64
	logger              *slog.Logger
	requestIDFunc       func(context.Context) string
	requestIDHeaderKey  string
}

// NewRFC7662Introspector creates an Introspector that calls an RFC 7662 compliant
// token introspection endpoint. Scheme() returns "bearer".
func NewRFC7662Introspector(introspectURL string, opts ...RFC7662Option) (Introspector, error) {
	raw := strings.TrimSpace(introspectURL)
	if raw == "" {
		return nil, fmt.Errorf("introspectURL must not be empty")
	}

	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		raw = "http://" + raw
	}

	cfg := &rfc7662Config{
		timeout:             defaultTimeout,
		maxResponseBodySize: defaultMaxResponseBodySize,
		logger:              newLogger(slog.LevelError),
		requestIDHeaderKey:  "x-request-id",
	}
	for _, opt := range opts {
		opt(cfg)
	}

	return &rfc7662Introspector{
		httpClient:          &http.Client{Timeout: cfg.timeout},
		introspectURL:       raw,
		maxResponseBodySize: cfg.maxResponseBodySize,
		logger:              cfg.logger,
		requestIDFunc:       cfg.requestIDFunc,
		requestIDHeaderKey:  cfg.requestIDHeaderKey,
	}, nil
}

func (i *rfc7662Introspector) Scheme() string { return "bearer" }

func (i *rfc7662Introspector) Introspect(ctx context.Context, credential string) (*IntrospectionResult, error) {
	// Build request body
	reqBody, err := json.Marshal(map[string]string{"token": credential})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to marshal request body: %v", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, i.introspectURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+credential)

	// Forward request ID if configured
	if i.requestIDFunc != nil {
		if id := i.requestIDFunc(ctx); id != "" && i.requestIDHeaderKey != "" {
			req.Header.Set(i.requestIDHeaderKey, id)
		}
	}

	// Send request
	resp, err := i.httpClient.Do(req)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "introspection request failed: %v", err)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, i.maxResponseBodySize))
	if err != nil {
		i.logger.Error("failed to read response body", "error", err)
		respBody = nil
	}

	// Handle non-2xx
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusUnauthorized {
			return nil, status.Error(codes.Unauthenticated, "token rejected by introspection endpoint")
		}
		i.logger.Error("introspection endpoint error", "status", resp.StatusCode)
		return nil, status.Errorf(codes.Internal, "introspection endpoint returned %d", resp.StatusCode)
	}

	// Parse response
	var raw map[string]any
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to parse introspection response: %v", err)
	}

	// Check active
	active, _ := raw["active"].(bool)
	if !active {
		return nil, status.Error(codes.Unauthenticated, "token is not active")
	}

	// Map claims to IntrospectionResult
	result := &IntrospectionResult{
		Claims: make(map[string]any),
	}

	// Subject
	if sub, ok := raw["sub"].(string); ok {
		result.Subject = sub
	}

	// Scopes
	if scopesRaw, ok := raw["scopes"]; ok {
		if arr, ok := scopesRaw.([]any); ok {
			for _, s := range arr {
				if str, ok := s.(string); ok {
					result.Scopes = append(result.Scopes, str)
				}
			}
		}
	}

	// ExpiresAt
	if exp, ok := raw["exp"].(float64); ok {
		result.ExpiresAt = time.Unix(int64(exp), 0).UTC()
	}

	// Remaining claims (exclude promoted fields)
	promoted := map[string]bool{"active": true, "sub": true, "scopes": true, "exp": true}
	for k, v := range raw {
		if !promoted[k] {
			result.Claims[k] = v
		}
	}

	return result, nil
}
