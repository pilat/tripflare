package logging

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// LegoAdapter routes go-acme/lego log output through slog.
// It implements github.com/go-acme/lego/v4/log.StdLogger;
// the compile-time check lives in cmd/tripflare/main.go to avoid importing lego here.
type LegoAdapter struct {
	logger *slog.Logger
}

// NewLegoAdapter returns an adapter that sends lego log output through slog.Default().
func NewLegoAdapter() *LegoAdapter {
	return &LegoAdapter{logger: slog.Default()}
}

func (a *LegoAdapter) Print(args ...any) {
	a.log(fmt.Sprint(args...))
}

func (a *LegoAdapter) Println(args ...any) {
	a.log(fmt.Sprintln(args...))
}

func (a *LegoAdapter) Printf(format string, args ...any) {
	a.log(fmt.Sprintf(format, args...))
}

func (a *LegoAdapter) Fatal(args ...any) {
	a.logger.Error(trimNewline(fmt.Sprint(args...)))
	os.Exit(1)
}

func (a *LegoAdapter) Fatalln(args ...any) {
	a.logger.Error(trimNewline(fmt.Sprintln(args...)))
	os.Exit(1)
}

func (a *LegoAdapter) Fatalf(format string, args ...any) {
	a.logger.Error(trimNewline(fmt.Sprintf(format, args...)))
	os.Exit(1)
}

func (a *LegoAdapter) log(msg string) {
	msg = trimNewline(msg)
	level, stripped := parseLegoPrefix(msg)
	a.logger.Log(context.TODO(), level, stripped)
}

func parseLegoPrefix(msg string) (slog.Level, string) {
	switch {
	case strings.HasPrefix(msg, "[WARN] "):
		return slog.LevelWarn, msg[len("[WARN] "):]
	case strings.HasPrefix(msg, "[INFO] "):
		return slog.LevelInfo, msg[len("[INFO] "):]
	default:
		return slog.LevelInfo, msg
	}
}

func trimNewline(s string) string {
	return strings.TrimRight(s, "\n")
}
