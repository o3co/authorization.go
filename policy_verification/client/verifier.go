package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// VerifierClient 認可チェックを行うクライアント
type VerifierClient interface {
	Verify(ctx context.Context, resource, action string) error
}

const defaultMaxResponseBodySize int64 = 1024 * 1024 // 1MB

// Option verifierClient の設定オプション
type Option func(*verifierClient)

// WithMaxResponseBodySize レスポンスボディの最大読み取りサイズを設定する（バイト単位）
func WithMaxResponseBodySize(size int64) Option {
	return func(c *verifierClient) {
		c.maxResponseBodySize = size
	}
}

// verifierClient 認可クライアントの実装
type verifierClient struct {
	httpClient          *http.Client
	verifyURL           string
	maxResponseBodySize int64
}

// NewVerifierClient 認可クライアントのコンストラクタ
// baseURL が不正な場合はエラーを返す。
func NewVerifierClient(httpClient *http.Client, baseURL string, opts ...Option) (VerifierClient, error) {
	if httpClient == nil {
		return nil, fmt.Errorf("httpClient must not be nil")
	}

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
	verifyURL := base.String()

	c := &verifierClient{
		httpClient:          httpClient,
		verifyURL:           verifyURL,
		maxResponseBodySize: defaultMaxResponseBodySize,
	}
	for _, opt := range opts {
		opt(c)
	}

	return c, nil
}

type token struct {
	TokenType string
	Value     string
}

// getToken gRPCメタデータからAuthorizationトークンを取得
func getToken(ctx context.Context) (*token, error) {
	md, ok := metadata.FromIncomingContext(ctx)

	if !ok {
		return nil, fmt.Errorf("no metadata found in context")
	}

	// "authorization" キーで取得 (全て小文字になる)
	values := md["authorization"]

	if len(values) == 0 {
		return nil, fmt.Errorf("no authorization header found")
	}

	raw := values[0]

	parts := strings.Fields(raw)
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid authorization header format")
	}

	tokenType := parts[0]
	tokenValue := parts[1]
	return &token{TokenType: tokenType, Value: tokenValue}, nil
}

// Verify 権限チェックを実行
func (c *verifierClient) Verify(ctx context.Context, resource, action string) error {
	// --- 認可トークン取得 -------------------------------------------------
	tok, err := getToken(ctx)

	if err != nil {
		return status.Errorf(codes.Unauthenticated, "failed to get authorization token: %v", err)
	}

	// --- リクエストボディ作成 -----------------------------------------------
	// 認可サーバーへ渡す JSON ボディを作る。
	reqBody := map[string]string{"resource": resource, "action": action}
	jsonData, err := json.Marshal(reqBody)

	if err != nil {
		// JSON マーシャリングに失敗したら内部エラーとして扱う。
		return status.Errorf(codes.Internal, "failed to marshal request body: %v", err)
	}

	// --- HTTP リクエスト作成 -----------------------------------------------
	// Context を紐付けたリクエストを作成することで、呼び出し元のキャンセルやタイムアウトを継承する。
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.verifyURL, bytes.NewReader(jsonData))

	if err != nil {
		return status.Errorf(codes.Internal, "failed to create request: %v", err)
	}

	// 必要なヘッダをセット
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", tok.TokenType+" "+tok.Value) // JWT を Authorization ヘッダで送信

	// --- リクエスト送信 ---------------------------------------------------
	resp, err := c.httpClient.Do(req)

	if err != nil {
		// ネットワークエラーやタイムアウトなどは内部エラーとして扱う。
		return status.Errorf(codes.Internal, "request failed: %v", err)
	}

	// レスポンスボディは必ず Close する（リソースリーク防止）。
	defer resp.Body.Close()

	// ボディを最大 maxResponseBodySize バイトまで読み出す（メモリ保護）。
	// 読み取りに失敗した場合は部分データを捨て、空として扱う。
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, c.maxResponseBodySize))
	if err != nil {
		logger.Error("failed to read response body", "error", err)
		respBody = nil
	}
	logger.Debug("response received", "status", resp.StatusCode)

	// レスポンスボディは常にフルで出力せず、エラー時のみかつ長さを制限してログに出す。
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		const maxLoggedBodySize = 1024
		logBody := respBody
		if len(logBody) > maxLoggedBodySize {
			logBody = logBody[:maxLoggedBodySize]
		}
		logger.Error("error response from authorization server", "status", resp.StatusCode, "body", string(logBody))
	}
	// --- ステータスコードに基づく判定 -------------------------------------
	// 2xx 系は成功として扱う。
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	// 403 は権限不足 → PermissionDenied を返す。
	if resp.StatusCode == http.StatusForbidden {
		return status.Error(codes.PermissionDenied, "access denied")
	}

	// 401 はトークン無効/期限切れ → Unauthenticated を返す。
	if resp.StatusCode == http.StatusUnauthorized {
		return status.Error(codes.Unauthenticated, "invalid or expired token")
	}

	// その他は内部エラーとして扱い、レスポンスボディを含めて原因追跡をしやすくする。
	logger.Error("unexpected authorization service response", "status", resp.StatusCode)
	return status.Errorf(codes.Internal, "authorization service error: %d, body: %s", resp.StatusCode, "Failed to verify policy")
}
