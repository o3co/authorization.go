# authorization.go

gRPC サーバー向けの認可ミドルウェアライブラリです。Protobuf のカスタムメソッドオプションにポリシーを宣言し、インターセプターチェーンで自動的に認可チェックを行う仕組みを提供します。

## モジュール構成

```text
authorization.go/
├── protobuf_policy_option/   # Protobuf オプションからポリシーを解析し Context に注入
└── policy_verification/      # Context のポリシーを使って認可サーバーへ検証リクエストを送信
```

2 つのモジュールは独立した Go モジュールであり、責務を分離しています。

| モジュール | 責務 |
| --- | --- |
| `protobuf_policy_option` | `.proto` に書かれたポリシー定義を読み取り、リクエストフィールドからリソースを解決して `context.Context` に格納する |
| `policy_verification` | `context.Context` からポリシーを取り出し、外部の認可サーバーへ HTTP リクエストを送って許可/拒否を判定する |

## インターセプターチェーン

2 つのインターセプターを **この順番で** チェーンしてください。

```text
[1] protobuf_policy_option.Interceptor  →  ポリシーを解決して ctx に注入
[2] policy_verification.Interceptor     →  ctx のポリシーを使って認可チェック
```

順序が逆だと `policy_verification.Interceptor` はポリシーを取得できず、認可チェックがスキップされます。

```go
import (
    "log/slog"
    policyoption       "github.com/o3co/authorization.go/protobuf_policy_option"
    policyverification "github.com/o3co/authorization.go/policy_verification"
    pvclient           "github.com/o3co/authorization.go/policy_verification/client"
)

verifier, err := pvclient.NewVerifierClient(
    httpClient,
    "http://auth-service/",
    pvclient.WithLogLevel(slog.LevelError), // デフォルト: LevelError
)
if err != nil { ... }

grpc.NewServer(
    grpc.ChainUnaryInterceptor(
        policyoption.Interceptor(
            policyoption.WithLogLevel(slog.LevelError), // デフォルト: LevelError
        ),
        policyverification.Interceptor(verifier,
            policyverification.WithLogLevel(slog.LevelError), // デフォルト: LevelError
        ),
    ),
)
```

## ライセンス

MIT
