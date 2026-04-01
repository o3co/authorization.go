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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	defaultTimeout             = 10 * time.Second
	defaultMaxResponseBodySize = 1024 * 1024 // 1MB
)

// authFunc returns an HTTP header key/value pair for endpoint authentication.
// The credential parameter is the token being inspected (used by WithSelfIntrospect).
// If authFunc is nil, no Authorization header is sent.
type authFunc func(credential string) (key, value string)

type rfc7662Config struct {
	timeout             time.Duration
	maxResponseBodySize int64
	logger              *slog.Logger
	requestIDFunc       func(context.Context) string
	requestIDHeaderKey  string
	authFunc            authFunc
	useJSONBody         bool
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

// WithClientCredentials sets Basic authentication for the introspection endpoint.
// This is the recommended mode for production (RFC 7662 §2.1).
func WithClientCredentials(clientID, clientSecret string) RFC7662Option {
	if clientID == "" {
		panic("clientID must not be empty")
	}
	if clientSecret == "" {
		panic("clientSecret must not be empty")
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(clientID + ":" + clientSecret))
	return func(c *rfc7662Config) {
		c.authFunc = func(_ string) (string, string) {
			return "Authorization", "Basic " + encoded
		}
	}
}

// WithBearerAuth sets a fixed service-level Bearer token for the introspection endpoint.
func WithBearerAuth(token string) RFC7662Option {
	if token == "" {
		panic("token must not be empty")
	}
	return func(c *rfc7662Config) {
		c.authFunc = func(_ string) (string, string) {
			return "Authorization", "Bearer " + token
		}
	}
}

// WithSelfIntrospect forwards the inspected token as Bearer authentication.
// This is the legacy behavior — the token being inspected is reused as the
// endpoint credential. Suitable for internal networks where the introspection
// endpoint trusts the caller implicitly.
func WithSelfIntrospect() RFC7662Option {
	return func(c *rfc7662Config) {
		c.authFunc = func(credential string) (string, string) {
			return "Authorization", "Bearer " + credential
		}
	}
}

// WithJSONBody switches the request body format to application/json.
// By default, the introspector uses application/x-www-form-urlencoded per RFC 7662 §2.1.
func WithJSONBody() RFC7662Option {
	return func(c *rfc7662Config) {
		c.useJSONBody = true
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
	authFunc            authFunc
	useJSONBody         bool
}

// NewRFC7662Introspector creates an Introspector that calls an RFC 7662 compliant
// token introspection endpoint. Scheme() returns "bearer".
func NewRFC7662Introspector(introspectURL string, opts ...RFC7662Option) (Introspector, error) {
	raw := strings.TrimSpace(introspectURL)
	if raw == "" {
		return nil, fmt.Errorf("introspectURL must not be empty")
	}

	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		raw = "https://" + raw
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
		authFunc:            cfg.authFunc,
		useJSONBody:         cfg.useJSONBody,
	}, nil
}

func (i *rfc7662Introspector) Scheme() string { return "bearer" }

func (i *rfc7662Introspector) Introspect(ctx context.Context, credential string) (*IntrospectionResult, error) {
	// Build request body
	var reqBody []byte
	var contentType string
	if i.useJSONBody {
		var err error
		reqBody, err = json.Marshal(map[string]string{"token": credential})
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to marshal request body: %v", err)
		}
		contentType = "application/json"
	} else {
		reqBody = []byte(url.Values{"token": {credential}}.Encode())
		contentType = "application/x-www-form-urlencoded"
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, i.introspectURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")

	// Set endpoint authentication
	if i.authFunc != nil {
		key, value := i.authFunc(credential)
		req.Header.Set(key, value)
	}

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

	// Read response body with truncation detection
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, i.maxResponseBodySize+1))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to read introspection response: %v", err)
	}
	if int64(len(respBody)) > i.maxResponseBodySize {
		return nil, status.Error(codes.Internal, "introspection response body exceeds size limit")
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

	// Scopes: supports both RFC 7662 "scope" (space-separated string) and
	// "scopes" (JSON array, used by auth.provider). If both are present,
	// "scopes" takes precedence.
	if scopesRaw, ok := raw["scopes"]; ok {
		if arr, ok := scopesRaw.([]any); ok {
			for _, s := range arr {
				if str, ok := s.(string); ok {
					result.Scopes = append(result.Scopes, str)
				}
			}
		}
	} else if scopeStr, ok := raw["scope"].(string); ok && scopeStr != "" {
		result.Scopes = strings.Fields(scopeStr)
	}

	// ExpiresAt
	if exp, ok := raw["exp"].(float64); ok {
		result.ExpiresAt = time.Unix(int64(exp), 0).UTC()
	}

	// Remaining claims (exclude promoted fields)
	promoted := map[string]bool{"active": true, "sub": true, "scope": true, "scopes": true, "exp": true}
	for k, v := range raw {
		if !promoted[k] {
			result.Claims[k] = v
		}
	}

	return result, nil
}
