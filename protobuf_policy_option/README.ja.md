# protobuf_policy_option

`protobuf_policy_option` は protobuf レジストリから `(o3.policy)` カスタムメソッドオプションを読み取り、リソース文字列内の `<placeholder>` トークンを受信リクエストのフィールドで解決し、結果を後続インターセプターが参照できるよう `context.Context` に格納する gRPC サーバーインターセプターモジュールです。`grpc.authz` を構成する 2 つのモジュールのうちの 1 つで、ポリシーの宣言と解決側を担います。適用（enforcement）側は `policy_verification` が担当します。

## パブリック API

### Interceptor

```go
func Interceptor(opts ...Option) grpc.UnaryServerInterceptor
```

各 RPC 呼び出しに対して以下を行う Unary サーバーインターセプターを返します。

1. proto レジストリから `(o3.policy)` メソッドオプションを検索する（初回以降はインターセプターインスタンスごとにキャッシュ）。
2. `policy_verification.Interceptor` がチェーン設定ミスを検出できるよう、自身が実行されたことを context にマークする。
3. ポリシーオプションが見つからない場合は次のハンドラーへ通過する。
4. `field_mappings` とリクエストフィールドを使ってリソース文字列内の `<placeholder>` トークンを解決する。
5. 解決済みの `Policy{Resource, Action}` を context に格納する。

### StreamInterceptor

```go
func StreamInterceptor(opts ...Option) grpc.StreamServerInterceptor
```

`Interceptor` と同じポリシー検索・コンテキスト注入ロジックを持つストリームサーバーインターセプターを返します。ストリーミング RPC では `field_mappings` は**非対応**です。メソッドのオプションに `field_mappings` が含まれている場合は `codes.Internal` でストリームを拒否します。ストリーミングメソッドには静的なリソース文字列を使用してください。

### WithLogLevel

```go
func WithLogLevel(level slog.Level) Option
```

インターセプターのログレベルを設定します。デフォルト: `slog.LevelError`。

### コンテキストヘルパー

```go
func PolicyFromContext(ctx context.Context) (*Policy, bool)
```

`Interceptor` が格納した解決済みポリシーと、ポリシーが存在したかどうかを示す bool 値を返します。解決されたリソースやアクションを参照する必要があるカスタムインターセプターやミドルウェアから使用します。

```go
func InterceptorRanFromContext(ctx context.Context) bool
```

この context で `protobuf_policy_option.Interceptor`（または `StreamInterceptor`）がすでに実行されている場合に true を返します。インターセプターチェーンの設定ミスを検出するために `policy_verification` が内部的に使用します。

### Policy 型

```go
type Policy struct {
    Resource string
    Action   string
}
```

## Proto のセットアップ

`(o3.policy)` メソッドオプション拡張を使うには、`.proto` ファイルに `policy.proto`（`schema` サブパッケージ）をインポートしてください。

```proto
syntax = "proto3";

import "policy.proto";

service ItemService {
  rpc GetItem(GetItemRequest) returns (GetItemResponse) {
    option (o3.policy) = {
      resource: "items/<id>"
      action:   "read"
      field_mappings: [
        { placeholder: "id", request_field: "id" }
      ]
    };
  }
}
```

このオプションは `policy.v1` protobuf パッケージにフィールド番号 `50000` で定義されています。

## field_mappings の説明

`field_mappings` はリソーステンプレート内のプレースホルダー名と、リクエストメッセージの proto フィールド名のマッピングです。

| field_mappings フィールド | 意味 |
| --- | --- |
| `placeholder` | リソーステンプレート内で使用する名前。`<name>` の形式で記述します（例: `"id"` は `<id>` に対応） |
| `request_field` | 値を取り出すリクエストメッセージの proto フィールド名 |

サポートするスカラーフィールド型: `string`, `bytes`, `int32/64`, `uint32/64`, `bool`。`repeated` フィールド、`map` フィールド、ネストしたメッセージ型は非対応です。非対応の型や存在しないフィールドが指定された場合、インターセプターは `codes.Internal` を返します。

例: `resource: "items/<id>"` かつ `field_mappings: [{ placeholder: "id", request_field: "id" }]` で、リクエストの `id` が `"42"` の場合、解決後のリソースは `"items/42"` になります。

## 使い方

```go
import (
    "log/slog"
    policyoption "github.com/o3co/grpc.authz/protobuf_policy_option"
)

grpc.NewServer(
    grpc.ChainUnaryInterceptor(
        policyoption.Interceptor(               // policy_verification より前に置く
            policyoption.WithLogLevel(slog.LevelWarn),
        ),
        // ... 他のインターセプター
    ),
    grpc.ChainStreamInterceptor(
        policyoption.StreamInterceptor(         // policy_verification より前に置く
            policyoption.WithLogLevel(slog.LevelWarn),
        ),
        // ... 他のインターセプター
    ),
)
```

カスタムインターセプターで解決済みポリシーを参照する：

```go
import policyoption "github.com/o3co/grpc.authz/protobuf_policy_option"

policy, ok := policyoption.PolicyFromContext(ctx)
if ok {
    fmt.Println(policy.Resource, policy.Action)
}
```

完全なセットアップとインターセプターチェーンの要件はルートの README を参照してください。
