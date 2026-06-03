package bot

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// NewLogger creates the process logger using stdout as the destination.
func NewLogger(cfg Config) *slog.Logger {
	return NewLoggerWithWriter(cfg, os.Stdout)
}

// NewLoggerWithWriter creates a text slog logger using cfg's default log
// level.
//
// Unknown log levels fall back to warning to keep production logging quiet by
// default.
func NewLoggerWithWriter(cfg Config, w io.Writer) *slog.Logger {
	level := slog.LevelWarn
	switch strings.ToLower(cfg.Logging.LogLevel["Default"]) {
	case "trace", "debug":
		level = slog.LevelDebug
	case "information", "info":
		level = slog.LevelInfo
	case "warning", "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}

	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{
		Level: level,
	}))
}
