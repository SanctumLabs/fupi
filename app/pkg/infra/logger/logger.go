package logger

import (
	"context"
	"sync"
)

// globalLoggers is the collection of App Logger that is shared globally.
var globalLoggers = map[string]Logger{}
var globalLoggersLock = sync.RWMutex{}

// Logger that defines how loggers should be implemented
type Logger interface {
	With(args ...any) Logger

	WithoutCaller() Logger

	// Info logs a message at level Info.
	Info(args ...any)

	// Infof uses fmt.Sprintf to construct and log a message at INFO level
	Infof(format string, args ...any)

	Infow(msg string, args ...any)

	// InfoContext logs a message at level Info and adds the context to the log entry.
	// This is useful for adding request IDs, trace IDs, etc. to the log entry.
	InfoContext(ctx context.Context, args ...any)

	// Debug logs a message at level Debug.
	Debug(args ...any)

	// Debugf uses fmt.Sprintf to construct and log a message at DEBUG level
	Debugf(format string, args ...any)

	Debugw(msg string, args ...any)

	// DebugContext logs a message at level Debug.
	DebugContext(ctx context.Context, args ...any)

	// Warn logs a message at level Warn.
	Warn(args ...any)

	// WarnContext logs a message at level Warn and adds the context to the log entry.
	WarnContext(ctx context.Context, args ...any)

	Warnw(msg string, args ...any)

	// Error logs a message at level Error.
	Error(args ...any)

	// ErrorContext logs a message at level Error with additional context
	ErrorContext(ctx context.Context, args ...any)

	// Errorf uses fmt.Sprintf to construct and log a message at ERROR level
	Errorf(format string, args ...any)

	Errorw(msg string, args ...any)

	// Fatal uses fmt.Sprint to construct and log a message, then calls os.Exit.
	Fatal(args ...any)

	// Fatalf uses fmt.Sprintf to construct and log a message, then calls os.Exit.
	Fatalf(format string, args ...any)

	Fatalw(msg string, args ...any)

	// FatalContext logs a fatal message at level Fatal with additional context
	FatalContext(ctx context.Context, args ...any)

	// Sync flushes any buffered log entries.
	Sync() error
}
