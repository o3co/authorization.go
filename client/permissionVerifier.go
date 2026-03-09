package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// PermissionVerifierClient 認可チェックを行うクライアント
type PermissionVerifierClient interface {
	Verify(ctx context.Context, resource, action string) error
}

// PermissionVerifierClient 認可クライアントの実装
type permissionVerifierClient struct {
	httpClient *http.Client
	baseURL    string
}

// NewPermissionVerifierClient 認可クライアントのコンストラクタ
func NewPermissionVerifierClient(httpClient *http.Client, baseURL string) PermissionVerifierClient {
	return &permissionVerifierClient{
		httpClient: httpClient,
		baseURL:    baseURL,
	}
}

type Token struct {
	TokenType string `json:"tokenType"`
	Value     string `json:"value"`
}

// GetToken gRPCメタデータからAuthorizationトークンを取得
func GetToken(ctx context.Context) (*Token, error) {
	md, ok := metadata.FromIncomingContext(ctx)

	if !ok {
		return nil, fmt.Errorf("no metadata found in context")
	}

	// "authorization" キーで取得 (全て小文字になる)
	values := md["authorization"]

	if len(values) == 0 {
		return nil, fmt.Errorf("no authorization header found")
	}

	token := values[0]

	parts := strings.Fields(token)
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid authorization header format")
	}

	tokenType := parts[0]
	tokenValue := parts[1]
	return &Token{TokenType: tokenType, Value: tokenValue}, nil
}

// Verify 権限チェックを実行
func (c *permissionVerifierClient) Verify(ctx context.Context, resource, action string) error {
	// --- 認可トークン取得 -------------------------------------------------
	token, err := GetToken(ctx)

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

	// --- エンドポイント URL の組み立て -------------------------------------
	// base URL とパスを安全に結合して /verify エンドポイントを作る。
	// 単純な文字列連結だとスラッシュの有無で壊れるため、url.ResolveReference を使う。
	// ただし base URL にスキームが含まれていない場合（例: "localhost:8080"）
	// url.Parse はスキーム無しの URL として扱うため、ResolveReference が
	// 不正な結果 (例: "localhost:///verify") を返すことがある。
	// そのため明示的にスキームを補完する。
	rawBase := strings.TrimSpace(c.baseURL)

	if rawBase != "" && !strings.HasPrefix(rawBase, "http://") && !strings.HasPrefix(rawBase, "https://") {
		rawBase = "http://" + rawBase
	}

	base, err := url.Parse(rawBase)

	if err != nil {
		return status.Errorf(codes.Internal, "invalid authorization base url: %v", err)
	}

	verifyURL := base.ResolveReference(&url.URL{Path: "/verify"}).String()

	// --- HTTP リクエスト作成 -----------------------------------------------
	// Context を紐付けたリクエストを作成することで、呼び出し元のキャンセルやタイムアウトを継承する。
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, verifyURL, bytes.NewReader(jsonData))

	if err != nil {
		return status.Errorf(codes.Internal, "failed to create request: %v", err)
	}

	// 必要なヘッダをセット
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", token.TokenType+" "+token.Value) // JWT を Authorization ヘッダで送信

	// --- リクエスト送信 ---------------------------------------------------
	resp, err := c.httpClient.Do(req)

	if err != nil {
		// ネットワークエラーやタイムアウトなどは内部エラーとして扱う。
		return status.Errorf(codes.Internal, "request failed: %v", err)
	}

	// レスポンスボディは必ず Close する（リソースリーク防止）。
	defer resp.Body.Close()

	// ボディを読み出してログに出力（デバッグに有用）。読み取り失敗は無視して続行。
	respBody, _ := io.ReadAll(resp.Body)
	log.Printf("[AuthClient] response status: %d", resp.StatusCode)
	log.Printf("[AuthClient] response body: %s", string(respBody))

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
	return status.Errorf(codes.Internal, "authorization service error: %d, body: %s", resp.StatusCode, string(respBody))
}
