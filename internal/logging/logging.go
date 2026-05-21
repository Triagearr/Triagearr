// Package logging configures the process-wide slog logger. JSON format when
// stderr is not a TTY (so it ships well to homelab log aggregators); text
// format when running interactively.
package logging

import (
	"log/slog"
	"os"
	"strings"

	"golang.org/x/term"
)

// Setup configures slog as the default logger and returns the handle.
// The level is read from TRIAGEARR_LOG_LEVEL (debug|info|warn|error).
func Setup() *slog.Logger {
	level := parseLevel(os.Getenv("TRIAGEARR_LOG_LEVEL"))
	opts := &slog.HandlerOptions{Level: level}
	var inner slog.Handler
	if term.IsTerminal(int(os.Stderr.Fd())) {
		inner = slog.NewTextHandler(os.Stderr, opts)
	} else {
		inner = slog.NewJSONHandler(os.Stderr, opts)
	}
	handler := NewRedactHandler(inner)
	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
