# request_tracking

[![Go Reference](https://pkg.go.dev/badge/github.com/o3co/grpc.authz/request_tracking.svg)](https://pkg.go.dev/github.com/o3co/grpc.authz/request_tracking)

[English](README.md)

`x-request-id` を管理する gRPC サーバーインターセプター。incoming metadata からリクエスト ID を抽出するか、存在しない場合は新規生成し、context に保存する。

## インストール

```bash
go get github.com/o3co/grpc.authz/request_tracking
```

## 使い方

### インターセプター

```go
import rt "github.com/o3co/grpc.authz/request_tracking"

grpc.NewServer(
    grpc.ChainUnaryInterceptor(
        rt.Interceptor(),
    ),
    grpc.ChainStreamInterceptor(
        rt.StreamInterceptor(),
    ),
)
```

### ハンドラーでリクエスト ID を取得

```go
func (s *server) GetPost(ctx context.Context, req *pb.GetPostRequest) (*pb.Post, error) {
    requestID := rt.RequestIDFromContext(ctx)
    // ...
}
```

### HTTP 伝播

下流の HTTP サービスを呼び出す際に context から ID を取得してヘッダーを設定する:

```go
if id := rt.RequestIDFromContext(ctx); id != "" {
    httpReq.Header.Set("x-request-id", id)
}
```

### policy_verification との併用

認可パイプラインと併用する場合は、最初のインターセプターとして追加する:

```go
grpc.NewServer(
    grpc.ChainUnaryInterceptor(
        rt.Interceptor(),                        // リクエストトラッキング（opt-in）
        policyoption.Interceptor(),              // ポリシー解決
        policyverification.Interceptor(verifier), // 認可チェック
    ),
)
```

## リクエスト ID フォーマット

生成される ID は `YYYYMMDDHHmmss_<uuid-v4-hex>` 形式（例: `20260331120000_550e8400e29b41d4a716446655440000`）。

クライアント（または上流のプロキシ）が gRPC metadata で `x-request-id` を提供した場合、インターセプターはそれをそのまま使用し、新規生成は行わない。

## ライセンス

Apache License 2.0
