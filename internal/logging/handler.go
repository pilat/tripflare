package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
)

type prettyHandler struct {
	w      io.Writer
	mu     sync.Mutex
	level  slog.Leveler
	attrs  []slog.Attr
	groups []string
}

func NewPrettyHandler(w io.Writer, level slog.Leveler) slog.Handler {
	return &prettyHandler{w: w, level: level}
}

func (h *prettyHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

func (h *prettyHandler) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder

	fmt.Fprintf(&b, "[%s] %s", r.Level.String(), r.Message)

	prefix := strings.Join(h.groups, ".")
	writeAttr := func(a slog.Attr) {
		key := a.Key
		if prefix != "" {
			key = prefix + "." + key
		}

		fmt.Fprintf(&b, " %s=%s", key, a.Value.String())
	}

	for _, a := range h.attrs {
		writeAttr(a)
	}

	r.Attrs(func(a slog.Attr) bool {
		writeAttr(a)
		return true
	})

	b.WriteByte('\n')

	h.mu.Lock()
	defer h.mu.Unlock()

	if _, err := io.WriteString(h.w, b.String()); err != nil {
		return fmt.Errorf("write log line: %w", err)
	}

	return nil
}

func (h *prettyHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &prettyHandler{
		w:      h.w,
		level:  h.level,
		attrs:  append(sliceCopy(h.attrs), attrs...),
		groups: h.groups,
	}
}

func (h *prettyHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}

	return &prettyHandler{
		w:      h.w,
		level:  h.level,
		attrs:  sliceCopy(h.attrs),
		groups: append(sliceCopy2(h.groups), name),
	}
}

func sliceCopy2(s []string) []string {
	c := make([]string, len(s))
	copy(c, s)

	return c
}

func sliceCopy(s []slog.Attr) []slog.Attr {
	c := make([]slog.Attr, len(s))
	copy(c, s)

	return c
}
