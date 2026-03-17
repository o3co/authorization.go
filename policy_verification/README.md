# policy_verification

`context.Context` に格納された認可ポリシーを取り出し、外部の認可サーバーへ HTTP リクエストを送信して許可/拒否を判定する gRPC サーバーインターセプターと HTTP クライアントです。

## 目的

gRPC リクエストの認可チェックを外部の認可サービスに委譲します。`protobuf_policy_option` インターセプターが解決したリソースとアクションを受け取り、認可サーバーの `/verify` エンドポイントへ POST することで、認可ロジックをアプリケーションコードから分離します。

## 特性

- **外部認可サービス連携**: 認可ロジックを外部 HTTP サービスに委譲するため、認可ポリシーの変更をアプリケーションの再デプロイなしに反映できます。
- **JWT パススルー**: gRPC メタデータの `Authorization` ヘッダをそのまま認可サーバーへ転送します。
- **gRPC ステータスコードマッピング**: 認可サーバーのレスポンスを適切な gRPC ステータスコードに変換します（`403` → `PermissionDenied`、`401` → `Unauthenticated`）。
- **レスポンスボディサイズ制限**: デフォルト 1MB、`WithMaxResponseBodySize` オプションで変更可能。メモリ保護のため大きなレスポンスを自動的に切り捨てます。
- **Context キャンセル伝播**: `http.NewRequestWithContext` を使用しているため、gRPC のタイムアウトやキャンセルが認可サーバーへの HTTP リクエストにも伝播します。

## インストール

```bash
go get github.com/o3co/authorization.go/policy_verification
```

## 使い方

### 1. `VerifierClient` を生成する

```go
import pvclient "github.com/o3co/authorization.go/policy_verification/client"

verifier, err := pvclient.NewVerifierClient(
    &http.Client{Timeout: 5 * time.Second},
    "http://auth-service/",
    // オプション: レスポンスボディの最大読み取りサイズ（デフォルト 1MB）
    pvclient.WithMaxResponseBodySize(512 * 1024),
)
if err != nil { ... }
```

`baseURL` には認可サーバーのベース URL を渡します。インターセプターは自動的に `/verify` を末尾に付加します。`http://` または `https://` プレフィックスがない場合は `http://` が補完されます。

### 2. インターセプターを登録する

```go
import (
    policyoption      "github.com/o3co/authorization.go/protobuf_policy_option"
    policyverification "github.com/o3co/authorization.go/policy_verification"
)

grpc.NewServer(
    grpc.ChainUnaryInterceptor(
        policyoption.Interceptor,                    // [1] ポリシー解決（必ず先に登録）
        policyverification.Interceptor(verifier),    // [2] 認可チェック
    ),
)
```

### 3. 認可サーバーの仕様

インターセプターは以下の HTTP リクエストを送信します。

```text
POST /verify
Content-Type: application/json
Authorization: <gRPC メタデータの Authorization ヘッダをそのまま転送>

{"resource": "items/42", "action": "read"}
```

| レスポンスステータス | gRPC ステータスコード |
| --- | --- |
| `2xx` | `OK`（認可成功） |
| `401` | `Unauthenticated` |
| `403` | `PermissionDenied` |
| その他 | `Internal` |

## 注意点

- **インターセプターの順序**: `protobuf_policy_option.Interceptor` を必ず **前** に登録してください。Context にポリシーが存在しない場合（`protobuf_policy_option` が未登録など）、このインターセプターは認可チェックをスキップしてリクエストを素通りさせます。
- **`Authorization` ヘッダ必須**: gRPC メタデータに `Authorization` ヘッダが存在しない場合は `Unauthenticated` エラーを返します。クライアントは必ずトークンを付与してください。
- **認可サーバーの可用性**: 認可サーバーが応答しない場合はネットワークエラーとして `Internal` エラーになります。認可サーバーの SLA がサービス全体の可用性に直接影響するため、タイムアウトの設定を適切に行ってください。
- **レスポンスボディの非公開**: 認可サーバーのエラーレスポンスボディはログに出力しますが、gRPC クライアントへは返しません。内部情報の漏洩を防ぐためです。

## パッケージ構成

```text
policy_verification/
├── interceptor.go     # gRPC インターセプター本体
└── client/
    └── verifier.go    # 認可サーバーへの HTTP クライアント
```
