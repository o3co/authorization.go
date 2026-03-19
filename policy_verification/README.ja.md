# policy_verification

`policy_verification` は `context.Context` に格納された解決済み認可ポリシーを読み取り、外部認可サーバーを呼び出して適用する gRPC サーバーインターセプターモジュールです。`grpc.authz` を構成する 2 つのモジュールのうちの 1 つで、適用（enforcement）側を担います。ポリシーの宣言と解決は `protobuf_policy_option` が担当します。

## パブリック API

### Interceptor

```go
func Interceptor(verifierEndpoint endpoint.VerifierEndpoint, opts ...Option) grpc.UnaryServerInterceptor
```

各 RPC 呼び出しに対して以下を行う Unary サーバーインターセプターを返します。

1. `x-request-id` を取得または生成し、context に格納する。
2. `protobuf_policy_option.Interceptor` が実行済みかを確認する（未登録の場合は `codes.Internal` を返す）。
3. context から解決済みポリシーを読み取る。ポリシーが設定されていない場合（メソッドに `(o3.policy)` オプションがない場合）はそのまま通過する。
4. `verifierEndpoint.Verify(ctx, resource, action)` を呼び出し、エラーがあればそのまま返す。

### StreamInterceptor

```go
func StreamInterceptor(verifierEndpoint endpoint.VerifierEndpoint, opts ...Option) grpc.StreamServerInterceptor
```

`Interceptor` と同じチェーンガード・通過ロジックを持つストリームサーバーインターセプターを返します。ポリシーが存在する場合は、ストリームオープン時ではなく **`RecvMsg` が呼ばれるたびに** `Verify` を呼び出すようにストリームをラップします。

### WithLogLevel

```go
func WithLogLevel(level slog.Level) Option
```

インターセプターのログレベルを設定します。デフォルト: `slog.LevelError`。

### endpoint パッケージ

```go
import "github.com/o3co/grpc.authz/policy_verification/endpoint"
```

| シンボル | 説明 |
| --- | --- |
| `VerifierEndpoint` | `Verify(ctx context.Context, resource, action string) error` を持つインターフェース |
| `NewRESTEndpoint(baseURL string, opts ...Option) (VerifierEndpoint, error)` | `<baseURL>/verify` へ POST する REST 実装を作成する |
| `WithTimeout(d time.Duration) Option` | HTTP クライアントのタイムアウト（デフォルト: 10s） |
| `WithLogLevel(level slog.Level) Option` | エンドポイントのログレベル（デフォルト: `slog.LevelError`） |
| `WithMaxResponseBodySize(size int64) Option` | レスポンスボディの最大読み取りサイズ（デフォルト: 1 MB） |
| `WithRequestID(ctx, id) context.Context` | x-request-id を context に格納する（内部使用） |
| `RequestIDFromContext(ctx) string` | context から x-request-id を取得する |

## endpointtest パッケージ

`endpointtest` パッケージはテスト用のモック `VerifierEndpoint` 実装を提供します。テストファイルからのみインポートしてください。

```go
import "github.com/o3co/grpc.authz/policy_verification/endpointtest"
```

| 関数 | 説明 |
| --- | --- |
| `Allow() VerifierEndpoint` | 常に nil（許可）を返す |
| `Deny() VerifierEndpoint` | 常に `codes.PermissionDenied` を返す |
| `Func(fn) VerifierEndpoint` | `Verify` のたびに `fn` を呼び出す |
| `CtxWithBearerToken(ctx, token) context.Context` | gRPC incoming metadata に `Authorization: Bearer <token>` を注入する |
| `CtxWithRequestID(ctx, id) context.Context` | gRPC incoming metadata に `x-request-id` を注入する |
| `AssertGRPCCode(t, err, code)` | `err` が指定コードの gRPC ステータスエラーであることをアサートする |

## 使い方

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
    pvendpoint.WithMaxResponseBodySize(512 * 1024),
    pvendpoint.WithLogLevel(slog.LevelWarn),
)
if err != nil { /* エラー処理 */ }

grpc.NewServer(
    grpc.ChainUnaryInterceptor(
        policyoption.Interceptor(),              // 必ず先頭に置く
        policyverification.Interceptor(verifier),
    ),
    grpc.ChainStreamInterceptor(
        policyoption.StreamInterceptor(),        // 必ず先頭に置く
        policyverification.StreamInterceptor(verifier),
    ),
)
```

カスタムの認可バックエンドを使用するには `VerifierEndpoint` を実装してください。

```go
import "github.com/o3co/grpc.authz/policy_verification/endpoint"

type myVerifier struct{}

func (v *myVerifier) Verify(ctx context.Context, resource, action string) error {
    // カスタム認可ロジック
    return nil
}
```

完全なセットアップと proto オプションのリファレンスはルートの README を参照してください。
