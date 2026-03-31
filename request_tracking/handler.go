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
	"context"
	"log/slog"
)

// HandlerOption configures the request ID log handler.
type HandlerOption func(*handlerConfig)

type handlerConfig struct {
	attributeKey string
}

// WithAttributeKey sets the log attribute key for the request ID.
// Default is "x-request-id".
func WithAttributeKey(key string) HandlerOption {
	return func(c *handlerConfig) {
		c.attributeKey = key
	}
}

// requestIDHandler is a slog.Handler that injects the request ID from context
// into every log record as an attribute.
type requestIDHandler struct {
	next         slog.Handler
	attributeKey string
}

// NewRequestIDHandler wraps a slog.Handler to automatically add the request ID
// from the context to every log record. If no request ID is present in the
// context, the record is passed through unchanged.
//
// Usage:
//
//	slog.SetDefault(slog.New(
//	    requesttracking.NewRequestIDHandler(slog.NewTextHandler(os.Stderr, nil)),
//	))
//
//	// then anywhere:
//	slog.InfoContext(ctx, "handling request")
//	// → time=... level=INFO msg="handling request" x-request-id=abc123
func NewRequestIDHandler(next slog.Handler, opts ...HandlerOption) slog.Handler {
	cfg := &handlerConfig{attributeKey: "x-request-id"}
	for _, opt := range opts {
		opt(cfg)
	}
	return &requestIDHandler{
		next:         next,
		attributeKey: cfg.attributeKey,
	}
}

func (h *requestIDHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *requestIDHandler) Handle(ctx context.Context, r slog.Record) error {
	if id := RequestIDFromContext(ctx); id != "" {
		r.AddAttrs(slog.String(h.attributeKey, id))
	}
	return h.next.Handle(ctx, r)
}

func (h *requestIDHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &requestIDHandler{
		next:         h.next.WithAttrs(attrs),
		attributeKey: h.attributeKey,
	}
}

func (h *requestIDHandler) WithGroup(name string) slog.Handler {
	return &requestIDHandler{
		next:         h.next.WithGroup(name),
		attributeKey: h.attributeKey,
	}
}
