# grpc.authz

`grpc.authz` は Go 向けの gRPC 認可ミドルウェアライブラリです。`.proto` のメソッドオプションにアクセスポリシー（リソース + アクション）を直接宣言し、インターセプター経由で自動的に認可チェックを行います。ハンドラーにチェックを手書きする必要はありません。

## なぜ grpc.authz を使うのか

認可ポリシーがコードの中に散らばると、API 仕様から乖離しやすくなります。レビューで見逃しやすく、リファクタ時に壊れやすく、新しい RPC を追加するたびに定型コードが必要になります。ポリシーを `.proto` のメソッド定義に同居させることで、ルールは API 設計と同じ場所に可視化され、コードレビューで監査しやすく、ランタイムで自動的に適用されます。

`grpc.authz` は意図的に軽量に設計されています。すでに protobuf を使っているチームは OPA や Casbin ほどの重厚な仕組みを必要とせず、「誰がどのリソースに対して何をできるか」を宣言し、確実に適用できる仕組みだけで十分なことが多いです。

## 仕組み

```text
gRPC リクエスト
     │
     ▼
┌─────────────────────────────────────────┐
│  protobuf_policy_option.Interceptor     │  .proto の (o3.policy) オプションを読み取り、
│                                         │  field_mappings でリソースを解決し、
│                                         │  Policy{Resource, Action} を ctx に注入
└──────────────────┬──────────────────────┘
                   │ ctx に解決済みポリシーを格納
                   ▼
┌─────────────────────────────────────────┐
│  policy_verification.Interceptor        │  ctx からポリシーを取り出し、
│                                         │  認可サーバーの /verify へ POST し、
│                                         │  HTTP ステータス → gRPC ステータスコードへ変換
└──────────────────┬──────────────────────┘
                   │
                   ▼
          ハンドラー（アプリコード）
```

2 つのモジュールは独立した Go モジュールであり、責務を明確に分離しています。

| モジュール | 責務 |
| --- | --- |
| `protobuf_policy_option` | proto レジストリから `(o3.policy)` メソッドオプションを読み取り、リクエストフィールドで `<placeholder>` を解決し、結果を `context.Context` に格納する |
| `policy_verification` | context から解決済みポリシーを読み取り、外部認可サーバーへ `POST /verify` を送り、HTTP レスポンスを適切な gRPC ステータスコードに変換する |

モジュールを分離することで、認可バックエンド（REST の代わりに gRPC など）を差し替えてもポリシー宣言層には影響がなく、それぞれを独立してユニットテストできます。

## インターセプターチェーン

2 つのインターセプターは **必ずこの順番で** チェーンしてください。

```text
[1] protobuf_policy_option.Interceptor   →  ポリシーを ctx に注入
[2] policy_verification.Interceptor      →  ctx のポリシーを読み取って検証
```

順序が逆になっているか `protobuf_policy_option.Interceptor` が未登録の場合、`policy_verification.Interceptor` はすべてのリクエストに対して `codes.Internal` (`protobuf_policy_option.Interceptor is not registered in the interceptor chain`) を返します。

```go
import (
    "log/slog"
    "time"
    policyoption       "github.com/o3co/grpc.authz/protobuf_policy_option"
    policyverification "github.com/o3co/grpc.authz/policy_verification"
    pvendpoint         "github.com/o3co/grpc.authz/policy_verification/endpoint"
)

verifier, err := pvendpoint.NewRESTEndpoint(
    "http://auth-service/",
    pvendpoint.WithTimeout(5 * time.Second),
    pvendpoint.WithLogLevel(slog.LevelError),
)
if err != nil { /* エラー処理 */ }

grpc.NewServer(
    grpc.ChainUnaryInterceptor(
        policyoption.Interceptor(
            policyoption.WithLogLevel(slog.LevelError),
        ),
        policyverification.Interceptor(verifier,
            policyverification.WithLogLevel(slog.LevelError),
        ),
    ),
    grpc.ChainStreamInterceptor(
        policyoption.StreamInterceptor(
            policyoption.WithLogLevel(slog.LevelError),
        ),
        policyverification.StreamInterceptor(verifier,
            policyverification.WithLogLevel(slog.LevelError),
        ),
    ),
)
```

## Proto オプションリファレンス

認可が必要なメソッドには `(o3.policy)` オプションを宣言します。

```proto
syntax = "proto3";

import "policy.proto";  // (o3.policy) 拡張を提供

service PostService {

  // 静的リソース — フィールド抽出不要
  rpc ListPosts(ListPostsRequest) returns (ListPostsResponse) {
    option (o3.policy) = {
      resource: "posts"   // /verify に送るリソース識別子（リテラル）
      action: "list"      // /verify に送るアクション文字列
    };
  }

  // 動的リソース — リクエストフィールドからプレースホルダーを解決
  rpc GetPost(GetPostRequest) returns (GetPostResponse) {
    option (o3.policy) = {
      resource: "posts/<id>"          // <id> はランタイムで置換される
      action: "read"
      field_mappings: [
        { placeholder: "id", request_field: "id" }
        // placeholder: リソーステンプレート内の名前（山括弧なし）
        // request_field: リクエストメッセージの proto フィールド名
      ]
    };
  }
}
```

`field_mappings` がサポートするフィールド型はスカラー型のみです（`string`, `bytes`, `int32/64`, `uint32/64`, `bool`）。`repeated` フィールド、`map` フィールド、ネストしたメッセージ型は非対応です。

## クイックスタート（Unary）

```go
package main

import (
    "log"
    "log/slog"
    "net"
    "time"

    "google.golang.org/grpc"

    policyoption       "github.com/o3co/grpc.authz/protobuf_policy_option"
    policyverification "github.com/o3co/grpc.authz/policy_verification"
    pvendpoint         "github.com/o3co/grpc.authz/policy_verification/endpoint"

    // 生成した proto パッケージ
    postv1 "example.com/myapp/gen/post/v1"
)

func main() {
    verifier, err := pvendpoint.NewRESTEndpoint(
        "http://auth-service/",
        pvendpoint.WithTimeout(5 * time.Second),
    )
    if err != nil {
        log.Fatalf("verifier の作成に失敗しました: %v", err)
    }

    srv := grpc.NewServer(
        grpc.ChainUnaryInterceptor(
            policyoption.Interceptor(
                policyoption.WithLogLevel(slog.LevelWarn),
            ),
            policyverification.Interceptor(verifier),
        ),
    )

    postv1.RegisterPostServiceServer(srv, &postServiceServer{})

    lis, err := net.Listen("tcp", ":50051")
    if err != nil {
        log.Fatalf("listen に失敗しました: %v", err)
    }
    log.Fatal(srv.Serve(lis))
}
```

`(o3.policy)` オプションが設定されていないメソッドは認可チェックなしで素通りします。

## Streaming RPC

`StreamInterceptor` も同じ順番でチェーンに追加してください。

```go
grpc.ChainStreamInterceptor(
    policyoption.StreamInterceptor(),
    policyverification.StreamInterceptor(verifier),
)
```

ストリーミング RPC では、ストリームオープン時ではなく **`RecvMsg` が呼ばれるたびに** 認可チェックが行われます。これにより、ストリーム中にトークンが失効した場合も次のメッセージ受信時に拒否され、古い認証情報でストリーム全体が通過することを防ぎます。

**`field_mappings` はストリーミング RPC では使用できません。** ストリーム確立時にリクエストメッセージが利用できないため、プレースホルダーを含むリソーステンプレートを解決できません。ストリーミングメソッドの proto オプションに `field_mappings` が含まれている場合、インターセプターは `codes.Internal` を返します。代わりに静的なリソース文字列を使用してください。

```proto
rpc WatchPosts(WatchPostsRequest) returns (stream Post) {
  option (o3.policy) = {
    resource: "posts"   // 静的 — field_mappings なし
    action: "watch"
  };
}
```

## サービスのテスト

`endpointtest` パッケージはテスト用のモック `VerifierEndpoint` 実装を提供します。テストファイルからのみインポートしてください。

```go
import "github.com/o3co/grpc.authz/policy_verification/endpointtest"
```

```go
// 常に許可 — 正常系のテストに使用
verifier := endpointtest.Allow()

// 常に codes.PermissionDenied で拒否 — アクセス拒否のテストに使用
verifier := endpointtest.Deny()

// カスタムロジック — テスト内でリソースとアクションを検査
verifier := endpointtest.Func(func(ctx context.Context, resource, action string) error {
    if resource == "posts/123" && action == "read" {
        return nil
    }
    return status.Error(codes.PermissionDenied, "access denied")
})
```

テスト用コンテキストを構築するヘルパー関数：

```go
// gRPC incoming metadata に "Authorization: Bearer <token>" を注入
ctx = endpointtest.CtxWithBearerToken(ctx, "my-token")

// 決定論的なテストアサーションのために x-request-id を注入
ctx = endpointtest.CtxWithRequestID(ctx, "test-request-id")
```

エラーの gRPC ステータスコードをアサートする：

```go
// err が codes.PermissionDenied であることを検証
endpointtest.AssertGRPCCode(t, err, codes.PermissionDenied)
```

## 認可サーバーの仕様

`policy_verification` モジュールは RPC 呼び出しごとに 1 回（ストリーミングでは `RecvMsg` ごとに 1 回）HTTP リクエストを送信します。

```http
POST /verify
Content-Type: application/json
Authorization: Bearer <gRPC incoming metadata から転送したトークン>
x-request-id: 20260318120530_a1b2c3d4e5f6...

{"resource": "posts/123", "action": "read"}
```

ヘッダー転送ルール：

- `Authorization`：必須。gRPC の `authorization` メタデータからそのまま転送します。存在しない場合、リクエスト送信前にインターセプターが `codes.Unauthenticated` を返します。
- `x-request-id`：gRPC メタデータに存在する場合に転送します。存在しない場合は `YYYYMMDDHHmmss_<uuid-v4>` 形式で新たに生成されます。

レスポンス → gRPC ステータスコードのマッピング：

| HTTP レスポンス | gRPC ステータスコード |
| --- | --- |
| `2xx` | `codes.OK`（リクエスト続行） |
| `401` | `codes.Unauthenticated` |
| `403` | `codes.PermissionDenied` |
| その他 | `codes.Internal` |

認可サーバーのレスポンスボディは gRPC クライアントには返されません。内部情報漏洩を防ぐため、デバッグ用にエラーレベルでログ出力（最大 1 KB）されます。

## ライセンス

Apache 2.0
