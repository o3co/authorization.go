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

package requesttracking

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
)

// Verify that the handler adds x-request-id to log records when present in context.
func TestRequestIDHandler_AddsRequestID(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewTextHandler(&buf, &slog.HandlerOptions{})
	handler := NewRequestIDHandler(base)
	logger := slog.New(handler)

	ctx := WithRequestID(context.Background(), "test-id-123")
	logger.InfoContext(ctx, "hello")

	output := buf.String()
	if !bytes.Contains([]byte(output), []byte("x-request-id=test-id-123")) {
		t.Errorf("expected x-request-id=test-id-123 in log output, got: %s", output)
	}
}

// Verify that the handler does not add x-request-id when absent from context.
func TestRequestIDHandler_NoRequestID(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewTextHandler(&buf, &slog.HandlerOptions{})
	handler := NewRequestIDHandler(base)
	logger := slog.New(handler)

	logger.InfoContext(context.Background(), "hello")

	output := buf.String()
	if bytes.Contains([]byte(output), []byte("x-request-id")) {
		t.Errorf("expected no x-request-id in log output, got: %s", output)
	}
}

// Verify that WithAttributeKey changes the log attribute key.
func TestRequestIDHandler_WithAttributeKey(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewTextHandler(&buf, &slog.HandlerOptions{})
	handler := NewRequestIDHandler(base, WithAttributeKey("trace-id"))
	logger := slog.New(handler)

	ctx := WithRequestID(context.Background(), "trace-456")
	logger.InfoContext(ctx, "hello")

	output := buf.String()
	if !bytes.Contains([]byte(output), []byte("trace-id=trace-456")) {
		t.Errorf("expected trace-id=trace-456 in log output, got: %s", output)
	}
}

// Verify that WithGroup works correctly with the handler.
func TestRequestIDHandler_WithGroup(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewTextHandler(&buf, &slog.HandlerOptions{})
	handler := NewRequestIDHandler(base)
	logger := slog.New(handler).WithGroup("req")

	ctx := WithRequestID(context.Background(), "grouped-id")
	logger.InfoContext(ctx, "hello")

	output := buf.String()
	// x-request-id should be at top level, not inside the group
	if !bytes.Contains([]byte(output), []byte("x-request-id=grouped-id")) {
		t.Errorf("expected x-request-id=grouped-id in log output, got: %s", output)
	}
}

// Verify that WithAttrs works correctly with the handler.
func TestRequestIDHandler_WithAttrs(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewTextHandler(&buf, &slog.HandlerOptions{})
	handler := NewRequestIDHandler(base)
	logger := slog.New(handler).With("component", "auth")

	ctx := WithRequestID(context.Background(), "attrs-id")
	logger.InfoContext(ctx, "hello")

	output := buf.String()
	if !bytes.Contains([]byte(output), []byte("x-request-id=attrs-id")) {
		t.Errorf("expected x-request-id=attrs-id in log output, got: %s", output)
	}
	if !bytes.Contains([]byte(output), []byte("component=auth")) {
		t.Errorf("expected component=auth in log output, got: %s", output)
	}
}

// Verify that Enabled delegates to the next handler.
func TestRequestIDHandler_Enabled(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	handler := NewRequestIDHandler(base)

	ctx := context.Background()
	if handler.Enabled(ctx, slog.LevelInfo) {
		t.Error("expected Info to be disabled when base level is Warn")
	}
	if !handler.Enabled(ctx, slog.LevelWarn) {
		t.Error("expected Warn to be enabled when base level is Warn")
	}
}
