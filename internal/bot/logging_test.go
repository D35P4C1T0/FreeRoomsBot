package bot

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestNewLoggerHonorsDefaultLevel(t *testing.T) {
	cfg := Config{}
	cfg.Logging.LogLevel = map[string]string{"Default": "Warning"}

	var buf bytes.Buffer
	logger := NewLoggerWithWriter(cfg, &buf)
	logger.Debug("hidden")
	logger.Warn("shown")

	out := buf.String()
	if strings.Contains(out, "hidden") {
		t.Fatalf("debug log should be hidden at warning level: %s", out)
	}
	if !strings.Contains(out, "shown") {
		t.Fatalf("warn log missing: %s", out)
	}

	if !logger.Enabled(context.Background(), slog.LevelWarn) {
		t.Fatal("logger should enable warn level")
	}
}
