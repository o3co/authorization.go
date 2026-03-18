# policy_verification

`context.Context` に格納された認可ポリシーを取り出し、外部の認可サーバーへ HTTP リクエストを送信して許可/拒否を判定する gRPC サーバーインターセプターと REST エンドポイント実装です。

## 目的

gRPC リクエストの認可チェックを外部の認可サービスに委譲します。`protobuf_policy_option` インターセプターが解決したリソースとアクションを受け取り、認可サーバーの `/verify` エンドポイントへ POST することで、認可ロジックをアプリケーションコードから分離します。

## 特性

- **外部認可サービス連携**: 認可ロジックを外部 HTTP サービスに委譲するため、認可ポリシーの変更をアプリケーションの再デプロイなしに反映できます。
- **JWT パススルー**: gRPC メタデータの `Authorization` ヘッダをそのまま認可サーバーへ転送します。
- **x-request-id 伝播**: gRPC メタデータから `x-request-id` を抽出し、認可サーバーへの HTTP リクエストに転送します。メタデータに存在しない場合は `YYYYMMDDHHmmss_<uuid>` 形式で自動生成します。
- **gRPC ステータスコードマッピング**: 認可サーバーのレスポンスを適切な gRPC ステータスコードに変換します（`403` → `PermissionDenied`、`401` → `Unauthenticated`）。
- **レスポンスボディサイズ制限**: デフォルト 1MB、`WithMaxResponseBodySize` オプションで変更可能。メモリ保護のため大きなレスポンスを自動的に切り捨てます。
- **Context キャンセル伝播**: `http.NewRequestWithContext` を使用しているため、gRPC のタイムアウトやキャンセルが認可サーバーへの HTTP リクエストにも伝播します。
- **プラガブルエンドポイント**: `VerifierEndpoint` インターフェースを実装することで、REST 以外の認可バックエンド（gRPC など）にも差し替えられます。

## インストール

```bash
go get github.com/o3co/grpc.authz/policy_verification
```

## 使い方

### 1. `RESTEndpoint` を生成する

```go
import (
    "log/slog"
    "time"
    pvendpoint "github.com/o3co/grpc.authz/policy_verification/endpoint"
)

verifier, err := pvendpoint.NewRESTEndpoint(
    "http://auth-service/",
    pvendpoint.WithTimeout(5 * time.Second),          // オプション: タイムアウト（デフォルト: 10s）
    pvendpoint.WithMaxResponseBodySize(512 * 1024),   // オプション: レスポンスボディの最大読み取りサイズ（デフォルト: 1MB）
    pvendpoint.WithLogLevel(slog.LevelError),          // オプション: ログレベル（デフォルト: LevelError）
)
if err != nil { ... }
```

`baseURL` には認可サーバーのベース URL を渡します。インターセプターは自動的に `/verify` を末尾に付加します。

### 2. インターセプターを登録する

```go
import (
    "log/slog"
    policyoption       "github.com/o3co/grpc.authz/protobuf_policy_option"
    policyverification "github.com/o3co/grpc.authz/policy_verification"
)

grpc.NewServer(
    grpc.ChainUnaryInterceptor(
        policyoption.Interceptor(                         // [1] ポリシー解決（必ず先に登録）
            policyoption.WithLogLevel(slog.LevelError),
        ),
        policyverification.Interceptor(verifier,          // [2] 認可チェック
            policyverification.WithLogLevel(slog.LevelError),
        ),
    ),
)
```

### 3. 認可サーバーの仕様

インターセプターは以下の HTTP リクエストを送信します。

```text
POST /verify
Content-Type: application/json
Authorization: <gRPC メタデータの Authorization ヘッダをそのまま転送>
x-request-id: 20260318120530_a1b2c3d4e5f6...

{"resource": "items/42", "action": "read"}
```

| レスポンスステータス | gRPC ステータスコード |
| --- | --- |
| `2xx` | `OK`（認可成功） |
| `401` | `Unauthenticated` |
| `403` | `PermissionDenied` |
| その他 | `Internal` |

### 4. カスタムエンドポイントを実装する

REST 以外のバックエンドを使いたい場合は `VerifierEndpoint` インターフェースを実装してください。

```go
import "github.com/o3co/grpc.authz/policy_verification/endpoint"

type myEndpoint struct{}

func (e *myEndpoint) Verify(ctx context.Context, resource, action string) error {
    // 認可ロジックを実装する
    return nil
}
```

## 注意点

- **インターセプターの順序**: `protobuf_policy_option.Interceptor` を必ず **前** に登録してください。未登録の場合、全リクエストに対して `Internal` エラーを返します。
- **`Authorization` ヘッダ必須**: gRPC メタデータに `Authorization` ヘッダが存在しない場合は `Unauthenticated` エラーを返します。クライアントは必ずトークンを付与してください。
- **認可サーバーの可用性**: 認可サーバーが応答しない場合はネットワークエラーとして `Internal` エラーになります。認可サーバーの SLA がサービス全体の可用性に直接影響するため、`WithTimeout` で適切なタイムアウトを設定してください。
- **レスポンスボディの非公開**: 認可サーバーのエラーレスポンスボディはログに出力しますが、gRPC クライアントへは返しません。内部情報の漏洩を防ぐためです。

## オプション一覧

### `Interceptor`

| オプション | 説明 | デフォルト |
| --- | --- | --- |
| `WithLogLevel(slog.Level)` | ログ出力レベルを設定する | `slog.LevelError` |

### `NewRESTEndpoint`

| オプション | 説明 | デフォルト |
| --- | --- | --- |
| `WithTimeout(time.Duration)` | 認可サーバーへの HTTP タイムアウト | `10s` |
| `WithLogLevel(slog.Level)` | ログ出力レベルを設定する | `slog.LevelError` |
| `WithMaxResponseBodySize(int64)` | レスポンスボディの最大読み取りサイズ（バイト） | `1048576`（1MB） |

## パッケージ構成

```text
policy_verification/
├── interceptor.go     # gRPC インターセプター本体
├── logger.go          # ログレベル制御
├── internal/
│   └── logger.go      # 内部ロガー
└── endpoint/
    ├── endpoint.go                 # VerifierEndpoint インターフェース・context ヘルパー
    ├── rest_o3_policy_verifier.go  # REST 実装（NewRESTEndpoint）
    └── logger.go                   # ログレベル制御
```
