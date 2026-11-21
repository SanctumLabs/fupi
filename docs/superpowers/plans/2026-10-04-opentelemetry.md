# OpenTelemetry Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the Fupi API observable inside the local observability and ELK stacks: traces in Tempo, metrics in Prometheus and Grafana, JSON logs in Elasticsearch, all tied together by one W3C trace ID, with the existing dashboard, alert and trace-to-logs link working without edits.

**Architecture:** A new `app/pkg/infra/telemetry` package installs the OpenTelemetry SDK (OTLP/gRPC to the collector) and the slog handler that adds `trace_id`/`span_id`. A small custom Fiber middleware, first in the chain, creates the HTTP server span and the `http.server.request.duration` metric; a slog access-log middleware replaces Fiber's text logger. `otelpgx` and `redisotel` instrument Postgres and Redis, identity use cases get spans, the Bids-era `/metrics` code is removed, and `main` wires telemetry in and flushes it after the server drains.

**Tech Stack:** Go 1.26, Fiber v2.52, OpenTelemetry Go v1.44.0 (API already required; SDK, `sdk/metric`, OTLP gRPC exporters added), `github.com/exaring/otelpgx`, `github.com/redis/go-redis/extra/redisotel/v9`, testify, testcontainers (Postgres, Redis).

**Spec:** `docs/superpowers/specs/2026-10-04-opentelemetry-design.md` (decisions D1–D11 are binding; this plan argues from it).

## Global Constraints

- Go `1.26.0` (`go.mod`). All commands run from the repo root `/Users/lusina/Projects/SanctumLabs/fupi`, on branch `feat/otel-instrumentation`.
- **Commit trailer (D11):** every commit made while executing this plan ends with the `Co-Authored-By` trailer given in the session's attribution instructions. The `git commit` snippets below omit the trailer text on purpose; add it to each commit.
- **Download gate:** nothing is downloaded without the user's explicit go-ahead naming source and size. `go.opentelemetry.io/otel/sdk` and `.../sdk/metric` at v1.44.0 are already in the module cache: Task 3 adds them with `GOPROXY=off`, which fails instead of downloading. Everything else new (`otlptracegrpc`, `otlpmetricgrpc` and their dependencies, `otelpgx`, `redisotel`, and `go mod tidy`, which resolves test-only dependencies of dependencies) needs the go-ahead first; Task 9 starts with that stop.
- **Version pins** (resolved against the proxy only after the go-ahead; if a pin cannot be met, stop and ask): every `go.opentelemetry.io/otel/...` module at **v1.44.0**, in lockstep with `go.opentelemetry.io/otel v1.44.0` already in `go.mod`; `github.com/redis/go-redis/extra/redisotel/v9` at **v9.19.0**, equal to `go-redis/v9 v9.19.0` already in `go.mod`; `github.com/exaring/otelpgx` at its latest (v0.12.0 when this was written; needs Go 1.25+ and pgx v5). No existing requirement (pgx, go-redis, grpc, otel) may be bumped without asking.
- Environment variables are the standard `OTEL_*` ones; the service name defaults to `fupi`; `OTEL_SDK_DISABLED=true` installs no-ops (D2).
- Traces and metrics leave over OTLP/gRPC; logs stay JSON on stdout (D1). `/health` and `/health/ready` get no span, no metric and only a debug-level log line (D8).
- Never record SQL parameters, Redis command text, the query string, the client address, request headers, an email address, a username or a token in a span (D7, spec section 5 and 7).
- Telemetry can never affect a request: exporters are asynchronous, the error handler logs each distinct error at most once a minute (D9).
- Unit tests are untagged; Docker-backed tests carry `//go:build integration`. Use testify `require`/`assert`. `go test ./...` must be green at the end of every task.
- Format every Go file you create or edit with `gofmt -w` (do not reformat files you only touch elsewhere: `app/config/database.go` and others are not gofmt-clean and are out of scope).
- Do not modify `.env` (the developer's own, gitignored file) or `/etc/hosts`. Do not touch the legacy-tagged code, `fly.toml`, or slice 1 and 2 files other than the ones named in a task.
- Before starting any Docker stack check `docker ps`: the developer's own containers must not be disturbed.

## Plan notes (where this plan departs from, or sharpens, the spec's wording)

Found while reading the code and probing real Fiber; each needs the reviewer's eye.

1. **No process detector in the resource (spec section 4 says "process and host detectors").** The collector's Prometheus exporter turns every resource attribute into a label (`resource_to_telemetry_conversion`), so `process.command_args`, `process.owner`, `process.pid` and the rest would land on every series of every metric. The resource carries `service.name`, `service.version`, `deployment.environment.name`, `host.*` and the telemetry SDK attributes only.
2. **Identity spans do not call `RecordError` (spec section 7 says "record an error on failure").** The domain errors embed what the caller sent (`email %s is invalid`, `first name %s is invalid`, `token %s is invalid`), and `RecordError` copies the error text into an `exception` event. The span instead gets status `Error` whose description, and the `error.type` attribute, is the error's class (`invalid_parameter`, `unauthorized`, `forbidden`, `not_found`, `conflict`, `unavailable`, `internal`). Task 7's test proves the premise and the property.
3. **A returned error has no status yet.** Fiber's error handler runs after the outermost middleware returns, so a middleware wrapped around the handlers sees 200 for a request that is about to become 409 or 500 (the recover middleware also returns a panic as an error). The span middleware and the access log both call the app's error handler themselves (`settle`, as Fiber's own logger does), which is also what makes the spec's "a panic is already a 500 when the span ends" true.
4. **"Unmatched route" detection.** After the handlers run, Fiber reports the catch-all `/` of the first middleware for a path that matched nothing (and for a wrong method). Probed against real Fiber v2.52.15: a `/` route for any request path other than `/` means no route matched. Spans are then named `METHOD` and metrics have no `http.route` label.
5. **`ServerConfig.ProbePaths`** carries the probe paths into `NewServer`, so `infra/server` does not import `api/probes`.
6. **`app/pkg/infra/database/postgres/postgres_client.go` imports `golang.org/x/exp/slog`.** Its lines reach the process logger only through the `log` package bridge: probed, every line comes out at level INFO with `WARN`/`ERROR` as a prefix of the message and the attributes flattened into it. Task 8 switches the import to `log/slog` (needed for spec section 6's JSON shape).
7. **`Setup` failing is not fatal.** A malformed `OTEL_*` value that makes an exporter constructor fail logs a warning and the API runs without telemetry, the same degraded-not-fatal policy as Redis.
8. **`go mod tidy` and `go.sum`.** `tidy` also resolves test dependencies of dependencies, so it cannot run offline; it is part of the download gate, and it is what drops `github.com/prometheus/client_golang` (spec section 8). Task 6 deletes the code that used it; Task 12 runs `tidy`.
9. **Known limitation (documented in Task 14):** the readiness checks' Redis `PING` (and any metric a library records per Postgres call) is still counted by the libraries' own metrics; only spans are suppressed under `telemetry.Unsampled`.
10. **Postgres client startup spans.** The pool's background connections are created with a background context, so they appear as small root traces named `connect` at startup and when a connection is replaced. That is the library's behaviour, not request work.

## Review Focus

Failure modes the spec implies but the obvious tests do not cover, most likely first. Each has a pinning test or drill in the owning task.

1. Strings taken from the request (`c.Path()`, host, user agent, scheme) point into fasthttp buffers that the next request on the connection overwrites, and spans are exported after the request is over, so an attribute would show another request's path → Task 4 (`TestOTelMiddleware_AttributesSurviveTheNextRequestOnTheSameConnection`, plus a mutation check that it fails without `strings.Clone`).
2. Domain error text embeds the caller's email, names and tokens, so recording the error would put personal data in Tempo → Task 7 (`TestRegister_AFailureMarksTheSpanWithTheClassOfTheErrorNotItsText`, with a mutation check).
3. A client-chosen path must never become a metric label or span name (unbounded cardinality), and `/` must still be a valid route → Task 4 (`TestOTelMiddleware_TellsTheRootRouteFromAnUnmatchedPath`, `..._AWrongMethodIsAnUnmatchedRouteToo`, `..._AnUnmatchedRouteHasNoRouteLabel`).
4. A handler's returned error, and a panic, must show up with their final status (409, 500) in the span, the histogram and the access log, and only a 5xx marks the span as an error → Task 4 (`..._RecordsTheStatusOfAReturnedError`, `..._AFiveHundredMarksTheSpanAsAnError`, `..._APanicBecomesAFiveHundredSpanError`) and Task 5 (the real chain).
5. A stopped collector must neither slow requests nor flood the log, and startup must not fail → Task 3 (rate limit), Task 9 (`TestSetup_DoesNotFailOrBlockWithoutACollector`) and the Task 13 drill.

## File Structure

| File | Responsibility |
|---|---|
| `app/config/app.go` | `LoggingSettings`, `LoadLogging`, `App.Logging` |
| `app/pkg/infra/telemetry/log.go` | `ServiceName`, `NewLogger` (JSON or text handler adding service, trace and span IDs) |
| `app/pkg/infra/telemetry/errors.go` | rate-limited OpenTelemetry error handler |
| `app/pkg/infra/telemetry/unsampled.go` | `Unsampled(ctx)` for the readiness checks |
| `app/pkg/infra/telemetry/setup.go` | `Setup`: resource, exporters, providers, propagators, shutdown |
| `app/pkg/infra/server/middleware/route.go` | `settle`, `routeTemplate`, probe-path helpers |
| `app/pkg/infra/server/middleware/otel.go` | span and metrics middleware |
| `app/pkg/infra/server/middleware/access_log.go` | slog access-log middleware |
| `app/pkg/infra/tracing/` | `GetTraceID` prefers the span; corrected package doc |
| `app/internal/application/identity/tracing.go` | use-case spans |
| `app/pkg/infra/monitoring/health/registry.go` | checks run under `Unsampled` |
| `app/pkg/infra/database/postgres/postgres_client.go` | `otelpgx` tracer and pool stats; `log/slog` |
| `app/pkg/infra/cache/redis/client.go` | `redisotel` tracing and metrics |
| `app/cmd/main.go` | logger install, `Setup` and flush, `ProbePaths` |
| `deploy/app/compose.yml`, `.env.example` | `OTEL_*` variables |
| `deploy/observability/grafana/dashboards/fupi-service.json` | Dependencies row |
| `docs/LocalInfrastructure.md`, `docs/Deployment.md`, `docs/adr/0017-...md` | documentation |

---

### Task 1: Logging settings and the correlated slog handler

**Files:**
- Modify: `app/config/app.go`
- Modify: `app/config/app_test.go`
- Modify: `.env.example` (delete line 2, `LOG_LEVEL=debug`)
- Create: `app/pkg/infra/telemetry/log.go`
- Create: `app/pkg/infra/telemetry/log_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `config.LoggingSettings{Level slog.Level; Format string}`, `config.LoadLogging(lookup Lookup) (LoggingSettings, error)` (on an invalid value the returned settings are the defaults), field `config.App.Logging`; `telemetry.ServiceName() string`; `telemetry.NewLogger(w io.Writer, format string, level slog.Level, service string) *slog.Logger`. Task 12 calls `NewLogger` and reads `cfg.Logging`; Task 4 tests build their logger with it.

- [ ] **Step 1: Write the failing test for the duplicate `.env.example` key**

In `app/config/app_test.go` add `"os"` and `"strings"` to the imports (keep them sorted with `"testing"`), then append:

```go
// A key set twice in .env.example is ambiguous to read and silently resolved by the last line (LOG_LEVEL was set to
// debug and then to info).
func TestEnvExample_SetsEachKeyOnce(t *testing.T) {
	raw, err := os.ReadFile("../../.env.example")
	require.NoError(t, err)

	seen := map[string]int{}
	for _, line := range strings.Split(string(raw), "\n") {
		key, _, found := strings.Cut(line, "=")
		if !found || strings.HasPrefix(strings.TrimSpace(key), "#") {
			continue
		}
		seen[strings.TrimSpace(key)]++
	}
	for key, count := range seen {
		assert.Equal(t, 1, count, "%s is set %d times in .env.example", key, count)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./app/config -run TestEnvExample_SetsEachKeyOnce`
Expected: FAIL with `LOG_LEVEL is set 2 times in .env.example`.

- [ ] **Step 3: Remove the duplicate**

In `.env.example` delete line 2 (`LOG_LEVEL=debug`); the later `LOG_LEVEL="info"` (the value godotenv already resolved to) stays.

- [ ] **Step 4: Run the config tests**

Run: `go test ./app/config`
Expected: `ok`.

- [ ] **Step 5: Write the failing tests for `LoadLogging`**

In `app/config/app_test.go` add `"log/slog"` to the imports and append:

```go
func TestLoadLogging_DefaultsToJSONAtInfo(t *testing.T) {
	settings, err := LoadLogging(lookupOf(nil))
	require.NoError(t, err)

	assert.Equal(t, LoggingSettings{Level: slog.LevelInfo, Format: "json"}, settings)
}

func TestLoadLogging_ReadsLevelAndFormat(t *testing.T) {
	cases := map[string]struct {
		env  map[string]string
		want LoggingSettings
	}{
		"debug text":        {map[string]string{"LOG_LEVEL": "debug", "LOG_FORMAT": "text"}, LoggingSettings{slog.LevelDebug, "text"}},
		"warn":              {map[string]string{"LOG_LEVEL": "warn"}, LoggingSettings{slog.LevelWarn, "json"}},
		"warning alias":     {map[string]string{"LOG_LEVEL": "warning"}, LoggingSettings{slog.LevelWarn, "json"}},
		"error":             {map[string]string{"LOG_LEVEL": "error"}, LoggingSettings{slog.LevelError, "json"}},
		"upper case":        {map[string]string{"LOG_LEVEL": "DEBUG", "LOG_FORMAT": "TEXT"}, LoggingSettings{slog.LevelDebug, "text"}},
		"empty means unset": {map[string]string{"LOG_LEVEL": "", "LOG_FORMAT": ""}, LoggingSettings{slog.LevelInfo, "json"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			settings, err := LoadLogging(lookupOf(tc.env))
			require.NoError(t, err)
			assert.Equal(t, tc.want, settings)
		})
	}
}

func TestLoadLogging_RejectsUnknownValuesAndFallsBackToTheDefaults(t *testing.T) {
	settings, err := LoadLogging(lookupOf(map[string]string{"LOG_LEVEL": "verbose", "LOG_FORMAT": "xml"}))

	require.Error(t, err)
	assert.ErrorContains(t, err, "LOG_LEVEL")
	assert.ErrorContains(t, err, "LOG_FORMAT")
	assert.NotContains(t, err.Error(), "verbose", "error text names variables, never values")
	assert.Equal(t, LoggingSettings{Level: slog.LevelInfo, Format: "json"}, settings, "usable settings so the error itself can be logged")
}

func TestLoad_IncludesTheLoggingSettings(t *testing.T) {
	app, err := Load(lookupOf(map[string]string{"LOG_FORMAT": "text"}))
	require.NoError(t, err)
	assert.Equal(t, "text", app.Logging.Format)

	_, err = Load(lookupOf(map[string]string{"LOG_FORMAT": "xml"}))
	assert.ErrorContains(t, err, "LOG_FORMAT", "an unknown format is a startup error")
}
```

- [ ] **Step 6: Run them to verify they fail**

Run: `go test ./app/config`
Expected: build failure `undefined: LoggingSettings` (and `LoadLogging`).

- [ ] **Step 7: Implement `LoadLogging` and add it to `Load`**

In `app/config/app.go` add `"log/slog"` and `"strings"` to the imports, then add the type above `App`, the field in `App`, the call in `Load`, and the loader above `LoadDatabase`:

```go
// LoggingSettings configures the process logger.
type LoggingSettings struct {
	Level slog.Level
	// Format is "json" (one object per line, what the ELK pipeline parses) or "text" (for a terminal).
	Format string
}
```

In `App`, after `Auth AuthConfig`:

```go
	Logging         LoggingSettings
```

In `Load`, after the `LoadAuth` call and its `errs = append(errs, err)`:

```go
	app.Logging, err = LoadLogging(lookup)
	errs = append(errs, err)
```

Above `// LoadDatabase reads the Postgres settings.`:

```go
// LoadLogging reads LOG_LEVEL (debug, info, warn or error; default info) and LOG_FORMAT (json or text; default json). A
// value that is not one of those is an error, and the settings returned alongside it are the defaults, so the caller can
// still log the error.
func LoadLogging(lookup Lookup) (LoggingSettings, error) {
	r := newReader(lookup)
	settings := LoggingSettings{Level: slog.LevelInfo, Format: "json"}

	if value, ok := r.raw("LOG_LEVEL"); ok {
		switch strings.ToLower(value) {
		case "debug":
			settings.Level = slog.LevelDebug
		case "info":
			settings.Level = slog.LevelInfo
		case "warn", "warning":
			settings.Level = slog.LevelWarn
		case "error":
			settings.Level = slog.LevelError
		default:
			r.fail("LOG_LEVEL must be one of debug, info, warn or error")
		}
	}
	if value, ok := r.raw("LOG_FORMAT"); ok {
		switch format := strings.ToLower(value); format {
		case "json", "text":
			settings.Format = format
		default:
			r.fail("LOG_FORMAT must be json or text")
		}
	}
	return settings, r.err()
}
```

Run `gofmt -w app/config/app.go` and check `git diff app/config/app.go` shows only these changes.

- [ ] **Step 8: Run the config tests**

Run: `go test ./app/config`
Expected: `ok` (including `TestLoad_EnvExampleDocumentsTheDefaults`, which now also pins `LOG_LEVEL` and `LOG_FORMAT` to the loader's defaults).

- [ ] **Step 9: Write the failing tests for the logger**

Create `app/pkg/infra/telemetry/log_test.go`:

```go
package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
)

const (
	testTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	testSpanID  = "00f067aa0ba902b7"
)

// spanContext returns a context that carries a span, sampled or not.
func spanContext(t *testing.T, flags trace.TraceFlags) context.Context {
	t.Helper()
	traceID, err := trace.TraceIDFromHex(testTraceID)
	require.NoError(t, err)
	spanID, err := trace.SpanIDFromHex(testSpanID)
	require.NoError(t, err)
	return trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID, SpanID: spanID, TraceFlags: flags,
	}))
}

func logLine(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var line map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &line), buf.String())
	return line
}

func TestNewLogger_JSONCarriesTheKeysTheELKPipelineMaps(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(&buf, "json", slog.LevelInfo, "fupi")

	logger.InfoContext(spanContext(t, trace.FlagsSampled), "hello", "user", "u-1")

	line := logLine(t, &buf)
	assert.Equal(t, "hello", line["msg"])
	assert.Equal(t, "INFO", line["level"])
	assert.NotEmpty(t, line["time"])
	assert.Equal(t, "fupi", line["service"])
	assert.Equal(t, "u-1", line["user"])
	assert.Equal(t, testTraceID, line["trace_id"])
	assert.Equal(t, testSpanID, line["span_id"])
}

func TestNewLogger_OmitsTheIDsWithoutASpan(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(&buf, "json", slog.LevelInfo, "fupi")

	logger.InfoContext(context.Background(), "no span")
	logger.Info("no context at all")

	for _, raw := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		assert.NotContains(t, raw, "trace_id")
		assert.NotContains(t, raw, "span_id")
		assert.Contains(t, raw, `"service":"fupi"`)
	}
}

// A span that was sampled out still has IDs, and the log line is still worth finding by them.
func TestNewLogger_AddsTheIDsOfAnUnsampledSpanToo(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(&buf, "json", slog.LevelInfo, "fupi")

	logger.InfoContext(spanContext(t, 0), "sampled out")

	assert.Equal(t, testTraceID, logLine(t, &buf)["trace_id"])
}

// slog.With wraps the handler through WithAttrs; a wrapper that forgot to wrap its result would silently stop adding IDs.
func TestNewLogger_DerivedLoggersKeepAddingTheIDs(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(&buf, "json", slog.LevelInfo, "fupi")

	logger.With("component", "x").WithGroup("g").InfoContext(spanContext(t, trace.FlagsSampled), "derived", "k", "v")

	line := logLine(t, &buf)
	assert.Equal(t, "x", line["component"])
	group, ok := line["g"].(map[string]any)
	require.True(t, ok, "WithGroup must still group the call's attributes")
	assert.Equal(t, "v", group["k"])
	// Handle runs inside the open group, so the IDs land in it; what matters is that they are not lost.
	assert.Equal(t, testTraceID, group["trace_id"])
}

func TestNewLogger_HonoursTheLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(&buf, "json", slog.LevelWarn, "fupi")

	logger.Info("dropped")
	logger.Debug("dropped")
	assert.Empty(t, buf.String())

	logger.Warn("kept")
	assert.Contains(t, buf.String(), "kept")
}

func TestNewLogger_TextFormatIsForTerminals(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(&buf, "text", slog.LevelInfo, "fupi")

	logger.InfoContext(spanContext(t, trace.FlagsSampled), "hello")

	out := buf.String()
	assert.False(t, strings.HasPrefix(out, "{"), "text format must not be JSON: %s", out)
	assert.Contains(t, out, "msg=hello")
	assert.Contains(t, out, "service=fupi")
	assert.Contains(t, out, "trace_id="+testTraceID)
}

func TestServiceName(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "")
	assert.Equal(t, "fupi", ServiceName(), "an empty value counts as unset")

	t.Setenv("OTEL_SERVICE_NAME", "  fupi-api ")
	assert.Equal(t, "fupi-api", ServiceName())
}
```

- [ ] **Step 10: Run them to verify they fail**

Run: `go test ./app/pkg/infra/telemetry`
Expected: build failure `undefined: NewLogger` (and `ServiceName`).

- [ ] **Step 11: Implement the logger**

Create `app/pkg/infra/telemetry/log.go`:

```go
package telemetry

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"

	"go.opentelemetry.io/otel/trace"
)

const defaultServiceName = "fupi"

// ServiceName is the name this process reports: OTEL_SERVICE_NAME, or "fupi" when it is not set. The log lines and the
// telemetry resource both use it, so a log line and the trace it belongs to name the same service.
func ServiceName() string {
	if name := strings.TrimSpace(os.Getenv("OTEL_SERVICE_NAME")); name != "" {
		return name
	}
	return defaultServiceName
}

// NewLogger builds the process logger. The format is "json" (one object per line, the shape the ELK pipeline parses) or
// "text" (for a terminal). Every record carries the service name, and the trace and span IDs of the context passed to the
// log call when that context holds a span, so use the ...Context forms (slog.InfoContext) on the request path.
func NewLogger(w io.Writer, format string, level slog.Level, service string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if format == "text" {
		handler = slog.NewTextHandler(w, opts)
	} else {
		handler = slog.NewJSONHandler(w, opts)
	}
	return slog.New(correlationHandler{handler.WithAttrs([]slog.Attr{slog.String("service", service)})})
}

// correlationHandler adds trace_id and span_id to every record whose context carries a span. It wraps the handler it
// returns from WithAttrs and WithGroup, so a logger derived with slog.With keeps adding them.
type correlationHandler struct {
	next slog.Handler
}

func (h correlationHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h correlationHandler) Handle(ctx context.Context, record slog.Record) error {
	if sc := trace.SpanContextFromContext(ctx); sc.HasTraceID() {
		record.AddAttrs(slog.String("trace_id", sc.TraceID().String()))
		if sc.HasSpanID() {
			record.AddAttrs(slog.String("span_id", sc.SpanID().String()))
		}
	}
	return h.next.Handle(ctx, record)
}

func (h correlationHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return correlationHandler{h.next.WithAttrs(attrs)}
}

func (h correlationHandler) WithGroup(name string) slog.Handler {
	return correlationHandler{h.next.WithGroup(name)}
}
```

- [ ] **Step 12: Run the tests**

Run: `gofmt -l app/config/app.go app/pkg/infra/telemetry; go test ./app/config ./app/pkg/infra/telemetry`
Expected: no gofmt output; both `ok`. (`TestNewLogger_DerivedLoggersKeepAddingTheIDs` is the one that fails if `WithAttrs`/`WithGroup` return the inner handler unwrapped.)

- [ ] **Step 13: Commit**

```bash
git add app/config/app.go app/config/app_test.go .env.example app/pkg/infra/telemetry
git commit -m "feat(telemetry): add the logging settings and the slog handler that carries trace IDs"
```

---

### Task 2: One trace ID: `tracing.GetTraceID` prefers the span

**Files:**
- Modify: `app/pkg/infra/tracing/tracer.go`
- Modify: `app/pkg/infra/tracing/doc.go` (replace the false description)
- Create: `app/pkg/infra/tracing/tracer_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `tracing.GetTraceID(ctx)` returns the W3C trace ID of the span in `ctx`, else the stored legacy value, else `""`. The zap logger (`app/pkg/infra/logger/app_logger.go`) and the gRPC client/interceptor already call it, so they carry the W3C ID without further edits.

- [ ] **Step 1: Write the failing tests**

Create `app/pkg/infra/tracing/tracer_test.go`:

```go
package tracing

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
)

const w3cTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"

func withSpan(t *testing.T, ctx context.Context) context.Context {
	t.Helper()
	traceID, err := trace.TraceIDFromHex(w3cTraceID)
	require.NoError(t, err)
	spanID, err := trace.SpanIDFromHex("00f067aa0ba902b7")
	require.NoError(t, err)
	return trace.ContextWithSpanContext(ctx, trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled,
	}))
}

func TestGetTraceID_PrefersTheW3CTraceIDOfTheSpan(t *testing.T) {
	ctx := context.WithValue(context.Background(), TraceIDKey, "legacy-id")

	assert.Equal(t, w3cTraceID, GetTraceID(withSpan(t, ctx)))
}

func TestGetTraceID_FallsBackToTheStoredIDWithoutASpan(t *testing.T) {
	ctx := context.WithValue(context.Background(), TraceIDKey, "legacy-id")

	assert.Equal(t, "legacy-id", GetTraceID(ctx))
}

func TestGetTraceID_IsEmptyWithNeitherASpanNorAStoredID(t *testing.T) {
	assert.Empty(t, GetTraceID(context.Background()))
}

// NewContext only generates a trace ID when there is none, so inside a span it must not shadow the W3C ID.
func TestNewContext_KeepsTheSpansTraceIDAndStillAddsTheOtherIDs(t *testing.T) {
	ctx := NewContext(withSpan(t, context.Background()))

	assert.Equal(t, w3cTraceID, GetTraceID(ctx))
	assert.NotEmpty(t, GetRequestID(ctx))
	assert.NotEmpty(t, GetCorrelationID(ctx))
}

func TestNewContext_GeneratesATraceIDWithoutASpan(t *testing.T) {
	ctx := NewContext(context.Background())

	assert.NotEmpty(t, GetTraceID(ctx))
	assert.NotEqual(t, w3cTraceID, GetTraceID(ctx))
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./app/pkg/infra/tracing`
Expected: FAIL `TestGetTraceID_PrefersTheW3CTraceIDOfTheSpan` (got `legacy-id`) and `TestNewContext_KeepsTheSpansTraceIDAndStillAddsTheOtherIDs`; the other three pass.

- [ ] **Step 3: Implement**

In `app/pkg/infra/tracing/tracer.go` add `"go.opentelemetry.io/otel/trace"` to the imports (after the `entity` import) and replace `GetTraceID`:

```go
// GetTraceID returns the W3C trace ID of the OpenTelemetry span in ctx, so logs, spans and outgoing calls carry one ID.
// Without a span it returns the ID NewContext stored, and "" when there is neither.
func GetTraceID(ctx context.Context) string {
	if sc := trace.SpanContextFromContext(ctx); sc.HasTraceID() {
		return sc.TraceID().String()
	}
	if id, ok := ctx.Value(TraceIDKey).(string); ok {
		return id
	}
	return ""
}
```

Replace the whole of `app/pkg/infra/tracing/doc.go` with:

```go
// Package tracing carries the request, correlation and trace identifiers of a request through its context, and between
// services in gRPC metadata.
//
// It is not an OpenTelemetry wrapper: spans, exporters and propagation live in app/pkg/infra/telemetry. GetTraceID
// returns the W3C trace ID of the OpenTelemetry span in the context, so the logs, the spans and the outgoing calls all
// show the same ID; the KSUID-style ID that NewContext generates is only the fallback for a context without a span.
package tracing
```

- [ ] **Step 4: Run the tests**

Run: `gofmt -l app/pkg/infra/tracing; go test ./app/pkg/infra/tracing ./app/pkg/infra/logger ./app/pkg/infra/clients/...`
Expected: no gofmt output; `ok` for each package that has tests.

- [ ] **Step 5: Commit**

```bash
git add app/pkg/infra/tracing
git commit -m "fix(tracing): report the W3C trace ID of the span and correct the package documentation"
```

---

### Task 3: Rate-limited error handler and `Unsampled`

**Files:**
- Modify: `go.mod` (two `require` lines, from the module cache)
- Create: `app/pkg/infra/telemetry/errors.go`, `errors_test.go`, `unsampled.go`, `unsampled_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `newErrorHandler(interval time.Duration, now func() time.Time) *errorHandler` (implements `otel.ErrorHandler`); `telemetry.Unsampled(ctx context.Context) context.Context`. Task 8 calls `Unsampled`; Task 9 installs the handler with `otel.SetErrorHandler`. The SDK modules this task adds are used by the tests of Tasks 3, 4, 7 and 8.

- [ ] **Step 1: Add the SDK modules from the module cache (no network)**

Run:

```bash
GOFLAGS=-mod=mod GOPROXY=off go get go.opentelemetry.io/otel/sdk@v1.44.0 go.opentelemetry.io/otel/sdk/metric@v1.44.0
git diff --stat go.mod go.sum
```

Expected: `go.mod` gains exactly `go.opentelemetry.io/otel/sdk v1.44.0` and `go.opentelemetry.io/otel/sdk/metric v1.44.0`; `go.sum` is unchanged (both hashes are already in it). If `go get` fails with `module lookup disabled by GOPROXY=off`, the cache lacks something: stop and ask for the download go-ahead instead of retrying with the network on.

- [ ] **Step 2: Write the failing tests**

Create `app/pkg/infra/telemetry/errors_test.go`:

```go
package telemetry

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// captureDefaultLogger sends the default slog logger to the returned buffer until the test ends.
func captureDefaultLogger(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &buf
}

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

func TestErrorHandler_LogsADistinctErrorOncePerInterval(t *testing.T) {
	buf := captureDefaultLogger(t)
	clock := &fakeClock{now: time.Unix(1_000, 0)}
	handler := newErrorHandler(time.Minute, clock.Now)
	refused := errors.New("rpc error: code = Unavailable desc = connection refused")

	for range 20 {
		handler.Handle(refused)
		clock.now = clock.now.Add(2 * time.Second)
	}
	assert.Equal(t, 1, strings.Count(buf.String(), "telemetry export failed"), "20 repeats within a minute are one line")

	clock.now = clock.now.Add(time.Minute)
	handler.Handle(refused)
	assert.Equal(t, 2, strings.Count(buf.String(), "telemetry export failed"), "the same error is logged again after the interval")
}

func TestErrorHandler_LogsEachDistinctErrorOnItsOwn(t *testing.T) {
	buf := captureDefaultLogger(t)
	handler := newErrorHandler(time.Minute, (&fakeClock{now: time.Unix(1_000, 0)}).Now)

	handler.Handle(errors.New("traces: connection refused"))
	handler.Handle(errors.New("metrics: connection refused"))
	handler.Handle(errors.New("traces: connection refused"))

	assert.Equal(t, 2, strings.Count(buf.String(), "telemetry export failed"))
	assert.Contains(t, buf.String(), "traces: connection refused")
	assert.Contains(t, buf.String(), "metrics: connection refused")
}

func TestErrorHandler_IgnoresANilError(t *testing.T) {
	buf := captureDefaultLogger(t)

	newErrorHandler(time.Minute, time.Now).Handle(nil)

	assert.Empty(t, buf.String())
}

// A flood of ever-different messages must not grow the handler's memory without bound.
func TestErrorHandler_ForgetsOldErrorsWhenTooManyAreDistinct(t *testing.T) {
	captureDefaultLogger(t)
	clock := &fakeClock{now: time.Unix(1_000, 0)}
	handler := newErrorHandler(time.Minute, clock.Now)

	for i := range 10 * maxTrackedErrors {
		handler.Handle(fmt.Errorf("error number %d", i))
	}

	assert.LessOrEqual(t, len(handler.lastSeen), maxTrackedErrors)
}
```

Create `app/pkg/infra/telemetry/unsampled_test.go`:

```go
package telemetry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// recordingProvider is a tracer provider with the SDK's default (parent-based, always-on) sampler.
func recordingProvider(t *testing.T) (*sdktrace.TracerProvider, *tracetest.InMemoryExporter) {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	return provider, exporter
}

func TestUnsampled_SpansBeneathItAreNotRecorded(t *testing.T) {
	provider, exporter := recordingProvider(t)
	tracer := provider.Tracer("test")

	_, control := tracer.Start(context.Background(), "control")
	control.End()
	require.Len(t, exporter.GetSpans(), 1, "a root span is recorded, so the sampler really is on")
	exporter.Reset()

	ctx, probe := tracer.Start(Unsampled(context.Background()), "pg ping")
	assert.False(t, probe.IsRecording())
	assert.True(t, trace.SpanContextFromContext(ctx).IsValid(), "the child still has valid IDs, so nothing under it breaks")
	probe.End()

	assert.Empty(t, exporter.GetSpans())
}

func TestUnsampled_KeepsTheIDsOfAnExistingSpanButStopsRecording(t *testing.T) {
	provider, exporter := recordingProvider(t)
	tracer := provider.Tracer("test")

	parentCtx, parent := tracer.Start(context.Background(), "request")
	defer parent.End()

	ctx := Unsampled(parentCtx)
	childCtx, child := tracer.Start(ctx, "ping")
	child.End()

	assert.Equal(t, parent.SpanContext().TraceID(), trace.SpanContextFromContext(ctx).TraceID())
	assert.False(t, trace.SpanContextFromContext(ctx).IsSampled())
	assert.Equal(t, parent.SpanContext().TraceID(), trace.SpanContextFromContext(childCtx).TraceID())
	assert.Empty(t, exporter.GetSpans(), "the child of an unsampled parent is not recorded")
	assert.True(t, trace.SpanContextFromContext(parentCtx).IsSampled(), "the caller's own context is not changed")
}

func TestUnsampled_GivesEachCallItsOwnIDs(t *testing.T) {
	first := trace.SpanContextFromContext(Unsampled(context.Background()))
	second := trace.SpanContextFromContext(Unsampled(context.Background()))

	assert.True(t, first.IsValid())
	assert.NotEqual(t, first.TraceID(), second.TraceID())
}
```

- [ ] **Step 3: Run them to verify they fail**

Run: `go test ./app/pkg/infra/telemetry`
Expected: build failure `undefined: newErrorHandler` / `undefined: Unsampled`.

- [ ] **Step 4: Implement**

Create `app/pkg/infra/telemetry/errors.go`:

```go
package telemetry

import (
	"log/slog"
	"sync"
	"time"
)

// maxTrackedErrors bounds the memory of the handler: when more distinct errors than this are seen within one interval the
// oldest records are forgotten, which at worst repeats a line.
const maxTrackedErrors = 64

// errorHandler is the OpenTelemetry error handler. The SDK reports every failed export to it, which for an unreachable
// collector means one error every few seconds for as long as the collector is away. It logs each distinct error at
// most once per interval, so the log carries one line a minute instead.
type errorHandler struct {
	mu       sync.Mutex
	interval time.Duration
	now      func() time.Time
	lastSeen map[string]time.Time
}

func newErrorHandler(interval time.Duration, now func() time.Time) *errorHandler {
	return &errorHandler{interval: interval, now: now, lastSeen: map[string]time.Time{}}
}

// Handle implements otel.ErrorHandler.
func (h *errorHandler) Handle(err error) {
	if err == nil {
		return
	}
	if !h.admit(err.Error()) {
		return
	}
	slog.Warn("telemetry export failed", "error", err)
}

// admit reports whether message has not been logged within the interval, and records that it is being logged now.
func (h *errorHandler) admit(message string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	now := h.now()
	if last, seen := h.lastSeen[message]; seen && now.Sub(last) < h.interval {
		return false
	}
	if len(h.lastSeen) >= maxTrackedErrors {
		for key, last := range h.lastSeen {
			if now.Sub(last) >= h.interval {
				delete(h.lastSeen, key)
			}
		}
		if len(h.lastSeen) >= maxTrackedErrors {
			clear(h.lastSeen)
		}
	}
	h.lastSeen[message] = now
	return true
}
```

Create `app/pkg/infra/telemetry/unsampled.go`:

```go
package telemetry

import (
	"context"
	"encoding/binary"
	"math/rand/v2"

	"go.opentelemetry.io/otel/trace"
)

// Unsampled returns ctx carrying a parent span that is valid but not sampled. A parent-based sampler, the SDK default,
// records nothing beneath such a parent, so work done under it (the readiness checks' Postgres and Redis pings) leaves
// no spans and does not start a new trace on every probe. If ctx already holds a span its IDs are kept and only the
// sampled flag is cleared. With a plain always_on sampler this has no effect.
func Unsampled(ctx context.Context) context.Context {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		sc = trace.NewSpanContext(trace.SpanContextConfig{TraceID: randomTraceID(), SpanID: randomSpanID()})
	}
	return trace.ContextWithSpanContext(ctx, sc.WithTraceFlags(sc.TraceFlags()&^trace.FlagsSampled))
}

// The IDs only have to be valid (non-zero); nothing is exported under them.
func randomTraceID() (id trace.TraceID) {
	binary.BigEndian.PutUint64(id[:8], rand.Uint64()|1)
	binary.BigEndian.PutUint64(id[8:], rand.Uint64())
	return id
}

func randomSpanID() (id trace.SpanID) {
	binary.BigEndian.PutUint64(id[:], rand.Uint64()|1)
	return id
}
```

- [ ] **Step 5: Run the tests**

Run: `gofmt -l app/pkg/infra/telemetry; go test -race ./app/pkg/infra/telemetry`
Expected: no gofmt output; `ok`. (`maxTrackedErrors` is used only inside the package, so the `unused` linter stays quiet; `errorLogInterval` is deliberately defined in Task 9 where it is first used.)

- [ ] **Step 6: Commit**

```bash
git add go.mod app/pkg/infra/telemetry
git commit -m "feat(telemetry): add the rate-limited error handler and Unsampled"
```

---

### Task 4: HTTP span, metrics and access-log middleware

**Files:**
- Create: `app/pkg/infra/server/middleware/route.go`, `otel.go`, `access_log.go`
- Create: `app/pkg/infra/server/middleware/otel_test.go`, `access_log_test.go`

**Interfaces:**
- Consumes: `telemetry.NewLogger` (Task 1) in `access_log_test.go`; the SDK modules (Task 3).
- Produces: `middleware.OTelConfig{SkipPaths []string; TracerProvider trace.TracerProvider; MeterProvider metric.MeterProvider; Propagator propagation.TextMapPropagator}`; `middleware.OTelMiddleware(cfg OTelConfig) fiber.Handler`; `middleware.AccessLogMiddleware(quietPaths []string) fiber.Handler`; unexported `settle`, `routeTemplate`, `pathSet`, `inSet`. Task 5 wires both middlewares into `NewServer`.

- [ ] **Step 1: Write the failing tests for the span middleware**

Create `app/pkg/infra/server/middleware/otel_test.go`. It runs the middleware inside real Fiber, with an in-memory span exporter and a manual metric reader, and covers: span name and attributes, the root route versus an unmatched path, a wrong method, a returned error's status, 5xx marking, a panic, probe exclusion (including `/health/`, `/Health`), an incoming `traceparent`, a root trace without one, the histogram (name, unit, buckets, exactly four labels), no route label when unmatched, active requests, privacy of query string and headers, and the buffer-reuse test over a real listener.

```go
package middleware

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	fiberrecover "github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

var probePaths = []string{"/health", "/health/ready"}

// fixture is a Fiber app with OTelMiddleware in front of a few routes, wired to an in-memory span exporter and a manual
// metric reader so a test can read back exactly what would have been exported.
type fixture struct {
	app     *fiber.App
	spans   *tracetest.InMemoryExporter
	metrics *sdkmetric.ManualReader
}

func newFixture(t *testing.T) *fixture {
	t.Helper()

	spans := tracetest.NewInMemoryExporter()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(spans)))
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() {
		_ = tracerProvider.Shutdown(context.Background())
		_ = meterProvider.Shutdown(context.Background())
	})

	f := &fixture{app: fiber.New(), spans: spans, metrics: reader}
	f.app.Use(OTelMiddleware(OTelConfig{
		SkipPaths:      probePaths,
		TracerProvider: tracerProvider,
		MeterProvider:  meterProvider,
		Propagator:     propagation.TraceContext{},
	}))
	f.app.Use(fiberrecover.New())
	f.app.Get("/", func(c *fiber.Ctx) error { return c.SendString("root") })
	f.app.Get("/users/:id", func(c *fiber.Ctx) error { return c.SendString("user") })
	f.app.Get("/health", func(c *fiber.Ctx) error { return c.SendString("ok") })
	f.app.Get("/health/ready", func(c *fiber.Ctx) error { return c.SendString("ok") })
	f.app.Get("/conflict", func(c *fiber.Ctx) error { return fiber.NewError(fiber.StatusConflict, "taken") })
	f.app.Get("/boom", func(c *fiber.Ctx) error { return errors.New("boom") })
	f.app.Get("/panic", func(c *fiber.Ctx) error { panic("kaboom") })
	return f
}

func (f *fixture) do(t *testing.T, method, target string, headers ...string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := f.app.Test(req)
	require.NoError(t, err)
	return resp
}

func (f *fixture) onlySpan(t *testing.T) tracetest.SpanStub {
	t.Helper()
	spans := f.spans.GetSpans()
	require.Len(t, spans, 1)
	return spans[0]
}

func attrs(kvs []attribute.KeyValue) map[string]attribute.Value {
	out := make(map[string]attribute.Value, len(kvs))
	for _, kv := range kvs {
		out[string(kv.Key)] = kv.Value
	}
	return out
}

func (f *fixture) collect(t *testing.T) map[string]metricdata.Metrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, f.metrics.Collect(context.Background(), &rm))
	out := map[string]metricdata.Metrics{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			out[m.Name] = m
		}
	}
	return out
}

func (f *fixture) durationPoints(t *testing.T) []metricdata.HistogramDataPoint[float64] {
	t.Helper()
	m, ok := f.collect(t)["http.server.request.duration"]
	if !ok {
		return nil
	}
	histogram, ok := m.Data.(metricdata.Histogram[float64])
	require.True(t, ok, "the duration must be a float64 histogram, got %T", m.Data)
	return histogram.DataPoints
}

func TestOTelMiddleware_NamesTheSpanAfterTheRouteTemplate(t *testing.T) {
	f := newFixture(t)

	resp := f.do(t, "GET", "/users/42", "User-Agent", "curl/8")
	assert.Equal(t, 200, resp.StatusCode)

	span := f.onlySpan(t)
	assert.Equal(t, "GET /users/:id", span.Name, "the template, never the id, so span names stay low-cardinality")
	assert.Equal(t, trace.SpanKindServer, span.SpanKind)
	assert.Equal(t, codes.Unset, span.Status.Code)

	got := attrs(span.Attributes)
	assert.Equal(t, "GET", got["http.request.method"].AsString())
	assert.Equal(t, "/users/:id", got["http.route"].AsString())
	assert.Equal(t, int64(200), got["http.response.status_code"].AsInt64())
	assert.Equal(t, "/users/42", got["url.path"].AsString())
	assert.Equal(t, "http", got["url.scheme"].AsString())
	assert.Equal(t, "example.com", got["server.address"].AsString())
	assert.Equal(t, "curl/8", got["user_agent.original"].AsString())
}

// Fiber reports its catch-all "/" for a path that matched no route, so "/" is only a route when the path is "/".
func TestOTelMiddleware_TellsTheRootRouteFromAnUnmatchedPath(t *testing.T) {
	f := newFixture(t)

	f.do(t, "GET", "/")
	root := f.onlySpan(t)
	assert.Equal(t, "GET /", root.Name)
	assert.Equal(t, "/", attrs(root.Attributes)["http.route"].AsString())
	f.spans.Reset()

	resp := f.do(t, "GET", "/no/such/path/123")
	assert.Equal(t, 404, resp.StatusCode)
	missing := f.onlySpan(t)
	assert.Equal(t, "GET", missing.Name, "an unmatched route is named after the method only")
	_, hasRoute := attrs(missing.Attributes)["http.route"]
	assert.False(t, hasRoute, "the raw path must never become the route")
	assert.Equal(t, codes.Unset, missing.Status.Code, "a 404 is not a server error")
	assert.Equal(t, "/no/such/path/123", attrs(missing.Attributes)["url.path"].AsString())
}

func TestOTelMiddleware_AWrongMethodIsAnUnmatchedRouteToo(t *testing.T) {
	f := newFixture(t)

	resp := f.do(t, "DELETE", "/users/42")

	assert.Equal(t, 405, resp.StatusCode)
	span := f.onlySpan(t)
	assert.Equal(t, "DELETE", span.Name)
	assert.Equal(t, int64(405), attrs(span.Attributes)["http.response.status_code"].AsInt64())
	_, hasRoute := attrs(span.Attributes)["http.route"]
	assert.False(t, hasRoute)
}

// A handler that returns an error has not written its response yet; the status the span records must be the final one.
func TestOTelMiddleware_RecordsTheStatusOfAReturnedError(t *testing.T) {
	f := newFixture(t)

	resp := f.do(t, "GET", "/conflict")

	assert.Equal(t, 409, resp.StatusCode, "the client still gets the error handler's answer")
	span := f.onlySpan(t)
	assert.Equal(t, int64(409), attrs(span.Attributes)["http.response.status_code"].AsInt64())
	assert.Equal(t, codes.Unset, span.Status.Code, "a 4xx is the client's error, not the server span's")
}

func TestOTelMiddleware_AFiveHundredMarksTheSpanAsAnError(t *testing.T) {
	f := newFixture(t)

	resp := f.do(t, "GET", "/boom")

	assert.Equal(t, 500, resp.StatusCode)
	span := f.onlySpan(t)
	assert.Equal(t, codes.Error, span.Status.Code)
	assert.Equal(t, "500", attrs(span.Attributes)["error.type"].AsString())
	assert.Equal(t, "GET /boom", span.Name)
}

func TestOTelMiddleware_APanicBecomesAFiveHundredSpanError(t *testing.T) {
	f := newFixture(t)

	resp := f.do(t, "GET", "/panic")

	assert.Equal(t, 500, resp.StatusCode)
	span := f.onlySpan(t)
	assert.Equal(t, codes.Error, span.Status.Code)
	assert.Equal(t, int64(500), attrs(span.Attributes)["http.response.status_code"].AsInt64())
}

func TestOTelMiddleware_ProbesGetNoSpanAndNoMetric(t *testing.T) {
	f := newFixture(t)

	for _, target := range []string{"/health", "/health/", "/Health", "/health/ready", "/HEALTH/READY/"} {
		resp := f.do(t, "GET", target)
		assert.Equal(t, 200, resp.StatusCode, target+" must still be served")
	}

	assert.Empty(t, f.spans.GetSpans())
	assert.Empty(t, f.durationPoints(t))
}

func TestOTelMiddleware_ContinuesAnIncomingTraceparent(t *testing.T) {
	f := newFixture(t)
	const traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	const parentSpanID = "00f067aa0ba902b7"

	var inHandler trace.SpanContext
	f.app.Get("/trace", func(c *fiber.Ctx) error {
		inHandler = trace.SpanContextFromContext(c.UserContext())
		return c.SendStatus(200)
	})

	f.do(t, "GET", "/trace", "traceparent", "00-"+traceID+"-"+parentSpanID+"-01")

	span := f.onlySpan(t)
	assert.Equal(t, traceID, span.SpanContext.TraceID().String(), "the request joins the caller's trace")
	assert.Equal(t, parentSpanID, span.Parent.SpanID().String())
	assert.True(t, span.Parent.IsRemote())
	assert.Equal(t, span.SpanContext.SpanID(), inHandler.SpanID(), "the handler sees the server span, so what it starts nests under it")
	assert.Equal(t, traceID, inHandler.TraceID().String())
}

func TestOTelMiddleware_StartsARootTraceWithoutATraceparent(t *testing.T) {
	f := newFixture(t)

	f.do(t, "GET", "/users/1")

	span := f.onlySpan(t)
	assert.False(t, span.Parent.IsValid())
	assert.True(t, span.SpanContext.TraceID().IsValid())
}

func TestOTelMiddleware_RecordsTheRequestDurationHistogram(t *testing.T) {
	f := newFixture(t)

	f.do(t, "GET", "/users/42")

	m := f.collect(t)["http.server.request.duration"]
	assert.Equal(t, "s", m.Unit, "the unit is what makes the collector name it ..._seconds")
	points := f.durationPoints(t)
	require.Len(t, points, 1)
	assert.Equal(t, uint64(1), points[0].Count)
	assert.Equal(t, []float64{0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10}, points[0].Bounds)

	got := points[0].Attributes
	method, _ := got.Value("http.request.method")
	route, _ := got.Value("http.route")
	status, _ := got.Value("http.response.status_code")
	scheme, _ := got.Value("url.scheme")
	assert.Equal(t, "GET", method.AsString())
	assert.Equal(t, "/users/:id", route.AsString())
	assert.Equal(t, int64(200), status.AsInt64())
	assert.Equal(t, "http", scheme.AsString())
	assert.Equal(t, 4, got.Len(), "exactly the four labels the dashboard and the alert use, nothing high-cardinality")
}

func TestOTelMiddleware_AnUnmatchedRouteHasNoRouteLabel(t *testing.T) {
	f := newFixture(t)

	f.do(t, "GET", "/no/such/path/123")

	points := f.durationPoints(t)
	require.Len(t, points, 1)
	_, hasRoute := points[0].Attributes.Value("http.route")
	assert.False(t, hasRoute)
	status, _ := points[0].Attributes.Value("http.response.status_code")
	assert.Equal(t, int64(404), status.AsInt64())
}

func TestOTelMiddleware_CountsRequestsInFlight(t *testing.T) {
	f := newFixture(t)

	var duringRequest int64
	f.app.Get("/slow", func(c *fiber.Ctx) error {
		duringRequest = f.activeRequests(t)
		return c.SendStatus(200)
	})

	f.do(t, "GET", "/slow")

	assert.Equal(t, int64(1), duringRequest, "the request being served is counted")
	assert.Equal(t, int64(0), f.activeRequests(t), "and is no longer counted once it is done")
}

func (f *fixture) activeRequests(t *testing.T) int64 {
	t.Helper()
	m, ok := f.collect(t)["http.server.active_requests"]
	require.True(t, ok)
	sum, ok := m.Data.(metricdata.Sum[int64])
	require.True(t, ok, "got %T", m.Data)
	var total int64
	for _, point := range sum.DataPoints {
		total += point.Value
	}
	return total
}

// Privacy: the query string and the headers can hold tokens and personal data, and none of it is recorded.
func TestOTelMiddleware_NeverRecordsTheQueryStringOrHeaders(t *testing.T) {
	f := newFixture(t)

	f.do(t, "GET", "/users/42?token=query-secret&email=jane@example.com",
		"Authorization", "Bearer header-secret", "Cookie", "session=cookie-secret", "X-Forwarded-For", "203.0.113.9")

	span := f.onlySpan(t)
	for key, value := range attrs(span.Attributes) {
		assert.NotContains(t, value.String(), "query-secret", key)
		assert.NotContains(t, value.String(), "jane@example.com", key)
		assert.NotContains(t, value.String(), "header-secret", key)
		assert.NotContains(t, value.String(), "cookie-secret", key)
		assert.NotContains(t, value.String(), "203.0.113.9", key)
		assert.False(t, strings.HasPrefix(key, "http.request.header"), key)
		assert.NotEqual(t, "client.address", key)
	}
	assert.Equal(t, "/users/42", attrs(span.Attributes)["url.path"].AsString())
}

// fasthttp reuses a connection's buffers for the next request, and a span is exported after its request is over: an
// attribute that still pointed into those buffers would show the next request's path.
func TestOTelMiddleware_AttributesSurviveTheNextRequestOnTheSameConnection(t *testing.T) {
	f := newFixture(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = f.app.Listener(ln) }()
	t.Cleanup(func() { _ = f.app.Shutdown() })
	client := &http.Client{}
	base := "http://" + ln.Addr().String()

	for _, target := range []string{"/first-path-aaaaaaaa", "/second-path-bbbbbbb"} {
		resp, err := client.Get(base + target)
		require.NoError(t, err)
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}

	spans := f.spans.GetSpans()
	require.Len(t, spans, 2)
	assert.Equal(t, "/first-path-aaaaaaaa", attrs(spans[0].Attributes)["url.path"].AsString())
	assert.Equal(t, "/second-path-bbbbbbb", attrs(spans[1].Attributes)["url.path"].AsString())
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./app/pkg/infra/server/middleware`
Expected: build failure `undefined: OTelMiddleware` (and `OTelConfig`).

- [ ] **Step 3: Implement the helpers and the middleware**

Create `app/pkg/infra/server/middleware/route.go`:

```go
package middleware

import (
	"strings"

	"github.com/gofiber/fiber/v2"
)

// settle runs the app's error handler for an error a handler returned, so the response, and with it the status code, is
// final when the caller reads it. It reports the error as handled. Fiber's own logger middleware does the same; without
// it a middleware that wraps the handlers would see the status 200 of a request that is about to become a 500.
func settle(c *fiber.Ctx, err error) {
	if err == nil {
		return
	}
	if handlerErr := c.App().ErrorHandler(c, err); handlerErr != nil {
		_ = c.SendStatus(fiber.StatusInternalServerError)
	}
}

// routeTemplate returns the route pattern that served the request (/users/:id), which is the only form of the path that
// may become a metric label or part of a span name. The second result is false when no route matched. Fiber then still
// reports the catch-all "/" of the first middleware, so a "/" route for any other request path means "not found" or
// "method not allowed", and the raw path must not be used. requestPath is the path as it was before the handlers ran.
func routeTemplate(c *fiber.Ctx, requestPath string) (string, bool) {
	route := c.Route()
	if route == nil || (route.Path == "/" && requestPath != "/") {
		return "", false
	}
	return route.Path, true
}

// pathSet builds a lookup of paths for inSet.
func pathSet(paths []string) map[string]struct{} {
	set := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		set[strings.ToLower(path)] = struct{}{}
	}
	return set
}

// inSet reports whether path is in set the way Fiber's router would match it: ignoring case and a trailing slash.
func inSet(set map[string]struct{}, path string) bool {
	path = strings.ToLower(path)
	if _, ok := set[path]; ok {
		return true
	}
	if len(path) > 1 {
		_, ok := set[strings.TrimSuffix(path, "/")]
		return ok
	}
	return false
}
```

Create `app/pkg/infra/server/middleware/otel.go`:

```go
package middleware

import (
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const instrumentationName = "github.com/sanctumlabs/fupi/app/pkg/infra/server/middleware"

// OTelConfig configures OTelMiddleware. The zero value uses the global tracer provider, meter provider and propagator,
// which is what the running API wants; tests inject their own.
type OTelConfig struct {
	// SkipPaths are requests that get no span and no metric (the health probes).
	SkipPaths      []string
	TracerProvider trace.TracerProvider
	MeterProvider  metric.MeterProvider
	Propagator     propagation.TextMapPropagator
}

// OTelMiddleware traces and measures every HTTP request: a server span named "METHOD route" (continuing an incoming W3C
// traceparent), stored in the request's user context so everything the handler starts nests under it, plus the
// http.server.request.duration histogram and the http.server.active_requests counter. Put it first in the chain, so the
// middleware behind it see the span. It never records the query string, the client address or request headers.
func OTelMiddleware(cfg OTelConfig) fiber.Handler {
	if cfg.TracerProvider == nil {
		cfg.TracerProvider = otel.GetTracerProvider()
	}
	if cfg.MeterProvider == nil {
		cfg.MeterProvider = otel.GetMeterProvider()
	}
	if cfg.Propagator == nil {
		cfg.Propagator = otel.GetTextMapPropagator()
	}

	tracer := cfg.TracerProvider.Tracer(instrumentationName)
	meter := cfg.MeterProvider.Meter(instrumentationName)
	duration, err := meter.Float64Histogram("http.server.request.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Duration of HTTP server requests."),
		metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10),
	)
	if err != nil {
		otel.Handle(err)
	}
	active, err := meter.Int64UpDownCounter("http.server.active_requests",
		metric.WithUnit("{request}"),
		metric.WithDescription("Number of HTTP requests being served."),
	)
	if err != nil {
		otel.Handle(err)
	}
	skip := pathSet(cfg.SkipPaths)

	return func(c *fiber.Ctx) error {
		// fasthttp reuses its buffers once the handler returns, and spans and metrics are exported afterwards, so
		// every string taken from the request is copied.
		path := strings.Clone(c.Path())
		if inSet(skip, path) {
			return c.Next()
		}

		start := time.Now()
		method := strings.Clone(c.Method())
		methodAttr := attribute.String("http.request.method", method)
		schemeAttr := attribute.String("url.scheme", strings.Clone(c.Protocol()))

		ctx := cfg.Propagator.Extract(c.UserContext(), headerCarrier{&c.Request().Header})
		ctx, span := tracer.Start(ctx, method,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				methodAttr,
				schemeAttr,
				attribute.String("url.path", path),
				attribute.String("server.address", strings.Clone(c.Hostname())),
				attribute.String("user_agent.original", strings.Clone(c.Get(fiber.HeaderUserAgent))),
			),
		)
		c.SetUserContext(ctx)

		inFlight := metric.WithAttributes(methodAttr, schemeAttr)
		active.Add(ctx, 1, inFlight)

		settle(c, c.Next())

		status := c.Response().StatusCode()
		statusAttr := attribute.Int("http.response.status_code", status)
		spanAttrs := []attribute.KeyValue{statusAttr}
		metricAttrs := []attribute.KeyValue{methodAttr, schemeAttr, statusAttr}
		if route, matched := routeTemplate(c, path); matched {
			routeAttr := attribute.String("http.route", route)
			spanAttrs = append(spanAttrs, routeAttr)
			metricAttrs = append(metricAttrs, routeAttr)
			span.SetName(method + " " + route)
		}
		span.SetAttributes(spanAttrs...)
		if status >= fiber.StatusInternalServerError {
			span.SetAttributes(attribute.String("error.type", strconv.Itoa(status)))
			span.SetStatus(codes.Error, "")
		}
		span.End()

		active.Add(ctx, -1, inFlight)
		duration.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(metricAttrs...))
		return nil
	}
}

// requestHeader is the part of fasthttp's request header the propagator needs.
type requestHeader interface {
	Peek(key string) []byte
	Set(key, value string)
	VisitAll(f func(key, value []byte))
}

// headerCarrier reads incoming headers for the propagator. string(...) copies, so what it returns outlives the request.
type headerCarrier struct{ header requestHeader }

func (c headerCarrier) Get(key string) string { return string(c.header.Peek(key)) }

func (c headerCarrier) Set(key, value string) { c.header.Set(key, value) }

func (c headerCarrier) Keys() []string {
	var keys []string
	c.header.VisitAll(func(key, _ []byte) { keys = append(keys, string(key)) })
	return keys
}
```

- [ ] **Step 4: Run the tests**

Run: `gofmt -l app/pkg/infra/server/middleware; go test -race -count=1 ./app/pkg/infra/server/middleware`
Expected: no gofmt output; `ok`.

- [ ] **Step 5: Mutation check: the buffer-reuse test must bite**

Temporarily change `path := strings.Clone(c.Path())` in `otel.go` to `path := c.Path()` and run:

Run: `go test -count=3 -run AttributesSurviveTheNextRequest ./app/pkg/infra/server/middleware`
Expected: FAIL (the first span's `url.path` shows the second request's path). Restore the `strings.Clone` and re-run; expected: `ok`. If the test passes with the clone removed, the test proves nothing: stop and fix the test before going on.

- [ ] **Step 6: Write the failing tests for the access log**

Create `app/pkg/infra/server/middleware/access_log_test.go`:

```go
package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	fiberrecover "github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/sanctumlabs/fupi/app/pkg/infra/telemetry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// logTo installs a JSON default logger at the given level that writes to the returned buffer until the test ends.
func logTo(t *testing.T, level slog.Level) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(telemetry.NewLogger(&buf, "json", level, "fupi-test"))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &buf
}

func logLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for _, raw := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if raw == "" {
			continue
		}
		var line map[string]any
		require.NoError(t, json.Unmarshal([]byte(raw), &line), raw)
		lines = append(lines, line)
	}
	return lines
}

// accessLogApp is the chain the server uses: tracing first, then the request ID, then the access log.
func accessLogApp(t *testing.T) *fiber.App {
	t.Helper()
	provider := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	app := fiber.New()
	app.Use(OTelMiddleware(OTelConfig{SkipPaths: probePaths, TracerProvider: provider, Propagator: propagation.TraceContext{}}))
	app.Use(RequestIdMiddleware())
	app.Use(AccessLogMiddleware(probePaths))
	app.Use(fiberrecover.New())
	app.Get("/users/:id", func(c *fiber.Ctx) error { return c.SendString("hello") })
	app.Get("/conflict", func(c *fiber.Ctx) error { return fiber.NewError(fiber.StatusConflict, "taken") })
	app.Get("/panic", func(c *fiber.Ctx) error { panic("kaboom") })
	app.Get("/health", func(c *fiber.Ctx) error { return c.SendString("ok") })
	return app
}

func TestAccessLog_WritesOneLineWithTheRequestsFacts(t *testing.T) {
	buf := logTo(t, slog.LevelInfo)
	app := accessLogApp(t)

	req := httptest.NewRequest("GET", "/users/42?token=secret", nil)
	req.Header.Set("X-Request-ID", "req-123")
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	lines := logLines(t, buf)
	require.Len(t, lines, 1)
	line := lines[0]
	assert.Equal(t, "request", line["msg"])
	assert.Equal(t, "GET", line["method"])
	assert.Equal(t, "/users/:id", line["route"])
	assert.Equal(t, "/users/42", line["path"], "the query string is not logged")
	assert.EqualValues(t, 200, line["status"])
	assert.EqualValues(t, 5, line["bytes"])
	assert.Equal(t, "req-123", line["request_id"])
	assert.GreaterOrEqual(t, line["duration_ms"], float64(0))
	assert.Equal(t, "fupi-test", line["service"])
	assert.NotContains(t, buf.String(), "secret")
}

func TestAccessLog_CarriesTheTraceAndSpanIDsOfTheRequest(t *testing.T) {
	buf := logTo(t, slog.LevelInfo)
	app := accessLogApp(t)
	var spanContext trace.SpanContext
	app.Get("/trace", func(c *fiber.Ctx) error {
		spanContext = trace.SpanContextFromContext(c.UserContext())
		return c.SendStatus(200)
	})

	resp, err := app.Test(httptest.NewRequest("GET", "/trace", nil))
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	lines := logLines(t, buf)
	require.Len(t, lines, 1)
	require.True(t, spanContext.IsValid())
	assert.Equal(t, spanContext.TraceID().String(), lines[0]["trace_id"])
	assert.Equal(t, spanContext.SpanID().String(), lines[0]["span_id"])
}

func TestAccessLog_LogsTheFinalStatusOfAReturnedErrorAndOfAPanic(t *testing.T) {
	buf := logTo(t, slog.LevelInfo)
	app := accessLogApp(t)

	for _, target := range []string{"/conflict", "/panic"} {
		_, err := app.Test(httptest.NewRequest("GET", target, nil))
		require.NoError(t, err)
	}

	lines := logLines(t, buf)
	require.Len(t, lines, 2)
	assert.EqualValues(t, 409, lines[0]["status"])
	assert.EqualValues(t, 500, lines[1]["status"])
}

func TestAccessLog_AnUnmatchedPathHasNoRoute(t *testing.T) {
	buf := logTo(t, slog.LevelInfo)
	app := accessLogApp(t)

	_, err := app.Test(httptest.NewRequest("GET", "/no/such/path", nil))
	require.NoError(t, err)

	lines := logLines(t, buf)
	require.Len(t, lines, 1)
	assert.Equal(t, "", lines[0]["route"])
	assert.Equal(t, "/no/such/path", lines[0]["path"])
	assert.EqualValues(t, 404, lines[0]["status"])
}

func TestAccessLog_ProbesAreLoggedAtDebugOnly(t *testing.T) {
	buf := logTo(t, slog.LevelInfo)
	app := accessLogApp(t)

	_, err := app.Test(httptest.NewRequest("GET", "/health", nil))
	require.NoError(t, err)
	assert.Empty(t, buf.String(), "at info level a probe leaves no line")

	debug := logTo(t, slog.LevelDebug)
	_, err = app.Test(httptest.NewRequest("GET", "/health", nil))
	require.NoError(t, err)
	lines := logLines(t, debug)
	require.Len(t, lines, 1)
	assert.Equal(t, "DEBUG", lines[0]["level"])
	assert.Equal(t, "/health", lines[0]["path"])
}
```

- [ ] **Step 7: Run them to verify they fail**

Run: `go test ./app/pkg/infra/server/middleware -run AccessLog`
Expected: build failure `undefined: AccessLogMiddleware`.

- [ ] **Step 8: Implement the access log**

Create `app/pkg/infra/server/middleware/access_log.go`:

```go
package middleware

import (
	"log/slog"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

// AccessLogMiddleware writes one line per request to the default slog logger: method, route, path, status, duration,
// response bytes and request ID. The line is logged with the request context, so it carries the trace and span IDs when
// OTelMiddleware is in front of it. Requests to quietPaths (the health probes) are logged at debug level only, so a
// probe polled every few seconds does not fill the log. Put it behind RequestIdMiddleware.
func AccessLogMiddleware(quietPaths []string) fiber.Handler {
	quiet := pathSet(quietPaths)

	return func(c *fiber.Ctx) error {
		start := time.Now()
		path := strings.Clone(c.Path())

		settle(c, c.Next())

		level := slog.LevelInfo
		if inSet(quiet, path) {
			level = slog.LevelDebug
		}
		route, _ := routeTemplate(c, path)
		requestID, _ := c.Locals("requestid").(string)

		slog.LogAttrs(c.UserContext(), level, "request",
			slog.String("method", c.Method()),
			slog.String("route", route),
			slog.String("path", path),
			slog.Int("status", c.Response().StatusCode()),
			slog.Float64("duration_ms", float64(time.Since(start).Microseconds())/1000),
			slog.Int("bytes", len(c.Response().Body())),
			slog.String("request_id", requestID),
		)
		return nil
	}
}
```

- [ ] **Step 9: Run the package tests**

Run: `gofmt -l app/pkg/infra/server/middleware; go test -race -count=1 ./app/pkg/infra/server/middleware`
Expected: no gofmt output; `ok`.

- [ ] **Step 10: Commit**

```bash
git add app/pkg/infra/server/middleware
git commit -m "feat(server): add the OpenTelemetry span and metrics middleware and a slog access log"
```

---

### Task 5: Wire tracing and the access log into the server

**Files:**
- Modify: `app/pkg/infra/server/server.go`
- Modify: `app/pkg/infra/server/config.go`
- Delete: `app/pkg/infra/server/middleware/logger.go` (Fiber's plain-text logger, replaced by the access log)
- Create: `app/pkg/infra/server/instrumentation_test.go`

**Interfaces:**
- Consumes: `middleware.OTelMiddleware`, `middleware.OTelConfig`, `middleware.AccessLogMiddleware` (Task 4); `telemetry.NewLogger` (Task 1).
- Produces: `server.ServerConfig.ProbePaths []string` (probe routes: no span, no metric, debug-level access log); `NewServer` order: OTel, request ID, access log, CORS, swagger, Helmet, Idempotency, Recover. Task 12 sets `ProbePaths` from `main`.

- [ ] **Step 1: Write the failing tests**

Create `app/pkg/infra/server/instrumentation_test.go`. They build the real server (its real chain) with the global tracer provider pointed at an in-memory exporter:

```go
package server

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/sanctumlabs/fupi/app/pkg/infra/server/router"
	"github.com/sanctumlabs/fupi/app/pkg/infra/telemetry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// instrumented builds the real server (its real middleware chain) with the global tracer provider and propagator
// pointed at an in-memory exporter, and the default logger at a JSON buffer, until the test ends.
func instrumented(t *testing.T, routes ...router.Route) (*Server, *tracetest.InMemoryExporter, *bytes.Buffer) {
	t.Helper()

	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)))
	previousProvider, previousPropagator := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(telemetry.NewLogger(&logs, "json", slog.LevelInfo, "fupi-test"))

	t.Cleanup(func() {
		slog.SetDefault(previousLogger)
		otel.SetTextMapPropagator(previousPropagator)
		otel.SetTracerProvider(previousProvider)
		_ = provider.Shutdown(context.Background())
	})

	srv := NewServer(ServerConfig{AppName: "fupi-test", ProbePaths: []string{"/health", "/health/ready"}})
	srv.RegisterHandlers([]router.Router{stubRouter{routes: routes}})
	return srv, exporter, &logs
}

func TestNewServer_TracesARequestAndLogsItWithTheSameTraceID(t *testing.T) {
	srv, exporter, logs := instrumented(t,
		router.NewGetRoute("/users/:id", func(c *fiber.Ctx) error { return c.SendString("hi") }),
	)

	resp, err := srv.App().Test(httptest.NewRequest("GET", "/users/42", nil))
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "GET /users/:id", spans[0].Name)

	var line map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(logs.String())), &line), logs.String())
	assert.Equal(t, "request", line["msg"])
	assert.Equal(t, spans[0].SpanContext.TraceID().String(), line["trace_id"], "the access log line and the span share one trace ID")
	assert.NotEmpty(t, line["request_id"], "the request ID middleware runs before the access log")
}

func TestNewServer_AHandlerPanicIsAFiveHundredSpanError(t *testing.T) {
	srv, exporter, _ := instrumented(t,
		router.NewGetRoute("/panic", func(c *fiber.Ctx) error { panic("kaboom") }),
	)

	resp, err := srv.App().Test(httptest.NewRequest("GET", "/panic", nil))
	require.NoError(t, err)

	assert.Equal(t, 500, resp.StatusCode)
	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, codes.Error, spans[0].Status.Code)
}

func TestNewServer_ProbesAreNeitherTracedNorLoggedAtInfo(t *testing.T) {
	srv, exporter, logs := instrumented(t,
		router.NewGetRoute("/health", func(c *fiber.Ctx) error { return c.SendString("ok") }),
		router.NewGetRoute("/health/ready", func(c *fiber.Ctx) error { return c.SendString("ok") }),
	)

	for _, target := range []string{"/health", "/health/ready"} {
		resp, err := srv.App().Test(httptest.NewRequest("GET", target, nil))
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode)
	}

	assert.Empty(t, exporter.GetSpans())
	assert.Empty(t, logs.String())
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./app/pkg/infra/server -run 'NewServer_(Traces|AHandler|Probes)'`
Expected: build failure `unknown field ProbePaths in struct literal of type ServerConfig`.

- [ ] **Step 3: Implement**

In `app/pkg/infra/server/config.go`, add to `ServerConfig` after the `Environment` field:

```go
		// ProbePaths are the health probe routes. They get no span and no metric, and their access log line is debug level.
		ProbePaths []string
```

In `app/pkg/infra/server/server.go` replace

```go
	// middleware
	app.Use(middleware.RequestIdMiddleware())
	app.Use(middleware.LoggerMiddleware())
```

with

```go
	// middleware. Tracing comes first so every middleware behind it, and the access log in particular, sees the request's span.
	app.Use(middleware.OTelMiddleware(middleware.OTelConfig{SkipPaths: cfg.ProbePaths}))
	app.Use(middleware.RequestIdMiddleware())
	app.Use(middleware.AccessLogMiddleware(cfg.ProbePaths))
```

Delete the file:

```bash
git rm app/pkg/infra/server/middleware/logger.go
```

- [ ] **Step 4: Run the tests**

Run: `gofmt -l app/pkg/infra/server; go test -race -count=1 ./app/pkg/infra/server/...`
Expected: no gofmt output; `ok` for `server` and `server/middleware`.

- [ ] **Step 5: Commit**

```bash
git add app/pkg/infra/server
git commit -m "feat(server): put the span middleware first and replace Fiber's text logger with the access log"
```

---

### Task 6: Remove the Bids-era metrics code

**Files:**
- Modify: `app/pkg/infra/server/server.go` (the `/metrics` route)
- Modify: `app/pkg/infra/server/grpc_server.go` (the interceptor line)
- Modify: `app/cmd/main.go` (the `"/metrics"` public path)
- Modify: `app/pkg/infra/server/instrumentation_test.go` (one more test)
- Delete: `app/pkg/infra/server/middleware/monitoring.go`, `app/pkg/infra/monitoring/metrics/` (the package and its `prometheus/` subpackage), `app/pkg/infra/server/interceptors/metrics_interceptor.go`, `app/pkg/infra/server/interceptors/utils.go` (its only function, `extractMethodName`, was used only by the interceptor)

**Interfaces:**
- Consumes: nothing.
- Produces: nothing new; after this task nothing in the repository imports `github.com/prometheus/client_golang` (Task 12's `go mod tidy` drops it from `go.mod`).

- [ ] **Step 1: Write the failing test**

Append to `app/pkg/infra/server/instrumentation_test.go`:

```go
// The Fiber monitor page was unauthenticated, exposed runtime statistics and was not Prometheus. Metrics leave over OTLP.
func TestNewServer_DoesNotServeAMetricsPage(t *testing.T) {
	srv := NewServer(ServerConfig{AppName: "fupi-test"})

	resp, err := srv.App().Test(httptest.NewRequest("GET", "/metrics", nil))
	require.NoError(t, err)

	assert.Equal(t, 404, resp.StatusCode)
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./app/pkg/infra/server -run TestNewServer_DoesNotServeAMetricsPage`
Expected: FAIL, `expected: 404`, `actual  : 200` (the Fiber monitor page).

- [ ] **Step 3: Remove the code**

In `app/pkg/infra/server/server.go` delete

```go
	app.Get("/metrics", middleware.MonitoringMiddleware())

```

In `app/pkg/infra/server/grpc_server.go` delete the line `			interceptors.GrpcServerMetricsInterceptor(),` from the `ChainUnaryInterceptor` call.

In `app/cmd/main.go` delete the line `				"/metrics",` from `PublicPaths`.

```bash
git rm app/pkg/infra/server/middleware/monitoring.go
git rm -r app/pkg/infra/monitoring/metrics
git rm app/pkg/infra/server/interceptors/metrics_interceptor.go app/pkg/infra/server/interceptors/utils.go
```

- [ ] **Step 4: Verify the build, vet and the whole suite**

Run: `go build ./... && go vet ./app/... && go test ./...`
Expected: no build or vet output; every package `ok` or `no test files` (the suite takes about a minute; `app/cmd` includes a three-second Postgres-unreachable test).

- [ ] **Step 5: Check nothing else was orphaned**

Run: `grep -rn 'prometheus/client_golang\|monitoring/metrics\|EnvMetricsEnabled\|MonitoringMiddleware\|GrpcServerMetricsInterceptor' --include='*.go' . ; ~/go/bin/golangci-lint-v2 run --max-same-issues=0 --max-issues-per-linter=0 ./app/pkg/infra/server/... ./app/cmd/...`
Expected: `grep` prints nothing (the legacy-tagged files use the `legacy_` prefix and `app/server/middleware`, not these names; if `grep` lists one of those, read it before touching it). `golangci-lint` reports only `customRecoveryFunc is unused` in `interceptors/recovery_interceptor.go`, which is old dead code: leave it and mention it in the final message.

- [ ] **Step 6: Commit**

```bash
git add -A app
git commit -m "refactor(server): remove the unauthenticated /metrics page and the Bids-era metrics code"
```

---

### Task 7: Identity use-case spans

**Files:**
- Create: `app/internal/application/identity/tracing.go`, `tracing_test.go`
- Modify: `app/internal/application/identity/service.go`, `register.go`, `login.go`, `refresh.go`, `verify.go`

**Interfaces:**
- Consumes: nothing from earlier tasks (the SDK test modules come from Task 3).
- Produces: `Service.tracer trace.Tracer` (set by `NewService` from `otel.Tracer`); `(*Service).startUseCase(ctx, name) (context.Context, trace.Span)`; `endUseCase(span, err)`; `errorClass(err) string`. Spans are named `identity.Register|Login|Refresh|VerifyEmail`.

- [ ] **Step 1: Write the failing tests**

Create `app/internal/application/identity/tracing_test.go` (suite methods on the existing `IdentityServiceTestSuite`, plus one table test):

```go
package identityapp

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/sanctumlabs/fupi/app/internal/core/entity"
	"github.com/sanctumlabs/fupi/app/internal/domain/identity"
	"github.com/sanctumlabs/fupi/app/pkg/errdefs"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/mock/gomock"
)

// traced makes the service record its spans into the returned exporter, under a "request" span like the HTTP middleware's.
func (suite *IdentityServiceTestSuite) traced() (context.Context, *tracetest.InMemoryExporter, trace.Span) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)))
	suite.T().Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	suite.service.tracer = provider.Tracer("test")

	ctx, request := provider.Tracer("test").Start(context.Background(), "request")
	return ctx, exporter, request
}

// useCaseSpan returns the one span a use case recorded; the request span is still open, so it is not in the exporter.
func (suite *IdentityServiceTestSuite) useCaseSpan(exporter *tracetest.InMemoryExporter, name string) tracetest.SpanStub {
	spans := exporter.GetSpans()
	suite.Require().Len(spans, 1)
	suite.Equal(name, spans[0].Name)
	return spans[0]
}

// spanText is everything a span could leak: its status text, attributes and events.
func spanText(span tracetest.SpanStub) string {
	text := span.Status.Description
	for _, attribute := range span.Attributes {
		text += " " + attribute.Value.String()
	}
	for _, event := range span.Events {
		text += " " + event.Name
		for _, attribute := range event.Attributes {
			text += " " + attribute.Value.String()
		}
	}
	return text
}

func (suite *IdentityServiceTestSuite) TestRegister_RecordsASpanBeneathTheRequestSpan() {
	ctx, exporter, request := suite.traced()
	cmd := validCommand()
	suite.mockUsers.EXPECT().Save(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, user identity.User) (identity.User, error) { return user, nil })
	suite.mockNotifier.EXPECT().SendEmailVerification(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)

	_, err := suite.service.Register(ctx, cmd)
	suite.Require().NoError(err)

	span := suite.useCaseSpan(exporter, "identity.Register")
	suite.Equal(request.SpanContext().SpanID(), span.Parent.SpanID(), "the use case nests under the request")
	suite.Equal(codes.Unset, span.Status.Code)
	for _, secret := range []string{cmd.Email, cmd.Username, cmd.Password, cmd.FirstName} {
		suite.NotContains(spanText(span), secret, "a span must not carry what the caller sent")
	}
}

func (suite *IdentityServiceTestSuite) TestRegister_AFailureMarksTheSpanWithTheClassOfTheErrorNotItsText() {
	ctx, exporter, _ := suite.traced()
	cmd := validCommand()
	cmd.Email = "not-an-email"

	_, err := suite.service.Register(ctx, cmd)
	suite.Require().Error(err)
	suite.Require().Contains(err.Error(), "not-an-email", "the premise: this error's own text embeds what the caller sent")

	span := suite.useCaseSpan(exporter, "identity.Register")
	suite.Equal(codes.Error, span.Status.Code)
	suite.Equal("invalid_parameter", span.Status.Description)
	suite.NotContains(spanText(span), "not-an-email")
	for _, event := range span.Events {
		suite.NotEqual("exception", event.Name, "RecordError would copy the error text into the span")
	}
}

func (suite *IdentityServiceTestSuite) TestLogin_RecordsASpanAndMarksAWrongPasswordAsUnauthorized() {
	ctx, exporter, _ := suite.traced()
	user := suite.registeredUser("the-right-password")
	suite.mockUsers.EXPECT().FetchByEmail(gomock.Any(), gomock.Any()).Return(*user, nil)

	_, _, err := suite.service.Login(ctx, "john.doe@fupi.com", "the-wrong-password")
	suite.Require().Error(err)

	span := suite.useCaseSpan(exporter, "identity.Login")
	suite.Equal(codes.Error, span.Status.Code)
	suite.Equal("unauthorized", span.Status.Description)
	suite.NotContains(spanText(span), "john.doe@fupi.com")
	suite.NotContains(spanText(span), "the-wrong-password")
}

func (suite *IdentityServiceTestSuite) TestLogin_ASuccessLeavesTheSpanUnset() {
	ctx, exporter, _ := suite.traced()
	const password = "s3cret-password"
	user := suite.registeredUser(password)
	userID := entity.IDToString(user.ID())
	suite.mockUsers.EXPECT().FetchByEmail(gomock.Any(), gomock.Any()).Return(*user, nil)
	suite.mockTokens.EXPECT().GenerateAccessToken(userID).Return("access-token", nil)
	suite.mockTokens.EXPECT().GenerateRefreshToken(userID).Return("refresh-token", nil)

	_, _, err := suite.service.Login(ctx, "john.doe@fupi.com", password)
	suite.Require().NoError(err)

	span := suite.useCaseSpan(exporter, "identity.Login")
	suite.Equal(codes.Unset, span.Status.Code)
	suite.NotContains(spanText(span), "access-token", "tokens are never recorded")
}

func (suite *IdentityServiceTestSuite) TestRefresh_RecordsASpanAndNeverTheToken() {
	ctx, exporter, _ := suite.traced()
	suite.mockTokens.EXPECT().Authenticate("a-bad-refresh-token").Return("", errdefs.Unauthorized(errors.New("bad signature")))

	_, err := suite.service.Refresh(ctx, "a-bad-refresh-token")
	suite.Require().Error(err)

	span := suite.useCaseSpan(exporter, "identity.Refresh")
	suite.Equal(codes.Error, span.Status.Code)
	suite.Equal("unauthorized", span.Status.Description)
	suite.NotContains(spanText(span), "a-bad-refresh-token")
}

func (suite *IdentityServiceTestSuite) TestVerifyEmail_RecordsASpanAndNeverTheToken() {
	ctx, exporter, _ := suite.traced()
	suite.mockUsers.EXPECT().FetchByVerificationToken(gomock.Any(), "a-secret-token").
		Return(identity.User{}, errdefs.NotFound(errors.New("no rows")))

	_, err := suite.service.VerifyEmail(ctx, "a-secret-token")
	suite.Require().Error(err)

	span := suite.useCaseSpan(exporter, "identity.VerifyEmail")
	suite.Equal(codes.Error, span.Status.Code)
	suite.Equal("invalid_parameter", span.Status.Description, "the use case reports an unknown token as an invalid one")
	suite.NotContains(spanText(span), "a-secret-token")
}

func TestErrorClass(t *testing.T) {
	cases := map[string]struct {
		err  error
		want string
	}{
		"invalid parameter": {errdefs.InvalidParameter(errors.New("x")), "invalid_parameter"},
		"unauthorized":      {errdefs.Unauthorized(errors.New("x")), "unauthorized"},
		"forbidden":         {errdefs.Forbidden(errors.New("x")), "forbidden"},
		"not found":         {errdefs.NotFound(errors.New("x")), "not_found"},
		"conflict":          {errdefs.Conflict(errors.New("x")), "conflict"},
		"unavailable":       {errdefs.Unavailable(errors.New("x")), "unavailable"},
		"wrapped":           {fmt.Errorf("context: %w", errdefs.Conflict(errors.New("x"))), "conflict"},
		"unclassified":      {errors.New("connection refused"), "internal"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, errorClass(tc.err))
		})
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./app/internal/application/identity -run 'TestErrorClass|TestIdentityServiceTestSuite'`
Expected: build failure `suite.service.tracer undefined` and `undefined: errorClass`.

- [ ] **Step 3: Implement**

Create `app/internal/application/identity/tracing.go`:

```go
package identityapp

import (
	"context"

	"github.com/sanctumlabs/fupi/app/pkg/errdefs"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const instrumentationName = "github.com/sanctumlabs/fupi/app/internal/application/identity"

// startUseCase starts the span of one use case, named identity.<useCase>, as a child of the request's span.
func (svc *Service) startUseCase(ctx context.Context, useCase string) (context.Context, trace.Span) {
	return svc.tracer.Start(ctx, "identity."+useCase)
}

// endUseCase finishes the span of a use case. A failure sets the span's status and its error.type to the class of the
// failure and never to its text: the errors in this package embed what the caller sent ("email x is invalid", "token x
// is invalid"), and a span must not carry an email address, a name or a token.
func endUseCase(span trace.Span, err error) {
	if err != nil {
		class := errorClass(err)
		span.SetAttributes(attribute.String("error.type", class))
		span.SetStatus(codes.Error, class)
	}
	span.End()
}

// errorClass names the kind of an error with the same classes the HTTP layer maps to status codes.
func errorClass(err error) string {
	switch {
	case errdefs.IsInvalidParameter(err):
		return "invalid_parameter"
	case errdefs.IsUnauthorized(err):
		return "unauthorized"
	case errdefs.IsForbidden(err):
		return "forbidden"
	case errdefs.IsNotFound(err):
		return "not_found"
	case errdefs.IsConflict(err):
		return "conflict"
	case errdefs.IsUnavailable(err):
		return "unavailable"
	default:
		return "internal"
	}
}
```

In `service.go` add `"go.opentelemetry.io/otel"` and `"go.opentelemetry.io/otel/trace"` to the imports, add the field `tracer    trace.Tracer` to `Service` (between `notifier` and `logPrefix`) and in `NewService` add `tracer:    otel.Tracer(instrumentationName),` between `notifier` and `logPrefix`.

In each use case, give the results names so the deferred call can see the final error, and open the span as the first statement. The `_` results keep every existing `return a, b, c` as it is. None of the four functions declares a local `err`, so the name is free.

`register.go`:

```go
func (svc *Service) Register(ctx context.Context, cmd RegisterCommand) (_ identity.User, err error) {
	ctx, span := svc.startUseCase(ctx, "Register")
	defer func() { endUseCase(span, err) }()

	handlerLogPrefix := fmt.Sprintf("%s<Register>", svc.logPrefix)
```

`login.go`:

```go
func (svc *Service) Login(ctx context.Context, email, password string) (_ identity.User, _ TokenPair, err error) {
	ctx, span := svc.startUseCase(ctx, "Login")
	defer func() { endUseCase(span, err) }()

	handlerLogPrefix := fmt.Sprintf("%s<Login>", svc.logPrefix)
```

`refresh.go`:

```go
func (svc *Service) Refresh(ctx context.Context, refreshToken string) (_ TokenPair, err error) {
	ctx, span := svc.startUseCase(ctx, "Refresh")
	defer func() { endUseCase(span, err) }()

	handlerLogPrefix := fmt.Sprintf("%s<Refresh>", svc.logPrefix)
```

`verify.go`:

```go
func (svc *Service) VerifyEmail(ctx context.Context, token string) (_ identity.User, err error) {
	ctx, span := svc.startUseCase(ctx, "VerifyEmail")
	defer func() { endUseCase(span, err) }()

	handlerLogPrefix := fmt.Sprintf("%s<VerifyEmail>", svc.logPrefix)
```

(Each snippet replaces the existing signature line and the `handlerLogPrefix` line directly under it; the rest of each function is untouched.)

- [ ] **Step 4: Run the tests**

Run: `gofmt -l app/internal/application/identity; go test -race -count=1 ./app/internal/application/identity`
Expected: no gofmt output; `ok` (about 25 seconds: the existing suite hashes passwords with bcrypt).

- [ ] **Step 5: Mutation check: the no-personal-data test must bite**

Temporarily change `endUseCase` so the failure branch reads

```go
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
```

Run: `go test -count=1 ./app/internal/application/identity -run 'TestIdentityServiceTestSuite/TestRegister_AFailure'`
Expected: FAIL, the message names `not-an-email` and `Should not be: "exception"`. Restore the code and re-run: `ok`. (This is also the proof that the spec's "record an error" would have leaked: see plan note 2.)

- [ ] **Step 6: Commit**

```bash
git add app/internal/application/identity
git commit -m "feat(identity): add a span per use case that records the error class and never the error text"
```

---

### Task 8: Logging sweep: readiness checks and the Postgres client

**Files:**
- Modify: `app/pkg/infra/monitoring/health/registry.go`
- Modify: `app/pkg/infra/monitoring/health/registry_test.go`
- Modify: `app/pkg/infra/database/postgres/postgres_client.go` (import only)
- Create: `app/pkg/infra/database/postgres/postgres_client_test.go`

**Interfaces:**
- Consumes: `telemetry.Unsampled` and `telemetry.NewLogger` (Tasks 1 and 3).
- Produces: `Registry.Run` runs every check under `telemetry.Unsampled(ctx)`; the readiness failure line is logged with the caller's context; the Postgres client logs through `log/slog`.

- [ ] **Step 1: Record the baseline of the logging sweep**

Set `SCRATCH` to the session's scratchpad directory, create a throwaway lint config there (outside the repository) and run it:

```bash
cat > "$SCRATCH/sloglint.yml" <<'EOF'
version: "2"
linters:
  default: none
  enable:
    - sloglint
  settings:
    sloglint:
      context: scope
EOF
~/go/bin/golangci-lint-v2 run -c "$SCRATCH/sloglint.yml" --max-same-issues=0 --max-issues-per-linter=0 ./app/...
```

Expected: six findings: `app/cmd/main.go` (three, in `run`), `app/pkg/infra/server/server.go` (two, in `Serve` and `ServeListener`) and `app/pkg/infra/monitoring/health/registry.go:93`. Only the last one is on the request path (the readiness endpoint calls `Run` with the request's context). The others are process-lifecycle lines logged with the process context, which carries no span, so they stay as they are: calls without a request context are left alone (spec section 6).

- [ ] **Step 2: Write the failing registry tests**

In `app/pkg/infra/monitoring/health/registry_test.go` extend the imports to:

```go
import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sanctumlabs/fupi/app/pkg/infra/telemetry"
	"github.com/stretchr/testify/assert"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)
```

and append:

```go
// The checks ping Postgres and Redis, and the probes are polled every few seconds: a ping that started a trace each time
// would fill Tempo with noise.
func TestRegistry_ChecksRunUnderAnUnsampledParentSoTheirPingsAreNotTraced(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	tracer := provider.Tracer("test")

	registry := NewRegistry(time.Second)
	registry.Add(Check{Name: "postgres", Required: true, Fn: func(ctx context.Context) error {
		_, span := tracer.Start(ctx, "ping")
		span.End()
		return nil
	}})

	registry.Run(context.Background())
	assert.Empty(t, exporter.GetSpans(), "the check's span is not recorded")

	_, control := tracer.Start(context.Background(), "ping")
	control.End()
	assert.Len(t, exporter.GetSpans(), 1, "the same call outside the registry is recorded, so the sampler is on")
}

func TestRegistry_ALogLineFromAFailedCheckCarriesTheCallersTrace(t *testing.T) {
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(telemetry.NewLogger(&buf, "json", slog.LevelInfo, "fupi-test"))
	t.Cleanup(func() { slog.SetDefault(previous) })
	traceID, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	spanID, _ := trace.SpanIDFromHex("00f067aa0ba902b7")
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled,
	}))

	registry := NewRegistry(time.Second)
	registry.Add(Check{Name: "redis", Required: false, Fn: down})
	registry.Run(ctx)

	assert.Contains(t, buf.String(), `"msg":"health check failed"`)
	assert.Contains(t, buf.String(), `"trace_id":"4bf92f3577b34da6a3ce929d0e0e4736"`, "the line uses the caller's context, not the unsampled one made for the checks")
}
```

- [ ] **Step 3: Run them to verify they fail**

Run: `go test ./app/pkg/infra/monitoring/health`
Expected: FAIL `TestRegistry_ChecksRunUnderAnUnsampledParentSoTheirPingsAreNotTraced` (the check's span is recorded) and `TestRegistry_ALogLineFromAFailedCheckCarriesTheCallersTrace` (no `trace_id` in the line).

- [ ] **Step 4: Implement**

In `app/pkg/infra/monitoring/health/registry.go` add the import (after the standard-library group):

```go
	"github.com/sanctumlabs/fupi/app/pkg/infra/telemetry"
```

Replace the doc comment and the top of `Run` through the goroutine launch so it reads:

```go
// Run executes every check in parallel, each bounded by the registry's timeout. The checks run under an unsampled parent
// span, so the Postgres and Redis pings they make leave no spans: the probes are polled every few seconds and would
// otherwise start a trace each time.
func (r *Registry) Run(ctx context.Context) Report {
	if r.draining.Load() {
		return Report{Status: StatusDraining, Checks: map[string]string{}}
	}

	checkCtx := telemetry.Unsampled(ctx)
	errs := make([]error, len(r.checks))
	var wg sync.WaitGroup
	for i, check := range r.checks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = r.runOne(checkCtx, check)
		}()
	}
```

and change the failure line to use the caller's context:

```go
		slog.WarnContext(ctx, "health check failed", "check", check.Name, "required", check.Required, "error", errs[i])
```

- [ ] **Step 5: Run the registry tests**

Run: `gofmt -l app/pkg/infra/monitoring; go test -race -count=1 ./app/pkg/infra/monitoring/...`
Expected: no gofmt output; `ok`.

- [ ] **Step 6: Write the failing Postgres client test**

Create `app/pkg/infra/database/postgres/postgres_client_test.go`:

```go
package postgres

import (
	"bytes"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The client used golang.org/x/exp/slog, a separate package with its own default logger, so its lines bypassed the
// process logger: they were plain text in a JSON stream and never carried a trace ID.
func TestNewPostgresClient_LogsThroughTheDefaultSlogLogger(t *testing.T) {
	if testing.Short() {
		t.Skip("takes about three seconds of connection retries")
	}
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	_, err := NewPostgresClient(PostgresDatabaseConfig{
		Host: "127.0.0.1", Port: "1", Name: "fupidb", Username: "u", Password: "p", SslMode: "disable",
		MaxConns: 1, ConnTimeout: time.Second,
	})

	require.Error(t, err)
	assert.Contains(t, buf.String(), "connecting to database")
	assert.Contains(t, buf.String(), `"level":"ERROR"`)
}
```

- [ ] **Step 7: Run it to verify it fails**

Run: `go test ./app/pkg/infra/database/postgres -run LogsThroughTheDefaultSlogLogger`
Expected: FAIL after about three seconds: the buffer holds lines whose `"level":"INFO"` is wrong and whose `msg` starts with `WARN`/`ERROR` (`does not contain "\"level\":\"ERROR\""`).

- [ ] **Step 8: Switch the import**

In `app/pkg/infra/database/postgres/postgres_client.go` move the logging import from the third-party group to the standard-library group, so the block reads:

```go
import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/lib/pq"
	"github.com/sanctumlabs/fupi/app/pkg/infra/database"
)
```

(The `slog.InfoContext`/`WarnContext`/`ErrorContext` calls in the file have the same signatures in both packages.)

- [ ] **Step 9: Run the tests and the sweep check**

Run: `gofmt -l app/pkg/infra/database/postgres; go test -race -count=1 ./app/pkg/infra/database/postgres; ~/go/bin/golangci-lint-v2 run -c "$SCRATCH/sloglint.yml" --max-same-issues=0 --max-issues-per-linter=0 ./app/...`
Expected: no gofmt output; `ok` (about four seconds); `sloglint` reports five findings, all in `app/cmd/main.go` and `app/pkg/infra/server/server.go` (process lifecycle, left as they are). Then `rm "$SCRATCH/sloglint.yml"`.

- [ ] **Step 10: Commit**

```bash
git add app/pkg/infra/monitoring app/pkg/infra/database/postgres
git commit -m "fix(logging): run readiness checks unsampled, log their failures with the caller's context, and use log/slog in the Postgres client"
```

---

### Task 9: `telemetry.Setup` (needs the download go-ahead)

**Files:**
- Modify: `go.mod`, `go.sum` (new modules, after the go-ahead)
- Create: `app/pkg/infra/telemetry/setup.go`, `setup_test.go`

**Interfaces:**
- Consumes: `newErrorHandler` (Task 3), `defaultServiceName` (Task 1's `log.go`), the SDK modules (Task 3).
- Produces: `telemetry.Options{ServiceVersion, Environment string}`; `telemetry.Setup(ctx context.Context, opts Options) (shutdown func(context.Context) error, err error)`; unexported `newResource`, `metricReaderOptions`, `errorLogInterval`, `defaultMetricInterval`. Task 12 calls `Setup`; Tasks 10 and 11 rely on the global providers it installs.

- [ ] **Step 1: STOP and ask for the download go-ahead**

Use AskUserQuestion. Say, in the question text: the tasks from here need new Go modules; they are fetched with `go get` from `proxy.golang.org` (checked against `sum.golang.org`) into the module cache and recorded in `go.mod`/`go.sum`; nothing else is downloaded (every Docker image the drill needs is already local). The modules and pins:

- `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc` and `.../otlpmetric/otlpmetricgrpc` at v1.44.0, with their dependencies (`otlptrace`, `go.opentelemetry.io/proto/otlp`, `grpc-gateway/v2`, `cenkalti/backoff`, ...);
- `github.com/exaring/otelpgx` (latest, v0.12.0 at plan time);
- `github.com/redis/go-redis/extra/redisotel/v9` at v9.19.0 with `rediscmd`;
- whatever `go mod tidy` additionally resolves (test-only dependencies of these modules).

State an estimate (a few MB, expected under 15 MB in total; the exact figure is not known before resolution) and that no existing requirement (pgx, go-redis, grpc, OpenTelemetry core) will be bumped: if resolution would bump one, you stop and ask again. Options: "Go ahead (Recommended)" and "Not now". Do not run any command below that touches the network until the answer is yes. If it is no, stop here and report Tasks 1 to 8 as done.

- [ ] **Step 2: Add the exporter modules**

Run:

```bash
go get go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc@v1.44.0 go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc@v1.44.0
git diff go.mod
```

Expected: only added requirement lines (the two exporters plus indirect ones). No existing line changes version. If `google.golang.org/grpc`, `go.opentelemetry.io/otel`, `github.com/jackc/pgx/v5` or `github.com/redis/go-redis/v9` changed, revert (`git checkout go.mod go.sum`) and ask.

- [ ] **Step 3: Write the failing tests**

Create `app/pkg/infra/telemetry/setup_test.go`. It starts a fake OTLP/gRPC collector in the test process (the Export methods of the generated trace and metrics services), points `OTEL_EXPORTER_OTLP_ENDPOINT` at it and reads back what `Setup`'s providers export:

```go
package telemetry

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	"google.golang.org/grpc"
)

// collector records what is exported to it.
type collector struct {
	mu      sync.Mutex
	traces  []*coltracepb.ExportTraceServiceRequest
	metrics []*colmetricspb.ExportMetricsServiceRequest
}

func (c *collector) traceRequests() []*coltracepb.ExportTraceServiceRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]*coltracepb.ExportTraceServiceRequest(nil), c.traces...)
}

func (c *collector) metricRequests() []*colmetricspb.ExportMetricsServiceRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]*colmetricspb.ExportMetricsServiceRequest(nil), c.metrics...)
}

type traceService struct {
	coltracepb.UnimplementedTraceServiceServer
	c *collector
}

func (s *traceService) Export(_ context.Context, req *coltracepb.ExportTraceServiceRequest) (*coltracepb.ExportTraceServiceResponse, error) {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	s.c.traces = append(s.c.traces, req)
	return &coltracepb.ExportTraceServiceResponse{}, nil
}

type metricsService struct {
	colmetricspb.UnimplementedMetricsServiceServer
	c *collector
}

func (s *metricsService) Export(_ context.Context, req *colmetricspb.ExportMetricsServiceRequest) (*colmetricspb.ExportMetricsServiceResponse, error) {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	s.c.metrics = append(s.c.metrics, req)
	return &colmetricspb.ExportMetricsServiceResponse{}, nil
}

// startCollector serves OTLP/gRPC on a free port and returns the collector and the endpoint URL to export to.
func startCollector(t *testing.T) (*collector, string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	c := &collector{}
	server := grpc.NewServer()
	coltracepb.RegisterTraceServiceServer(server, &traceService{c: c})
	colmetricspb.RegisterMetricsServiceServer(server, &metricsService{c: c})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	return c, "http://" + listener.Addr().String()
}

// resetGlobals puts the global providers, propagator and error handler back to no-ops when the test ends, because Setup
// replaces them for the whole process.
func resetGlobals(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		otel.SetTracerProvider(tracenoop.NewTracerProvider())
		otel.SetMeterProvider(metricnoop.NewMeterProvider())
		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator())
		otel.SetErrorHandler(otel.ErrorHandlerFunc(func(error) {}))
	})
}

func resourceAttribute(attrs []*commonpb.KeyValue, key string) (string, bool) {
	for _, kv := range attrs {
		if kv.GetKey() == key {
			return kv.GetValue().GetStringValue(), true
		}
	}
	return "", false
}

func shutdownWithin(t *testing.T, shutdown func(context.Context) error, d time.Duration) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	return shutdown(ctx)
}

func TestSetup_ExportsSpansAndMetricsOnShutdownWithTheServiceResource(t *testing.T) {
	c, endpoint := startCollector(t)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", endpoint)
	t.Setenv("OTEL_SERVICE_NAME", "")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "")
	resetGlobals(t)

	shutdown, err := Setup(context.Background(), Options{ServiceVersion: "1.2.3", Environment: "test"})
	require.NoError(t, err)

	_, span := otel.Tracer("test").Start(context.Background(), "unit-of-work")
	span.End()
	counter, err := otel.Meter("test").Int64Counter("test.requests")
	require.NoError(t, err)
	counter.Add(context.Background(), 1)

	require.NoError(t, shutdownWithin(t, shutdown, 5*time.Second), "shutdown flushes both providers")

	traces := c.traceRequests()
	require.Len(t, traces, 1)
	resourceSpans := traces[0].GetResourceSpans()
	require.Len(t, resourceSpans, 1)
	attrs := resourceSpans[0].GetResource().GetAttributes()
	name, _ := resourceAttribute(attrs, "service.name")
	version, _ := resourceAttribute(attrs, "service.version")
	environment, _ := resourceAttribute(attrs, "deployment.environment.name")
	assert.Equal(t, "fupi", name, "the service name defaults to fupi: the dashboard filters on it")
	assert.Equal(t, "1.2.3", version)
	assert.Equal(t, "test", environment)
	for _, key := range []string{"process.pid", "process.command_args", "process.owner"} {
		_, found := resourceAttribute(attrs, key)
		assert.False(t, found, "%s would become a label on every metric series", key)
	}
	assert.Equal(t, "unit-of-work", resourceSpans[0].GetScopeSpans()[0].GetSpans()[0].GetName())

	var metricNames []string
	for _, request := range c.metricRequests() {
		for _, resourceMetrics := range request.GetResourceMetrics() {
			metricName, _ := resourceAttribute(resourceMetrics.GetResource().GetAttributes(), "service.name")
			assert.Equal(t, "fupi", metricName)
			for _, scope := range resourceMetrics.GetScopeMetrics() {
				for _, m := range scope.GetMetrics() {
					metricNames = append(metricNames, m.GetName())
				}
			}
		}
	}
	assert.Contains(t, metricNames, "test.requests")
}

// The standard variables win over the defaults the application passes in.
func TestSetup_TheEnvironmentOverridesTheDefaults(t *testing.T) {
	c, endpoint := startCollector(t)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", endpoint)
	t.Setenv("OTEL_SERVICE_NAME", "billing")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "deployment.environment.name=staging,custom.team=payments")
	resetGlobals(t)

	shutdown, err := Setup(context.Background(), Options{ServiceVersion: "1.2.3", Environment: "test"})
	require.NoError(t, err)
	_, span := otel.Tracer("test").Start(context.Background(), "work")
	span.End()
	require.NoError(t, shutdownWithin(t, shutdown, 5*time.Second))

	traces := c.traceRequests()
	require.Len(t, traces, 1)
	attrs := traces[0].GetResourceSpans()[0].GetResource().GetAttributes()
	name, _ := resourceAttribute(attrs, "service.name")
	environment, _ := resourceAttribute(attrs, "deployment.environment.name")
	team, _ := resourceAttribute(attrs, "custom.team")
	version, _ := resourceAttribute(attrs, "service.version")
	assert.Equal(t, "billing", name)
	assert.Equal(t, "staging", environment)
	assert.Equal(t, "payments", team)
	assert.Equal(t, "1.2.3", version, "what the environment does not set keeps the application's value")
}

func TestSetup_DisabledInstallsNothing(t *testing.T) {
	c, endpoint := startCollector(t)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", endpoint)
	t.Setenv("OTEL_SDK_DISABLED", "true")
	resetGlobals(t)

	shutdown, err := Setup(context.Background(), Options{})
	require.NoError(t, err)

	_, isSDK := otel.GetTracerProvider().(*sdktrace.TracerProvider)
	assert.False(t, isSDK, "the global provider stays the no-op")
	_, span := otel.Tracer("test").Start(context.Background(), "work")
	assert.False(t, span.IsRecording())
	span.End()
	require.NoError(t, shutdownWithin(t, shutdown, time.Second))
	assert.Empty(t, c.traceRequests())
	assert.Empty(t, c.metricRequests())
}

// A collector that is not there must not fail startup, slow the work being traced, or hold shutdown past its deadline.
func TestSetup_DoesNotFailOrBlockWithoutACollector(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:1")
	resetGlobals(t)
	captureDefaultLogger(t)

	start := time.Now()
	shutdown, err := Setup(context.Background(), Options{})
	require.NoError(t, err)
	assert.Less(t, time.Since(start), time.Second, "Setup must not dial the collector")

	tracer := otel.Tracer("test")
	begin := time.Now()
	for range 2000 {
		_, span := tracer.Start(context.Background(), "work")
		span.End()
	}
	assert.Less(t, time.Since(begin), time.Second, "recording spans never waits for the exporter")

	done := make(chan struct{})
	go func() {
		_ = shutdownWithin(t, shutdown, 500*time.Millisecond)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown must honour its deadline when the collector is unreachable")
	}
}

func TestSetup_PropagatesW3CTraceContextAndBaggage(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:1")
	resetGlobals(t)

	shutdown, err := Setup(context.Background(), Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = shutdownWithin(t, shutdown, 200*time.Millisecond) })

	fields := otel.GetTextMapPropagator().Fields()
	assert.Contains(t, fields, "traceparent")
	assert.Contains(t, fields, "baggage")
}

func TestSetup_InstallsTheRateLimitedErrorHandler(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:1")
	resetGlobals(t)
	buf := captureDefaultLogger(t)

	shutdown, err := Setup(context.Background(), Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = shutdownWithin(t, shutdown, 200*time.Millisecond) })

	for range 5 {
		otel.Handle(errors.New("export failed: connection refused"))
	}

	assert.Equal(t, 1, strings.Count(buf.String(), "telemetry export failed"))
}

func TestMetricReaderOptions_DefaultsToTheStacksScrapeInterval(t *testing.T) {
	t.Setenv("OTEL_METRIC_EXPORT_INTERVAL", "")
	assert.Len(t, metricReaderOptions(), 1, "unset (or empty) means the 15 second default")

	t.Setenv("OTEL_METRIC_EXPORT_INTERVAL", "1000")
	assert.Empty(t, metricReaderOptions(), "a set variable is left to the SDK")
}

func TestSetup_HonoursTheMetricExportIntervalVariable(t *testing.T) {
	c, endpoint := startCollector(t)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", endpoint)
	t.Setenv("OTEL_METRIC_EXPORT_INTERVAL", "100")
	resetGlobals(t)

	shutdown, err := Setup(context.Background(), Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = shutdownWithin(t, shutdown, 2*time.Second) })
	counter, err := otel.Meter("test").Int64Counter("test.interval")
	require.NoError(t, err)
	counter.Add(context.Background(), 1)

	assert.Eventually(t, func() bool { return len(c.metricRequests()) > 0 }, 3*time.Second, 20*time.Millisecond,
		"metrics must arrive without a shutdown when the interval is 100 ms")
}
```

- [ ] **Step 4: Run them to verify they fail**

Run: `go test ./app/pkg/infra/telemetry`
Expected: build failure `undefined: Setup` (and `Options`, `metricReaderOptions`).

- [ ] **Step 5: Implement**

Create `app/pkg/infra/telemetry/setup.go`:

```go
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

const (
	// errorLogInterval is how often the same export error may be logged.
	errorLogInterval = time.Minute
	// defaultMetricInterval is the export interval when OTEL_METRIC_EXPORT_INTERVAL is not set: the stack's scrape interval.
	defaultMetricInterval = 15 * time.Second
)

// Options are the settings Setup takes from the application. Everything else comes from the standard OTEL_* variables
// (OTEL_EXPORTER_OTLP_ENDPOINT, OTEL_SERVICE_NAME, OTEL_RESOURCE_ATTRIBUTES, OTEL_TRACES_SAMPLER, OTEL_SDK_DISABLED, ...).
type Options struct {
	// ServiceVersion is recorded as service.version.
	ServiceVersion string
	// Environment is recorded as deployment.environment.name.
	Environment string
}

// Setup installs the global tracer provider, meter provider and propagators (W3C trace context and baggage) and returns
// the function that flushes and stops them. Traces and metrics go to the OTLP/gRPC endpoint the standard variables name
// (default localhost:4317). Setup does not dial: the exporters connect lazily and export in the background, so a missing
// collector never fails startup or slows a request; the SDK's errors are logged at most once a minute per distinct error.
// With OTEL_SDK_DISABLED=true it installs nothing and the returned function does nothing.
func Setup(ctx context.Context, opts Options) (shutdown func(context.Context) error, err error) {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("OTEL_SDK_DISABLED")), "true") {
		return func(context.Context) error { return nil }, nil
	}

	otel.SetErrorHandler(newErrorHandler(errorLogInterval, time.Now))
	res := newResource(ctx, opts)

	traceExporter, err := otlptracegrpc.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("create the trace exporter: %w", err)
	}
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithBatcher(traceExporter), sdktrace.WithResource(res))

	metricExporter, err := otlpmetricgrpc.New(ctx)
	if err != nil {
		_ = tracerProvider.Shutdown(ctx)
		return nil, fmt.Errorf("create the metric exporter: %w", err)
	}
	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter, metricReaderOptions()...)),
		sdkmetric.WithResource(res),
	)

	otel.SetTracerProvider(tracerProvider)
	otel.SetMeterProvider(meterProvider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	return func(ctx context.Context) error {
		return errors.Join(tracerProvider.Shutdown(ctx), meterProvider.Shutdown(ctx))
	}, nil
}

// newResource describes this process. The application's values are the defaults and the standard variables
// (OTEL_SERVICE_NAME, OTEL_RESOURCE_ATTRIBUTES) override them, because the detectors are applied in this order and a later
// one wins. There is no process detector: the collector turns every resource attribute into a label on every metric series,
// and process.command_args or process.pid are not labels anyone wants.
func newResource(ctx context.Context, opts Options) *resource.Resource {
	defaults := []attribute.KeyValue{attribute.String("service.name", defaultServiceName)}
	if opts.ServiceVersion != "" {
		defaults = append(defaults, attribute.String("service.version", opts.ServiceVersion))
	}
	if opts.Environment != "" {
		defaults = append(defaults, attribute.String("deployment.environment.name", opts.Environment))
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(defaults...),
		resource.WithFromEnv(),
		resource.WithHost(),
		resource.WithTelemetrySDK(),
	)
	if err != nil {
		// New always returns a resource: a partly detected one, or one whose schema URLs conflicted, is still usable.
		otel.Handle(err)
	}
	return res
}

// metricReaderOptions sets the export interval to defaultMetricInterval unless OTEL_METRIC_EXPORT_INTERVAL says otherwise;
// the SDK's own default is a minute, which would leave the dashboards behind the 15 second scrape.
func metricReaderOptions() []sdkmetric.PeriodicReaderOption {
	if strings.TrimSpace(os.Getenv("OTEL_METRIC_EXPORT_INTERVAL")) != "" {
		return nil
	}
	return []sdkmetric.PeriodicReaderOption{sdkmetric.WithInterval(defaultMetricInterval)}
}
```

If a signature differs from the one used here (the library documentation at plan time: `otlptracegrpc.New(ctx, ...Option) (*otlptrace.Exporter, error)`, `otlpmetricgrpc.New(ctx, ...Option) (*Exporter, error)`, `otelpgx`, `redisotel`), adjust minimally, run `go doc` on the symbol, and ledger a `Ruling:` line.

- [ ] **Step 6: Run the tests**

Run: `gofmt -l app/pkg/infra/telemetry; go test -race -count=1 ./app/pkg/infra/telemetry`
Expected: no gofmt output; `ok`. If `TestSetup_TheEnvironmentOverridesTheDefaults` fails because the application's value wins over `OTEL_SERVICE_NAME`, the detector order is the other way round in this SDK version: reorder the options (or merge the env resource over the defaults with `resource.Merge`) until it passes; ledger a ruling. If `TestSetup_DoesNotFailOrBlockWithoutACollector` shows `Setup` taking about a second or more, an exporter constructor is dialing: wrap the constructors in a context with a 200 ms timeout or switch to the lazy option, and ledger it.

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum app/pkg/infra/telemetry
git commit -m "feat(telemetry): add Setup for OTLP traces and metrics with a lazy, rate-limited exporter"
```

---

### Task 10: Postgres tracing and pool metrics

**Files:**
- Modify: `go.mod`, `go.sum`
- Modify: `app/pkg/infra/database/postgres/postgres_client.go`
- Create: `app/pkg/infra/database/postgres/postgres_client_integration_test.go`

**Interfaces:**
- Consumes: the global tracer and meter providers (set by `Setup`, Task 9, or by a test).
- Produces: every pooled connection traces its queries with `otelpgx`; `NewPostgresClient` registers the pool statistics as metrics. Nothing else in the repository changes signature.

- [ ] **Step 1: Add the module**

Run:

```bash
go get github.com/exaring/otelpgx
git diff go.mod
go doc github.com/exaring/otelpgx NewTracer
go doc github.com/exaring/otelpgx RecordStats
```

Expected: `otelpgx` added and no existing requirement bumped (see Task 9 step 2); `NewTracer(opts ...Option) *Tracer` and `RecordStats(db PoolStats, opts ...StatsOption) error`. Its default records the SQL text and not the arguments (`WithIncludeQueryParameters` is opt-in; do not use it).

- [ ] **Step 2: Write the failing integration tests**

Create `app/pkg/infra/database/postgres/postgres_client_integration_test.go`:

```go
//go:build integration

package postgres_test

import (
	"context"
	"strings"
	"testing"

	"github.com/sanctumlabs/fupi/app/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func spanText(span tracetest.SpanStub) string {
	text := span.Name
	for _, attribute := range span.Attributes {
		text += " " + attribute.Key + "=" + attribute.Value.String()
	}
	for _, event := range span.Events {
		text += " " + event.Name
		for _, attribute := range event.Attributes {
			text += " " + attribute.Value.String()
		}
	}
	return text
}

// A query inside a request is a child span that carries the SQL text; its arguments (which can hold an email address or
// a token) are never recorded.
func TestNewPostgresClient_TracesQueriesWithTheirTextAndNeverTheirArguments(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		_ = provider.Shutdown(context.Background())
	})

	ctx := context.Background()
	client := test.TestPostgresDatabaseClientHelper(t, ctx) // created after the provider is global, as in main
	t.Cleanup(client.Close)
	exporter.Reset() // the connection and migration spans of the setup

	const secret = "jane.private@example.com"
	requestCtx, request := provider.Tracer("test").Start(ctx, "request")
	var echoed string
	require.NoError(t, client.GetDB().QueryRow(requestCtx, "SELECT $1::text", secret).Scan(&echoed))
	request.End()
	require.Equal(t, secret, echoed)

	var withQueryText, children int
	for _, span := range exporter.GetSpans() {
		assert.NotContains(t, spanText(span), secret, "span %q must not carry the query's arguments", span.Name)
		if span.SpanContext.TraceID() != request.SpanContext().TraceID() || span.Name == "request" {
			continue
		}
		children++
		if strings.Contains(spanText(span), "SELECT $1::text") {
			withQueryText++
		}
	}
	assert.Positive(t, children, "the query runs beneath the request's span")
	assert.Positive(t, withQueryText, "the SQL text is recorded")
}

func TestNewPostgresClient_RecordsPoolStatistics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	previous := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	t.Cleanup(func() {
		otel.SetMeterProvider(previous)
		_ = provider.Shutdown(context.Background())
	})

	ctx := context.Background()
	client := test.TestPostgresDatabaseClientHelper(t, ctx)
	t.Cleanup(client.Close)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &rm))
	var names []string
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			names = append(names, scope.Scope.Name+" "+m.Name)
		}
	}
	t.Logf("pool metrics: %v", names) // the Dependencies row of the dashboard uses what this prints
	assert.NotEmpty(t, names, "the pool statistics must be registered on the global meter provider")
}
```

- [ ] **Step 3: Run them to verify they fail**

Run: `go test -tags integration -count=1 -run 'TestNewPostgresClient_(Traces|Records)' ./app/pkg/infra/database/postgres`
Expected: FAIL: no child spans (`children` is zero) and no pool metrics. (Docker must be running; the `postgres:16.2-alpine` image is already local.)

- [ ] **Step 4: Implement**

In `postgres_client.go` add `"github.com/exaring/otelpgx"` to the import block (third-party group, alphabetical), then in `NewPostgresClient` directly after `poolConfig.ConnConfig.ConnectTimeout = config.ConnTimeout`:

```go
	// Trace every query and transaction step. The SQL text is recorded, its arguments are not (otelpgx's default); keep it so.
	poolConfig.ConnConfig.Tracer = otelpgx.NewTracer()
```

and after the `if connectionErr != nil { ... return nil, connectionErr }` block, before the `✅ connected to DB` log:

```go
	// Export the pool's statistics (acquired, idle and waiting connections) as metrics. A failure here only costs those metrics.
	if statsErr := otelpgx.RecordStats(pg.db); statsErr != nil {
		slog.WarnContext(ctx, fmt.Sprintf("%s> could not register the pool metrics", logPrefix), "error", statsErr)
	}
```

- [ ] **Step 5: Run the tests**

Run: `gofmt -l app/pkg/infra/database/postgres; go test -count=1 ./app/pkg/infra/database/postgres; go test -tags integration -count=1 -v -run 'TestNewPostgresClient_(Traces|Records)' ./app/pkg/infra/database/postgres`
Expected: no gofmt output; unit `ok`; both integration tests `PASS`, and the `-v` output of the second one lists the pool metric names: copy them into the ledger for Task 14.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum app/pkg/infra/database/postgres
git commit -m "feat(postgres): trace queries with otelpgx and export the pool statistics"
```

---

### Task 11: Redis tracing and metrics

**Files:**
- Modify: `go.mod`, `go.sum`
- Modify: `app/pkg/infra/cache/redis/client.go`
- Modify: `app/pkg/infra/cache/redis/client_integration_test.go`

**Interfaces:**
- Consumes: the global tracer and meter providers.
- Produces: `NewRedisClient` instruments the universal client; command spans carry the command name, never a key or a value (D7).

- [ ] **Step 1: Add the module**

Run:

```bash
go get github.com/redis/go-redis/extra/redisotel/v9@v9.19.0
git diff go.mod
go doc github.com/redis/go-redis/extra/redisotel/v9 InstrumentTracing
go doc github.com/redis/go-redis/extra/redisotel/v9 WithDBStatement
```

Expected: `redisotel` and `rediscmd` added at v9.19.0; `github.com/redis/go-redis/v9` stays at v9.19.0. If v9.19.0 of the extra module does not exist or demands a newer core, stop and ask (do not bump `go-redis` unasked). `InstrumentTracing(rdb redis.UniversalClient, opts ...TracingOption) error` and `WithDBStatement(on bool) TracingOption` are expected.

- [ ] **Step 2: Write the failing integration test**

In `app/pkg/infra/cache/redis/client_integration_test.go` add to the imports `"go.opentelemetry.io/otel"`, `sdktrace "go.opentelemetry.io/otel/sdk/trace"` and `"go.opentelemetry.io/otel/sdk/trace/tracetest"` (keep the block sorted), and append:

```go
// A Redis key can be a user's URL and a value a token, so a span records which command ran and nothing else.
func TestRedisClient_TracesCommandsWithoutTheirKeysOrValues(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		_ = provider.Shutdown(context.Background())
	})
	client, _ := startRedis(t) // created after the provider is global, as in main
	ctx := context.Background()

	const key, value = "user:secret-key-123", "secret-value-456"
	requestCtx, request := provider.Tracer("test").Start(ctx, "request")
	require.NoError(t, client.Set(requestCtx, cache.CacheItem{Key: key, Value: value}))
	got, err := client.Get(requestCtx, key)
	require.NoError(t, err)
	request.End()
	require.Equal(t, value, got.Value)

	var commands []string
	for _, span := range exporter.GetSpans() {
		text := span.Name
		for _, attribute := range span.Attributes {
			text += " " + string(attribute.Key) + "=" + attribute.Value.String()
		}
		for _, event := range span.Events {
			text += " " + event.Name
			for _, attribute := range event.Attributes {
				text += " " + attribute.Value.String()
			}
		}
		assert.NotContains(t, text, "secret-key-123", "span %q", span.Name)
		assert.NotContains(t, text, "secret-value-456", "span %q", span.Name)
		if span.SpanContext.TraceID() == request.SpanContext().TraceID() && span.Name != "request" {
			commands = append(commands, strings.ToLower(span.Name))
		}
	}
	assert.Contains(t, strings.Join(commands, " "), "set", "the SET is a child span of the request")
	assert.Contains(t, strings.Join(commands, " "), "get", "the GET is a child span of the request")
}
```

(`strings` is already imported in this file.)

- [ ] **Step 3: Run it to verify it fails**

Run: `go test -tags integration -count=1 -run TestRedisClient_TracesCommandsWithoutTheirKeysOrValues ./app/pkg/infra/cache/redis`
Expected: FAIL: no `set` or `get` child spans (nothing instruments the client yet).

- [ ] **Step 4: Implement**

In `app/pkg/infra/cache/redis/client.go` add `"log/slog"` to the standard-library imports and `"github.com/redis/go-redis/extra/redisotel/v9"` to the third-party group, then replace the `return &redisClient{ client: redisGo.NewUniversalClient(...), }, nil` of `NewRedisClient` with:

```go
	client := redisGo.NewUniversalClient(&redisGo.UniversalOptions{
		Addrs:      config.Address,
		Username:   config.Username,
		Password:   config.Password,
		DB:         config.Database,
		MasterName: config.MasterName,
	})

	// A span and a metric per command. The command text is switched off: keys and values can hold URLs and tokens (D7).
	// Instrumenting only registers hooks; a failure costs the telemetry, not the client.
	if err := redisotel.InstrumentTracing(client, redisotel.WithDBStatement(false)); err != nil {
		slog.Warn("redis: tracing is not enabled", "error", err)
	}
	if err := redisotel.InstrumentMetrics(client); err != nil {
		slog.Warn("redis: metrics are not enabled", "error", err)
	}

	return &redisClient{client: client}, nil
```

- [ ] **Step 5: Run the tests**

Run: `gofmt -l app/pkg/infra/cache/redis; go test -count=1 ./app/pkg/infra/cache/redis; go test -tags integration -count=1 ./app/pkg/infra/cache/redis`
Expected: no gofmt output; unit `ok`; integration `ok` (the `redis:7-alpine` image is already local). If the secret appears in a span, `WithDBStatement(false)` is not honoured by this version: ledger it and use `redisotel.WithCommandFilter` or a custom hook that records the command name only before going on.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum app/pkg/infra/cache/redis
git commit -m "feat(redis): trace commands and record client metrics without keys or values"
```

---

### Task 12: Wire telemetry into `main`, compose and `.env.example`

**Files:**
- Modify: `go.mod`, `go.sum` (via `go mod tidy`)
- Modify: `app/cmd/main.go`
- Create: `app/cmd/telemetry_test.go`
- Modify: `deploy/app/compose.yml`
- Modify: `.env.example`

**Interfaces:**
- Consumes: `telemetry.Setup`, `telemetry.Options`, `telemetry.NewLogger`, `telemetry.ServiceName` (Tasks 1 and 9), `config.App.Logging` (Task 1), `server.ServerConfig.ProbePaths` (Task 5), `pkg.Version`.
- Produces: `startTelemetry(ctx, cfg) (flush func())` and the test seam `setupTelemetry` in `app/cmd/main.go` (everything stays in that one file: `make run` is `go run app/cmd/main.go`).

- [ ] **Step 1: Write the failing tests**

Create `app/cmd/telemetry_test.go`:

```go
package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/sanctumlabs/fupi/app/config"
	"github.com/sanctumlabs/fupi/app/pkg"
	"github.com/sanctumlabs/fupi/app/pkg/infra/telemetry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type setupFunc = func(context.Context, telemetry.Options) (func(context.Context) error, error)

func stubSetup(t *testing.T, stub setupFunc) {
	t.Helper()
	previous := setupTelemetry
	setupTelemetry = stub
	t.Cleanup(func() { setupTelemetry = previous })
}

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &buf
}

func TestStartTelemetry_PassesTheVersionAndTheEnvironment(t *testing.T) {
	var got telemetry.Options
	stubSetup(t, func(_ context.Context, opts telemetry.Options) (func(context.Context) error, error) {
		got = opts
		return func(context.Context) error { return nil }, nil
	})

	startTelemetry(context.Background(), config.App{Environment: "staging"})()

	assert.Equal(t, telemetry.Options{ServiceVersion: pkg.Version, Environment: "staging"}, got)
}

// The run context is cancelled by the SIGTERM that starts the shutdown, so a flush that reused it would give up at once.
func TestStartTelemetry_TheFlushGetsItsOwnDeadlineAfterTheRunContextIsCancelled(t *testing.T) {
	var flushErr error
	var hasDeadline bool
	stubSetup(t, func(context.Context, telemetry.Options) (func(context.Context) error, error) {
		return func(ctx context.Context) error {
			flushErr = ctx.Err()
			_, hasDeadline = ctx.Deadline()
			return nil
		}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	flush := startTelemetry(ctx, config.App{})
	cancel() // the SIGTERM

	flush()

	require.NoError(t, flushErr, "the flush context must not be the cancelled run context")
	assert.True(t, hasDeadline, "and it must be bounded")
}

// A telemetry problem must never stop the API.
func TestStartTelemetry_ASetupFailureDisablesTelemetryInsteadOfStoppingTheAPI(t *testing.T) {
	buf := captureLog(t)
	stubSetup(t, func(context.Context, telemetry.Options) (func(context.Context) error, error) {
		return nil, errors.New("bad exporter configuration")
	})

	flush := startTelemetry(context.Background(), config.App{})

	require.NotNil(t, flush)
	assert.NotPanics(t, flush)
	assert.Contains(t, buf.String(), "telemetry is disabled")
	assert.Contains(t, buf.String(), "bad exporter configuration")
}

func TestStartTelemetry_AFailingFlushIsLoggedNotFatal(t *testing.T) {
	buf := captureLog(t)
	stubSetup(t, func(context.Context, telemetry.Options) (func(context.Context) error, error) {
		return func(context.Context) error { return errors.New("collector unreachable") }, nil
	})

	assert.NotPanics(t, startTelemetry(context.Background(), config.App{}))

	assert.Contains(t, buf.String(), "flushing telemetry")
	assert.Contains(t, buf.String(), "collector unreachable")
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./app/cmd -run StartTelemetry`
Expected: build failure `undefined: setupTelemetry` and `undefined: startTelemetry`.

- [ ] **Step 3: Implement in `app/cmd/main.go`**

Imports: add `"github.com/sanctumlabs/fupi/app/pkg"` and `"github.com/sanctumlabs/fupi/app/pkg/infra/telemetry"` (keep the block sorted).

In the `const` block add:

```go
	// telemetryFlushTimeout bounds the final export of spans and metrics at shutdown.
	telemetryFlushTimeout = 5 * time.Second
```

Below `healthcheckTimeout` add:

```go
// setupTelemetry is telemetry.Setup. It is a variable so a test can replace it.
var setupTelemetry = telemetry.Setup
```

Replace the start of `main` after the `healthcheck` branch, from `if err := godotenv.Load()` through the warnings loop, with:

```go
	dotenvErr := godotenv.Load()

	cfg, err := config.Load(os.LookupEnv)
	// The logger is installed once the configuration is read, so LOG_LEVEL and LOG_FORMAT apply. On a configuration error
	// Load still returns the default logging settings, so the error itself is logged as JSON like everything else.
	slog.SetDefault(telemetry.NewLogger(os.Stdout, cfg.Logging.Format, cfg.Logging.Level, telemetry.ServiceName()))
	if dotenvErr != nil {
		slog.Warn("no .env file found, relying on the environment", "error", dotenvErr)
	}
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	for _, warning := range cfg.Warnings {
		slog.Warn(warning)
	}
```

Add `startTelemetry` after `probeHost`:

```go
// startTelemetry installs the OpenTelemetry SDK and returns the function that flushes it. Call the flush once the server
// has drained, so the drain's spans and the final metrics are exported. Telemetry never stops the API: when the SDK cannot
// start the API runs without it, and a failing flush is only logged.
func startTelemetry(ctx context.Context, cfg config.App) (flush func()) {
	shutdown, err := setupTelemetry(ctx, telemetry.Options{ServiceVersion: pkg.Version, Environment: cfg.Environment})
	if err != nil {
		slog.WarnContext(ctx, "telemetry is disabled: the OpenTelemetry SDK could not start", "error", err)
		return func() {}
	}
	return func() {
		// ctx is already cancelled when this runs (that is what began the shutdown), so the flush gets its own deadline.
		flushCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), telemetryFlushTimeout)
		defer cancel()
		if err := shutdown(flushCtx); err != nil {
			slog.WarnContext(flushCtx, "flushing telemetry", "error", err)
		}
	}
}
```

In `run`, make telemetry the first thing, before the Postgres client (the pool statistics register on the global meter provider when the client is created), and deferred first so it runs last:

```go
func run(ctx context.Context, cfg config.App) error {
	flushTelemetry := startTelemetry(ctx, cfg)
	defer flushTelemetry()

	dbClient, err := postgres.NewPostgresClient(cfg.Database.Postgres)
```

and in the `server.ServerConfig{...}` literal add `ProbePaths: []string{probes.LivePath, probes.ReadyPath},` after `Environment: cfg.Environment,`.

- [ ] **Step 4: Run the tests**

Run: `gofmt -l app/cmd; go test -count=1 ./app/cmd`
Expected: no gofmt output; `ok`. This includes the existing `TestRun_ReturnsAnErrorWhenPostgresIsUnreachable`, which now also proves `run` returns promptly with telemetry on and no collector listening (its 30 second context would expire otherwise).

- [ ] **Step 5: Add the variables to compose and `.env.example`**

In `deploy/app/compose.yml`, in `x-app-env`, after `AUTH_SECRET: ${AUTH_SECRET:-fupi-secret}` add:

```yaml
  OTEL_EXPORTER_OTLP_ENDPOINT: http://otel-collector:4317
  OTEL_SERVICE_NAME: fupi
```

In `.env.example` replace

```
# METRICS
METRICS_ADDRESS=":5555"
METRICS_ENABLED=false
```

with the block below (it stays above the `# --- Local infrastructure` line, which is where the value allowlist begins, so the URL is allowed). `METRICS_READER_PASSWORD` belongs to the ELK stack and stays.

```
# OpenTelemetry: traces and metrics go over OTLP/gRPC to the collector of the observability stack
# (make infra.observability.up). Logs stay JSON on stdout. OTEL_SDK_DISABLED=true turns it all off. Keep a
# parent-based sampler: the health probes rely on it. See docs/LocalInfrastructure.md, "Observing the app".
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4317
OTEL_SERVICE_NAME=fupi
OTEL_TRACES_SAMPLER=parentbased_always_on
```

- [ ] **Step 6: Tidy the module graph**

This is covered by the Task 9 go-ahead (tidy resolves test-only dependencies). Run:

```bash
go mod tidy
git diff go.mod
go build ./... && go vet ./app/...
```

Expected in the diff: `github.com/prometheus/client_golang` removed (and the indirect `prometheus/*`, `beorn7/perks` lines nothing else needs); the new modules moved to the direct block where the code imports them; no existing module raised or lowered other than by being dropped. If tidy bumps an unrelated requirement, stop and ask.

- [ ] **Step 7: Run the repository checks**

Run: `go test ./... && bash scripts/infra_test.sh && make infra.config && make lint.workflows`
Expected: all green (`make infra.config` renders every compose profile with the new variables; `infra_env_check.sh` runs inside it). Then lint the changed Go: `~/go/bin/golangci-lint-v2 run --max-same-issues=0 --max-issues-per-linter=0 ./app/pkg/infra/telemetry/... ./app/pkg/infra/server/... ./app/pkg/infra/tracing/... ./app/internal/application/identity/... ./app/pkg/infra/monitoring/... ./app/pkg/infra/database/... ./app/pkg/infra/cache/... ./app/cmd/... ./app/config/...`: no finding in a line this slice wrote (older findings such as `customRecoveryFunc` stay).

- [ ] **Step 8: Commit**

```bash
git add go.mod go.sum app/cmd deploy/app/compose.yml .env.example
git commit -m "feat(app): start telemetry in run, flush it after the drain, and pass the OTEL variables to the container"
```

---

### Task 13: Live drill (single mode)

No code is written in this task. It proves success criteria 1 to 5 against the real stacks, measures the dependency metric names Task 14 needs, and checks the failure behaviour. Run every command from the repo root. Set `SCRATCH` to the session's scratchpad directory (temporary files go there, not in the repository).

**Files:** none (findings go into the ledger; Task 14 uses the measured metric names).

**Interfaces:**
- Consumes: everything from Tasks 1 to 12 on the branch; the already-local images of the observability, ELK, Postgres, Redis stacks and `fupi-app:local`'s pinned base images.
- Produces: ledger lines `Task 13: dependency metrics: ...` (names and label names of the Postgres pool and Redis metrics), and a `Task 13: Ruling:` line for any drill finding that needed a change.

- [ ] **Step 1: Pre-flight**

Run: `docker ps --format '{{.Names}}\t{{.Ports}}'` and `docker ps -a --format '{{.Names}}' | head -30`
Expected: note what is running that is not yours to touch (the developer's own containers, for example the stopped `supabase_*_koin` ones). If anything already publishes `8085`, `3000`, `9090`, `3200`, `4317`, `9200` or `5432`, stop and ask. Do not stop, remove or restart anything you did not start.

- [ ] **Step 2: Start the stacks**

Run, one after the other:

```bash
make infra.observability.up
make infra.elk.up MODE=single
make infra.app.up MODE=single
curl -s localhost:8085/health/ready
```

Expected: each `make` ends with `ready: <profile>`; the last command prints `{"status":"ok","checks":{"postgres":"up","redis":"up"}}`. The API image is built from the branch (`make infra.app.up` rebuilds it). If the build fails on a module download, that is covered by the Task 9 go-ahead; anything else, stop and report.

- [ ] **Step 3: Send a known trace**

```bash
BASE=http://localhost:8085/api/v1/fupi
T1=4bf92f3577b34da6a3ce929d0e0e4736
TP="traceparent: 00-$T1-00f067aa0ba902b7-01"
curl -s -o /dev/null -w 'register %{http_code}\n' -H "$TP" -H 'content-type: application/json' \
  -d '{"username":"drill","first_name":"Dri","last_name":"Ll","email":"drill@example.com","password":"drill-password-1"}' "$BASE/auth/register"
curl -s -o /dev/null -w 'login %{http_code}\n' -H "$TP" -H 'content-type: application/json' \
  -d '{"email":"drill@example.com","password":"drill-password-1"}' "$BASE/auth/login"
for i in 1 2 3; do curl -s -o /dev/null localhost:8085/health; curl -s -o /dev/null localhost:8085/health/ready; done
```

Expected: `register 201`, `login 200`. (If the user already exists from an earlier run the second drill gets 409: use a new username and email.)

- [ ] **Step 4: Tempo has the trace (criterion 1) and no personal data (criterion 5)**

```bash
for i in $(seq 1 12); do curl -sf "localhost:3200/api/traces/$T1" -o "$SCRATCH/t1.json" && break; sleep 5; done
python3 - "$SCRATCH/t1.json" <<'EOF'
import json, sys
doc = json.load(open(sys.argv[1]))
batches = doc.get("batches") or doc.get("resourceSpans") or []
names, scopes, services = [], set(), set()
for b in batches:
    for attr in b.get("resource", {}).get("attributes", []):
        if attr["key"] == "service.name":
            services.add(attr["value"].get("stringValue"))
    for ss in b.get("scopeSpans") or b.get("instrumentationLibrarySpans") or []:
        scopes.add((ss.get("scope") or ss.get("instrumentationLibrary") or {}).get("name"))
        names += [span["name"] for span in ss.get("spans", [])]
print("services:", services)
print("scopes:  ", sorted(s for s in scopes if s))
print("spans:   ", names)
assert services == {"fupi"}, services
assert any(n.startswith("POST ") and n.endswith("/auth/register") for n in names), "no HTTP register span"
assert any(n.startswith("POST ") and n.endswith("/auth/login") for n in names), "no HTTP login span"
assert "identity.Register" in names and "identity.Login" in names, "no use-case spans"
assert any("otelpgx" in (s or "") for s in scopes), "no Postgres spans"
text = json.dumps(doc)
for secret in ("drill@example.com", "drill-password-1", "Bearer"):
    assert secret not in text, "personal data in a span: " + secret
print("ok")
EOF
curl -s --get localhost:3200/api/search --data-urlencode 'q={ resource.service.name = "fupi" && name =~ ".*health.*" }' --data-urlencode 'limit=20'
```

Expected: `services: {'fupi'}`, the HTTP, use-case and `otelpgx` spans listed, `ok`; the last command prints `{"traces":[],...}` (no probe span). If the trace is not found after a minute, look at `docker logs fupi-app-single-1` for `telemetry export failed` and at the collector's log before changing anything.

- [ ] **Step 5: Prometheus has the metrics, the dashboard panels have data, the alert expression evaluates (criterion 2)**

Wait at least 30 seconds after the traffic (the metric interval is 15 s and `rate` needs two samples), send a second burst so there is a rate, then query:

```bash
for i in 1 2 3 4 5; do curl -s -o /dev/null -H 'content-type: application/json' -d '{"email":"drill@example.com","password":"wrong"}' "$BASE/auth/login"; done
sleep 45
curl -s localhost:9090/api/v1/query --data-urlencode 'query=sum by (http_route, http_response_status_code) (http_server_request_duration_seconds_count{service_name="fupi"})' | python3 -m json.tool
curl -s localhost:9090/api/v1/query --data-urlencode 'query=http_server_request_duration_seconds_count{service_name="fupi",http_route=~"/health.*"}' | python3 -m json.tool
curl -s localhost:9090/api/v1/query --data-urlencode 'query=histogram_quantile(0.99, sum by (le) (rate(http_server_request_duration_seconds_bucket{service_name="fupi"}[5m]))) > 0.1'
```

Expected: series for `http_route="/api/v1/fupi/auth/register"` code `201`, `/auth/login` codes `200` and `401`; the second query returns an empty `result` (probes are not counted); the third returns `"status":"success"` (an empty result means p99 is under 100 ms).

Then run every Prometheus panel of the dashboard through Grafana itself:

```bash
python3 - <<'EOF'
import base64, json, urllib.request
def call(path, body=None):
    req = urllib.request.Request("http://localhost:3000" + path, data=None if body is None else json.dumps(body).encode(),
                                 headers={"Content-Type": "application/json",
                                          "Authorization": "Basic " + base64.b64encode(b"admin:fupi-grafana-dev").decode()})
    return json.load(urllib.request.urlopen(req))
dash = call("/api/dashboards/uid/fupi-service")["dashboard"]
failed = False
for panel in dash["panels"]:
    for target in panel.get("targets", []):
        if panel.get("datasource", {}).get("type") != "prometheus" or "expr" not in target:
            continue
        body = {"queries": [{"refId": "A", "datasource": panel["datasource"], "expr": target["expr"], "instant": True}], "from": "now-15m", "to": "now"}
        frames = call("/api/ds/query", body)["results"]["A"].get("frames", [])
        values = sum(len(f["data"]["values"][-1]) if f["data"]["values"] else 0 for f in frames)
        print(f'{panel["title"]!r}: {len(frames)} frame(s), {values} value(s)')
        failed |= values == 0
raise SystemExit(1 if failed else 0)
EOF
```

Expected: every listed panel prints at least one value and the exit status is 0 (the 5xx ratio panel may show `0`, which counts; a panel with no frames at all fails). The password in this snippet is the documented development default of the local stack (`docs/LocalInfrastructure.md`).

- [ ] **Step 6: Elasticsearch has the log line with the same trace ID, and Grafana's link query finds it (criterion 3)**

```bash
for i in $(seq 1 12); do
  curl -s -u elastic:fupi-elastic-dev 'localhost:9200/logs-fupi-*/_search?size=20' -H 'content-type: application/json' \
    -d "{\"query\":{\"match_phrase\":{\"trace.id\":\"$T1\"}},\"_source\":[\"@timestamp\",\"message\",\"log.level\",\"service.name\",\"trace.id\",\"span.id\",\"app.route\",\"app.status\",\"app.request_id\"]}" \
    -o "$SCRATCH/es.json"
  python3 -c 'import json,sys; sys.exit(0 if json.load(open(sys.argv[1]))["hits"]["total"]["value"] else 1)' "$SCRATCH/es.json" && break
  sleep 5
done
python3 -m json.tool "$SCRATCH/es.json" | head -60
curl -s -u elastic:fupi-elastic-dev 'localhost:9200/logs-fupi-*/_count' -H 'content-type: application/json' -d '{"query":{"match_phrase":{"app.path":"/health"}}}'
```

Expected: hits whose `message` is `request` (the access log, with `app.route` `/api/v1/fupi/auth/register`, `app.status` 201, `app.request_id`) and the use-case lines (`IdentityService<Register> Registered user`), all with `service.name` `fupi`, `log.level` `INFO` and `trace.id` equal to `$T1`; the count query prints `"count":0` (probes are not logged at info). If a `fupi` log line has no `trace.id`, the JSON keys do not match `deploy/elk/logstash/pipeline/20-filter.conf`: compare the raw container line (`docker logs fupi-app-single-1 | head -3`) with the filter before changing code.

Then run the query Grafana's trace-to-logs link builds (`trace.id:"<id>"`, `deploy/observability/grafana/provisioning/datasources/datasources.yml`) through the Elasticsearch datasource:

```bash
python3 - "$T1" <<'EOF'
import base64, json, sys, urllib.request
body = {"queries": [{"refId": "A", "datasource": {"type": "elasticsearch", "uid": "elasticsearch"},
                     "query": 'trace.id:"%s"' % sys.argv[1], "metrics": [{"type": "logs", "id": "1", "settings": {"limit": "20"}}],
                     "bucketAggs": [], "timeField": "@timestamp"}], "from": "now-1h", "to": "now"}
req = urllib.request.Request("http://localhost:3000/api/ds/query", data=json.dumps(body).encode(),
                             headers={"Content-Type": "application/json",
                                      "Authorization": "Basic " + base64.b64encode(b"admin:fupi-grafana-dev").decode()})
res = json.load(urllib.request.urlopen(req))["results"]["A"]
rows = sum(len(f["data"]["values"][0]) if f["data"]["values"] else 0 for f in res.get("frames", []))
print("log rows found through the link's query:", rows)
raise SystemExit(0 if rows else 1)
EOF
```

Expected: at least one row. (If Grafana rejects the query model, take it from the panel JSON of "Application logs (Elasticsearch)" in the dashboard and keep the query string; ledger the adjustment.)

- [ ] **Step 7: Measure the dependency metrics (input for Task 14)**

Send a few readiness probes (they ping Redis) and list the metrics:

```bash
for i in 1 2 3 4 5 6; do curl -s -o /dev/null localhost:8085/health/ready; sleep 1; done
sleep 40
curl -s localhost:9090/api/v1/label/__name__/values | python3 -c 'import json,sys; print("\n".join(n for n in json.load(sys.stdin)["data"] if any(k in n for k in ("db_client","redis","pgx","pool","connections"))))'
```

For each name print its labels and one sample, for example `curl -s localhost:9090/api/v1/series --get --data-urlencode 'match[]=<name>{service_name="fupi"}' | python3 -m json.tool`, and write one ledger line `Task 13: dependency metrics: <name> labels <...> ; ...` for the Postgres pool (connections in use/idle/max, acquire waits) and for Redis (command count and duration, with the label that holds the command name). Task 14 builds the Dependencies row from exactly these names. Note in the ledger whether the readiness `PING` shows in the Redis series (it is expected to: plan note 9).

- [ ] **Step 8: The collector goes away and comes back (criterion 4)**

```bash
for i in $(seq 1 20); do curl -s -o /dev/null -w '%{time_total}\n' localhost:8085/health; done | sort -n | tail -3
docker compose --profile observability stop otel-collector
for i in $(seq 1 20); do curl -s -o /dev/null -w '%{time_total}\n' localhost:8085/health; done | sort -n | tail -3
curl -s -o /dev/null -w 'login while the collector is down: %{http_code} in %{time_total}s\n' -H 'content-type: application/json' -d '{"email":"drill@example.com","password":"drill-password-1"}' "$BASE/auth/login"
sleep 130
docker logs fupi-app-single-1 2>&1 | grep -c 'telemetry export failed'
docker logs fupi-app-single-1 2>&1 | grep 'telemetry export failed' | tail -3
```

Expected: the slowest `/health` times are in the same range before and after (milliseconds), the login still answers 200 in its usual time, and the count of `telemetry export failed` lines is a handful (roughly one per signal per minute, so at most about ten after two minutes), not hundreds. Then:

```bash
docker compose --profile observability start otel-collector
T2=0af7651916cd43dd8448eb211c80319c
curl -s -o /dev/null -H "traceparent: 00-$T2-00f067aa0ba902b7-01" -H 'content-type: application/json' -d '{"email":"drill@example.com","password":"drill-password-1"}' "$BASE/auth/login"
for i in $(seq 1 36); do curl -sf "localhost:3200/api/traces/$T2" -o /dev/null && echo "trace $T2 arrived after about $((i*5))s" && break; sleep 5; done
```

Expected: the trace arrives without restarting the API (gRPC reconnects with backoff; allow up to three minutes). If it never does, record it as a finding.

- [ ] **Step 9: SIGTERM flushes the last spans (criterion 4)**

```bash
T3=1af7651916cd43dd8448eb211c80319d
curl -s -o /dev/null -H "traceparent: 00-$T3-00f067aa0ba902b7-01" -H 'content-type: application/json' -d '{"email":"drill@example.com","password":"drill-password-1"}' "$BASE/auth/login"
docker stop -t 20 fupi-app-single-1
docker inspect -f 'exit code {{.State.ExitCode}}' fupi-app-single-1
for i in $(seq 1 12); do curl -sf "localhost:3200/api/traces/$T3" -o /dev/null && echo "trace $T3 found" && break; sleep 5; done
```

Expected: exit code `0` and `trace ... found`: the request was made less than a second before the stop, while the batch exporter waits five seconds by default, so only the shutdown flush can have exported it. Then bring the API back: `make infra.app.up MODE=single`.

- [ ] **Step 10: `OTEL_SDK_DISABLED` and the text log format (run on the host)**

```bash
go build -o "$SCRATCH/fupi" ./app/cmd
OTEL_SDK_DISABLED=true LOG_FORMAT=text HTTP_PORT=8086 "$SCRATCH/fupi" > "$SCRATCH/host.log" 2>&1 &
echo $! > "$SCRATCH/host.pid"
sleep 3
T4=2af7651916cd43dd8448eb211c80319e
curl -s -o /dev/null -w '%{http_code}\n' -H "traceparent: 00-$T4-00f067aa0ba902b7-01" -H 'content-type: application/json' -d '{"email":"drill@example.com","password":"drill-password-1"}' http://localhost:8086/api/v1/fupi/auth/login
kill "$(cat "$SCRATCH/host.pid")"; sleep 2
head -5 "$SCRATCH/host.log"
sleep 30
curl -s -o /dev/null -w 'tempo answers %{http_code} for the disabled run (404 expected)\n' "localhost:3200/api/traces/$T4"
```

Expected: `200`; the log is readable `key=value` text with `service=fupi` (no JSON); Tempo answers 404 for `$T4`. (The host API reaches the same Postgres and Redis through their default `localhost` ports.) `rm "$SCRATCH/fupi" "$SCRATCH/host.pid" "$SCRATCH/host.log"` afterwards.

- [ ] **Step 11: Tear down what the drill started**

```bash
make infra.app.down
make infra.elk.down
make infra.observability.down
make infra.core.down MODE=single
docker ps --format '{{.Names}}'
```

Expected: only containers that were running before Step 1 remain. (The data volumes are kept, which is also what the developer's `make infra.*` flows expect.) If a finding in Steps 4 to 10 needed a code change, make it test-first in the owning task's package, commit it as `fix(telemetry): ...` (with the trailer) and ledger it as `Task 13: Ruling: ...`; no commit is needed otherwise.

---

### Task 14: Dependencies row, alert comment, documentation and ADR

**Files:**
- Modify: `deploy/observability/grafana/dashboards/fupi-service.json`
- Modify: `deploy/observability/prometheus/rules/stack.yml` (one comment)
- Modify: `docs/LocalInfrastructure.md`, `docs/Deployment.md`
- Create: `docs/adr/0017-the-api-exports-telemetry-over-otlp-and-keeps-logs-on-stdout.md`

**Interfaces:**
- Consumes: the metric and label names from the Task 13 ledger line.
- Produces: the dashboard version 2 with a Dependencies row; the documentation of success criterion 7.

- [ ] **Step 1: Add the Dependencies row to the dashboard**

The file keeps one panel per line. Add the new panels as new lines after the last one (the "Recent traces (Tempo)" table, `id` 7, `y` 21, `h` 9, so the row starts at `y` 30), and change `"version": 1` to `"version": 2`. Do this with the script below, after filling the five names at the top from the Task 13 ledger line; it inserts text and leaves every other line exactly as it is.

```bash
python3 - <<'EOF'
import json, re

# From the Task 13 ledger line "dependency metrics": the Prometheus name of each series and the label that distinguishes it.
PG_CONNECTIONS = "<pool connections metric>"     # e.g. the "connections in use / idle" gauge
PG_CONNECTIONS_LABEL = "<its state label>"
PG_WAITS = "<pool acquire waits metric>"         # rate of waits for a connection; drop panel 10 if the library has none
REDIS_DURATION = "<redis command duration histogram, without _bucket/_count>"
REDIS_COMMAND_LABEL = "<its command-name label>"

path = "deploy/observability/grafana/dashboards/fupi-service.json"
text = open(path).read()
prom = {"type": "prometheus", "uid": "prometheus"}

def panel(id, title, x, expr, legend, unit):
    return {"id": id, "type": "timeseries", "title": title, "gridPos": {"x": x, "y": 31, "w": 12, "h": 8}, "datasource": prom,
            "targets": [{"refId": "A", "expr": expr, "legendFormat": legend}], "fieldConfig": {"defaults": {"unit": unit}, "overrides": []}}

panels = [
    {"id": 8, "type": "row", "title": "Dependencies", "gridPos": {"x": 0, "y": 30, "w": 24, "h": 1}, "collapsed": False, "panels": []},
    panel(9, "Postgres pool connections", 0, f'sum by ({PG_CONNECTIONS_LABEL}) ({PG_CONNECTIONS}{{service_name="fupi"}})', f"{{{{{PG_CONNECTIONS_LABEL}}}}}", "short"),
    panel(10, "Postgres pool waits per second", 12, f'sum(rate({PG_WAITS}{{service_name="fupi"}}[5m]))', "waits", "ops"),
    panel(11, "Redis commands per second", 0, f'sum by ({REDIS_COMMAND_LABEL}) (rate({REDIS_DURATION}_count{{service_name="fupi"}}[5m]))', f"{{{{{REDIS_COMMAND_LABEL}}}}}", "ops"),
    panel(12, "Redis command latency p99", 12, f'histogram_quantile(0.99, sum by (le) (rate({REDIS_DURATION}_bucket{{service_name="fupi"}}[5m])))', "p99", "s"),
]
lines = ",\n".join("    " + json.dumps(p, separators=(",", ":")) for p in panels)

# the last panel line is the Tempo table; the panels array closes with "  ]"
match = re.search(r'(\n)(  \]\n\})\s*$', text)
assert match, "unexpected end of the dashboard file"
text = text[:match.start()] + ",\n" + lines + text[match.start():]
text = text.replace('"version": 1,', '"version": 2,', 1)
open(path, "w").write(text)
json.loads(text)  # still valid JSON
EOF
git diff --stat deploy/observability/grafana/dashboards/fupi-service.json
```

Expected: the diff adds the new panel lines and changes the `version` line; if the file ends differently from what the pattern assumes, the `assert` fails: fix the insertion by hand instead (insert before the line that closes the `panels` array), keeping the one-panel-per-line style. If the library records no waits metric, delete panel 10 and ledger it.

- [ ] **Step 2: Check the new panels against the running stack**

Grafana re-reads the file every few seconds (provisioning `updateIntervalSeconds`). With the stacks from Task 13 running again (repeat its Steps 2 and 3 and 7 if you tore them down, so the series exist), run the panel script of Task 13 Step 5 once more.
Expected: all panels, including the four new ones, print at least one value and the exit status is 0. Then `curl -s -u admin:fupi-grafana-dev localhost:3000/api/dashboards/uid/fupi-service | python3 -c 'import json,sys; d=json.load(sys.stdin)["dashboard"]; print(d["version"], len(d["panels"]))'` prints `2 12`. Stop the stacks again as in Task 13 Step 11.

- [ ] **Step 3: Fix the alert comment that is no longer true**

In `deploy/observability/prometheus/rules/stack.yml` replace

```yaml
      # Inert until the application exports OpenTelemetry HTTP metrics (a later slice).
```

with

```yaml
      # Fed by the API's http.server.request.duration histogram (OpenTelemetry, via the collector).
```

- [ ] **Step 4: Write the ADR**

Create `docs/adr/0017-the-api-exports-telemetry-over-otlp-and-keeps-logs-on-stdout.md`:

```markdown
---
status: accepted
---

# The API exports traces and metrics over OTLP, keeps logs on stdout, and instruments HTTP with a small Fiber middleware

The API sends traces and metrics to the OpenTelemetry Collector over OTLP/gRPC, configured with the standard `OTEL_*` variables (the service name defaults to `fupi`, `OTEL_SDK_DISABLED=true` turns it off). The collector feeds Tempo and the Prometheus scrape. Logs stay one JSON object per line on stdout, which Filebeat and Logstash already ship to Elasticsearch; they carry `trace_id` and `span_id` when the log call's context has a span.

There is one trace ID everywhere: the W3C trace ID of the OpenTelemetry span. `tracing.GetTraceID` returns it when a span is present and falls back to the old KSUID-style value only without one, so the logs, the spans and the gRPC metadata agree and Grafana can jump from a span to its log lines.

HTTP is instrumented by about 150 lines of Fiber middleware, first in the chain, instead of an adapter library. It emits exactly the metric the dashboard and the `RedirectLatencyHigh` alert query (`http.server.request.duration`, seconds, with `http_route` and `http_response_status_code`), takes the route template from Fiber (never the raw path), and records no query string, header or client address. Postgres (`otelpgx`) and Redis (`redisotel`) use their libraries' instrumentation with SQL arguments and Redis command text switched off. The Identity use cases add spans that record the class of an error, never its text, because the domain's error messages embed the caller's input.

The unauthenticated Fiber `/metrics` page, the `monitoring/metrics` package that registered another project's Prometheus metrics, and the gRPC metrics interceptor are removed: OTLP is the one metrics path.

## Considered options

- **A Prometheus `/metrics` endpoint on the API** — the collector would not be needed for metrics, but it is a second pipeline next to the traces, it needs its own authentication story, and the stack was built around OTLP.
- **OTLP for logs too** — one pipeline for everything, but it would bypass Filebeat and Logstash, which already map the JSON keys to ECS fields, and logs would stop working when only the ELK stack is running.
- **The OpenTelemetry Fiber contrib middleware** — less code here, but its metric names, labels and route handling would have to be adapted to what the dashboard queries, and it brings its own dependency tree.
- **Keeping both trace IDs** — no code change, but a log line and its trace could not be matched.

## Consequences

- The API needs a reachable collector only to export; without one it serves normally and logs one `telemetry export failed` line a minute per distinct error.
- A parent-based sampler (the default) is required for the readiness checks to stay out of Tempo: they run under an unsampled parent span. Production should keep a parent-based sampler and lower the rate (`OTEL_TRACES_SAMPLER=parentbased_traceidratio`).
- The readiness `PING` to Redis still appears in the Redis command metrics; only spans are suppressed.
- Logs from an API run on the host reach the terminal only; Filebeat reads container logs.
- Writing the trace context into `outbox_events.headers` and Kafka instrumentation belong to the outbox relay slice.
```

- [ ] **Step 5: Document "Observing the app" in `docs/LocalInfrastructure.md`**

Insert this section between `## The app in a container` and `## The stacks`:

````markdown
## Observing the app

The API sends traces and metrics over OTLP to the collector of the observability stack and writes JSON logs to stdout, which Filebeat ships to Elasticsearch when the API runs as a container. The three are tied together by one W3C trace ID.

```bash
make infra.observability.up       # collector, Tempo, Prometheus, Alertmanager, Grafana
make infra.elk.up MODE=single     # Elasticsearch, Logstash, Kibana, Filebeat (MODE=ha for the cluster)
make infra.app.up MODE=single     # the API; its OTEL_* variables point at the collector
```

`make infra.app.up` does not start the observability or ELK stacks (they are separate stacks, see the memory budget). Without the collector the API still serves; it logs one `telemetry export failed` line a minute.

Follow one request. Send it with a `traceparent` of your own, so you know the trace ID:

```bash
curl -s -H 'traceparent: 00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01' -H 'content-type: application/json' \
  -d '{"email":"you@example.com","password":"your-password"}' localhost:8085/api/v1/fupi/auth/login
```

- **Traces:** Grafana (<http://localhost:3000>), Explore, Tempo, "TraceQL" with `{ resource.service.name = "fupi" }`, or "Trace ID" with `4bf92f3577b34da6a3ce929d0e0e4736`. The trace is the HTTP server span `POST /api/v1/fupi/auth/login`, the `identity.Login` use-case span and the Postgres query spans. "Logs for this span" jumps to the Elasticsearch lines with that `trace.id`.
- **Metrics:** the "Fupi service" dashboard (folder Fupi): requests per second and latency by route, the 5xx ratio, application logs and recent traces, and a Dependencies row with the Postgres pool and Redis commands. In Prometheus the request metric is `http_server_request_duration_seconds_*` with `service_name`, `http_route` and `http_response_status_code`.
- **Logs:** Kibana (<http://localhost:5601>), data view `logs-fupi-*`, filter `trace.id : "4bf92f3577b34da6a3ce929d0e0e4736"`. The access log line is the message `request` with the method, route, status, duration and request ID.

What to know:

- `/health` and `/health/ready` produce no spans and no HTTP metrics, and their access log lines are debug level. The readiness check's Redis `PING` still shows in the Redis command metrics.
- Never recorded in a span: SQL arguments, Redis keys and values, query strings, request headers, client addresses, email addresses, usernames and tokens.
- An API run on your host (`make run`) still exports to the collector on `localhost:4317`. Its logs go to your terminal only (Filebeat reads container logs, not your terminal); `LOG_FORMAT=text` makes them readable, `LOG_LEVEL=debug` shows the probes.
- Settings are the standard OpenTelemetry variables, listed in `.env.example`: `OTEL_EXPORTER_OTLP_ENDPOINT` (default `http://localhost:4317`), `OTEL_SERVICE_NAME` (default `fupi`), `OTEL_TRACES_SAMPLER` (default `parentbased_always_on`; keep a parent-based sampler, the readiness checks rely on it), `OTEL_METRIC_EXPORT_INTERVAL` (milliseconds, default 15000) and `OTEL_SDK_DISABLED=true` to turn it all off.
````

In `### Observability`, replace the sentence `The service dashboard shows "No data" until the app emits OpenTelemetry metrics.` (it is split across two lines in the bullet about Grafana) with `The service dashboard fills in once the API runs (see "Observing the app").`; keep the rest of the bullet and re-wrap if needed.

- [ ] **Step 6: Document the variables in `docs/Deployment.md`**

After the paragraph that starts `Every variable, its default and its unit is listed in the app-connectivity spec`, add:

```markdown
Optional telemetry and logging variables (the defaults suit the local stack; none is required in production):

| Variable | Meaning |
|---|---|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | the OpenTelemetry Collector's OTLP/gRPC address, for example `http://otel-collector:4317` (default `http://localhost:4317`); an `https://` address uses TLS |
| `OTEL_SERVICE_NAME` | the service name on traces, metrics and log lines (default `fupi`) |
| `OTEL_TRACES_SAMPLER`, `OTEL_TRACES_SAMPLER_ARG` | default `parentbased_always_on`, which is too much for production traffic: set for example `parentbased_traceidratio` with `0.05`. Keep a parent-based sampler: the readiness checks run under an unsampled parent so they leave no spans |
| `OTEL_METRIC_EXPORT_INTERVAL` | milliseconds between metric exports (default 15000) |
| `OTEL_SDK_DISABLED` | `true` turns tracing and metrics off |
| `LOG_LEVEL` | `debug`, `info` (default), `warn` or `error` |
| `LOG_FORMAT` | `json` (default; one object per line with `time`, `level`, `msg`, `service`, and `trace_id`/`span_id` inside a request) or `text` |

A collector that is unreachable never stops the API or slows a request; the API logs one `telemetry export failed` line a minute. On SIGTERM the API exports its last spans and metrics after it has drained, waiting at most five seconds.
```

- [ ] **Step 7: Run the final checks**

Run: `go test ./... && bash scripts/infra_test.sh && make infra.config && make lint.workflows && git diff --check`
Expected: all green and no whitespace errors. Read the rendered diff of the two Markdown files once for broken fences.

- [ ] **Step 8: Commit**

```bash
git add deploy/observability docs
git commit -m "docs: document observing the app, add the Dependencies dashboard row and record ADR-0017"
```

---

## Spec coverage (self-review)

| Spec requirement | Task |
|---|---|
| Success criterion 1: trace with HTTP, use-case and Postgres spans | Tasks 4, 7, 10; drill Task 13 step 4 |
| Criterion 2: Prometheus series, dashboard, alert expression | Task 4 (metric shape), drill step 5 |
| Criterion 3: same `trace.id` in Elasticsearch and the Grafana link | Tasks 1, 5; drill step 6 |
| Criterion 4: collector down, SIGTERM flush | Tasks 3, 9, 12; drill steps 8 and 9 |
| Criterion 5: probes silent; no Redis keys or SQL parameters in spans | Tasks 4, 5, 8, 10, 11; drill step 4 |
| Criterion 6: `/metrics`, Bids-era metrics and dependency gone; suite green | Tasks 6, 12 |
| Criterion 7: docs | Task 14 |
| D1, D2 (OTLP, `OTEL_*`, disabled switch) | Task 9 |
| D3 custom Fiber middleware | Task 4 |
| D4 removal | Task 6 (+ `go mod tidy` in Task 12) |
| D5 `traceparent` in the outbox deferred | not in this plan (slice 5), recorded in ADR-0017 |
| D6 one trace ID | Task 2 |
| D7 no Redis text, no SQL arguments | Tasks 10, 11 |
| D8 probes untraced, readiness under `Unsampled` | Tasks 3, 4, 8 |
| D9 telemetry never affects a request | Tasks 3, 9, 12 |
| D10 `infra.app.up` does not start observability/ELK | Task 14 docs (no code change) |
| D11 trailer | Global Constraints |
| Section 6: JSON handler, access log, context sweep, `.env.example` duplicates, `LOG_*` config | Tasks 1, 4, 5, 8 |
| Section 9: compose variables, `.env.example` block, dashboard row, docs, ADR | Tasks 12, 14 |
| Section 11 risk: Unsampled needs a parent-based sampler | Tasks 3, 14 |
