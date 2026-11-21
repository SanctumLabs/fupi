# Outbox relay and the Kafka client — design

Status: draft for review · Date: 2026-10-05 · Slice 5 of 5

## 1. Intent

The API already writes domain events to `outbox_events` in the same transaction as the aggregate (ADR-0011), and the local stack already
runs Kafka with the topics `identity.events`, `url.events`, `url.access`, `url.invalidations`, `security.scan.requests` and
`webhook.deliveries`. Nothing connects the two: there is no Kafka client in `go.mod`, `ports/event_bus.go` and `pkg/infra/queue/kafka` are
empty, there is no `cmd/worker`, and every outbox row stays unsent forever. By ADR-0015 the API never talks to Kafka, so the relay is a
separate worker process, as the v2 spec describes.

This slice builds that worker: it drains `outbox_events` into Kafka with at-least-once delivery, keeps one aggregate's events in order,
survives Kafka outages and restarts, parks events Kafka permanently rejects, and continues the trace that wrote the event (writing the
trace context into `outbox_events.headers` was deferred from slice 4, its D5). It also builds the small Kafka producer client the relay uses.
Consumers are not part of it: none exists yet (Analytics and cache invalidation come later) and one would be speculative.

**What exists today (verified in the code)**

- `outbox_events`: `id` (the domain event's UUIDv7), `partition_key` (the aggregate ID, nullable), `destination` (the logical stream, which is
  also the topic name), `event_type`, `headers` and `payload` (JSON), `error_message`, `metadata`, `sent_time`, `processing_at`, timestamps and
  `deleted_at`. Only an index on `sent_time` exists. `postgresrepo.WriteOutboxEvents` writes the headers `event_id`, `event_type`,
  `aggregate_id` and `occurred_at`.
- The generic sqlc outbox queries cannot serve a relay: their `ORDER BY` sorts by the string `'ASC'` rather than by time, and the unsent
  queries take date-filter parameters nobody needs. The relay gets purpose-built queries and leaves the old ones alone.
- Kafka locally: KRaft, `kafka-1:9092` inside the `fupi` network and `localhost:19092` (HA also `29092`, `39092`) from the host, no
  authentication, auto-creation of topics off, replication factor 3 with `min.insync.replicas=2` in HA and 1 in single mode.
- The image is distroless static built with `CGO_ENABLED=0`, so a cgo client (librdkafka) is out.
- Slice 4 supplies `telemetry.Setup`, the slog JSON handler, `telemetry.Unsampled`, `health.Registry` and the strict config loader.

**Success criteria**

1. Registering a user on the running stack produces exactly one record on `identity.events` within about a second: key = the user ID,
   value = the event payload JSON, headers `event_id`, `event_type`, `aggregate_id`, `occurred_at` and `traceparent`; the outbox row has a
   `sent_time`.
2. With Kafka stopped the API still registers users and rows accumulate; when Kafka returns they are published in outbox order and none is lost.
3. Killing the worker mid-batch and restarting it delivers every event at least once; any duplicate carries the same `event_id`.
4. With two workers running exactly one publishes; killing it hands over to the other within the standby interval.
5. A record Kafka permanently rejects is parked after `OUTBOX_MAX_ATTEMPTS` attempts, visible with its error, and the rest keep flowing.
6. Rows sent more than `OUTBOX_RETENTION_DAYS` ago are deleted; unsent, parked and recent rows never are.
7. One trace ID runs from the HTTP request through an `outbox.publish` span to the record's `traceparent` header.
8. The worker has a working container health check and a compose stack, the image contains the binary, the docs describe all of it, and
   `go test ./...` and the repository checks stay green.

## 2. Scope

Slice table (slices 1 to 4 are on `feat/local-infra-stack`, whose PR is SanctumLabs/fupi#342):

| # | Slice | State |
|---|-------|-------|
| 1 | Local infrastructure stack | on `feat/local-infra-stack` |
| 2 | Production image, `app` stack, CI | on `feat/local-infra-stack` |
| 3 | App connectivity | on `feat/local-infra-stack` |
| 4 | OpenTelemetry | on `feat/local-infra-stack` |
| 5 | **This spec** — outbox relay and the Kafka client | |

**In scope:** `app/cmd/worker`; the relay use case and its two ports; the Postgres outbox adapter, migration `000003` and new sqlc queries;
the Kafka adapter and the franz-go producer wrapper in `pkg/infra/queue/kafka`; `traceparent` in the outbox headers; the worker's config,
telemetry, health endpoint and graceful shutdown; the `worker` compose stack, `scripts/infra.sh`, make targets and `docs/LocalInfrastructure.md`;
the image gaining `/app/worker`; relay metrics, two alert rules and a small worker dashboard; one ADR.

**Out of scope:** a Kafka consumer (added with the first real consumer), a schema registry or any payload format other than the JSON the
writer already stores, a dead-letter topic (parked rows stay in the table), exactly-once delivery, several concurrent relays, `LISTEN/NOTIFY`,
change-data-capture, Kafka authentication or TLS (the local stack has neither; production settings are documented, not built), and the
URL context's own events (it adopts the writer helper when its application layer lands).

## 3. Decisions

| # | Decision | Why |
|---|----------|-----|
| D1 | The Kafka client is franz-go (`github.com/twmb/franz-go`). Confirmed by the user. | Pure Go (works with `CGO_ENABLED=0` and the distroless image), idempotent producer, per-record results, a consumer in the same module for tests. |
| D2 | One relay is active at a time, elected with a Postgres advisory lock; extra workers stand by. Confirmed by the user. | Events are published in outbox order, so one aggregate's events stay in order (ADR-0011); outbox volumes (identity and URL creation, not redirects) do not need parallel relays. |
| D3 | A record Kafka permanently rejects is parked after a few attempts and the relay carries on; transient failures never park anything. Confirmed by the user. | One bad row must not stop every event, and a Kafka outage must not park the whole backlog. |
| D4 | The leader purges rows sent more than `OUTBOX_RETENTION_DAYS` ago (default 7). Confirmed by the user. | `outbox_events` would otherwise grow without bound. |
| D5 | The worker serves `/health` and `/health/ready` over `net/http`, and a separate `worker` compose stack runs it. Confirmed by the user. | The distroless image has no shell for a health check; a separate stack keeps `make infra.app.up` API-only, as ADR-0015 wants. |
| D6 | No consumer is built in this slice. Confirmed by the user. | None exists yet; it would be speculative. |
| D7 | Delivery is at-least-once; consumers de-duplicate on the `event_id` header (the outbox row ID). | The relay can crash between a publish and its `sent_time` update; the alternative needs Kafka transactions across two systems. |
| D8 | The relay polls Postgres (default every 100 ms), with no `LISTEN/NOTIFY`. | The v2 spec's design, simple, cheap on a partial index; 100 ms is far below any consumer's need. |
| D9 | The writer stores `traceparent` and `tracestate` in `headers`; the relay starts an `outbox.publish` span per record as a child of that context and sends the span's `traceparent` to Kafka. | A trace then continues through the outbox (slice 4, D5). Baggage is not stored: it can carry user data. |
| D10 | Layering: the franz-go wrapper is infrastructure (`pkg/infra/queue/kafka`), the port is `ports.EventPublisher` (`ports/event_bus.go`), its implementation is `internal/adapters/kafka`; the relay is `internal/application/outbox` behind `ports.OutboxDatastore`. | The existing convention: infra holds connection and client code, adapters implement ports, ports are named `…Datastore` (ADR-0013). |
| D11 | The worker's default service name is `fupi-worker`. | The dashboards and alerts that filter `service_name="fupi"` are about the API; the worker must not mix into them. |
| D12 | The relay's housekeeping queries (claim, backlog, purge, lock checks) run under `telemetry.Unsampled`. | Otherwise a poll every 100 ms would start ten traces a second. |
| D13 | The worker never migrates (ADR-0014): migration `000003` runs through the migrator and the compose `migrate` job like the others. | One migration entry point. |
| D14 | Every commit made while implementing this spec ends with the `Co-Authored-By` trailer given in the session's attribution instructions; example `git commit` snippets in plans omit the trailer text. | The project's attribution convention. |

## 4. Data and queries

Migration `000003_outbox_relay` (golang-migrate, up and down), also reflected in `sql/schema.sql` for sqlc:

```sql
ALTER TABLE outbox_events
  ADD COLUMN attempts  INTEGER NOT NULL DEFAULT 0,
  ADD COLUMN parked_at TIMESTAMP WITH TIME ZONE NULL;
COMMENT ON COLUMN outbox_events.attempts  IS 'Times the broker permanently rejected this event; transient failures are not counted';
COMMENT ON COLUMN outbox_events.parked_at IS 'When the relay gave up on this event after the maximum number of attempts; null if not parked';

CREATE INDEX ix_outbox_events_unsent_idx ON outbox_events (destination, created_at, id)
  WHERE sent_time IS NULL AND parked_at IS NULL AND deleted_at IS NULL;
CREATE INDEX ix_outbox_events_sent_time_purge_idx ON outbox_events (sent_time) WHERE sent_time IS NOT NULL;
```

New sqlc queries (`queries/outbox/outbox_relay_queries.sql`, hand-written, not the generic filter style):

- **Claim:** up to `batch` unsent, unparked, undeleted rows **per destination** ordered by `(created_at, id)`. Per destination so one
  destination whose topic is missing cannot starve the others. Ordering by `id` (UUIDv7, generated in sequence) keeps one transaction's events
  in order. Amended after the final review: the query walks the distinct destinations with a recursive skip scan over the partial index and reads
  each destination's first `batch` rows with a lateral `ORDER BY created_at, id LIMIT`, so its cost depends on the batch and the number of
  destinations and not on the backlog; the first draft used `row_number() OVER (PARTITION BY destination ...)`, which read every waiting row to
  hand out one batch (30000 rows read for 30 returned, measured), making a drain after a long outage quadratic.
- **Mark sent:** `UPDATE … SET sent_time = now() WHERE id = ANY($1)`.
- **Record rejection:** `attempts = attempts + 1, error_message = $2`.
- **Park:** `parked_at = now(), error_message = $2`.
- **Purge:** delete up to `limit` rows whose `sent_time` is older than a cutoff.
- **Backlog:** the unsent count, the oldest unsent `created_at` and the parked count, from the same partial index.

There is no `FOR UPDATE`: the advisory lock already makes one relay the only reader. Re-queueing a parked event by hand is
`UPDATE outbox_events SET parked_at = NULL, attempts = 0 WHERE id = '…'` (documented). The old generic queries stay for the existing
integration test.

`WriteOutboxEvents` (`outbox.go`) adds `traceparent` (and `tracestate` when set) to the stored headers when the context carries a valid span
context, using `propagation.TraceContext{}` directly (not the global propagator, so baggage is never stored). Rows written before this slice, or
outside a request, simply have no `traceparent`.

## 5. The relay (`internal/application/outbox`)

```go
// ports.OutboxDatastore (adapter: internal/adapters/postgres/outbox)
type OutboxDatastore interface {
    Acquire(ctx context.Context) (Lease, error)                // advisory lock on its own connection; ErrNotLeader when held elsewhere
    Claim(ctx context.Context, perDestination int) ([]OutboxEvent, error)
    MarkSent(ctx context.Context, ids []string) error
    RecordRejection(ctx context.Context, id, reason string) (attempts int, err error)
    Park(ctx context.Context, id, reason string) error
    Purge(ctx context.Context, olderThan time.Time, limit int) (int, error)
    Backlog(ctx context.Context) (Backlog, error)
}
type Lease interface { Alive(ctx context.Context) error; Release() }   // Alive pings the lock connection

// ports.EventPublisher (adapter: internal/adapters/kafka)
type EventPublisher interface {
    Publish(ctx context.Context, messages []Message) []PublishResult // one result per message, in order
    Ping(ctx context.Context) error
    Close()
}
type PublishResult struct { Err error; Permanent bool }
```

**Leadership.** `Acquire` opens a dedicated connection (`pgx.Connect`, not the pool) and runs `pg_try_advisory_lock(<constant key>)` on it. The
session lock disappears when the connection dies, so a crashed or partitioned leader loses it and, after a Patroni failover, the new primary
has none. A standby calls `Acquire` every `OUTBOX_STANDBY_INTERVAL` seconds. The leader pings the lock connection (`Alive`) at the start of each
cycle and stops publishing the moment it fails, then returns to standby. A lock-holding connection that is idle can be cut by a proxy
(HAProxy has timeouts), which is why it is used every cycle.

**One cycle.**

1. `Alive`; on failure leave leadership.
2. `Claim` (under `Unsampled`). If nothing is claimed: wait `OUTBOX_POLL_INTERVAL_MS`; every `defaultBacklogInterval` (5 s) refresh the
   backlog gauges; every 10 minutes purge (looping while a purge removes a full batch).
3. For each claimed event start an `outbox.publish` span (parent: the stored `traceparent`, else a new root), build the message (section 6), and
   call `Publish` once for the whole claim.
4. For each result, in order: an acknowledged record is collected for `MarkSent`; a transient error leaves the row unsent; a permanent error
   calls `RecordRejection` and, when `attempts` reaches `OUTBOX_MAX_ATTEMPTS`, `Park`.
5. `MarkSent` for the acknowledged ids (one statement, under `Unsampled`).
6. A cycle that acknowledged nothing sleeps with exponential backoff (200 ms doubling to 10 s); any progress resets it, and a cycle whose
   claim was full runs again at once.

**Guarantees.**

- *At-least-once:* a row gets `sent_time` only after Kafka acknowledged it (`acks=all`, idempotent producer). A crash between the publish and
  the `MarkSent` re-publishes that batch; consumers drop repeats on `event_id`.
- *Per-key order (in outbox order, see the risk on commit order below):* all events of a key are claimed in outbox order and produced in that order to one partition, which an idempotent
  producer delivers in order including across retries; a failure for a record fails the records after it in that partition and they are retried
  on the next cycle in order. Verified in the source of franz-go v1.22.1 (while planning): records are produced in order per partition, and
  `RecordDeliveryTimeout` and `RecordRetries` fail every record buffered for the partition after the one that fails ("gapless ordering"), so the
  per-key serial fallback an earlier draft held in reserve is not needed. The broker-stop integration test pins it.
- *Parking skips an event:* later events of the same aggregate continue after a parked one, so a parked event leaves a gap in that aggregate's
  stream. That is the price of D3, and it is why parking raises an alert.
- *Transient failures never count:* timeouts, connection errors and every retriable broker error leave the row untouched and only slow the relay
  down. A missing topic (auto-creation is off) is retriable in the Kafka protocol, so it shows up as a growing backlog and the backlog alert, not
  as parked rows.
- *Standby and split brain:* two workers publishing for a moment (the old leader has not noticed it lost the lock) causes duplicates, never loss.

## 6. The Kafka message and the producer (`pkg/infra/queue/kafka`, `internal/adapters/kafka`)

A claimed row becomes one record: topic = `destination`; key = `partition_key` (nil when null); value = `payload` bytes untouched; headers = every
entry of the row's `headers` JSON as a string header (`event_id`, `event_type`, `aggregate_id`, `occurred_at`) plus `content-type:
application/json`, plus `traceparent` set to the `outbox.publish` span's context (not the stored one), so a consumer's span is a child of the publish
span. The record's own timestamp is the producer's; `occurred_at` is the business time.

The producer wrapper builds one `kgo.Client` from `KAFKA_BROKERS` and `KAFKA_CLIENT_ID` with the idempotent producer, `acks=all`, a record
delivery timeout of `KAFKA_PUBLISH_TIMEOUT` seconds and a buffer at least as large as a batch. It exposes `Produce(ctx, []Record) []error` (a
synchronous batch with one error per record, `nil` meaning acknowledged), `Ping(ctx)` (a metadata request) and `Close`. franz-go connects lazily,
so constructing the producer never fails because Kafka is down.

Every `Produce` call is bounded by the publish timeout, and the client is configured with `AllowIdempotentProduceCancellation`. Found by the
broker-stop test: by default franz-go refuses to fail a record whose request is already on its way (with idempotent writes it cannot know
whether the broker stored it), so a broker that dies mid-request keeps `Produce` waiting until it returns, deadline or not. That would wedge
the relay loop and stall its shutdown. The price is a possible duplicate, which at-least-once delivery already allows (D7).

The producer wrapper classifies each error (`kafka.IsPermanent`, called by the adapter). **Permanent** is an explicit allowlist of the broker
answers that never change for the same record: `MESSAGE_TOO_LARGE`, `RECORD_LIST_TOO_LARGE`, `INVALID_TOPIC_EXCEPTION`, `INVALID_RECORD` and
`UNSUPPORTED_FOR_MESSAGE_FORMAT`; not every non-retriable code, so an unknown server error can never park an event. **Everything else is
transient**, including a delivery timeout and an unknown topic. The two authorization errors (`TOPIC_AUTHORIZATION_FAILED`,
`CLUSTER_AUTHORIZATION_FAILED`) are transient on purpose (amended after the final review): they say the client's configuration is wrong, not the
record, and listing them as permanent would park the whole backlog within seconds of an ACL mistake, against D3's own rationale; as transient
errors the events wait and flow once it is fixed. The classification has a table test.

Spans: `outbox.publish` is a producer span named `<destination> publish` with `messaging.system=kafka`, `messaging.destination.name`,
`messaging.operation.type=publish`, `messaging.message.id` (the event ID) and `outbox.event_type`; a failed record marks the span as an error with
the error class, not the message text. No payload, key or header value is recorded.

## 7. The worker (`app/cmd/worker`)

- **Config** (`config.LoadWorker`, strict, same loader and conventions as `config.Load`; it needs no `AUTH_SECRET` or Redis): the existing
  database, logging, `ENVIRONMENT`, `SERVER_HOST` and `SHUTDOWN_TIMEOUT` settings plus the table below. The development database password is
  refused outside development and test, as for the API.

| Variable | Default | Meaning |
|---|---|---|
| `KAFKA_BROKERS` | `localhost:19092` | comma-separated `host:port` list (HA from the host: `localhost:19092,localhost:29092,localhost:39092`) |
| `KAFKA_CLIENT_ID` | `fupi-worker` | |
| `KAFKA_PUBLISH_TIMEOUT` | `10` | seconds a record may take to be acknowledged before it counts as a transient failure |
| `OUTBOX_POLL_INTERVAL_MS` | `100` | milliseconds between cycles when nothing was claimed (at least 10) |
| `OUTBOX_BATCH_SIZE` | `100` | rows claimed per destination per cycle (1 to 1000) |
| `OUTBOX_MAX_ATTEMPTS` | `3` | permanent rejections before an event is parked (at least 1) |
| `OUTBOX_STANDBY_INTERVAL` | `5` | seconds between a standby's attempts to become leader |
| `OUTBOX_RETENTION_DAYS` | `7` | days sent rows are kept; `0` turns the purge off |
| `WORKER_HTTP_PORT` | `8086` | health listener |

- **Startup:** logger (default service name `fupi-worker` via a new `telemetry.Options.ServiceName` default and `telemetry.ServiceName(default)`),
  `telemetry.Setup` (flushed right after the relay stops and before the clients close, as in `run` for the API), Postgres client, the Kafka
  producer, the health server, then the relay loop. Kafka being down is not a startup error.
- **Health:** a small `net/http` handler (`probes` gains `NewHTTPHandler(registry)` on the same `LivePath`/`ReadyPath` and report shape).
  Liveness is 200 while the relay loop has completed a cycle (or a standby attempt) within the last 30 seconds, so a wedged loop is restarted.
  Readiness checks Postgres and Kafka (`Ping`), neither of which is a reason to restart. The image gains a `worker healthcheck` subcommand like the
  API's, used by the compose health check.
- **Shutdown:** on SIGTERM the relay finishes the batch in flight (bounded by the publish timeout and `SHUTDOWN_TIMEOUT`), releases the lock, the
  producer closes, the health server stops, telemetry flushes, and the process exits 0. A second signal ends it at once.

## 8. Observability

Metrics (meter `…/application/outbox`, exported by the worker's own `Setup`): `outbox.relay.published` (counter, attribute `destination`),
`outbox.relay.failures` (counter, `kind` = `transient` or `permanent`), `outbox.relay.parked` (counter), `outbox.relay.publish.duration`
(histogram, seconds, per batch), `outbox.relay.backlog` (gauge: unsent rows), `outbox.relay.oldest_unsent_age` (gauge, seconds),
`outbox.relay.parked_rows` (gauge) and `outbox.relay.leader` (gauge, 0 or 1). The backlog gauges come from the leader's 5 second `Backlog` query; a
standby reports only `leader = 0`.

Three alert rules in `deploy/observability/prometheus/rules/stack.yml`, all silent when no worker runs (the series are absent, matching how slice 1
avoids false "down" alerts): `OutboxBacklogOld` (`max(outbox_relay_oldest_unsent_age_seconds{service_name="fupi-worker"}) > 300` for 2 minutes),
`OutboxEventsParked` (`max(outbox_relay_parked_rows{service_name="fupi-worker"}) > 0` for 5 minutes) and, added after the final review,
`OutboxNoActiveRelay` (`sum(outbox_relay_leader{service_name="fupi-worker"}) == 0` for 2 minutes: workers are up but none holds the lease, so no
backlog gauge is reported and the other two cannot see the stall). A dashboard `fupi-worker.json` (folder Fupi)
shows the rates, the backlog and its oldest age, parked rows and the leader. The Prometheus names are confirmed from the running stack, as in slice 4.

## 9. Compose, image and documentation

- **Image:** the `Dockerfile` also builds `/app/worker` (one image: `fupi`, `migrator`, `worker`); `scripts/image_test.sh` pins it.
- **Stack:** `deploy/worker/compose.yml` with `worker-ha` and `worker-single` (the same hardening as the app services: read-only, no capabilities,
  `no-new-privileges`, explicit environment list, `stop_grace_period` 25 s), included from `docker-compose.yml`; `scripts/infra.sh` learns the stack
  `worker` (it brings up Postgres and Kafka for the mode first, then the worker, like `app` does); `make infra.worker.up|down MODE=…`;
  `scripts/infra_test.sh` covers the new commands; `infra.config` renders the new profiles. `KAFKA_BROKERS` is `kafka-1:9092` in single mode
  and the three brokers in HA.
- **Docs:** `docs/LocalInfrastructure.md` (a "Relaying events to Kafka" section: starting the stack, reading a topic, re-queueing a parked event,
  the failure drills), `docs/Deployment.md` (the worker command, its variables and the migration order), `.env.example`, ADR-0018 (D1 to D5 and
  D7 to D10: the franz-go choice, one leader-elected polling relay, at-least-once, parking, the layering), and a one-line update to ADR-0011's
  consequence "No relay exists yet".

## 10. Verification plan

Written test-first where there is code.

- **Unit tests** with fake ports and a fake clock: the relay cycle (marks only acknowledged ids, transient failure leaves rows and backs off, a
  permanent rejection counts then parks at the limit, progress resets the backoff, a full claim loops at once, leadership lost mid-cycle stops
  publishing, standby retries, purge cadence and the retention-zero switch, shutdown finishes the batch); message building (topic, key, headers,
  nil key, `traceparent` replaced by the publish span's); the error classification table; the config loader (defaults, bounds, `KAFKA_BROKERS`
  validation, the password guard); `WriteOutboxEvents` storing `traceparent` and never baggage; health liveness staleness.
- **Integration tests** (`-tags integration`, testcontainers): the sqlc queries against Postgres (claim order and per-destination limit, marking,
  parking, purge only touching old sent rows, the partial index being used); advisory-lock exclusivity between two connections and release when the
  connection closes; the producer against a Kafka container (record shape and headers, a closed broker mid-batch producing transient errors, an
  oversized record being permanent); and Postgres plus Kafka end to end (a written event arrives once with the right key and headers, and
  `sent_time` is set). The per-key ordering property is tested by stopping the broker during a batch with several records for one key and
  consuming the topic afterwards.
- **Live drill**, single mode, with `make infra.worker.up MODE=single`, the observability and ELK stacks and the API: (a) a registration
  appears on `identity.events` (read with the Kafka CLI in the broker container) with the headers and `traceparent`, the Tempo trace contains
  the `outbox.publish` span, and the row has `sent_time`; (b) stop Kafka, register several users, restart Kafka: the records arrive in order
  and the backlog gauge returns to zero, the alert expression evaluates; (c) kill the worker mid-batch and restart it: nothing is lost and a
  duplicate, if any, repeats its `event_id`; (d) run two workers: one publishes, kill it, the other takes over; (e) an oversized event is parked
  after three attempts while a normal one still flows, `OutboxEventsParked` evaluates, and re-queueing it works; (f) the purge deletes only old
  sent rows (retention shortened for the drill); (g) SIGTERM exits 0 and flushes telemetry. A short HA check (Patroni behind HAProxy plus three
  brokers) confirms the lock works through the proxy and the producer sees all brokers, if the machine's memory allows.
- `go test ./...`, `bash scripts/infra_test.sh`, `make infra.config`, `make lint.workflows`, `make lint.docker` and `golangci-lint` on the new and
  changed files stay clean.

## 11. Risks and open items

- **New Go modules.** `github.com/twmb/franz-go` v1.22.1 with `pkg/kmsg` v1.14.0 and `github.com/pierrec/lz4/v4`, which also raises the existing
  indirect requirement `github.com/klauspost/compress` from v1.18.6 to v1.20.0. All four are already in the local module cache, so adding them
  needs no download (`GOPROXY=off` proves it); the version bump of `klauspost/compress` (a compression library of the production binary) is
  still put to the user with the go-ahead. `go mod tidy` and the Docker image build (the builder downloads the Go modules) need the go-ahead too.
  The testcontainers Kafka module is not added: the test broker is a generic container running the local `apache/kafka:4.3.1` image, configured
  like the stack's `kafka-single`.
- **Head-of-line within a destination.** If one partition's leader is unavailable and more than `OUTBOX_BATCH_SIZE` of the oldest unsent rows of
  that destination belong to it, other keys on that destination wait until it recovers. Acceptable (it recovers with the partition) and visible on
  the backlog alert.
- **Outbox order is `(created_at, id)`, not commit order.** `created_at` is the transaction's start time and the relay only sees committed rows,
  so a transaction that starts earlier but commits later can be published after a later-started one. For one aggregate this needs two
  concurrent writers, which the aggregate row lock normally serializes (the writer does not enforce it); across aggregates no order is promised.
  Every event carries `occurred_at`, and a consumer that needs strictness orders by it. Delaying publication by a short visibility lag would
  narrow the window and is not done: it adds latency to every event for a rare case.
- **Duplicates on failover.** At-least-once means consumers must be idempotent on `event_id`; the future consumers will be built that way (the v2
  spec already says so).
- **Parked events leave a gap.** An aggregate's later events are delivered after a parked one; the gap is deliberate (D3) and alerted.
- **Memory.** The worker is a small Go process; the drill adds Kafka single (heap 512 MB) to the observability, ELK and core stacks already
  measured at about 4.2 GiB.
- **Service name default.** `telemetry.ServiceName` gains a parameter; the API's call passes `"fupi"`, so its behaviour is unchanged.
- **Kafka security.** The local stack has no TLS or SASL. The producer reads only `KAFKA_BROKERS`; adding SASL/TLS settings is a later,
  production-hardening change.
