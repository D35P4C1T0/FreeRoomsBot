package main

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// newLogger creates the process logger using stdout as the destination.
func newLogger(cfg config) *slog.Logger {
	return newLoggerWithWriter(cfg, os.Stdout)
}

// newLoggerWithWriter creates a text slog logger using cfg's default log
// level.
//
// Unknown log levels fall back to warning to keep production logging quiet by
// default.
func newLoggerWithWriter(cfg config, w io.Writer) *slog.Logger {
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
