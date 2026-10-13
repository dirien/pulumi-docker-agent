// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentrun

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/validate"
)

// docker-agent logs through slog.Default(). A run child's stderr ends up in the provider's
// debug log, and docker-agent's error logs quote model API errors verbatim, which can contain
// an API key (OpenAI echoes a rejected key). In the run child (child.go), records are
// therefore routed to the run instead of stderr: below WARN they are dropped, the rest is
// redacted and handed to Request.Log, which sends them to the provider. Records of other work
// in the process pass through, redacted while a run is active.

// runLog is the log sink of one run.
type runLog struct {
	secrets []string
	names   *nameHider
	log     func(slog.Level, string)
	mu      sync.Mutex
	closed  bool
}

func (r *runLog) handle(rec slog.Record, attrs []slog.Attr) {
	if rec.Level < slog.LevelWarn {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.log == nil {
		return
	}
	r.log(rec.Level, redact(r.names.hide(format(rec, attrs)), r.secrets))
}

func (r *runLog) close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
}

type runLogKey struct{}

// activeLog is the sink of the run in progress (runs are serialized).
var activeLog atomic.Pointer[runLog]

// startRunLog installs the log routing and returns the run's context.
func startRunLog(ctx context.Context, secrets []string, names *nameHider, log func(slog.Level, string),
) (context.Context, func()) {
	installLogHandler()
	rl := &runLog{secrets: secrets, names: names, log: log}
	activeLog.Store(rl)
	return context.WithValue(ctx, runLogKey{}, rl), func() {
		rl.close()
		activeLog.CompareAndSwap(rl, nil)
	}
}

var (
	installMu        sync.Mutex
	validateHandler  sync.Once
	backgroundCtxVal = context.Background()
)

// installLogHandler wraps the default slog handler, unless it is already wrapped.
func installLogHandler() {
	installMu.Lock()
	defer installMu.Unlock()
	// The validate package installs its own default handler (to turn docker-agent's
	// loader warnings into Pulumi warnings) the first time it validates. Make that happen
	// now, so that it never replaces this handler during a run.
	validateHandler.Do(func() {
		_ = validate.Validate(context.Background(), "agentrun-init.yaml", []byte("agents: {}\n"))
	})
	current := slog.Default().Handler()
	if _, ok := current.(*logHandler); ok {
		return
	}
	next := current
	if fmt.Sprintf("%T", current) == "*slog.defaultHandler" {
		// slog's built-in handler writes through the log package, which SetDefault
		// redirects to slog: wrapping it would loop.
		next = slog.NewTextHandler(os.Stderr, nil)
	}
	slog.SetDefault(slog.New(&logHandler{next: next}))
}

type logHandler struct {
	next  slog.Handler
	attrs []slog.Attr
}

// target returns the run a record belongs to: the run its context comes from, or the
// active run for records logged without a context.
func target(ctx context.Context) *runLog {
	if ctx == nil || ctx == backgroundCtxVal {
		return activeLog.Load()
	}
	rl, _ := ctx.Value(runLogKey{}).(*runLog)
	return rl
}

func (h *logHandler) Enabled(ctx context.Context, level slog.Level) bool {
	if target(ctx) != nil {
		return level >= slog.LevelWarn
	}
	return h.next.Enabled(ctx, level)
}

func (h *logHandler) Handle(ctx context.Context, rec slog.Record) error {
	if rl := target(ctx); rl != nil {
		rl.handle(rec, h.attrs)
		return nil
	}
	if rl := activeLog.Load(); rl != nil {
		redacted := slog.NewRecord(rec.Time, rec.Level, redact(format(rec, h.attrs), rl.secrets), rec.PC)
		return h.next.Handle(ctx, redacted)
	}
	return h.next.Handle(ctx, rec)
}

func (h *logHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &logHandler{next: h.next.WithAttrs(attrs), attrs: append(append([]slog.Attr(nil), h.attrs...), attrs...)}
}

func (h *logHandler) WithGroup(name string) slog.Handler {
	return &logHandler{next: h.next.WithGroup(name), attrs: h.attrs}
}

// format renders a record as "message key=value ...".
func format(rec slog.Record, attrs []slog.Attr) string {
	var b strings.Builder
	b.WriteString(rec.Message)
	write := func(a slog.Attr) bool {
		fmt.Fprintf(&b, " %s=%v", a.Key, a.Value.Any())
		return true
	}
	for _, a := range attrs {
		write(a)
	}
	rec.Attrs(write)
	return validate.Clean(b.String())
}
