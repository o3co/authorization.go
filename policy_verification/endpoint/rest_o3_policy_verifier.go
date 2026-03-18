// Copyright 2026 o3co Inc.
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

// Option はo3 REST エンドポイントの設定オプション
type Option func(*restO3PolicyVerifierEndpoint)

// WithTimeout HTTP クライアントのタイムアウトを設定する。未指定時のデフォルトは 10s。
func WithTimeout(d time.Duration) Option {
	if d <= 0 {
		panic(fmt.Sprintf("timeout must be positive, got %v", d))
	}
	return func(e *restO3PolicyVerifierEndpoint) {
		e.timeout = d
	}
}

// WithMaxResponseBodySize レスポンスボディの最大読み取りサイズを設定する（バイト単位）。
func WithMaxResponseBodySize(size int64) Option {
	if size <= 0 {
		panic(fmt.Sprintf("maxResponseBodySize must be positive, got %d", size))
	}
	return func(e *restO3PolicyVerifierEndpoint) {
		e.maxResponseBodySize = size
	}
}

// WithLogLevel ログレベルを指定する。未指定時のデフォルトは slog.LevelError。
func WithLogLevel(level slog.Level) Option {
	return func(e *restO3PolicyVerifierEndpoint) {
		e.logger = newLogger(level)
	}
}

// restO3PolicyVerifierEndpoint は o3 独自規格の REST 認可サービスへの VerifierEndpoint 実装
type restO3PolicyVerifierEndpoint struct {
	httpClient          *http.Client
	verifyURL           string
	timeout             time.Duration
	maxResponseBodySize int64
	logger              *slog.Logger
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

	e := &restO3PolicyVerifierEndpoint{
		verifyURL:           base.String(),
		timeout:             defaultTimeout,
		maxResponseBodySize: defaultMaxResponseBodySize,
		logger:              newLogger(slog.LevelError),
	}
	for _, opt := range opts {
		opt(e)
	}

	e.httpClient = &http.Client{Timeout: e.timeout}

	return e, nil
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

// getRequestID context または gRPC incoming metadata から x-request-id を取得する。
// context に値がある場合はそちらを優先し、なければ metadata を参照する。
// どちらにも存在しない場合は空文字を返す。
func getRequestID(ctx context.Context) string {
	if v := RequestIDFromContext(ctx); v != "" {
		return v
	}
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	if values := md["x-request-id"]; len(values) > 0 {
		return values[0]
	}
	return ""
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

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", tok.TokenType+" "+tok.Value)

	// x-request-id が存在する場合のみ転送する
	if requestID := getRequestID(ctx); requestID != "" {
		req.Header.Set("x-request-id", requestID)
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
		e.logger.Error("failed to read response body", "error", err)
		respBody = nil
	}

	requestID := getRequestID(ctx)
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

	if resp.StatusCode == http.StatusForbidden {
		e.logger.Error("error response from authorization server", "status", resp.StatusCode, "body", string(logBody), "x-request-id", requestID)
		return status.Error(codes.PermissionDenied, "access denied")
	}

	if resp.StatusCode == http.StatusUnauthorized {
		e.logger.Error("error response from authorization server", "status", resp.StatusCode, "body", string(logBody), "x-request-id", requestID)
		return status.Error(codes.Unauthenticated, "invalid or expired token")
	}

	e.logger.Error("unexpected authorization service response", "status", resp.StatusCode, "x-request-id", requestID)
	return status.Errorf(codes.Internal, "authorization service error: %d, body: %s", resp.StatusCode, "Failed to verify policy")
}
