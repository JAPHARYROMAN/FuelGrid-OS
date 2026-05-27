// Package logging builds the structured slog logger used by the API
// service. JSON output is the default; level and format are config-driven.
package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// New builds a structured slog logger from string-typed config values.
// Unknown levels default to info; unknown formats default to JSON.
func New(level, format string) *slog.Logger {
	return NewWithWriter(os.Stdout, level, format)
}

// NewWithWriter is the same as New but writes to a caller-supplied
// destination. Useful for tests.
func NewWithWriter(w io.Writer, level, format string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: parseLevel(level)}

	var handler slog.Handler
	if strings.EqualFold(format, "text") {
		handler = slog.NewTextHandler(w, opts)
	} else {
		handler = slog.NewJSONHandler(w, opts)
	}
	return slog.New(handler)
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
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
