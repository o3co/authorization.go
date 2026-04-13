# grpc.authz

[![CI](https://github.com/o3co/grpc.authz/actions/workflows/ci.yml/badge.svg)](https://github.com/o3co/grpc.authz/actions/workflows/ci.yml)
[![Go Reference (policy_verification)](https://pkg.go.dev/badge/github.com/o3co/grpc.authz/policy_verification.svg)](https://pkg.go.dev/github.com/o3co/grpc.authz/policy_verification)
[![Go Reference (protobuf_policy_option)](https://pkg.go.dev/badge/github.com/o3co/grpc.authz/protobuf_policy_option.svg)](https://pkg.go.dev/github.com/o3co/grpc.authz/protobuf_policy_option)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

[English](README.md)

`grpc.authz` は Go 向けの gRPC 認可ミドルウェアライブラリ。`.proto` のメソッドオプションにアクセスポリシー（リソース + アクション）を宣言し、インターセプターで適用。`protobuf_policy_option`（ポリシー宣言・解決）と `policy_verification`（認可バックエンドへの適用）の2つの独立モジュールで構成。

## なぜ必要か

認可ポリシーがハンドラのコードに散在すると、API コントラクトから乖離する。レビューで見落とされ、リファクタで壊れ、RPC ごとにボイラープレートが必要になる。ポリシーを `.proto` のメソッド定義に同居させることで、API 設計と同じ場所でルールが見え、コードレビューで監査でき、実行時に自動適用される。

## 動作の仕組み

```text
gRPC リクエスト
     │
     ▼
┌─────────────────────────────────────────┐
│  protobuf_policy_option.Interceptor     │  .proto の (o3co.authz.v1.policy) オプションを読み取り、
│                                         │  field_mappings でリクエストから値を解決し、
│                                         │  Policy{Resource, Action} を ctx に注入
└──────────────────┬──────────────────────┘
                   │ ctx がポリシーを運搬
                   ▼
┌─────────────────────────────────────────┐
│  policy_verification.Interceptor        │  ctx からポリシーを読み取り、
│                                         │  認可サーバーに POST /verify し、
│                                         │  HTTP ステータスを gRPC ステータスに変換
└──────────────────┬──────────────────────┘
                   │
                   ▼
             handler（あなたのコード）
```

2 つのモジュールは独立した Go モジュールで、責務を意図的に分離しています：

| モジュール | 責務 |
| --- | --- |
| `protobuf_policy_option` | proto レジストリから `(o3co.authz.v1.policy)` メソッドオプションを読み取り、`<placeholder>` トークンをリクエストフィールドで解決し、結果を `context.Context` に格納 |
| `policy_verification` | コンテキストから解決済みポリシーを読み取り、外部認可サーバーに `POST /verify` で問い合わせ、HTTP レスポンスを適切な gRPC ステータスコードに変換 |

分離することで、ポリシー宣言レイヤーに触れずに認可バックエンド（REST の代わりに gRPC など）を差し替え可能で、各関心事を独立してテストできます。

## インターセプタチェーン

2 つのインターセプタは必ずこの順序でチェーンしてください：

```text
[1] protobuf_policy_option.Interceptor   →  ポリシーを ctx に解決
[2] policy_verification.Interceptor      →  ctx からポリシーを読み取り検証
```

順序が逆、または `protobuf_policy_option.Interceptor` が未登録の場合、`policy_verification.Interceptor` は全リクエストで `codes.Internal`（`protobuf_policy_option.Interceptor is not registered in the interceptor chain`）を返します。

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
if err != nil { /* handle */ }

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

認可が必要なメソッドに `(o3co.authz.v1.policy)` オプションを宣言します：

```proto
syntax = "proto3";

import "policy.proto";  // (o3co.authz.v1.policy) 拡張を提供

service PostService {

  // 静的リソース — フィールド抽出不要
  rpc ListPosts(ListPostsRequest) returns (ListPostsResponse) {
    option (o3co.authz.v1.policy) = {
      resource: "posts"   // /verify に送信されるリソース識別子
      action: "list"      // /verify に送信されるアクション文字列
    };
  }

  // 動的リソース — リクエストフィールドからプレースホルダーを解決
  rpc GetPost(GetPostRequest) returns (GetPostResponse) {
    option (o3co.authz.v1.policy) = {
      resource: "posts/<id>"          // <id> は実行時に置換
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

`field_mappings` はスカラー型の proto フィールドをサポートします：`string`、`bytes`、`int32/64`、`uint32/64`、`bool`。`repeated` フィールド、`map` フィールド、ネストされたメッセージはサポートしていません。

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

    // 生成された proto パッケージ
    postv1 "example.com/myapp/gen/post/v1"
)

func main() {
    verifier, err := pvendpoint.NewRESTEndpoint(
        "http://auth-service/",
        pvendpoint.WithTimeout(5 * time.Second),
    )
    if err != nil {
        log.Fatalf("failed to create verifier: %v", err)
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
        log.Fatalf("listen: %v", err)
    }
    log.Fatal(srv.Serve(lis))
}
```

`(o3co.authz.v1.policy)` オプションのないメソッドは、認可チェックなしでそのまま通過します。

## ストリーミング RPC

`Interceptor` と同じチェーン順序で `StreamInterceptor` を使用します：

```go
grpc.ChainStreamInterceptor(
    policyoption.StreamInterceptor(),
    policyverification.StreamInterceptor(verifier),
)
```

ストリーミング RPC では、認可はストリーム開始時だけでなく**毎回の `RecvMsg` 呼び出し時**にチェックされます。ストリーム中にトークンが失効した場合、次のメッセージで拒否されます。

**ストリーミング RPC では `field_mappings` はサポートされていません。** ストリーム確立時にリクエストメッセージが利用できないため、プレースホルダー付きのリソーステンプレートは解決できません。ストリーミングメソッドの proto オプションに `field_mappings` が含まれている場合、インターセプタは `codes.Internal` を返します。代わりに静的リソース文字列を使用してください：

```proto
rpc WatchPosts(WatchPostsRequest) returns (stream Post) {
  option (o3co.authz.v1.policy) = {
    resource: "posts"   // 静的 — field_mappings なし
    action: "watch"
  };
}
```

## サービスのテスト

`endpointtest` パッケージは、テスト用のモック `VerifierEndpoint` 実装を提供します。テストファイルからのみインポートしてください。

```go
import "github.com/o3co/grpc.authz/policy_verification/endpointtest"
```

```go
// 常に許可 — 正常パスのテスト用
verifier := endpointtest.Allow()

// 常に拒否（codes.PermissionDenied） — アクセス拒否動作のテスト用
verifier := endpointtest.Deny()

// カスタムロジック — テストでリソースとアクションを検査
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

// 決定論的なテストアサーションのために既知の x-request-id を注入
ctx = endpointtest.CtxWithRequestID(ctx, "test-request-id")
```

エラーの gRPC ステータスコードをアサート：

```go
// err が codes.PermissionDenied であることをアサート
endpointtest.AssertGRPCCode(t, err, codes.PermissionDenied)
```

## 認可サーバーコントラクト

`policy_verification` モジュールは RPC 呼び出しごとに 1 つの HTTP リクエストを送信します（ストリーミングでは `RecvMsg` ごと）：

```http
POST /verify
Content-Type: application/json
Authorization: Bearer <gRPC incoming metadata から転送されたトークン>
x-request-id: 20260318120530_a1b2c3d4e5f6...

{"resource": "posts/123", "action": "read"}
```

ヘッダー転送ルール：

- `Authorization`：必須。gRPC `authorization` メタデータからそのまま転送。不在の場合、インターセプタはリクエスト送信前に `codes.Unauthenticated` を返します。
- `x-request-id`：gRPC メタデータに存在する場合に転送。不在の場合、`YYYYMMDDHHmmss_<32文字の16進数>` 形式で新しい ID が生成されます。

レスポンス → gRPC ステータスコードのマッピング：

| HTTP レスポンス | gRPC ステータスコード |
| --- | --- |
| `2xx` | `codes.OK`（リクエスト続行） |
| `401` | `codes.Unauthenticated` |
| `403` | `codes.PermissionDenied` |
| その他 | `codes.Internal` |

認可サーバーのレスポンスボディは gRPC クライアントに転送されません（内部情報の漏洩防止のため）。デバッグ用にエラーレベルでログ出力されます（最大 1 KB）。

## 代替バックエンド

`policy_verification` モジュールは `VerifierEndpoint` インターフェースを実装する任意の認可バックエンドで動作します。組み込みアダプタ：

### 静的ルール（外部サービス不要）

```go
import pvendpoint "github.com/o3co/grpc.authz/policy_verification/endpoint"

verifier := pvendpoint.NewStaticEndpoint([]pvendpoint.StaticRule{
    {Resource: "posts", Action: "list"},
    {Resource: "posts/*", Action: "read"},      // プレフィックスワイルドカード
    {Resource: "users", Action: "*"},            // 任意のアクション
})
```

ルールはローカルで評価されます。外部サービスは不要です。完全一致、`*`（全マッチ）、プレフィックスワイルドカード（`posts/*`）をサポートします。開発環境、シンプルなデプロイ、または OPA や Cedar 導入前の出発点として便利です。

### Open Policy Agent (OPA)

```go
import (
    "time"
    pvendpoint "github.com/o3co/grpc.authz/policy_verification/endpoint"
)

verifier, err := pvendpoint.NewOPAEndpoint(
    "http://opa:8181",       // OPA サーバー URL
    "authz/allow",           // Rego パッケージ/ルールパス
    pvendpoint.WithOPATimeout(5 * time.Second),
)
```

OPA はベアラートークン、リソース、アクションを `input` オブジェクトで受け取ります：

```json
{"input": {"resource": "posts/123", "action": "read", "token": "<bearer>"}}
```

`allow` を `true` または `false` に評価する Rego ポリシーを記述してください。

### Cedar agent (permitio/cedar-agent)

```go
import "context"

verifier, err := pvendpoint.NewCedarAgentEndpoint(
    "http://cedar-agent:8180",
    pvendpoint.WithCedarAgentPrincipalPrefix("User"),
    pvendpoint.WithCedarAgentPrincipalResolver(func(ctx context.Context, token string) string {
        // JWT からサブジェクトを抽出するか、トークンをそのまま返す
        return token
    }),
)
```

Cedar agent は Cedar エンティティ UID を受け取ります：

```json
{"principal": "User::\"subject\"", "action": "Action::\"read\"", "resource": "Resource::\"posts/123\""}
```

### カスタムバックエンド

他の認可システム用に `endpoint.VerifierEndpoint` を実装できます：

```go
type VerifierEndpoint interface {
    Verify(ctx context.Context, resource, action string) error
}
```

許可の場合は `nil`、拒否の場合は `status.Error(codes.PermissionDenied, ...)`、認証情報不足の場合は `status.Error(codes.Unauthenticated, ...)` を返してください。

## ライセンス

Apache 2.0
