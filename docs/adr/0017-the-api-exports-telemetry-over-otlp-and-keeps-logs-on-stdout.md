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

- The API needs a reachable collector only to export; without one it serves normally and logs a `telemetry export failed` line for each distinct error and signal at most once a minute.
- A parent-based sampler (the default) is required for the readiness checks to stay out of Tempo: they run under an unsampled parent span. Production should keep a parent-based sampler and lower the rate (`OTEL_TRACES_SAMPLER=parentbased_traceidratio`).
- The readiness checks' Redis `PING` and Postgres ping still count in the Redis and Postgres client metrics; only their spans are suppressed.
- A failed query or command records the driver's error message on its span, and PostgreSQL's own messages can echo a value it rejected; SQL arguments and Redis keys and values are otherwise never recorded.
- Logs from an API run on the host reach the terminal only; Filebeat reads container logs.
- Writing the trace context into `outbox_events.headers` and Kafka instrumentation belong to the outbox relay slice (ADR-0018).
