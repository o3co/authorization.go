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

// opaBuildConfig holds construction-time-only settings for NewOPAEndpoint.
type opaBuildConfig struct {
	timeout             time.Duration
	maxResponseBodySize int64
	logger              *slog.Logger
}

// OPAOption configures the OPA REST endpoint.
type OPAOption func(*opaBuildConfig)

// WithOPATimeout sets the HTTP client timeout. Panics if d <= 0.
func WithOPATimeout(d time.Duration) OPAOption {
	if d <= 0 {
		panic(fmt.Sprintf("timeout must be positive, got %v", d))
	}
	return func(c *opaBuildConfig) {
		c.timeout = d
	}
}

// WithOPAMaxResponseBodySize sets the maximum number of bytes read from the OPA response body.
// Panics if size <= 0.
func WithOPAMaxResponseBodySize(size int64) OPAOption {
	if size <= 0 {
		panic(fmt.Sprintf("maxResponseBodySize must be positive, got %d", size))
	}
	return func(c *opaBuildConfig) {
		c.maxResponseBodySize = size
	}
}

// WithOPALogLevel sets the log level for the OPA endpoint logger.
func WithOPALogLevel(level slog.Level) OPAOption {
	return func(c *opaBuildConfig) {
		c.logger = newLogger(level)
	}
}

// restOPAEndpoint is a VerifierEndpoint implementation that calls an OPA REST API.
type restOPAEndpoint struct {
	httpClient          *http.Client
	evaluateURL         string
	maxResponseBodySize int64
	logger              *slog.Logger
}

// opaRequest is the JSON body sent to OPA's data API.
type opaRequest struct {
	Input opaInput `json:"input"`
}

// opaInput contains the fields evaluated by the OPA policy.
type opaInput struct {
	Resource string `json:"resource"`
	Action   string `json:"action"`
	Token    string `json:"token"`
}

// opaResponse is the JSON body returned by OPA's data API.
// Result is a pointer so we can distinguish false from absent (undefined).
type opaResponse struct {
	Result *bool `json:"result,omitempty"`
}

// NewOPAEndpoint constructs a VerifierEndpoint that calls OPA's REST data API.
// The evaluate URL is constructed as: {baseURL}/v1/data/{policyPath}.
// Returns an error if baseURL or policyPath is empty, or if the URL is invalid.
func NewOPAEndpoint(baseURL, policyPath string, opts ...OPAOption) (VerifierEndpoint, error) {
	rawBase := strings.TrimSpace(baseURL)
	if rawBase == "" {
		return nil, fmt.Errorf("baseURL must not be empty")
	}

	rawPath := strings.TrimSpace(policyPath)
	if rawPath == "" {
		return nil, fmt.Errorf("policyPath must not be empty")
	}

	if !strings.HasPrefix(rawBase, "http://") && !strings.HasPrefix(rawBase, "https://") {
		rawBase = "http://" + rawBase
	}

	base, err := url.Parse(rawBase)
	if err != nil {
		return nil, fmt.Errorf("invalid OPA base URL: %w", err)
	}

	// Normalize: strip trailing slash from base path, strip leading slash from policyPath.
	rawPath = strings.TrimPrefix(rawPath, "/")
	base.Path = strings.TrimSuffix(base.Path, "/") + "/v1/data/" + rawPath

	cfg := &opaBuildConfig{
		timeout:             defaultTimeout,
		maxResponseBodySize: defaultMaxResponseBodySize,
		logger:              newLogger(slog.LevelError),
	}
	for _, opt := range opts {
		opt(cfg)
	}

	return &restOPAEndpoint{
		httpClient:          &http.Client{Timeout: cfg.timeout},
		evaluateURL:         base.String(),
		maxResponseBodySize: cfg.maxResponseBodySize,
		logger:              cfg.logger,
	}, nil
}

// Verify calls the OPA data API and returns nil if the policy result is true,
// codes.PermissionDenied if false or absent, codes.Unauthenticated if no token,
// and codes.Internal on HTTP or marshaling errors.
func (e *restOPAEndpoint) Verify(ctx context.Context, resource, action string) error {
	// Retrieve the bearer token from gRPC incoming metadata.
	tok, err := getToken(ctx)
	if err != nil {
		return status.Errorf(codes.Unauthenticated, "failed to get authorization token: %v", err)
	}

	// Build the OPA request body.
	reqBody := opaRequest{
		Input: opaInput{
			Resource: resource,
			Action:   action,
			Token:    tok.Value,
		},
	}
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return status.Errorf(codes.Internal, "failed to marshal OPA request body: %v", err)
	}

	// Create the HTTP request, binding the caller's context for cancellation propagation.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.evaluateURL, bytes.NewReader(jsonData))
	if err != nil {
		return status.Errorf(codes.Internal, "failed to create OPA request: %v", err)
	}

	requestID := getRequestID(ctx)

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	if requestID != "" {
		req.Header.Set("x-request-id", requestID)
	}

	// Send the request.
	resp, err := e.httpClient.Do(req)
	if err != nil {
		return status.Errorf(codes.Internal, "OPA request failed: %v", err)
	}
	defer resp.Body.Close()

	// Read the response body up to maxResponseBodySize bytes.
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, e.maxResponseBodySize))
	if err != nil {
		e.logger.Error("failed to read OPA response body", "error", err, "x-request-id", requestID)
		respBody = nil
	}

	e.logger.Debug("OPA response received", "status", resp.StatusCode, "x-request-id", requestID)

	// Non-2xx responses are treated as internal errors.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		const maxLoggedBodySize = 1024
		logBody := respBody
		if len(logBody) > maxLoggedBodySize {
			logBody = logBody[:maxLoggedBodySize]
		}
		e.logger.Error("error response from OPA", "status", resp.StatusCode, "body", string(logBody), "x-request-id", requestID)
		return status.Errorf(codes.Internal, "OPA returned non-2xx status: %d", resp.StatusCode)
	}

	// Parse the OPA decision.
	var opaResp opaResponse
	if err := json.Unmarshal(respBody, &opaResp); err != nil {
		return status.Errorf(codes.Internal, "failed to parse OPA response: %v", err)
	}

	// result absent (undefined) or false → deny.
	if opaResp.Result == nil || !*opaResp.Result {
		return status.Error(codes.PermissionDenied, "access denied by policy")
	}

	return nil
}
