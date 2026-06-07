package logging

import (
	"context"
	"log/slog"
	"testing"
)

type recordHandler struct {
	records []slog.Record
}

func (h *recordHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *recordHandler) WithAttrs([]slog.Attr) slog.Handler       { return h }
func (h *recordHandler) WithGroup(string) slog.Handler            { return h }
func (h *recordHandler) Handle(_ context.Context, r slog.Record) error {
	h.records = append(h.records, r)
	return nil
}

func TestLegoAdapter_Printf(t *testing.T) {
	h := &recordHandler{}
	adapter := &LegoAdapter{logger: slog.New(h)}

	adapter.Printf("[INFO] hello %s", "world")
	adapter.Printf("[WARN] something %s", "bad")
	adapter.Printf("plain message")

	if len(h.records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(h.records))
	}

	tests := []struct {
		wantLevel slog.Level
		wantMsg   string
	}{
		{slog.LevelInfo, "hello world"},
		{slog.LevelWarn, "something bad"},
		{slog.LevelInfo, "plain message"},
	}

	for i, tt := range tests {
		r := h.records[i]
		if r.Level != tt.wantLevel {
			t.Errorf("record[%d]: level = %v, want %v", i, r.Level, tt.wantLevel)
		}

		if r.Message != tt.wantMsg {
			t.Errorf("record[%d]: message = %q, want %q", i, r.Message, tt.wantMsg)
		}
	}
}

func TestLegoAdapter_Println(t *testing.T) {
	h := &recordHandler{}
	adapter := &LegoAdapter{logger: slog.New(h)}

	adapter.Println("[INFO] with newline")

	if len(h.records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(h.records))
	}

	r := h.records[0]
	if r.Level != slog.LevelInfo {
		t.Errorf("level = %v, want INFO", r.Level)
	}

	if r.Message != "with newline" {
		t.Errorf("message = %q, want %q", r.Message, "with newline")
	}
}

func TestLegoAdapter_Print(t *testing.T) {
	h := &recordHandler{}
	adapter := &LegoAdapter{logger: slog.New(h)}

	adapter.Print("[WARN] alert")

	if len(h.records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(h.records))
	}

	r := h.records[0]
	if r.Level != slog.LevelWarn {
		t.Errorf("level = %v, want WARN", r.Level)
	}

	if r.Message != "alert" {
		t.Errorf("message = %q, want %q", r.Message, "alert")
	}
}
