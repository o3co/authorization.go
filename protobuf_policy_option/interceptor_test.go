// Copyright 2026 o3co Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package policyoption

import (
	"context"
	"log/slog"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/descriptorpb"

	pb "github.com/o3co/grpc.authz/protobuf_policy_option/schema"
)

// --- parseFullMethodName ---

// gRPC フルメソッド名（/pkg.Service/Method 形式）を Service と Method に分解できることを確認する。
func TestParseFullMethodName(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantService string
		wantMethod  string
		wantErr     bool
	}{
		{
			// パッケージ付きの正規フォーマット
			name:        "valid full path",
			input:       "/sample.v1.SampleService/SearchSamples",
			wantService: "sample.v1.SampleService",
			wantMethod:  "SearchSamples",
		},
		{
			// パッケージなし（サービス名のみ）でも動作する
			name:        "simple service without package",
			input:       "/MyService/MyMethod",
			wantService: "MyService",
			wantMethod:  "MyMethod",
		},
		{
			// 空文字列はエラー
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			// 先頭の "/" がない場合はエラー
			name:    "no leading slash",
			input:   "MyService/MyMethod",
			wantErr: true,
		},
		{
			// "/" のみでメソッド区切りなしはエラー
			name:    "only slash",
			input:   "/",
			wantErr: true,
		},
		{
			// メソッド名区切り（2つ目の "/"）がない場合はエラー
			name:    "no method name separator",
			input:   "/MyService",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseFullMethodName(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Service != tc.wantService {
				t.Errorf("Service = %q, want %q", got.Service, tc.wantService)
			}
			if got.Method != tc.wantMethod {
				t.Errorf("Method = %q, want %q", got.Method, tc.wantMethod)
			}
		})
	}
}

// --- extractFieldFromRequest ---

// proto.Message のフィールドをリフレクションで取得し文字列に変換できることを確認する。
// サポートする型（string, int32, bool）と非サポート型（list, enum）、
// および proto.Message 以外の型や存在しないフィールドのエラーを検証する。
func TestExtractFieldFromRequest(t *testing.T) {
	log := newLogger(slog.LevelError)

	t.Run("string field", func(t *testing.T) {
		// string フィールドはそのまま返る
		req := &pb.Policy{Resource: "posts/123"}
		got, err := extractFieldFromRequest(log, req, "resource")
		if err != nil {
			t.Fatal(err)
		}
		if got != "posts/123" {
			t.Errorf("got %q, want %q", got, "posts/123")
		}
	})

	t.Run("empty string field", func(t *testing.T) {
		// ゼロ値の string フィールドは空文字を返す（エラーにならない）
		req := &pb.Policy{Resource: ""}
		got, err := extractFieldFromRequest(log, req, "resource")
		if err != nil {
			t.Fatal(err)
		}
		if got != "" {
			t.Errorf("got %q, want empty string", got)
		}
	})

	t.Run("int32 field", func(t *testing.T) {
		// int32 フィールドは十進数文字列に変換される
		num := int32(42)
		req := &descriptorpb.FieldDescriptorProto{Number: &num}
		got, err := extractFieldFromRequest(log, req, "number")
		if err != nil {
			t.Fatal(err)
		}
		if got != "42" {
			t.Errorf("got %q, want %q", got, "42")
		}
	})

	t.Run("bool field", func(t *testing.T) {
		// bool フィールドは "true"/"false" 文字列に変換される
		b := true
		req := &descriptorpb.FieldDescriptorProto{Proto3Optional: &b}
		got, err := extractFieldFromRequest(log, req, "proto3_optional")
		if err != nil {
			t.Fatal(err)
		}
		if got != "true" {
			t.Errorf("got %q, want %q", got, "true")
		}
	})

	t.Run("list field is unsupported", func(t *testing.T) {
		// repeated フィールドはリソーステンプレートへの埋め込みに不適切なためエラー
		req := &pb.Policy{
			FieldMappings: []*pb.FieldMapping{{Placeholder: "x"}},
		}
		_, err := extractFieldFromRequest(log, req, "field_mappings")
		if err == nil {
			t.Error("expected error for list field")
		}
	})

	t.Run("enum field is unsupported", func(t *testing.T) {
		// enum フィールドは数値/名前のどちらで返すか曖昧なため非サポート
		typ := descriptorpb.FieldDescriptorProto_TYPE_STRING
		req := &descriptorpb.FieldDescriptorProto{Type: &typ}
		_, err := extractFieldFromRequest(log, req, "type")
		if err == nil {
			t.Error("expected error for enum field")
		}
	})

	t.Run("field not found", func(t *testing.T) {
		// proto スキーマに存在しないフィールド名はエラー
		req := &pb.Policy{Resource: "x"}
		_, err := extractFieldFromRequest(log, req, "nonexistent_field")
		if err == nil {
			t.Error("expected error for missing field")
		}
	})

	t.Run("not a proto.Message", func(t *testing.T) {
		// proto.Message を実装していない型はリフレクションを使えないためエラー
		req := struct{ ID string }{ID: "x"}
		_, err := extractFieldFromRequest(log, req, "id")
		if err == nil {
			t.Error("expected error for non-proto.Message")
		}
	})
}

// --- resolveResourceFromRequest ---

// ポリシーのリソーステンプレートにリクエストのフィールド値を埋め込んで
// 最終的なリソース文字列を生成できることを確認する。
func TestResolveResourceFromRequest(t *testing.T) {
	log := newLogger(slog.LevelError)

	tests := []struct {
		name         string
		policy       *pb.Policy
		req          interface{}
		wantResource string
		wantAction   string
		wantErr      bool
	}{
		{
			// field_mappings が空の場合はテンプレートをそのまま返す
			name:         "no field mappings",
			policy:       &pb.Policy{Resource: "posts", Action: "read"},
			req:          &pb.Policy{},
			wantResource: "posts",
			wantAction:   "read",
		},
		{
			// <placeholder> をリクエストのフィールド値で置換する
			name: "placeholder replaced from request field",
			policy: &pb.Policy{
				Resource: "posts/<resource>",
				Action:   "write",
				FieldMappings: []*pb.FieldMapping{
					{Placeholder: "resource", RequestField: "resource"},
				},
			},
			req:          &pb.Policy{Resource: "my-post-id"},
			wantResource: "posts/my-post-id",
			wantAction:   "write",
		},
		{
			// 複数プレースホルダーをそれぞれ対応するフィールドで置換する
			name: "multiple placeholders replaced",
			policy: &pb.Policy{
				Resource: "<action>/<resource>",
				Action:   "read",
				FieldMappings: []*pb.FieldMapping{
					{Placeholder: "resource", RequestField: "resource"},
					{Placeholder: "action", RequestField: "action"},
				},
			},
			req:          &pb.Policy{Resource: "posts", Action: "edit"},
			wantResource: "edit/posts",
			wantAction:   "read",
		},
		{
			// テンプレートに含まれないプレースホルダーは無視される
			name: "placeholder not in template is skipped",
			policy: &pb.Policy{
				Resource: "posts",
				Action:   "write",
				FieldMappings: []*pb.FieldMapping{
					{Placeholder: "unused", RequestField: "resource"},
				},
			},
			req:          &pb.Policy{Resource: "val"},
			wantResource: "posts",
			wantAction:   "write",
		},
		{
			// resource が空のポリシーはプロト定義の不備なためエラー
			name:    "empty resource returns error",
			policy:  &pb.Policy{Resource: "", Action: "read"},
			req:     &pb.Policy{},
			wantErr: true,
		},
		{
			// action が空のポリシーはプロト定義の不備なためエラー
			name:    "empty action returns error",
			policy:  &pb.Policy{Resource: "posts", Action: ""},
			req:     &pb.Policy{},
			wantErr: true,
		},
		{
			// placeholder が空の field_mapping はプロト定義の不備なためエラー
			name: "mapping with empty placeholder returns error",
			policy: &pb.Policy{
				Resource: "posts/<id>",
				Action:   "read",
				FieldMappings: []*pb.FieldMapping{
					{Placeholder: "", RequestField: "resource"},
				},
			},
			req:     &pb.Policy{},
			wantErr: true,
		},
		{
			// request_field が空の field_mapping はプロト定義の不備なためエラー
			name: "mapping with empty request_field returns error",
			policy: &pb.Policy{
				Resource: "posts/<id>",
				Action:   "read",
				FieldMappings: []*pb.FieldMapping{
					{Placeholder: "id", RequestField: ""},
				},
			},
			req:     &pb.Policy{},
			wantErr: true,
		},
		{
			// request_field に指定したフィールドがリクエストに存在しない場合はエラー
			name: "field not found in request returns error",
			policy: &pb.Policy{
				Resource: "posts/<id>",
				Action:   "read",
				FieldMappings: []*pb.FieldMapping{
					{Placeholder: "id", RequestField: "nonexistent"},
				},
			},
			req:     &pb.Policy{Resource: "val"},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveResourceFromRequest(log, tc.policy, tc.req)
			if tc.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Resource != tc.wantResource {
				t.Errorf("Resource = %q, want %q", got.Resource, tc.wantResource)
			}
			if got.Action != tc.wantAction {
				t.Errorf("Action = %q, want %q", got.Action, tc.wantAction)
			}
		})
	}
}

// --- Context helpers ---

// markInterceptorRan が context に実行済みフラグを書き込み、
// InterceptorRanFromContext がそれを正しく読み取れることを確認する。
func TestInterceptorRanFromContext(t *testing.T) {
	ctx := context.Background()
	if InterceptorRanFromContext(ctx) {
		t.Error("expected false for fresh context")
	}
	ctx = markInterceptorRan(ctx)
	if !InterceptorRanFromContext(ctx) {
		t.Error("expected true after markInterceptorRan")
	}
}

// withPolicy が context にポリシーを書き込み、
// PolicyFromContext がそれを正しく読み取れることを確認する。
func TestPolicyFromContext(t *testing.T) {
	ctx := context.Background()
	p, ok := PolicyFromContext(ctx)
	if ok || p != nil {
		t.Error("expected nil policy from fresh context")
	}

	ctx = withPolicy(ctx, "posts/123", "read")
	p, ok = PolicyFromContext(ctx)
	if !ok {
		t.Fatal("expected ok=true after withPolicy")
	}
	if p.Resource != "posts/123" {
		t.Errorf("Resource = %q, want %q", p.Resource, "posts/123")
	}
	if p.Action != "read" {
		t.Errorf("Action = %q, want %q", p.Action, "read")
	}
}

// --- Interceptor ---

// proto レジストリに登録されていないメソッドはポリシーなしと判断し、
// handler をそのまま呼び出すことを確認する。
// また handler に渡される context に InterceptorRan フラグが立ち、
// ポリシーは設定されていないことも検証する。
func TestInterceptor_UnknownMethod_CallsHandler(t *testing.T) {
	interceptor := Interceptor()

	called := false
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		called = true
		if !InterceptorRanFromContext(ctx) {
			t.Error("InterceptorRanFromContext should be true inside handler")
		}
		_, hasPolicty := PolicyFromContext(ctx)
		if hasPolicty {
			t.Error("PolicyFromContext should be false for unregistered method")
		}
		return "ok", nil
	}

	ctx := context.Background()
	info := &grpc.UnaryServerInfo{FullMethod: "/unknown.Service/UnknownMethod"}
	resp, err := interceptor(ctx, nil, info, handler)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != "ok" {
		t.Errorf("unexpected response: %v", resp)
	}
	if !called {
		t.Error("handler was not called")
	}
}

// gRPC フルメソッド名の形式が不正な場合は handler を呼ばずに
// Internal エラーを返すことを確認する。
func TestInterceptor_InvalidMethodFormat_ReturnsError(t *testing.T) {
	interceptor := Interceptor()

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		t.Error("handler should not be called for invalid method format")
		return nil, nil
	}

	ctx := context.Background()
	info := &grpc.UnaryServerInfo{FullMethod: "not-a-valid-method"}
	_, err := interceptor(ctx, nil, info, handler)

	if err == nil {
		t.Fatal("expected error for invalid method name")
	}
}
