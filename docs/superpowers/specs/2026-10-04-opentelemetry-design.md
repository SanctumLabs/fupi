# OpenTelemetry: traces, metrics and correlated logs — design

Status: draft for review · Date: 2026-10-04 · Slice 4 of 5

## 1. Intent

Slice 1 built an observability stack (OpenTelemetry Collector, Tempo, Prometheus, Grafana, ELK) and slice 2 put the API in a container
on the same network, but the API emits nothing the stack can use. There is no OpenTelemetry SDK in the code: only the trace API is
imported (by the zap logger and the gRPC interceptors), so no span is ever created or exported. The `tracing` package is request-ID
helpers whose doc comment describes an OpenTelemetry wrapper that does not exist, and its `x-trace-id` is a KSUID-style value, not a
W3C trace ID. `/metrics` is Fiber's `monitor` page titled "Bids Service Metrics Page", served unauthenticated; it is not Prometheus.
The `monitoring/metrics` package registers Bids-service metrics from another project and nothing wires it. Request logs are Fiber's
plain-text logger and application logs use slog's text handler, while the ELK pipeline expects one JSON object per line.

This slice makes the API observable inside that stack: traces in Tempo, metrics in Prometheus and Grafana, logs in Elasticsearch, all
tied together by one W3C trace ID, with the existing dashboard, alert and trace-to-logs link working without edits.

**What the stack already expects (slice 1)**

- OTLP at the collector: gRPC `:4317` and HTTP `:4318` (`localhost` from the host, `otel-collector` from containers).
- Metrics named `http_server_request_duration_seconds_{bucket,count}` with the labels `service_name="fupi"`, `http_route` and
  `http_response_status_code` (the Fupi service dashboard and the `RedirectLatencyHigh` alert query exactly these). The collector's
  Prometheus exporter turns OpenTelemetry's `http.server.request.duration` (unit `s`) into that name and the resource's `service.name`
  into `service_name`.
- Traces whose resource has `service.name = "fupi"` (the dashboard's trace table queries it).
- Logs in the `logs-fupi-*` data stream with `service.name`, `log.level`, `message` and `trace.id`; Grafana's trace-to-logs link
  searches `trace.id:"<id>"`. The Logstash filter maps a JSON line's `time`, `level`, `msg`, `service`, `trace_id` and `span_id` to those
  fields.

**Success criteria**

1. With the observability and ELK stacks running, one request to the containerised API produces a Tempo trace
   (`resource.service.name = "fupi"`) containing the HTTP server span, an identity use-case span and the Postgres query spans.
2. Prometheus has `http_server_request_duration_seconds_count` with `service_name="fupi"`, `http_route` and `http_response_status_code`
   for that request, the Fupi service dashboard panels show data, and the `RedirectLatencyHigh` expression evaluates.
3. The same request's log line is in Elasticsearch with a `trace.id` equal to the Tempo trace ID, and Grafana's trace-to-logs link finds it.
4. With the collector stopped the API's responses are unaffected and the log is not flooded; on SIGTERM the telemetry is flushed.
5. Health probes create no spans and no metrics; Redis keys, values and SQL parameters never appear in a span.
6. The unauthenticated `/metrics` page, the Bids-era metrics code and its dependency are gone, and `go test ./...` stays green.
7. A new developer can follow the docs to see traces, metrics and logs for a request.

## 2. Scope

Slice table (slices 1 to 3 are on `feat/local-infra-stack`):

| # | Slice | State |
| --- | ------- | ------- |
| 1 | Local infrastructure stack | on `feat/local-infra-stack` (PR SanctumLabs/fupi#342, open) |
| 2 | Production image, `app` stack, CI | on `feat/local-infra-stack` |
| 3 | App connectivity | on `feat/local-infra-stack` |
| 4 | **This spec** — OpenTelemetry | |
| 5 | Outbox relay and the Kafka client | next |

**In scope:** a new `app/pkg/infra/telemetry` package; the Fiber server middleware and access log; the request-path logging sweep; the pgx
pool tracer and the Redis hooks; identity use-case spans; the `tracing` package clean-up; removal of the old metrics code; `app/config`
(`LOG_LEVEL`, `LOG_FORMAT`); `deploy/app/compose.yml`, `.env.example`, `docs/LocalInfrastructure.md`, `docs/Deployment.md`, one ADR, and
a Dependencies row on the Fupi service dashboard.

**Out of scope:** Kafka instrumentation and writing `traceparent` into `outbox_events.headers` (both slice 5, where the relay and the Kafka
client are designed); OTLP logs (logs reach Elasticsearch through Filebeat, as slice 1 built it); gRPC server and client instrumentation
(the gRPC code is not wired); profiling and exemplars; new alert rules; Sentry; the older unused `SlogLogger` and zap code beyond what
section 6 touches.

## 3. Decisions

| # | Decision | Why |
| --- | ---------- | ----- |
| D1 | Traces and metrics leave the process as OTLP over gRPC to the collector; logs stay JSON on stdout. | It is the pipeline slice 1 built: the collector feeds Tempo and the Prometheus scrape, Filebeat feeds Logstash. |
| D2 | The SDK is configured with the standard `OTEL_*` variables; the service name defaults to `fupi`; `OTEL_SDK_DISABLED=true` installs no-ops. | No custom configuration to learn, and with no variable set it targets `localhost:4317`, the address contract from slice 1. |
| D3 | The HTTP server is instrumented by a small custom Fiber middleware. Confirmed by the user. | It emits exactly the metric names and labels the dashboard and alert use, takes route templates from Fiber, and adds no dependency whose metric names would need adapting. |
| D4 | The `/metrics` page, the Bids-era `monitoring/metrics` package and the gRPC metrics interceptor are removed. Confirmed by the user. | OTLP is the one metrics path. The page is unauthenticated, exposes runtime stats and is not Prometheus; the package describes another project. |
| D5 | Writing the trace context into `outbox_events.headers` waits for slice 5. Confirmed by the user. | Writing and reading the header are one feature, "a trace continues through the outbox", designed and tested with the relay. |
| D6 | One trace ID everywhere: the W3C trace ID of the OpenTelemetry span. `tracing.GetTraceID` returns it when a span is present and falls back to the old value only without one. | Logs, spans and the zap and gRPC code then agree, and Grafana can link them. |
| D7 | Redis command text and SQL parameters are not recorded; a span carries the SQL text but never its arguments. | Keys and values can hold user data (URLs, tokens); sqlc queries are static, so their text is safe and useful. |
| D8 | Health probes are not traced or counted, and the readiness pings run under a non-sampled parent. | The probes are polled every few seconds; the pings would otherwise start a root trace each time. |
| D9 | Telemetry can never affect a request: exporters are asynchronous with short timeouts, and the SDK error handler logs each distinct error at most once a minute. | A stopped collector must not slow the API or fill the log. |
| D10 | `make infra.app.up` does not start the observability or ELK stacks. | They stay independent stacks, as in slice 2. |
| D11 | Every commit made while implementing this spec ends with the `Co-Authored-By` trailer given in the session's attribution instructions; example `git commit` snippets in plans omit the trailer text. | The project's attribution convention (the user confirmed it on 2026-10-04). |

## 4. The telemetry package (`app/pkg/infra/telemetry`)

```go
// Setup installs the global tracer provider, meter provider and propagators and returns a function that flushes and stops them.
func Setup(ctx context.Context, opts Options) (shutdown func(context.Context) error, err error)

type Options struct {
    ServiceVersion string // from app/pkg.Version
    Environment    string // ENVIRONMENT, recorded as deployment.environment.name
}
```

- **Resource:** `service.name` (`OTEL_SERVICE_NAME`, default `fupi`), `service.version`, `deployment.environment.name`, plus the SDK's
  host detector. The process detector is omitted on purpose: the collector turns every resource attribute into a label on every metric
  series, and `process.pid` or `process.command_args` are not labels anyone wants. The collector's Prometheus exporter turns
  `service.name` into the `service_name` label.
- **Traces:** OTLP/gRPC exporter behind the batch span processor, sampler from `OTEL_TRACES_SAMPLER` (default `parentbased_always_on`).
- **Metrics:** OTLP/gRPC exporter behind a periodic reader (interval from `OTEL_METRIC_EXPORT_INTERVAL`, default 15 seconds, the stack's scrape interval).
- **Propagation:** W3C `traceparent` and `baggage`.
- **Off switch:** `OTEL_SDK_DISABLED=true` returns a shutdown that does nothing and leaves the global no-op providers in place.
- **Failure handling (D9):** `Setup` does not dial: exporters connect lazily, so a missing collector never fails startup. The error
  handler logs each distinct error at most once a minute.
- **Shutdown:** `run` calls the returned function after the server has drained, with a 5 second deadline, so the drain's spans and the final
  metrics are exported. A failing flush is logged, never fatal.
- **Probe context:** `telemetry.Unsampled(ctx)` returns a context whose parent span context is valid but not sampled, so a parent-based
  sampler records nothing beneath it (D8). The readiness registry runs every check under it.

## 5. HTTP server (`app/pkg/infra/server/middleware/otel.go`)

The middleware is the first in the chain, before the request ID and the access log, so both see its span.

- **Skipped paths:** `/health` and `/health/ready` get no span and no metric.
- **Span:** it extracts the incoming trace context, starts a server span, and stores the new context with `SetUserContext`, so handlers,
  use cases, pgx and Redis nest under it. The name is `METHOD route-template`, set after the handler runs, when Fiber knows the route; an
  unmatched route gets the name `METHOD` and no `http.route` (cardinality). A 5xx response sets the span status to error.
- **Attributes:** `http.request.method`, `http.route`, `http.response.status_code`, `url.scheme`, `url.path` (without the query string),
  `server.address`, `user_agent.original`. The client address and request headers are not recorded.
- **Metrics:** `http.server.request.duration` (float64 histogram, unit `s`, the semantic-convention buckets 0.005 to 10) with
  `http.request.method`, `http.route`, `http.response.status_code` and `url.scheme`, and `http.server.active_requests` (up-down counter).
  These become `http_server_request_duration_seconds_*` and the labels the dashboard queries.
- **Panics:** the recover middleware is inside this one, so a panic is already a 500 when the span ends.

## 6. Logs

- **Format:** `main` installs a slog default handler. With `LOG_FORMAT=json` (the default) it writes one JSON object per line to stdout
  with slog's `time`, `level` and `msg` plus `service`, and `trace_id` and `span_id` when the log call's context has a valid span. These are
  the keys the Logstash filter already renames to `@timestamp`, `log.level`, `message`, `service.name`, `trace.id` and `span.id`.
  `LOG_FORMAT=text` is the human format for a terminal. `LOG_LEVEL` defaults to `info`. Both join `config.App` (a typed `Logging` section
  with the same strict loader, so an unknown format is a startup error).
- **Access log:** a slog middleware replaces Fiber's plain-text logger: method, route, path, status, duration, bytes and request ID, logged
  with the request context so it carries the IDs. Probe requests are logged at debug level only.
- **Context sweep:** request-path calls to `slog.Info`, `Warn`, `Error` and `Debug` that have a `ctx` in scope become the `...Context(ctx, ...)`
  forms (the auth middleware and the datastore wrappers already do this in places); calls without a request context are left alone.
- **One trace ID (D6):** `tracing.GetTraceID` prefers the span's W3C trace ID; the package's doc comment is corrected to say what it is
  (request and correlation ID helpers), and the zap logger and the gRPC client, which call it, now carry the W3C ID.
- **Duplicate keys:** `.env.example` sets `LOG_LEVEL` twice (`debug`, later `"info"`); the slice leaves one `LOG_LEVEL` and one `LOG_FORMAT`.
- **Shipping:** Filebeat reads container logs only, so an API run on the host does not reach Elasticsearch; its JSON goes to the terminal.

## 7. Dependencies and use cases

- **Postgres:** `otelpgx` is set as the pool's tracer in `NewPostgresClient` (`poolConfig.ConnConfig.Tracer`), giving a span per query and
  per transaction step, and its pool statistics are registered as metrics. SQL text is in the span, arguments are not (D7).
- **Redis:** `redisotel` instruments the universal client for tracing and metrics with the command text switched off (D7).
- **Identity:** `Register`, `Login`, `Refresh` and `VerifyEmail` each start a span named `identity.<UseCase>` from the request context,
  record an error on failure and set its status. Attributes never include an email address, a username or a token.
- **Probes (D8):** the registry runs checks under `telemetry.Unsampled(ctx)`, so the Postgres and Redis pings are not recorded.

## 8. Removed

The Fiber `/metrics` route and `MonitoringMiddleware`, and `"/metrics"` in the auth middleware's public paths; `app/pkg/infra/monitoring/metrics`
(the metrics server, the Bids-era Prometheus recorder, counters and gauges); `app/pkg/infra/server/interceptors/metrics_interceptor.go` and its
line in `grpc_server.go`; `METRICS_ADDRESS` and `METRICS_ENABLED` in `.env.example` (`METRICS_READER_PASSWORD` belongs to the ELK stack and
stays). `go mod tidy` then drops `github.com/prometheus/client_golang` if nothing else needs it.

## 9. Configuration, compose and documentation

- **Compose:** `deploy/app/compose.yml` adds `OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4317` and `OTEL_SERVICE_NAME=fupi` to the
  explicit environment list. A collector that is not running only produces the rate-limited error line. `.env.example` gains an `OTEL_*`
  block (`OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4317`, `OTEL_SERVICE_NAME`, `OTEL_TRACES_SAMPLER`).
- **Dashboard:** a Dependencies row on the Fupi service dashboard with the Postgres pool and Redis metrics, using the metric names measured
  from the running stack (recorded in the implementation notes).
- **Docs:** `docs/LocalInfrastructure.md` gains "Observing the app" (which stacks to start, where to look in Grafana, Tempo and Kibana, how
  to follow one request, the host-run log limitation); `docs/Deployment.md` gains the `OTEL_*` and `LOG_*` variables; ADR-0017 records D1, D3,
  D4 and D6.

## 10. Verification plan

Written test-first where there is code.

- **Unit tests** with the SDK's in-memory span recorder and a manual metric reader: the middleware (span name, attributes, route template,
  status and error marking, probe exclusion, incoming `traceparent` extraction, the metric's name, unit and attributes); the log handler
  (IDs present with a span, absent without, JSON keys); `Setup` (disabled mode, resource attributes, shutdown flush, no failure without a
  collector); the error handler's rate limit; `Unsampled` (a span started under it is not recorded); the identity spans (name, error status, no PII);
  `GetTraceID` preferring the span; the config loader's `Logging` section.
- **Integration tests** (`-tags integration`, testcontainers): a pgx query inside a parent span produces a child span with the SQL text and
  no arguments; a Redis command produces a span without its key or value.
- **Live drill**, single mode, with `make infra.observability.up`, `make infra.elk.up MODE=single` and `make infra.app.up MODE=single`:
  a registration and a login (a) show up in Tempo's search by `service.name=fupi` with the HTTP, use-case and query spans; (b) appear in
  Prometheus as `http_server_request_duration_seconds_count{service_name="fupi",...}`; (c) have a log line in Elasticsearch with the same
  `trace.id`; (d) make the dashboard panels non-empty and the trace-to-logs link find the line. Probes are absent from all three. Stopping
  the collector leaves response times unchanged and the log bounded; restarting it resumes export without restarting the API; SIGTERM
  flushes the last spans; `OTEL_SDK_DISABLED=true` produces nothing.
- `go test ./...`, `bash scripts/infra_test.sh`, `make infra.config`, `make lint.workflows` and `golangci-lint` on the new and changed files
  stay clean.

## 11. Risks and open items

- **New Go modules.** The OpenTelemetry SDK and OTLP exporters, `otelpgx` and `redisotel` are downloaded with `go get`, each with the user's
  go-ahead first, as with the gRPC bump in slice 2. Versions are resolved when implementing (they must agree with pgx v5, go-redis v9.19 and
  the OpenTelemetry API already in `go.mod`), and `go.mod` and `go.sum` change.
- **Metric names from the libraries.** `otelpgx` and `redisotel` name their metrics themselves; the Dependencies row uses what the running
  stack shows, not what the libraries' documentation says.
- **Memory.** The live drill runs the observability stack, ELK in single mode, the core stacks and the app together (about 4.2 GiB measured
  for `full-single` before the app).
- **Host-run API logs** do not reach Elasticsearch (Filebeat reads container logs only).
- **`Unsampled` relies on a parent-based sampler.** That is the default (`parentbased_always_on`). With a plain `always_on` sampler, set through
  `OTEL_TRACES_SAMPLER`, the readiness pings would be recorded as root traces; the docs say to keep a parent-based sampler.
- **Sampling** defaults to always-on, parent-based, which is right for the local stack and too much for production; production sets
  `OTEL_TRACES_SAMPLER` (documented, not defaulted).
- **Chained `workflow_run` CI** (slice 2's open item) is unrelated to this slice and untouched.
