package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"
	"sync"

	"github.com/sanctumlabs/curtz/app/pkg/infra/tracing"
)

const (
	timeFormat = "[15:04:05.000]"
)

type (
	// SlogLogger is a wrapper for slog.Logger with modified methods suitable fo this application
	SlogLogger struct {
		*slog.Logger
	}

	SlogHandler struct {
		slogHandler slog.Handler
		buffer      *bytes.Buffer
		mux         *sync.Mutex
	}
)

func NewSlogLogger(cfg *LogConfig) *SlogLogger {
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
	if cfg.Environment == "" {
		cfg.Environment = "production"
	}

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

	// Add a few default environmental attributes that always are included
	defaultAttrs := []slog.Attr{
		slog.String("service", "bidService"),
		slog.Int("pid", os.Getpid()),
		slog.String("environment", cfg.Environment),
		slog.String("go_version", buildInfo.GoVersion),
		slog.String("git_revision", gitRevision),
	}

	logLevel := determineLogLevel(cfg)

	slogLogger := slog.New(
		newSlogJsonHandler(&slog.HandlerOptions{
			AddSource: cfg.EnableCaller,
			Level:     logLevel,
		}).
			WithAttrs(defaultAttrs),
	)

	slog.SetDefault(slogLogger)

	return &SlogLogger{slogLogger}
}

func newSlogJsonHandler(opts *slog.HandlerOptions) *SlogHandler {
	if opts == nil {
		opts = &slog.HandlerOptions{}
	}
	b := &bytes.Buffer{}
	return &SlogHandler{
		buffer: b,
		slogHandler: slog.NewJSONHandler(b, &slog.HandlerOptions{
			Level:       opts.Level,
			AddSource:   opts.AddSource,
			ReplaceAttr: suppressDefaults(opts.ReplaceAttr),
		}),
		mux: &sync.Mutex{},
	}
}

func (h *SlogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.slogHandler.Enabled(ctx, level)
}

func (h *SlogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &SlogHandler{slogHandler: h.slogHandler.WithAttrs(attrs), buffer: h.buffer, mux: h.mux}
}

func (h *SlogHandler) WithGroup(name string) slog.Handler {
	return &SlogHandler{slogHandler: h.slogHandler.WithGroup(name), buffer: h.buffer, mux: h.mux}
}

func (h *SlogHandler) Handle(ctx context.Context, r slog.Record) error {
	if requestId, ok := ctx.Value(tracing.RequestIDKey).(string); ok {
		r.AddAttrs(slog.String(string(tracing.RequestIDKey), requestId))
	}
	if correlationId, ok := ctx.Value(tracing.CorrelationIDKey).(string); ok {
		r.AddAttrs(slog.String(string(tracing.CorrelationIDKey), correlationId))
	}
	if traceId, ok := ctx.Value(tracing.TraceIDKey).(string); ok {
		r.AddAttrs(slog.String(string(tracing.TraceIDKey), traceId))
	}

	level := r.Level.String() + ":"

	switch r.Level {
	case slog.LevelDebug:
		level = colorize(darkGray, level)
	case slog.LevelInfo:
		level = colorize(cyan, level)
	case slog.LevelWarn:
		level = colorize(lightYellow, level)
	case slog.LevelError:
		level = colorize(lightRed, level)
	}

	attrs, err := h.computeAttrs(ctx, r)
	if err != nil {
		return err
	}

	bytes, err := json.MarshalIndent(attrs, "", "  ")
	if err != nil {
		return fmt.Errorf("error when marshaling attrs: %w", err)
	}

	fmt.Println(
		colorize(lightGray, r.Time.Format(timeFormat)),
		level,
		colorize(white, r.Message),
		colorize(darkGray, string(bytes)),
	)

	return nil
}

func (h *SlogHandler) computeAttrs(
	ctx context.Context,
	r slog.Record,
) (map[string]any, error) {
	h.mux.Lock()
	defer func() {
		h.buffer.Reset()
		h.mux.Unlock()
	}()
	if err := h.slogHandler.Handle(ctx, r); err != nil {
		return nil, fmt.Errorf("error when calling inner handler's Handle: %w", err)
	}

	var attrs map[string]any
	err := json.Unmarshal(h.buffer.Bytes(), &attrs)
	if err != nil {
		return nil, fmt.Errorf("error when unmarshaling inner handler's Handle result: %w", err)
	}
	return attrs, nil
}

func suppressDefaults(
	next func([]string, slog.Attr) slog.Attr,
) func([]string, slog.Attr) slog.Attr {
	return func(groups []string, a slog.Attr) slog.Attr {
		if a.Key == slog.TimeKey ||
			a.Key == slog.LevelKey ||
			a.Key == slog.MessageKey {
			return slog.Attr{}
		}
		if next == nil {
			return a
		}
		return next(groups, a)
	}
}
