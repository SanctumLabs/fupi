package logger

import (
	"context"
	"fmt"
	"log"
	"os"
	"runtime/debug"

	"github.com/google/wire"
	"github.com/sanctumlabs/curtz/app/pkg/infra/tracing"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

type keyLogger int

// LoggerKey is the key used to retrieve the logger value from the request context.
const LoggerKey keyLogger = 0

type appLogger struct {
	*zap.SugaredLogger
}

var LoggerSet = wire.NewSet(New)

// New creates a new logger using the default configuration.
func New(cfg *LogConfig) Logger {
	if cfg == nil {
		cfg = &LogConfig{}
	}

	if !cfg.Development {
		cfg.Development = defaultLoggerDevelopment
	}

	if !cfg.EnableCaller {
		cfg.EnableCaller = defaultLoggerEnableCaller
	}
	if !cfg.EnableStackTrace {
		cfg.EnableStackTrace = defaultLoggerEnableStacktrace
	}
	if cfg.Format == "" {
		cfg.Format = defaultLoggerFormat
	}
	if cfg.Level == "" {
		cfg.Level = string(defaultLoggerLevel)
	}

	level := zap.InfoLevel
	if cfg.Level != "" {
		levelFromEnv, err := zapcore.ParseLevel(cfg.Level)
		if err != nil {
			log.Println(fmt.Errorf("Invalid level %s provided, defaulting to INFO: %w", cfg.Level, err))
		}

		level = levelFromEnv
	}

	logLevel := zap.NewAtomicLevelAt(level)

	// get the git revision
	var gitRevision string

	buildInfo, ok := debug.ReadBuildInfo()
	if ok {
		for _, v := range buildInfo.Settings {
			if v.Key == "vcs.revision" {
				gitRevision = v.Value
				break
			}
		}
	}

	defaultEncodingConfig := zapcore.EncoderConfig{
		TimeKey:        "timestamp",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "message",
		StacktraceKey:  "stacktrace",
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	if cfg.Development {
		defaultEncodingConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	} else {
		defaultEncodingConfig.EncodeLevel = zapcore.CapitalLevelEncoder
	}

	config := zap.Config{
		Level:             logLevel,
		Development:       cfg.Development,
		DisableCaller:     cfg.EnableCaller,
		DisableStacktrace: cfg.EnableStackTrace,
		Sampling:          &zap.SamplingConfig{},
		Encoding:          cfg.Format,
		EncoderConfig:     defaultEncodingConfig,
		OutputPaths: []string{
			"stderr",
		},
		ErrorOutputPaths: []string{
			"stderr",
		},
		InitialFields: map[string]interface{}{
			"pid":          os.Getpid(),
			"go_version":   buildInfo.GoVersion,
			"git_revision": gitRevision,
		},
	}

	l := zap.Must(config.Build())

	return NewSugar(l)
}

// NewTestLogger returns a test logger with observability capabilities.
func NewTestLogger() (Logger, *observer.ObservedLogs) {
	observedLogs, logObserver := observer.New(zap.InfoLevel)
	testLogger := zap.New(observedLogs)
	return NewSugar(testLogger), logObserver
}

func NewSugar(l *zap.Logger) Logger {
	return &appLogger{l.Sugar()}
}

// With returns a logger based off the root logger and decorates it with the arguments.
func (l *appLogger) With(args ...interface{}) Logger {
	return &appLogger{l.SugaredLogger.With(args...)}
}

func (l *appLogger) WithContext(ctx context.Context, args ...interface{}) Logger {
	sugaredLogger := l.SugaredLogger.With(args...)
	sugaredLogger = sugaredLogger.With(
		"x-trace-id", tracing.GetTraceID(ctx),
		"x-request-id", tracing.GetRequestID(ctx),
		"x-correlation-id", tracing.GetCorrelationID(ctx),
	)
	return &appLogger{sugaredLogger}
}

// WithoutCaller returns a logger that does not output the caller field and location of the calling code.
func (l *appLogger) WithoutCaller() Logger {
	return &appLogger{l.SugaredLogger.WithOptions(zap.WithCaller(false))}
}

// FromContext returns a logger from context. If none found, instantiate a new logger.
func FromContext(ctx context.Context) Logger {
	if l, ok := ctx.Value(LoggerKey).(Logger); ok {
		return l
	}

	// create a new logger if not found in context with default config
	return New(nil)
}

func WithContext(ctx context.Context, logger Logger) context.Context {
	if lp, ok := ctx.Value(LoggerKey).(Logger); ok {
		if lp == logger {
			// do not store same logger
			return ctx
		}
	}

	return context.WithValue(ctx, LoggerKey, logger)
}

// Sync flushes any buffered log entries.
func (l *appLogger) Sync() error {
	return l.SugaredLogger.Sync()
}

// DebugContext implements Logger.
func (l *appLogger) DebugContext(ctx context.Context, args ...any) {
	l.SugaredLogger.
		With(
			"x-trace-id", tracing.GetTraceID(ctx),
			"x-request-id", tracing.GetRequestID(ctx),
			"x-correlation-id", tracing.GetCorrelationID(ctx),
		).
		Debug(args...)
}

// ErrorContext implements Logger.
func (l *appLogger) ErrorContext(ctx context.Context, args ...any) {
	l.SugaredLogger.
		With(
			"x-trace-id", tracing.GetTraceID(ctx),
			"x-request-id", tracing.GetRequestID(ctx),
			"x-correlation-id", tracing.GetCorrelationID(ctx),
		).
		Error(args...)
}

// FatalContext implements Logger.
func (l *appLogger) FatalContext(ctx context.Context, args ...any) {
	l.SugaredLogger.
		With(
			"x-trace-id", tracing.GetTraceID(ctx),
			"x-request-id", tracing.GetRequestID(ctx),
			"x-correlation-id", tracing.GetCorrelationID(ctx),
		).
		Fatal(args...)
}

// InfoContext implements Logger.
func (l *appLogger) InfoContext(ctx context.Context, args ...any) {
	l.SugaredLogger.
		With(
			"x-trace-id", tracing.GetTraceID(ctx),
			"x-request-id", tracing.GetRequestID(ctx),
			"x-correlation-id", tracing.GetCorrelationID(ctx),
		).
		Info(args...)
}

// WarnContext implements Logger.
func (l *appLogger) WarnContext(ctx context.Context, args ...any) {
	l.SugaredLogger.
		With(
			"x-trace-id", tracing.GetTraceID(ctx),
			"x-request-id", tracing.GetRequestID(ctx),
			"x-correlation-id", tracing.GetCorrelationID(ctx),
		).
		Warn(args...)
}
