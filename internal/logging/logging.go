// Package logging provides centralized logging infrastructure for Relay.
// Components retrieve loggers via Component() instead of passing through constructors.
package logging

import (
	"log/slog"
	"os"
	"strings"
	"sync"
)

var (
	defaultLogger *slog.Logger
	once          sync.Once
	logLevel      slog.Level
)

// Init initializes the global logger. Call once at startup.
// Safe to call multiple times; only the first call takes effect.
func Init() {
	once.Do(func() {
		logLevel = parseLogLevel(os.Getenv("LOG_LEVEL"))
		opts := &slog.HandlerOptions{Level: logLevel}

		if os.Getenv("NO_COLOR") == "" {
			defaultLogger = slog.New(NewColorHandler(os.Stdout, opts))
		} else {
			defaultLogger = slog.New(slog.NewJSONHandler(os.Stdout, opts))
		}
	})
}

// Logger returns the global logger.
// Automatically initializes if not already done.
func Logger() *slog.Logger {
	if defaultLogger == nil {
		Init()
	}
	return defaultLogger
}

// Component returns a logger with component context.
// Use this at package level: var log = logging.Component("sender")
func Component(name string) *slog.Logger {
	return Logger().With("component", name)
}

// WithFields returns a logger with additional fields.
func WithFields(fields ...any) *slog.Logger {
	return Logger().With(fields...)
}

// Level returns the current log level.
func Level() slog.Level {
	if defaultLogger == nil {
		Init()
	}
	return logLevel
}

// IsDebugEnabled returns true if debug logging is enabled.
func IsDebugEnabled() bool {
	return Level() <= slog.LevelDebug
}

func parseLogLevel(s string) slog.Level {
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
