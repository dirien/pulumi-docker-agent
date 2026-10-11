// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package validate

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
)

// docker-agent's loader logs warnings through slog.Default() with the caller's context
// (for example "max_tokens is the maximum output tokens per response ..."). Validate
// installs a default handler once that captures records logged with a collecting
// context, so they can be reported as Pulumi warnings instead of raw provider stderr.

type warningsKey struct{}

type warningCollector struct {
	mu   sync.Mutex
	msgs []string
}

func (c *warningCollector) add(msg string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.msgs = append(c.msgs, msg)
}

func (c *warningCollector) messages() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.msgs...)
}

var installHandler sync.Once

func collectWarnings(ctx context.Context) (context.Context, *warningCollector) {
	installHandler.Do(func() {
		// Records that are not logged while validating keep going to stderr. A fresh text
		// handler is used rather than wrapping slog's default handler, which writes through
		// the log package and would loop once log output is redirected to slog.
		slog.SetDefault(slog.New(&collectingHandler{next: slog.NewTextHandler(os.Stderr, nil)}))
	})
	c := &warningCollector{}
	return context.WithValue(ctx, warningsKey{}, c), c
}

type collectingHandler struct {
	next  slog.Handler
	attrs []slog.Attr
}

func (h *collectingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	if _, ok := ctx.Value(warningsKey{}).(*warningCollector); ok {
		return level >= slog.LevelWarn
	}
	return h.next.Enabled(ctx, level)
}

func (h *collectingHandler) Handle(ctx context.Context, r slog.Record) error {
	c, ok := ctx.Value(warningsKey{}).(*warningCollector)
	if !ok {
		return h.next.Handle(ctx, r)
	}
	if r.Level < slog.LevelWarn {
		return nil
	}
	var b strings.Builder
	b.WriteString(r.Message)
	write := func(a slog.Attr) bool {
		fmt.Fprintf(&b, " %s=%v", a.Key, a.Value.Any())
		return true
	}
	for _, a := range h.attrs {
		write(a)
	}
	r.Attrs(write)
	c.add(Clean(b.String()))
	return nil
}

func (h *collectingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &collectingHandler{next: h.next.WithAttrs(attrs), attrs: append(append([]slog.Attr(nil), h.attrs...), attrs...)}
}

func (h *collectingHandler) WithGroup(name string) slog.Handler {
	return &collectingHandler{next: h.next.WithGroup(name), attrs: h.attrs}
}
