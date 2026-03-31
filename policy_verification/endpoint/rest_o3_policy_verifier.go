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

	rt "github.com/o3co/grpc.authz/request_tracking"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const defaultMaxResponseBodySize int64 = 1024 * 1024 // 1MB
const defaultTimeout = 10 * time.Second

// buildConfig は NewRESTEndpoint の構築時にのみ使用する一時設定。
// timeout など構築後に不要なフィールドをここで管理することで、
// restO3PolicyVerifierEndpoint の struct を実行時に必要なフィールドのみに絞る。
type buildConfig struct {
	timeout              time.Duration
	maxResponseBodySize  int64
	logger               *slog.Logger
	requestIDHeaderKey   string
}

// Option はo3 REST エンドポイントの設定オプション
type Option func(*buildConfig)

// WithTimeout HTTP クライアントのタイムアウトを設定する。未指定時のデフォルトは 10s。
func WithTimeout(d time.Duration) Option {
	if d <= 0 {
		panic(fmt.Sprintf("timeout must be positive, got %v", d))
	}
	return func(c *buildConfig) {
		c.timeout = d
	}
}

// WithMaxResponseBodySize レスポンスボディの最大読み取りサイズを設定する（バイト単位）。
func WithMaxResponseBodySize(size int64) Option {
	if size <= 0 {
		panic(fmt.Sprintf("maxResponseBodySize must be positive, got %d", size))
	}
	return func(c *buildConfig) {
		c.maxResponseBodySize = size
	}
}

// WithLogLevel ログレベルを指定する。未指定時のデフォルトは slog.LevelError。
func WithLogLevel(level slog.Level) Option {
	return func(c *buildConfig) {
		c.logger = newLogger(level)
	}
}

// WithRequestIDHeaderKey sets the HTTP header key for forwarding the request ID
// to the authorization server. Default is "x-request-id". Set to empty string to
// disable forwarding.
func WithRequestIDHeaderKey(key string) Option {
	return func(c *buildConfig) {
		c.requestIDHeaderKey = key
	}
}

// restO3PolicyVerifierEndpoint は o3 独自規格の REST 認可サービスへの VerifierEndpoint 実装
type restO3PolicyVerifierEndpoint struct {
	httpClient           *http.Client
	verifyURL            string
	maxResponseBodySize  int64
	logger               *slog.Logger
	requestIDHeaderKey   string
}

// NewRESTEndpoint o3 REST 認可エンドポイントのコンストラクタ。
// baseURL が不正な場合はエラーを返す。
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

	return &restO3PolicyVerifierEndpoint{
		httpClient:          &http.Client{Timeout: cfg.timeout},
		verifyURL:           base.String(),
		maxResponseBodySize: cfg.maxResponseBodySize,
		logger:              cfg.logger,
		requestIDHeaderKey:  cfg.requestIDHeaderKey,
	}, nil
}

type token struct {
	TokenType string
	Value     string
}

// getToken gRPC incoming metadata から Authorization トークンを取得する。
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

// Verify 権限チェックを実行する。
func (e *restO3PolicyVerifierEndpoint) Verify(ctx context.Context, resource, action string) error {
	// --- 認可トークン取得 -------------------------------------------------
	tok, err := getToken(ctx)
	if err != nil {
		return status.Errorf(codes.Unauthenticated, "failed to get authorization token: %v", err)
	}

	// --- リクエストボディ作成 -----------------------------------------------
	reqBody := map[string]string{"resource": resource, "action": action}
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return status.Errorf(codes.Internal, "failed to marshal request body: %v", err)
	}

	// --- HTTP リクエスト作成 -----------------------------------------------
	// Context を紐付けたリクエストを作成することで、呼び出し元のキャンセルやタイムアウトを継承する。
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.verifyURL, bytes.NewReader(jsonData))
	if err != nil {
		return status.Errorf(codes.Internal, "failed to create request: %v", err)
	}

	requestID := rt.RequestIDFromContext(ctx)

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", tok.TokenType+" "+tok.Value)

	if e.requestIDHeaderKey != "" && requestID != "" {
		req.Header.Set(e.requestIDHeaderKey, requestID)
	}

	// --- リクエスト送信 ---------------------------------------------------
	resp, err := e.httpClient.Do(req)
	if err != nil {
		return status.Errorf(codes.Internal, "request failed: %v", err)
	}
	defer resp.Body.Close()

	// ボディを最大 maxResponseBodySize バイトまで読み出す（メモリ保護）。
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, e.maxResponseBodySize))
	if err != nil {
		e.logger.Error("failed to read response body", "error", err, "x-request-id", requestID)
		respBody = nil
	}

	e.logger.Debug("response received", "status", resp.StatusCode, "x-request-id", requestID)

	// --- ステータスコードに基づく判定 -------------------------------------
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
