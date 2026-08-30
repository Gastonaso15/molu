package obs

import (
	"log/slog"
	"os"
	"strings"
)

// InitLogger sets up the global default slog logger based on level and format.
func InitLogger(levelStr, format string) *slog.Logger {
	var level slog.Level
	switch strings.ToLower(levelStr) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level: level,
	}

	var handler slog.Handler
	if strings.ToLower(format) == "json" {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		handler = slog.NewTextHandler(os.Stderr, opts)
	}

	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger
}

// RedactIfEnabled returns "[REDACTED]" if redaction is enabled and not in debug mode.
func RedactIfEnabled(payload interface{}, redact bool) interface{} {
	if redact {
		return "[REDACTED]"
	}
	return payload
}
