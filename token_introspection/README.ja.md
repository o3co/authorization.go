# token_introspection

[![Go Reference](https://pkg.go.dev/badge/github.com/o3co/grpc.authz/token_introspection.svg)](https://pkg.go.dev/github.com/o3co/grpc.authz/token_introspection)

[English](README.md)

プラグイン可能なイントロスペクションバックエンドを介してクレデンシャルを検証する gRPC サーバーインターセプター。認証スキーム（Bearer、Basic など）によるディスパッチ、ストラテジーチェーン評価、オプションのキャッシュをサポートする。

## インストール

```bash
go get github.com/o3co/grpc.authz/token_introspection
```

## 使用方法

### 基本: Bearer + RFC 7662

```go
import ti "github.com/o3co/grpc.authz/token_introspection"

rfc7662, _ := ti.NewRFC7662Introspector(
    "http://auth.provider:3000/oauth/introspect",
)

grpc.NewServer(
    grpc.ChainUnaryInterceptor(
        ti.Interceptor(
            ti.WithIntrospector(rfc7662),
        ),
    ),
    grpc.ChainStreamInterceptor(
        ti.StreamInterceptor(
            ti.WithIntrospector(rfc7662),
        ),
    ),
)
```

### キャッシュ付き

```go
grpc.NewServer(
    grpc.ChainUnaryInterceptor(
        ti.Interceptor(
            ti.WithIntrospector(rfc7662),
            ti.WithCache(ti.NewInMemoryCache(30 * time.Second)),
        ),
    ),
)
```

キャッシュキーは `SHA-256(scheme + ":" + credential)`。全スキームで1つのキャッシュインスタンスを共有する。成功した結果のみキャッシュされ、エラーはキャッシュされない。

### ハンドラーでクレームにアクセスする

```go
func (s *server) GetPost(ctx context.Context, req *pb.GetPostRequest) (*pb.Post, error) {
    result := ti.ResultFromContext(ctx)
    if result != nil {
        log.Info("user", "subject", result.Subject, "scopes", result.Scopes)
    }
    // ...
}
```

### リクエスト ID をイントロスペクションエンドポイントに転送する

```go
import rt "github.com/o3co/grpc.authz/request_tracking"

rfc7662, _ := ti.NewRFC7662Introspector(
    "http://auth.provider:3000/oauth/introspect",
    ti.WithRequestIDFunc(rt.RequestIDFromContext),
)
```

### 複数ストラテジー（同一スキーム）

```go
rfc7662, _ := ti.NewRFC7662Introspector(url)
jwtLocal := NewJWTIntrospector(publicKey) // future

grpc.NewServer(
    grpc.ChainUnaryInterceptor(
        ti.Interceptor(
            ti.WithIntrospector(rfc7662),   // bearer: 1st
            ti.WithIntrospector(jwtLocal),  // bearer: 2nd (fallthrough)
        ),
    ),
)
```

ストラテジーチェーンの評価: `codes.Unauthenticated` → 次を試みる; それ以外のエラー → 中断; 成功 → 返す。

### 認可パイプラインと組み合わせる

```go
grpc.NewServer(
    grpc.ChainUnaryInterceptor(
        rt.Interceptor(),                        // [1] request tracking (opt-in)
        ti.Interceptor(                          // [2] credential validation (opt-in)
            ti.WithIntrospector(rfc7662),
            ti.WithCache(ti.NewInMemoryCache(30 * time.Second)),
        ),
        policyoption.Interceptor(),              // [3] policy resolution
        policyverification.Interceptor(verifier), // [4] authorization check
    ),
)
```

### パブリック API（認証不要）

`Authorization` ヘッダーのないリクエストはエラーなしで通過する。ハンドラーは `ResultFromContext` から `nil` を受け取る。パブリック/プライベート混在 API をサポートする。

## ライセンス

Apache License 2.0
