// Copyright 2026 1o1 Co. Ltd.
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

// Verify that a gRPC full method name (in /pkg.Service/Method format) can be decomposed into Service and Method.
func TestParseFullMethodName(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantService string
		wantMethod  string
		wantErr     bool
	}{
		{
			// canonical format with package prefix
			name:        "valid full path",
			input:       "/sample.v1.SampleService/SearchSamples",
			wantService: "sample.v1.SampleService",
			wantMethod:  "SearchSamples",
		},
		{
			// works even without a package prefix (service name only)
			name:        "simple service without package",
			input:       "/MyService/MyMethod",
			wantService: "MyService",
			wantMethod:  "MyMethod",
		},
		{
			// empty string is an error
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			// missing leading "/" is an error
			name:    "no leading slash",
			input:   "MyService/MyMethod",
			wantErr: true,
		},
		{
			// only "/" with no method separator is an error
			name:    "only slash",
			input:   "/",
			wantErr: true,
		},
		{
			// missing method name separator (second "/") is an error
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

// Verify that proto.Message fields can be retrieved via reflection and converted to strings.
// Tests supported types (string, int32, bool), unsupported types (list, enum),
// and errors for non-proto.Message types and non-existent fields.
func TestExtractFieldFromRequest(t *testing.T) {
	log := newLogger(slog.LevelError)

	t.Run("string field", func(t *testing.T) {
		// string field is returned as-is
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
		// zero-value string field returns empty string (not an error)
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
		// int32 field is converted to a decimal string
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
		// bool field is converted to "true"/"false" string
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
		// repeated fields are unsuitable for embedding in resource templates; expect error
		req := &pb.Policy{
			FieldMappings: []*pb.FieldMapping{{Placeholder: "x"}},
		}
		_, err := extractFieldFromRequest(log, req, "field_mappings")
		if err == nil {
			t.Error("expected error for list field")
		}
	})

	t.Run("enum field is unsupported", func(t *testing.T) {
		// enum fields are unsupported because it is ambiguous whether to return a number or a name
		typ := descriptorpb.FieldDescriptorProto_TYPE_STRING
		req := &descriptorpb.FieldDescriptorProto{Type: &typ}
		_, err := extractFieldFromRequest(log, req, "type")
		if err == nil {
			t.Error("expected error for enum field")
		}
	})

	t.Run("field not found", func(t *testing.T) {
		// a field name that does not exist in the proto schema is an error
		req := &pb.Policy{Resource: "x"}
		_, err := extractFieldFromRequest(log, req, "nonexistent_field")
		if err == nil {
			t.Error("expected error for missing field")
		}
	})

	t.Run("not a proto.Message", func(t *testing.T) {
		// types that do not implement proto.Message cannot use reflection; expect error
		req := struct{ ID string }{ID: "x"}
		_, err := extractFieldFromRequest(log, req, "id")
		if err == nil {
			t.Error("expected error for non-proto.Message")
		}
	})
}

// --- resolveResourceFromRequest ---

// Verify that the final resource string is generated by embedding request field values
// into the policy's resource template.
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
			// when field_mappings is empty, the template is returned as-is
			name:         "no field mappings",
			policy:       &pb.Policy{Resource: "posts", Action: "read"},
			req:          &pb.Policy{},
			wantResource: "posts",
			wantAction:   "read",
		},
		{
			// <placeholder> is replaced with the request field value
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
			// multiple placeholders are each replaced by their corresponding field
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
			// placeholders not present in the template are ignored
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
			// a policy with an empty resource indicates a proto definition issue; expect error
			name:    "empty resource returns error",
			policy:  &pb.Policy{Resource: "", Action: "read"},
			req:     &pb.Policy{},
			wantErr: true,
		},
		{
			// a policy with an empty action indicates a proto definition issue; expect error
			name:    "empty action returns error",
			policy:  &pb.Policy{Resource: "posts", Action: ""},
			req:     &pb.Policy{},
			wantErr: true,
		},
		{
			// a field_mapping with an empty placeholder indicates a proto definition issue; expect error
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
			// a field_mapping with an empty request_field indicates a proto definition issue; expect error
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
			// when the field specified by request_field does not exist in the request, expect error
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

// Verify that markInterceptorRan writes the "ran" flag into the context
// and that InterceptorRanFromContext reads it back correctly.
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

// Verify that withPolicy writes the policy into the context
// and that PolicyFromContext reads it back correctly.
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

// Verify that a method not registered in the proto registry is treated as having no policy
// and that the handler is called directly.
// Also verifies that the context passed to the handler has the InterceptorRan flag set
// but no policy configured.
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

// Verify that an invalid gRPC full method name format returns an Internal error
// without calling the handler.
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
