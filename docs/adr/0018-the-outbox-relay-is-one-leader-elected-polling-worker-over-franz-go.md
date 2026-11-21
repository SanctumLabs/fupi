---
status: accepted
---

# The outbox relay is one leader-elected polling worker over franz-go, delivering at least once and parking events Kafka rejects for good

Domain events reach Kafka through a separate process, `worker` (`app/cmd/worker`, `/app/worker` in the image). The API never talks to Kafka (ADR-0015); it only writes `outbox_events` in the aggregate's transaction (ADR-0011).

**One active relay.** Workers compete for a Postgres advisory lock (`pg_try_advisory_lock`) held on a dedicated connection; the holder relays, the others stand by and retry every `OUTBOX_STANDBY_INTERVAL` seconds, and the lock is freed when the holder's connection goes away, so a crashed leader is replaced without any coordination service. One relay publishing in `(created_at, id)` order is what keeps one aggregate's events in order, and the outbox's volume (registrations and URL creations, not redirects) does not need parallel relays.

**Polling.** Every `OUTBOX_POLL_INTERVAL_MS` (default 100) the relay claims up to `OUTBOX_BATCH_SIZE` unsent, unparked rows per destination, publishes them in one call and marks the acknowledged ones sent. Per destination, so a destination whose topic is missing cannot starve the others. There is no `LISTEN/NOTIFY` and no `FOR UPDATE`: the lock already makes the relay the only reader, and the partial index on unsent rows makes the poll cheap.

**At least once.** The relay can stop between a publish and the update of `sent_time`, and a producer that gives up on a record in flight cannot know whether the broker stored it, so an event can be delivered twice. Consumers de-duplicate on the `event_id` header (the outbox row's UUIDv7). Exactly-once would need Kafka transactions spanning two systems.

**Failures are two different things.** A transient failure (Kafka down, a timeout, an unknown topic, anything not on the allowlist) never counts against an event and never parks one: the rows stay, the relay backs off from 200 ms to 10 s and the backlog alert says so. A permanent rejection, an explicit allowlist of broker answers that never change for the same record (`MESSAGE_TOO_LARGE`, `RECORD_LIST_TOO_LARGE`, `INVALID_TOPIC_EXCEPTION`, `INVALID_RECORD`, `UNSUPPORTED_FOR_MESSAGE_FORMAT`), increments `attempts`; at `OUTBOX_MAX_ATTEMPTS` (default 3) the row is parked (`parked_at`, `error_message`) and the relay carries on. The authorization errors are deliberately not on the list: they describe the client's configuration, not the record, so an ACL mistake makes events wait (and the backlog alert fire) instead of parking the whole backlog. A parked row stays in the table until someone fixes the cause and clears `parked_at` and `attempts`; there is no dead-letter topic.

**The client is franz-go.** It is pure Go, so it builds with `CGO_ENABLED=0` into the distroless image (a librdkafka client would need cgo), it has an idempotent producer with per-record results, and it keeps a partition's records in order when one fails. Every `Produce` call is bounded by `KAFKA_PUBLISH_TIMEOUT` and the client allows cancelling in-flight records (`AllowIdempotentProduceCancellation`): by default franz-go will not fail a record whose request is already on its way, so a broker that dies mid-request would keep the relay, and its shutdown, waiting until the broker returned. The price is a possible duplicate, which at-least-once delivery already allows.

**Layering.** The franz-go wrapper is infrastructure (`pkg/infra/queue/kafka`), the port is `ports.EventPublisher` and its adapter `internal/adapters/kafka`; the relay is a use case (`internal/application/outbox`) behind `ports.OutboxDatastore`, whose adapter is `internal/adapters/postgres/outbox`.

**Traces continue through the outbox.** The writer stores `traceparent` and `tracestate` (never baggage, which can carry user data) in `outbox_events.headers`; the relay starts an `outbox.publish` span per record as a child of that context and sends that span's `traceparent` in the Kafka record, so one trace ID runs from the HTTP request to the consumer. The relay's own queries run under `telemetry.Unsampled`, otherwise a poll every 100 ms would start ten traces a second. The worker's service name is `fupi-worker`, so the API's dashboards and alerts stay about the API.

**Sent rows are purged.** The leader deletes rows sent more than `OUTBOX_RETENTION_DAYS` ago (default 7, `0` keeps them), in batches; unsent and parked rows are never deleted.

## Considered options

- **A librdkafka-based client** (`confluent-kafka-go`) — the most widely used, but it needs cgo, which the static distroless image does not have.
- **Several relays claiming rows with `FOR UPDATE SKIP LOCKED`** — more throughput and no failover wait, but events of one aggregate could be published out of order, and nothing here needs the throughput.
- **`LISTEN/NOTIFY` or change-data-capture instead of polling** — lower latency, but a second moving part (a lost notification still needs the poll as a safety net, CDC needs Debezium and Kafka Connect) to save latency nobody needs.
- **A dead-letter topic for rejected events** — it moves the problem to another place nobody watches; a parked row, a gauge and an alert keep it where the fix (and the original payload) is.
- **Delaying publication a little to follow commit order** — `created_at` is the transaction's start time, so a transaction that started earlier and committed later can be published after a later-started one. A visibility lag would narrow that window and cost latency on every event; each event carries `occurred_at` and a consumer that needs strict order sorts by it.

## Consequences

- Consumers must be idempotent on `event_id`.
- Order is per `partition_key` (the aggregate ID) and follows `(created_at, id)`, not commit order. For one aggregate the difference needs two concurrent writers, which the aggregate's row lock normally serializes; across aggregates no order is promised.
- If one partition's leader is unavailable and more than `OUTBOX_BATCH_SIZE` of a destination's oldest unsent rows belong to it, other keys of that destination wait until it recovers; the backlog alert shows it.
- The worker needs Postgres and Kafka; it never migrates (ADR-0014) and never creates topics (the stack's `kafka-init` does).
- A second worker is a standby, not extra capacity.
- There is no consumer yet; the first one (Analytics, cache invalidation) brings its own decisions.
