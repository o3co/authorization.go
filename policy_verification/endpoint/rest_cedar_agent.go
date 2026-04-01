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
	"google.golang.org/grpc/status"
)

// cedarAgentBuildConfig holds construction-time-only settings for NewCedarAgentEndpoint.
type cedarAgentBuildConfig struct {
	timeout              time.Duration
	maxResponseBodySize  int64
	logger               *slog.Logger
	requestIDHeaderKey   string
	principalPrefix      string
	actionPrefix         string
	resourcePrefix       string
	principalResolver    func(ctx context.Context, token string) string
	requestIDFunc        func(context.Context) string
}

// CedarAgentOption configures the Cedar agent REST endpoint.
type CedarAgentOption func(*cedarAgentBuildConfig)

// WithCedarAgentTimeout sets the HTTP client timeout. Panics if d <= 0.
func WithCedarAgentTimeout(d time.Duration) CedarAgentOption {
	if d <= 0 {
		panic(fmt.Sprintf("timeout must be positive, got %v", d))
	}
	return func(c *cedarAgentBuildConfig) {
		c.timeout = d
	}
}

// WithCedarAgentMaxResponseBodySize sets the maximum number of bytes read from the Cedar agent
// response body. Panics if size <= 0.
func WithCedarAgentMaxResponseBodySize(size int64) CedarAgentOption {
	if size <= 0 {
		panic(fmt.Sprintf("maxResponseBodySize must be positive, got %d", size))
	}
	return func(c *cedarAgentBuildConfig) {
		c.maxResponseBodySize = size
	}
}

// WithCedarAgentLogLevel sets the log level for the Cedar agent endpoint logger.
func WithCedarAgentLogLevel(level slog.Level) CedarAgentOption {
	return func(c *cedarAgentBuildConfig) {
		c.logger = newLogger(level)
	}
}

// WithCedarAgentPrincipalPrefix sets the Cedar entity type prefix for the principal.
// Default is "User".
func WithCedarAgentPrincipalPrefix(prefix string) CedarAgentOption {
	return func(c *cedarAgentBuildConfig) {
		c.principalPrefix = prefix
	}
}

// WithCedarAgentActionPrefix sets the Cedar entity type prefix for the action.
// Default is "Action".
func WithCedarAgentActionPrefix(prefix string) CedarAgentOption {
	return func(c *cedarAgentBuildConfig) {
		c.actionPrefix = prefix
	}
}

// WithCedarAgentResourcePrefix sets the Cedar entity type prefix for the resource.
// Default is "Resource".
func WithCedarAgentResourcePrefix(prefix string) CedarAgentOption {
	return func(c *cedarAgentBuildConfig) {
		c.resourcePrefix = prefix
	}
}

// WithCedarAgentRequestIDHeaderKey sets the HTTP header key for forwarding the request ID
// to the Cedar agent. Default is "x-request-id". Set to empty string to disable forwarding.
// Only used when WithCedarAgentRequestIDFunc is also set.
func WithCedarAgentRequestIDHeaderKey(key string) CedarAgentOption {
	return func(c *cedarAgentBuildConfig) {
		c.requestIDHeaderKey = key
	}
}

// WithCedarAgentRequestIDFunc sets a function to extract the request ID from the context
// for forwarding to the Cedar agent as a header.
// If not set, no request ID is extracted and header forwarding is skipped.
func WithCedarAgentRequestIDFunc(fn func(context.Context) string) CedarAgentOption {
	return func(c *cedarAgentBuildConfig) {
		c.requestIDFunc = fn
	}
}

// WithCedarAgentPrincipalResolver sets a custom function to resolve the principal ID from the
// raw bearer token. The default resolver returns the token value as-is.
func WithCedarAgentPrincipalResolver(fn func(ctx context.Context, token string) string) CedarAgentOption {
	if fn == nil {
		panic("principalResolver must not be nil")
	}
	return func(c *cedarAgentBuildConfig) {
		c.principalResolver = fn
	}
}

// restCedarAgentEndpoint is a VerifierEndpoint implementation that calls a Cedar agent REST API.
type restCedarAgentEndpoint struct {
	httpClient           *http.Client
	authorizeURL         string
	maxResponseBodySize  int64
	logger               *slog.Logger
	requestIDHeaderKey   string
	principalPrefix      string
	actionPrefix         string
	resourcePrefix       string
	principalResolver    func(ctx context.Context, token string) string
	requestIDFunc        func(context.Context) string
}

// cedarAgentRequest is the JSON body sent to the Cedar agent's is_authorized API.
type cedarAgentRequest struct {
	Principal string         `json:"principal"`
	Action    string         `json:"action"`
	Resource  string         `json:"resource"`
	Context   map[string]any `json:"context"`
}

// cedarAgentResponse is the JSON body returned by the Cedar agent's is_authorized API.
type cedarAgentResponse struct {
	Decision string `json:"decision"`
}

// formatEntityUID formats a Cedar entity UID as {entityType}::"{id}".
func formatEntityUID(entityType, id string) string {
	return fmt.Sprintf(`%s::"%s"`, entityType, id)
}

// NewCedarAgentEndpoint constructs a VerifierEndpoint that calls the Cedar agent REST API.
// The authorize URL is constructed as: {baseURL}/v1/is_authorized.
// Returns an error if baseURL is empty or invalid.
func NewCedarAgentEndpoint(baseURL string, opts ...CedarAgentOption) (VerifierEndpoint, error) {
	rawBase := strings.TrimSpace(baseURL)
	if rawBase == "" {
		return nil, fmt.Errorf("baseURL must not be empty")
	}

	if !strings.HasPrefix(rawBase, "http://") && !strings.HasPrefix(rawBase, "https://") {
		rawBase = "http://" + rawBase
	}

	base, err := url.Parse(rawBase)
	if err != nil {
		return nil, fmt.Errorf("invalid Cedar agent base URL: %w", err)
	}

	base.Path = strings.TrimSuffix(base.Path, "/") + "/v1/is_authorized"

	cfg := &cedarAgentBuildConfig{
		timeout:             defaultTimeout,
		maxResponseBodySize: defaultMaxResponseBodySize,
		logger:              newLogger(slog.LevelError),
		requestIDHeaderKey:  "x-request-id",
		principalPrefix:     "User",
		actionPrefix:        "Action",
		resourcePrefix:      "Resource",
		principalResolver:   func(_ context.Context, token string) string { return token },
	}
	for _, opt := range opts {
		opt(cfg)
	}

	return &restCedarAgentEndpoint{
		httpClient:          &http.Client{Timeout: cfg.timeout},
		authorizeURL:        base.String(),
		maxResponseBodySize: cfg.maxResponseBodySize,
		logger:              cfg.logger,
		requestIDHeaderKey:  cfg.requestIDHeaderKey,
		principalPrefix:     cfg.principalPrefix,
		actionPrefix:        cfg.actionPrefix,
		resourcePrefix:      cfg.resourcePrefix,
		principalResolver:   cfg.principalResolver,
		requestIDFunc:       cfg.requestIDFunc,
	}, nil
}

// Verify calls the Cedar agent is_authorized API and returns nil if the decision is "Allow",
// codes.PermissionDenied if "Deny" (or any other decision), codes.Unauthenticated if no token,
// and codes.Internal on HTTP or marshaling errors.
func (e *restCedarAgentEndpoint) Verify(ctx context.Context, resource, action string) error {
	// Retrieve the bearer token from gRPC incoming metadata.
	tok, err := getToken(ctx)
	if err != nil {
		return status.Errorf(codes.Unauthenticated, "failed to get authorization token: %v", err)
	}

	// Resolve the principal ID from the token.
	principalID := e.principalResolver(ctx, tok.Value)

	// Build the Cedar agent request body using entity UID format.
	reqBody := cedarAgentRequest{
		Principal: formatEntityUID(e.principalPrefix, principalID),
		Action:    formatEntityUID(e.actionPrefix, action),
		Resource:  formatEntityUID(e.resourcePrefix, resource),
		Context:   map[string]any{},
	}
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return status.Errorf(codes.Internal, "failed to marshal Cedar agent request body: %v", err)
	}

	// Create the HTTP request, binding the caller's context for cancellation propagation.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.authorizeURL, bytes.NewReader(jsonData))
	if err != nil {
		return status.Errorf(codes.Internal, "failed to create Cedar agent request: %v", err)
	}

	var requestID string
	if e.requestIDFunc != nil {
		requestID = e.requestIDFunc(ctx)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	if e.requestIDHeaderKey != "" && requestID != "" {
		req.Header.Set(e.requestIDHeaderKey, requestID)
	}

	// Send the request.
	resp, err := e.httpClient.Do(req)
	if err != nil {
		return status.Errorf(codes.Internal, "Cedar agent request failed: %v", err)
	}
	defer resp.Body.Close()

	// Read the response body up to maxResponseBodySize bytes.
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, e.maxResponseBodySize))
	if err != nil {
		e.logger.Error("failed to read Cedar agent response body", "error", err, "x-request-id", requestID)
		respBody = nil
	}

	e.logger.Debug("Cedar agent response received", "status", resp.StatusCode, "x-request-id", requestID)

	// Non-2xx responses are treated as internal errors.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		const maxLoggedBodySize = 1024
		logBody := respBody
		if len(logBody) > maxLoggedBodySize {
			logBody = logBody[:maxLoggedBodySize]
		}
		e.logger.Error("error response from Cedar agent", "status", resp.StatusCode, "body", string(logBody), "x-request-id", requestID)
		return status.Errorf(codes.Internal, "Cedar agent returned non-2xx status: %d", resp.StatusCode)
	}

	// Parse the Cedar agent decision.
	var cedarResp cedarAgentResponse
	if err := json.Unmarshal(respBody, &cedarResp); err != nil {
		return status.Errorf(codes.Internal, "failed to parse Cedar agent response: %v", err)
	}

	if cedarResp.Decision == "Allow" {
		return nil
	}

	return status.Error(codes.PermissionDenied, "access denied by policy")
}
