# protobuf_policy_option

gRPC メソッドの `.proto` ファイルにカスタムオプションでポリシーを宣言し、受信リクエストのフィールドからリソースを解決して `context.Context` に注入する gRPC サーバーインターセプターです。

## 目的

認可に必要な「リソース」と「アクション」をコードではなく `.proto` で宣言することで、認可ポリシーをサービス定義と一元管理します。インターセプターは Protobuf のリフレクション API を使ってリクエスト到着時にポリシーを解決し、後続のインターセプターが参照できるよう `context.Context` に格納します。

## 特性

- **宣言的ポリシー**: ポリシーは `.proto` のメソッドオプションとして記述。実装コードへの散在を防ぎます。
- **テンプレートによるリソース解決**: リソース文字列に `<field_name>` 形式のプレースホルダーを書くと、受信リクエストの対応フィールドの値で自動置換されます。
- **キャッシュ**: `protoregistry.GlobalFiles` のスキャンは初回のみ実行し、結果を `sync.Map` にキャッシュします。2 回目以降のリクエストはキャッシュから取得するため、スキャンのオーバーヘッドはありません。
- **proto3 ゼロ値対応**: `int64=0` や空文字列などのゼロ値フィールドも正しく抽出できます。

## インストール

```bash
go get github.com/o3co/authorization.go/protobuf_policy_option
```

## 使い方

### 1. `.proto` にポリシーを宣言する

```protobuf
import "policy.proto";

service ItemService {
  rpc GetItem(GetItemRequest) returns (GetItemResponse) {
    option (policy.v1.policy) = {
      resource: "items/<id>"
      action: "read"
      field_mappings: [{ placeholder: "id", request_field: "id" }]
    };
  }
}
```

| フィールド | 説明 |
| --- | --- |
| `resource` | リソース識別子テンプレート。`<placeholder>` 形式でリクエストフィールドの値を埋め込めます。 |
| `action` | 実行するアクション（例: `read`, `write`, `delete`）。 |
| `field_mappings` | プレースホルダーとリクエストフィールドのマッピング。`placeholder` がテンプレート内の名前、`request_field` がリクエストの proto フィールド名です。 |

### 2. インターセプターを登録する

```go
import policyoption "github.com/o3co/authorization.go/protobuf_policy_option"

grpc.NewServer(
    grpc.ChainUnaryInterceptor(
        policyoption.Interceptor, // 必ず先頭に置く
        // ... 他のインターセプター
    ),
)
```

### 3. 後続インターセプターでポリシーを取得する

```go
import policyoption "github.com/o3co/authorization.go/protobuf_policy_option"

policy, ok := policyoption.PolicyFromContext(ctx)
if ok {
    fmt.Println(policy.Resource, policy.Action)
}
```

## 注意点

- **インターセプターの順序**: このインターセプターは `policy_verification.Interceptor` より **前** に登録してください。後に登録すると認可チェックがスキップされます。
- **ポリシー未定義のメソッド**: `.proto` にポリシーオプションが設定されていないメソッドはそのまま素通りします。認可が必要なメソッドには必ずオプションを設定してください。
- **対応フィールド型**: スカラー型（`string`, `bytes`, `int32/64`, `uint32/64`, `bool`）のみ対応しています。`repeated` フィールドや `map` フィールド、ネストしたメッセージ型はサポートしていません。
- **`field_mappings` のバリデーション**: `placeholder` または `request_field` が空文字の場合はリクエスト処理が `Internal` エラーで終了します。

## パッケージ構成

```text
protobuf_policy_option/
├── interceptor.go         # gRPC インターセプター本体
└── schema/
    ├── policy.proto       # Policy / FieldMapping メッセージ定義
    └── policy.pb.go       # protoc-gen-go 生成コード
```
