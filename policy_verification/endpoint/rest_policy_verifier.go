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

package endpoint

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const defaultMaxResponseBodySize int64 = 1024 * 1024 // 1MB
const defaultTimeout = 10 * time.Second

// buildConfig is a temporary configuration used only during NewRESTEndpoint construction.
// Managing fields like timeout here that are unnecessary after construction
// keeps restPolicyVerifierEndpoint's struct limited to only the fields needed at runtime.
type buildConfig struct {
	timeout              time.Duration
	maxResponseBodySize  int64
	logger               *slog.Logger
	requestIDHeaderKey   string
	requestIDFunc        func(context.Context) string
}

// Option configures the REST endpoint.
type Option func(*buildConfig)

// WithTimeout sets the HTTP client timeout. Default when unspecified is 10s.
func WithTimeout(d time.Duration) Option {
	if d <= 0 {
		panic(fmt.Sprintf("timeout must be positive, got %v", d))
	}
	return func(c *buildConfig) {
		c.timeout = d
	}
}

// WithMaxResponseBodySize sets the maximum number of bytes to read from the response body.
func WithMaxResponseBodySize(size int64) Option {
	if size <= 0 {
		panic(fmt.Sprintf("maxResponseBodySize must be positive, got %d", size))
	}
	return func(c *buildConfig) {
		c.maxResponseBodySize = size
	}
}

// WithLogLevel sets the log level. Default when unspecified is slog.LevelError.
func WithLogLevel(level slog.Level) Option {
	return func(c *buildConfig) {
		c.logger = newLogger(level)
	}
}

// WithRequestIDHeaderKey sets the HTTP header key for forwarding the request ID
// to the authorization server. Default is "x-request-id". Set to empty string to
// disable forwarding. Only used when WithRequestIDFunc is also set.
func WithRequestIDHeaderKey(key string) Option {
	return func(c *buildConfig) {
		c.requestIDHeaderKey = key
	}
}

// WithRequestIDFunc sets a function to extract the request ID from the context
// for forwarding to the authorization server as a header.
// If not set, no request ID is extracted and header forwarding is skipped.
func WithRequestIDFunc(fn func(context.Context) string) Option {
	return func(c *buildConfig) {
		c.requestIDFunc = fn
	}
}

// restPolicyVerifierEndpoint is a VerifierEndpoint implementation for the REST authorization service.
type restPolicyVerifierEndpoint struct {
	httpClient           *http.Client
	verifyURL            string
	maxResponseBodySize  int64
	logger               *slog.Logger
	requestIDHeaderKey   string
	requestIDFunc        func(context.Context) string
}

// NewRESTEndpoint is the constructor for the REST authorization endpoint.
// Returns an error if baseURL is invalid.
func NewRESTEndpoint(baseURL string, opts ...Option) (VerifierEndpoint, error) {
	rawBase := strings.TrimSpace(baseURL)
	if rawBase == "" {
		return nil, fmt.Errorf("baseURL must not be empty")
	}

	if !strings.HasPrefix(rawBase, "http://") && !strings.HasPrefix(rawBase, "https://") {
		rawBase = "http://" + rawBase
	}

	base, err := url.Parse(rawBase)
	if err != nil {
		return nil, fmt.Errorf("invalid authorization base url: %w", err)
	}

	base.Path = strings.TrimSuffix(base.Path, "/") + "/verify"

	cfg := &buildConfig{
		timeout:             defaultTimeout,
		maxResponseBodySize: defaultMaxResponseBodySize,
		logger:              newLogger(slog.LevelError),
		requestIDHeaderKey:  "x-request-id",
	}
	for _, opt := range opts {
		opt(cfg)
	}

	return &restPolicyVerifierEndpoint{
		httpClient:          &http.Client{Timeout: cfg.timeout},
		verifyURL:           base.String(),
		maxResponseBodySize: cfg.maxResponseBodySize,
		logger:              cfg.logger,
		requestIDHeaderKey:  cfg.requestIDHeaderKey,
		requestIDFunc:       cfg.requestIDFunc,
	}, nil
}

type token struct {
	TokenType string
	Value     string
}

// getToken retrieves the Authorization token from gRPC incoming metadata.
func getToken(ctx context.Context) (*token, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, fmt.Errorf("no metadata found in context")
	}

	values := md["authorization"]
	if len(values) == 0 {
		return nil, fmt.Errorf("no authorization header found")
	}

	parts := strings.Fields(values[0])
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid authorization header format")
	}

	return &token{TokenType: parts[0], Value: parts[1]}, nil
}

// Verify executes the authorization check.
func (e *restPolicyVerifierEndpoint) Verify(ctx context.Context, resource, action string) error {
	// --- Retrieve authorization token -------------------------------------------------
	tok, err := getToken(ctx)
	if err != nil {
		return status.Errorf(codes.Unauthenticated, "failed to get authorization token: %v", err)
	}

	// --- Build request body -----------------------------------------------
	reqBody := map[string]string{"resource": resource, "action": action}
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return status.Errorf(codes.Internal, "failed to marshal request body: %v", err)
	}

	// --- Create HTTP request -----------------------------------------------
	// Binding the caller's Context inherits cancellation and timeout from the caller.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.verifyURL, bytes.NewReader(jsonData))
	if err != nil {
		return status.Errorf(codes.Internal, "failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", tok.TokenType+" "+tok.Value)

	var requestID string
	if e.requestIDHeaderKey != "" && e.requestIDFunc != nil {
		if id := e.requestIDFunc(ctx); id != "" {
			requestID = id
			req.Header.Set(e.requestIDHeaderKey, id)
		}
	}

	// --- Send request ---------------------------------------------------
	resp, err := e.httpClient.Do(req)
	if err != nil {
		return status.Errorf(codes.Internal, "request failed: %v", err)
	}
	defer resp.Body.Close()

	// Read response body up to maxResponseBodySize bytes (memory protection).
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, e.maxResponseBodySize))
	if err != nil {
		e.logger.Error("failed to read response body", "error", err, "x-request-id", requestID)
		respBody = nil
	}

	e.logger.Debug("response received", "status", resp.StatusCode, "x-request-id", requestID)

	// --- Evaluate based on status code -------------------------------------
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	const maxLoggedBodySize = 1024
	logBody := respBody
	if len(logBody) > maxLoggedBodySize {
		logBody = logBody[:maxLoggedBodySize]
	}
	e.logger.Error("error response from authorization server", "status", resp.StatusCode, "body", string(logBody), "x-request-id", requestID)

	if resp.StatusCode == http.StatusForbidden {
		return status.Error(codes.PermissionDenied, "access denied")
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return status.Error(codes.Unauthenticated, "invalid or expired token")
	}

	return status.Errorf(codes.Internal, "authorization service error: %d", resp.StatusCode)
}
