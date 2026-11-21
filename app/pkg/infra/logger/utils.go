package logger

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
)

// toLogLevel converts to LogLevel
func toLogLevel(level string) LogLevel {
	switch strings.ToLower(level) {
	case "debug":
		return DebugLevel
	case "info":
		return InfoLevel
	case "warn":
		return WarnLevel
	case "error":
		return ErrorLevel
	case "fatal":
		return FatalLevel
	}

	// unsupported log level by the App
	return UndefinedLevel
}

func getLoggers() map[string]Logger {
	globalLoggersLock.RLock()
	defer globalLoggersLock.RUnlock()

	l := map[string]Logger{}
	for k, v := range globalLoggers {
		l[k] = v
	}

	return l
}

func colorize(colorCode int, v string) string {
	return fmt.Sprintf("\033[%sm%s%s", strconv.Itoa(colorCode), v, reset)
}

// parseLevel converts string log level to slog.Level
func parseLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	case "fatal":
		return slog.LevelError + 4
	default:
		return slog.LevelInfo
	}
}

// determineLogLevel sets appropriate log level based on environment and config
func determineLogLevel(cfg *LogConfig) slog.Level {
	// If explicitly set to production environment, use Info level
	if cfg.Environment == "production" {
		return slog.LevelInfo
	}

	// If in development mode, allow debug logs
	if cfg.Development {
		// If level is explicitly set, use it, otherwise default to debug for development
		if cfg.Level != "" {
			return parseLevel(cfg.Level)
		}
		return slog.LevelDebug
	}

	// For non-production, non-development environments, use the configured level
	return parseLevel(cfg.Level)
}
