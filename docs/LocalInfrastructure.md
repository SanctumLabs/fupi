# Local infrastructure

Fupi depends on Postgres, Redis and Kafka, and is observed with ELK (logs), Prometheus (metrics), Tempo (traces) and
Grafana. This repository runs all of them locally with Docker Compose. Each stack runs in one of two modes:

- **HA**: the clustered topology used in production (3 Kafka brokers, a 6-node Redis Cluster, Postgres with Patroni and
  2 replicas, 3 Elasticsearch nodes). Use it to exercise failover.
- **single**: one node per stack. Use it for everyday work; it needs a fraction of the memory.

Nothing starts without a profile: a bare `docker compose up` does nothing. Use the `make infra.*` commands below.

## Prerequisites

- Docker with Compose v2.20 or newer (`docker compose version`) and `make`.
- Docker Desktop memory (Settings, Resources): see the [memory budget](#memory-budget). HA stacks need much more than single mode.
- Linux hosts running ELK: `sudo sysctl -w vm.max_map_count=262144` (Docker Desktop already sets it).
- Verified on Docker Desktop for Mac (Apple silicon). Linux, rootless Docker, SELinux labels and non-default log drivers have not
  been exercised; Filebeat in particular reads `/var/lib/docker/containers` and assumes the default `json-file` log driver.
- Every `make infra.*` target creates `.env` from `.env.example` when it is missing. Raw `docker compose` commands need
  `.env` to exist, so run `make create.envfile` first.

## Quick start

```bash
make infra.core.up MODE=single      # Postgres + Redis + Kafka, one node each (lightest)
make infra.core.up                  # the same, HA (3 Postgres, 6 Redis, 3 Kafka)
make infra.observability.up         # Prometheus, Grafana, Tempo, Alertmanager, OpenTelemetry Collector
make infra.elk.up MODE=single       # Elasticsearch, Logstash, Kibana, Filebeat
make infra.full.up MODE=single      # every stack except legacy
make infra.ps                       # what is running and whether it is healthy
make infra.core.down                # stop (data is kept)
make infra.clean                    # remove every container AND volume except the legacy MongoDB and Redis (asks first)
```

`up` waits until every service is healthy and every one-shot job (topic creation, cluster formation, migrations,
Elasticsearch setup) has finished, or fails after five minutes and prints what is still pending.

## Modes and profiles

| Stack | HA profile | Single profile | Make target |
|---|---|---|---|
| Kafka + Kafka UI | `kafka-ha` | `kafka-single` | `infra.kafka.up` |
| Redis | `redis-ha` | `redis-single` | `infra.redis.up` |
| Postgres + migrations | `postgres-ha` | `postgres-single` | `infra.postgres.up` |
| ELK | `elk-ha` | `elk-single` | `infra.elk.up` |
| Postgres + Redis + Kafka | `core-ha` | `core-single` | `infra.core.up` |
| Everything except legacy | `full-ha` | `full-single` | `infra.full.up` |
| Outbox relay worker (needs Postgres and Kafka) | `worker-ha` | `worker-single` | `infra.worker.up` |
| Prometheus, Grafana, Tempo, Alertmanager, Collector | `observability` | `observability` | `infra.observability.up` |
| MongoDB + standalone Redis (deprecated) | `legacy` | `legacy` | `infra.legacy.up` |

- Stacks are independent: Kafka in HA with Postgres single is fine.
- **Never run both modes of one stack.** They use the same ports and names. `make infra.<stack>.up MODE=...` removes the
  other mode's containers first (volumes are kept, so switching back restores the data). With raw `docker compose`,
  stop one mode before starting the other.
- Observability has no HA mode: Prometheus, Grafana, Tempo and the Collector are single instances in every mode.

## Make commands

| Command | What it does |
|---|---|
| `make infra.<stack>.up [MODE=ha\|single]` / `.down` | start / stop a stack: `kafka redis postgres elk observability legacy core full app worker` |
| `make infra.wait STACK=kafka MODE=single` | wait until a stack is healthy |
| `make infra.ps` / `infra.stats` | status and health / live memory and CPU |
| `make infra.logs SERVICE=kafka-1` | follow logs (omit `SERVICE` for everything) |
| `make infra.config` | check that every profile renders and the env defaults agree |
| `make infra.clean` | remove all containers and volumes except the legacy MongoDB and Redis data |
| `make infra.clean.legacy` | remove the legacy MongoDB and Redis containers and their volumes (their data is lost) |
| `make infra.hosts` | print the `/etc/hosts` line needed for Redis HA from an app on your host |
| `make infra.migrate` | re-run the database migrations |
| `make infra.psql` | `psql` as the application user on the primary |
| `make infra.psql.admin` | `psql` as the superuser on the app database (for `EXPLAIN ANALYZE`, locks) |
| `make infra.pgstat [TOP=10]` / `infra.pgstat.reset` | top statements from `pg_stat_statements` / reset it; see [loadtest/README.md](../loadtest/README.md#diagnosing-a-slow-database) |
| `make infra.redis.cli` | `redis-cli` as the application user, cluster mode |
| `make infra.kafka.topics` | describe the Kafka topics |
| `make infra.patroni.list` | Patroni members and roles (Postgres HA) |
| `make infra.es.health` | Elasticsearch cluster health |

The `MODE` variable also selects which node the helper commands talk to (for example `make infra.psql MODE=single`).

## Connecting the application

The same names work in both modes; the lists just get shorter. Use the "host" column when the app runs on your machine
(for example `make run.dev`) and the "network" column when it runs in a container on the `fupi` network.

| Concern | App on the host | App in the compose network |
|---|---|---|
| Kafka bootstrap | HA `localhost:19092,localhost:29092,localhost:39092`; single `localhost:19092` | HA `kafka-1:9092,kafka-2:9092,kafka-3:9092`; single `kafka-1:9092` |
| Redis seed nodes | HA `localhost:7001`..`localhost:7006`; single `localhost:7001` | HA `redis-1:7001`..`redis-6:7006`; single `redis-1:7001` |
| Postgres (write) | `localhost:5432` | `postgres:5432` |
| Postgres (read) | `localhost:5433` | HA `postgres:5433`; single `postgres:5432` |
| OTLP (traces, metrics) | `http://localhost:4317` (gRPC) or `:4318` (HTTP) | `http://otel-collector:4317` |

Notes:

- **Redis HA announces hostnames** (`redis-1`..`redis-6`) so cluster clients can follow redirects. An app on your host must
  resolve them: run `make infra.hosts` and add the printed line to `/etc/hosts` once. A containerised app needs nothing.
  Single mode is a cluster of one, so it behaves like the HA cluster for multi-key rules (`CROSSSLOT`).
- Redis serves only logical database `0` (cluster mode), so `REDIS_DATABASE` must be `0`.
- Postgres reads on `5433` reach a replica in HA (and the same node in single mode); writes on a read port fail with
  "read-only transaction".
- Logs need no configuration: write JSON to stdout and Filebeat ships it (see ELK).

## Running the app against the stack

The API defaults match the stack, so on your host no `.env` edits are needed.

```bash
make infra.core.up MODE=single      # or HA: also run `make infra.hosts` once, and set the six-address REDIS_ADDRESS from .env.example in your own .env
go run ./app/cmd/migrator           # optional: infra.core.up already migrated through the compose job; a second run says "no change"
make run                            # the API on :8085
curl -s localhost:8085/health/ready
```

`go run ./app/cmd/migrator` is the same migration code the tests use (`postgres.Migrate`). Run it from the repository root, or set `MIGRATIONS_PATH`. `make run.with.migrations` runs it and then the API. The API never migrates at startup (ADR-0014).

| Endpoint | Meaning |
|---|---|
| `GET /health` | liveness: 200 while the process runs |
| `GET /health/ready` | readiness: 200 `ok` (everything up) or `degraded` (Redis down); 503 `unavailable` (Postgres down) or `draining` (shutting down) |

Readiness never includes error text; look in the API's log for the reason (ADR-0015). Postgres is required, Redis is optional, and the API does not connect to Kafka.

On SIGTERM or Ctrl-C the API turns readiness to 503, finishes in-flight requests for up to `SHUTDOWN_TIMEOUT` seconds (default 15), closes Redis and Postgres and exits 0. A second Ctrl-C during the drain ends it at once.

The variables you are likely to change are in `.env.example`; the full list with defaults and units (server, pool sizes, timeouts, `MIGRATIONS_PATH`) is in the spec, `docs/superpowers/specs/2026-10-04-app-connectivity-design.md` section 4. An empty value counts as unset, so `REDIS_USERNAME=` still sends `fupi-svc`; a Redis that only has a password needs `REDIS_USERNAME=default`. If `ENVIRONMENT` is not set and a development secret is in use, the API logs a warning at startup. A value that does not parse stops startup with a message naming the variable, and any `ENVIRONMENT` other than `development` or `test` refuses the development secrets (`AUTH_SECRET`, `DATABASE_PASSWORD`, `REDIS_PASSWORD`).

Debugging:

- `redis is down, continuing without it` at startup: check `REDIS_ADDRESS` (HA needs all six seed nodes and the `/etc/hosts` line from `make infra.hosts`) and `REDIS_USERNAME`/`REDIS_PASSWORD`.
- Postgres connection errors: the write port is `5432` in both modes; `5433` is the HA read port and rejects writes.
- An old `.env` copied from before this change carries Mongo-era values (`DATABASE_PORT=27017`, `REDIS_ADDRESS=localhost`,
  `AUTH_SECRET=<AUTH_SECRET>`, ...). They override the new defaults, so the API or the migrator dials the wrong port or stops
  with an invalid-configuration error. Refresh it with `cp .env.example .env` (re-apply any values of your own first).
- `make infra.psql`, `make infra.redis.cli` and `make infra.patroni.list` show the other side of each connection.

## The app in a container

The API also runs as a container built from the repository `Dockerfile`, on the same `fupi` network as the stacks, the way it will run in production.

```bash
make infra.app.up MODE=single      # or HA; brings up Postgres and Redis for that mode first, then builds and starts the API
curl -s localhost:8085/health/ready
make infra.app.down                # stops only the API; Postgres and Redis keep running
```

- It listens on `127.0.0.1:8085`, so stop an API running on your host first.
- Inside the network it reaches Postgres at `postgres:5432` and Redis at `redis-1:7001` (single) or `redis-1:7001` to `redis-6:7006` (HA), so `make infra.hosts` is not needed. Its environment is an explicit list in `deploy/app/compose.yml` (not your `.env`) and it runs with `ENVIRONMENT=development`.
- It runs with a read-only filesystem, no Linux capabilities and `no-new-privileges`. Its health check is `/app/fupi healthcheck`; `docker compose ps` shows `healthy` once it serves.
- `make infra.app.up` rebuilds the image each time (cached layers make it quick). `make build.docker`, `make lint.docker` and `make scan.docker` build, lint and scan the image on its own.
- The image has no shell. To look inside it use `docker cp fupi-app-single-1:/app/migrations -`, or test connectivity from a distroless debug container on the network: `docker run --rm -it --network fupi --entrypoint sh gcr.io/distroless/static-debian13:debug-nonroot` (pulls that image).

## Relaying events to Kafka

The outbox relay is a second process, `worker`, built into the same image. It reads `outbox_events` from Postgres and publishes each row to the Kafka topic named by its `destination` (`identity.events`), keyed by `partition_key` so one aggregate's events stay in order. The API never talks to Kafka.

```bash
make infra.worker.up MODE=single   # or HA; brings up Postgres and Kafka for that mode first, then builds and starts the worker
curl -s localhost:8086/health/ready
make infra.worker.down             # stops only the worker; Postgres and Kafka keep running
```

- It listens on `127.0.0.1:8086` (`GET /health`, `GET /health/ready`). Its environment is an explicit list in `deploy/worker/compose.yml` (not your `.env`), it runs with `ENVIRONMENT=development`, a read-only filesystem and no capabilities, and its health check is `/app/worker healthcheck`.
- One worker is active at a time: they compete for a Postgres advisory lock and the others stand by, taking over within `OUTBOX_STANDBY_INTERVAL` seconds (default 5) after the active one stops. To try a failover, run a second worker on your host with `OTEL_SERVICE_NAME=fupi-worker WORKER_HTTP_PORT=8087 go run ./app/cmd/worker` (`OTEL_SERVICE_NAME` is there because `.env.example` sets it to the API's name, which would make the worker report as the API; the defaults reach `localhost:19092` and `localhost:5432`; in HA mode also set `KAFKA_BROKERS=localhost:19092,localhost:29092,localhost:39092`).
- Read what it published, headers included (`event_id`, `event_type`, `aggregate_id`, `occurred_at`, and `traceparent` when the request had a trace), or look at the topic in Kafka UI (<http://localhost:8080>):

```bash
docker compose --profile kafka-single exec kafka-single /opt/kafka/bin/kafka-console-consumer.sh \
  --bootstrap-server kafka-1:9092 --topic identity.events --from-beginning --timeout-ms 5000 \
  --property print.key=true --property print.headers=true
```

  In HA mode use `--profile kafka-ha exec kafka-1`.
- Delivery is **at least once**: a worker killed between a publish and recording it can publish the event again, so a consumer de-duplicates on the `event_id` header. Order is per `partition_key`.
- **Kafka down:** nothing is lost and nothing is parked. Events wait in the table, the worker backs off (200 ms up to 10 s) and `OutboxBacklogOld` fires when the oldest has waited more than five minutes; when Kafka returns they are published in order.
- **Parked events:** a record Kafka permanently refuses (for example one larger than the broker accepts) is retried until `OUTBOX_MAX_ATTEMPTS` (default 3) and then parked: `parked_at` and `error_message` are set, the rest keep flowing, and `OutboxEventsParked` fires after five minutes. Look at them and, once the cause is fixed, put one back:

```bash
make infra.psql MODE=single
```

```sql
SELECT id, destination, event_type, attempts, error_message, parked_at FROM outbox_events WHERE parked_at IS NOT NULL;
UPDATE outbox_events SET parked_at = NULL, attempts = 0, error_message = NULL WHERE id = '<id>';
```

- **Purge:** the active worker deletes rows sent more than `OUTBOX_RETENTION_DAYS` ago (default 7; `0` keeps them) when it starts and every ten minutes. Unsent and parked rows are never deleted.
- **Observing it:** the "Fupi worker" dashboard (folder Fupi) and the traces: each published event is a span `<destination> publish` of the service `fupi-worker`, a child of the request that wrote the event, so a registration's trace ends in the Kafka record's `traceparent`. Metrics are `outbox_relay_*` (published, failures, publish duration, backlog, oldest unsent age, parked rows, leader); only the active relay reports the backlog, oldest age and parked rows, a standby reports just `leader = 0`. Three alerts watch it: `OutboxBacklogOld` (the oldest event has waited more than five minutes), `OutboxEventsParked` (anything is parked) and `OutboxNoActiveRelay` (workers are running but none is the active relay, so nothing is delivered). The collector keeps a stopped container's series for about five minutes, so after a worker restarts "Active relays" can briefly read 2.
- **URL expiry:** the same worker also runs the expiry job: every `EXPIRY_INTERVAL` seconds (default 60) the instance holding its own advisory lock (separate from the relay's, so it can be a different instance) moves ACTIVE URLs past their expiry to EXPIRED, `EXPIRY_BATCH_SIZE` at a time, one transaction per URL, which also writes `url.expired` to the outbox. The update is guarded by the `updated_at` the URL was loaded with: if a user suspended it or moved its expiry in between, the job skips it (a Conflict, not a failure) and a later run sees its new state. Redirects refuse an expired URL whether or not the job has run. Metrics are `url_expiry_*` (expired, failures, backlog, last run, leader); `url_expiry_backlog` is the true count of ACTIVE URLs past their expiry at the end of each run (0 when the job keeps up); `UrlExpiryBacklog` fires when it stays above 0 for 15 minutes and `UrlExpiryStalled` when the leader has not completed a run for 10 minutes.
- Settings (all in `.env.example`): `KAFKA_BROKERS`, `KAFKA_CLIENT_ID`, `KAFKA_PUBLISH_TIMEOUT`, `OUTBOX_POLL_INTERVAL_MS`, `OUTBOX_BATCH_SIZE`, `OUTBOX_MAX_ATTEMPTS`, `OUTBOX_STANDBY_INTERVAL`, `OUTBOX_RETENTION_DAYS`, `EXPIRY_INTERVAL`, `EXPIRY_BATCH_SIZE` and `WORKER_HTTP_PORT`.

## Observing the app

The API sends traces and metrics over OTLP to the collector of the observability stack and writes JSON logs to stdout, which Filebeat ships to Elasticsearch when the API runs as a container. The three are tied together by one W3C trace ID.

```bash
make infra.observability.up       # collector, Tempo, Prometheus, Alertmanager, Grafana
make infra.elk.up MODE=single     # Elasticsearch, Logstash, Kibana, Filebeat (MODE=ha for the cluster)
make infra.app.up MODE=single     # the API; its OTEL_* variables point at the collector
```

`make infra.app.up` does not start the observability or ELK stacks (they are separate stacks, see the memory budget). Without the collector the API still serves; it logs a `telemetry export failed` line for each distinct error and signal at most once a minute.

Follow one request. Send it with a `traceparent` of your own, so you know the trace ID:

```bash
curl -s -H 'traceparent: 00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01' -H 'content-type: application/json' \
  -d '{"email":"you@example.com","password":"your-password"}' localhost:8085/api/v1/fupi/auth/login
```

- **Traces:** Grafana (<http://localhost:3000>), Explore, Tempo, "TraceQL" with `{ resource.service.name = "fupi" }`, or "Trace ID" with `4bf92f3577b34da6a3ce929d0e0e4736`. The trace is the HTTP server span `POST /api/v1/fupi/auth/login`, the `identity.Login` use-case span and the Postgres query spans. "Logs for this span" jumps to the Elasticsearch lines with that `trace.id`.
- **Metrics:** the "Fupi service" dashboard (folder Fupi): requests per second and latency by route, the 5xx ratio, application logs and recent traces, and a Dependencies row with the Postgres pool and Redis commands. In Prometheus the request metric is `http_server_request_duration_seconds_*` with `service_name`, `http_route` and `http_response_status_code`.
- **Logs:** Kibana (<http://localhost:5601>), data view `logs-fupi-*`, filter `trace.id : "4bf92f3577b34da6a3ce929d0e0e4736"`. The access log line is the message `request` with the method, route, status, duration and request ID.

What to know:

- `/health` and `/health/ready` produce no spans and no HTTP metrics, and their access log lines are debug level. The readiness checks' Redis `PING` and Postgres ping still count in the Redis and Postgres client metrics (only their spans are suppressed).
- Telemetry is best effort: spans produced while the collector is away, and for a few seconds after it comes back, are dropped (they are never queued without limit). Metrics are cumulative, so the counters catch up once exporting resumes, which it does by itself without restarting the API.
- Never recorded as span attributes: SQL arguments, Redis keys and values, query strings, client addresses, request headers (apart from `User-Agent` and `Host`, which become `user_agent.original` and `server.address`), email addresses, usernames and tokens. A failed query or command records the driver's error message as the library reports it, and PostgreSQL's own messages can echo a value it rejected (for example `invalid input syntax for type uuid`), so treat error events as diagnostic data.
- An API run on your host (`make run`) still exports to the collector on `localhost:4317`. Its logs go to your terminal only (Filebeat reads container logs, not your terminal); `LOG_FORMAT=text` makes them readable, `LOG_LEVEL=debug` shows the probes.
- Settings are the standard OpenTelemetry variables, listed in `.env.example`: `OTEL_EXPORTER_OTLP_ENDPOINT` (default `http://localhost:4317`), `OTEL_SERVICE_NAME` (default `fupi`), `OTEL_TRACES_SAMPLER` (default `parentbased_always_on`; keep a parent-based sampler, the readiness checks rely on it), `OTEL_METRIC_EXPORT_INTERVAL` (milliseconds, default 15000) and `OTEL_SDK_DISABLED=true` to turn it all off.

## The stacks

### Kafka

- HA: `kafka-1..3` in KRaft mode, each both broker and controller; replication factor 3, `min.insync.replicas=2`.
  Single: `kafka-single` (alias `kafka-1`), replication factor 1.
- Topics are created at start-up and are idempotent: `url.events`, `identity.events`, `url.accessed`, `url.invalidations`,
  `security.scan.requests`, `webhook.deliveries`. Auto-creation is off.
- Kafka UI: <http://localhost:8080>. Describe topics with `make infra.kafka.topics`.
- No authentication or TLS locally.

### Redis

- HA: `redis-1..6` (3 masters, 3 replicas). Single: `redis-single`, one node owning all slots. Both run in cluster mode.
- Eviction `allkeys-lru`, 128 MB per node, AOF persistence. Application user `fupi-svc` cannot run dangerous commands
  (`FLUSHALL`, `KEYS`, `CONFIG`...).
- `make infra.redis.cli` opens a cluster-aware shell.

### Postgres

- HA: `patroni-1..3` managed by Patroni, a 3-member etcd quorum and HAProxy. `localhost:5432` always reaches the current
  primary, `localhost:5433` the replicas. Replication is asynchronous; set `synchronous_mode: true` in
  `deploy/postgres/patroni.yml` for zero data loss at the cost of write latency.
- Single: one plain Postgres 18 node; both ports reach it.
- The `migrate` job applies `app/internal/adapters/postgres/migrations` on every start (a no-op when up to date) and
  records them in `schema_migrations`.
- HAProxy stats: <http://localhost:8404>. Patroni REST: `localhost:8008`..`8010` (`/cluster`, `/metrics`).

### ELK

- Path: your app logs JSON to stdout, Filebeat reads every container's logs of this compose project, Logstash parses
  them, Elasticsearch stores them in the data stream `logs-fupi-default` (rolled over daily, deleted after 7 days),
  Kibana shows them.
- JSON log lines are mapped to `message`, `log.level`, `service.name`, `trace.id`, `span.id`; any other line keeps its text
  and takes the compose service as `service.name`.
- HA: 3 Elasticsearch nodes (transport TLS between them), 2 Logstash, 2 Kibana behind nginx. Single: one of each.
- Kibana: <http://localhost:5601>, log in as `elastic`. First time: Stack Management, Data Views, create `logs-fupi-*`
  with timestamp `@timestamp`.
- Filebeat runs as root and mounts the Docker socket read-only to read container logs. This is the one least-privilege
  exception, acceptable for local development only.

### Observability

- The app sends OTLP to the OpenTelemetry Collector (`localhost:4317`). Traces go to Tempo, metrics are re-exposed to
  Prometheus. Logs do not pass through the Collector.
- Grafana: <http://localhost:3000>. Datasources Prometheus, Tempo and Elasticsearch are provisioned; Tempo links spans to
  logs by `trace.id`. Dashboards "Stack overview" and "Fupi service" are in the folder "Fupi". The service dashboard
  fills in once the API runs (see "Observing the app").
- Prometheus: <http://localhost:9090>; Alertmanager: <http://localhost:9093>; Tempo: <http://localhost:3200>.
- Prometheus finds exporters by DNS, so only running components appear as targets. To get notifications, replace the
  `null` receiver in `deploy/observability/alertmanager.yml` with a Slack, email or webhook receiver and reload.
- More dashboards: import community dashboards by ID in Grafana (Dashboards, New, Import): Kafka 7589, Redis 763,
  Postgres 9628, Elasticsearch 14191, Go processes 6671.

### Legacy

`make infra.legacy.up` starts MongoDB 4.4 (end-of-life) and the old standalone Redis for the code behind the `legacy`
build tag. They are unchanged apart from binding to `127.0.0.1`.

## Credentials and ports

All credentials are development defaults from `.env.example`. You can override them in `.env`, with two rules:

- Values may only contain letters, digits, `.`, `_` and `-`. They end up in shell scripts, SQL, JSON bodies and connection
  URLs, where anything else (`$`, `#`, `@`, `/`, `:`, `%`, quotes, spaces) silently changes the meaning. `make infra.<stack>.up`
  and `make infra.config` refuse to continue when a value breaks this.
- Passwords are applied when a volume is first created (Postgres roles, the Elasticsearch `elastic` user, Grafana's admin).
  Changing one later does not change the stored password, and the stack then fails to authenticate. Run `make infra.clean`
  (all data is lost) or change the password inside the service as well.

| Service | Address | Login |
|---|---|---|
| Kafka (host) | `localhost:19092` (HA also `29092`, `39092`) | none |
| Kafka UI | <http://localhost:8080> | none |
| Worker health | `localhost:8086` (`/health`, `/health/ready`) | none |
| Redis | `localhost:7001`..`7006` | user `fupi-svc`, password `fupi-svc`; admin (default user) `fupi-redis-admin` |
| Postgres | `localhost:5432` write, `5433` read | `fupi-user` / `fupi-pass`, database `fupidb`; superuser `postgres` / `fupi-postgres-admin` |
| Elasticsearch | `localhost:9200` | `elastic` / `fupi-elastic-dev` |
| Kibana | <http://localhost:5601> | `elastic` / `fupi-elastic-dev` |
| Grafana | <http://localhost:3000> | `admin` / `fupi-grafana-dev` |
| Prometheus, Alertmanager, Tempo | `9090`, `9093`, `3200` | none |
| OTLP | `4317` gRPC, `4318` HTTP | none |
| Legacy MongoDB / Redis | `27017` / `6379` | `fupiUser` / `fupiPassword` / none |

Every published port is bound to `127.0.0.1`. Do not reuse these values anywhere else.

## Memory budget

Docker Desktop has a fixed memory allocation (Settings, Resources). If a container dies with exit code 137 it was killed
for lack of memory. These figures were measured with `make infra.stats` on idle stacks (Docker Desktop on Apple silicon,
7.75 GiB allocated); expect more once Elasticsearch holds data. `full-ha` is the sum of the measured parts.

| Profile | Single | HA |
|---|---|---|
| `core` (Postgres, Redis, Kafka, Kafka UI) | 0.75 GiB | 2.0 GiB |
| `elk` | 3.0 GiB | 6.2 GiB |
| `observability` | 0.5 GiB | 0.5 GiB |
| `full` | **4.2 GiB** | **8.7 GiB**: does not fit 7.75 GiB, raise Docker to at least 12 GiB |

`full-single` therefore runs comfortably in the default allocation, and `elk-ha` alone needs most of it. Per-container
numbers: `make infra.stats`.

Heaps are small on purpose (512 MB for Kafka, Elasticsearch and Logstash); tune them with `KAFKA_HEAP`, `ES_HEAP` and
`LS_HEAP` in `.env`. On a small allocation run one stack at a time.

## Failure drills (HA)

| Stack | Do | Expect |
|---|---|---|
| Kafka | `docker compose --profile '*' stop kafka-2` | produce and consume keep working; `start` it and under-replicated partitions drain |
| Outbox relay | stop the worker (`make infra.worker.down`) while a second one runs on your host | the second worker becomes the relay within `OUTBOX_STANDBY_INTERVAL` seconds; no event is lost (a duplicate carries the same `event_id`) |
| Redis | stop a master (see `cluster nodes` in `make infra.redis.cli`) | a replica is promoted within ~10 seconds; writes continue |
| Postgres | stop the leader shown by `make infra.patroni.list` | a replica becomes leader (a few seconds after a graceful stop, about 30 seconds after a crash, measured); `localhost:5432` follows it once HAProxy's health checks see the new primary (about 6 more seconds) |
| Elasticsearch | stop `es-2` | cluster stays green or yellow; logs keep arriving |
| Logstash / Kibana | stop `logstash-1` / `kibana-1` | Filebeat uses `logstash-2`; Kibana stays reachable through nginx |

## Troubleshooting

First look: `make infra.ps` (health), `make infra.logs SERVICE=<name>`, `make infra.stats` (memory). With raw
`docker compose`, add `--profile '*'` to address a service by name.

| Symptom | Likely cause and fix |
|---|---|
| `stat .../.env: no such file` | raw `docker compose` without `.env`: `make create.envfile` |
| container `Exited (137)` | out of memory: raise Docker's memory, lower `*_HEAP`, or run fewer stacks |
| `port is already allocated` | the other mode of that stack is running, or another program uses the port (`lsof -i :5432`); `make infra.<stack>.up` clears the other mode |
| `no such service: kafka-1` | add `--profile '*'` to the raw `docker compose` command |
| `make infra.<stack>.up` times out | the message lists the pending services; read their logs |
| Kafka client from the host cannot connect | use the `localhost:19092,...` addresses; inside the network use `kafka-1:9092` |
| Kafka produce fails with `NOT_ENOUGH_REPLICAS` | HA needs 2 of 3 brokers for `acks=all`; start the stopped broker |
| Redis `lookup redis-2: no such host` | app on the host without the hosts entry: `make infra.hosts` |
| `make infra.<stack>.up` stops with "may only contain letters, digits..." | a value in `.env` has a character outside `A-Za-z0-9._-`; fix it there |
| Authentication fails after changing a password in `.env` | passwords are fixed when a volume is first created: `make infra.clean` (data is lost) or change it in the service too |
| Redis `CLUSTERDOWN` | wait ~10 seconds after a failure; check `cluster info`; if volumes were partly wiped, `make infra.clean` and start again |
| Redis `CROSSSLOT` | a multi-key command spans slots; use hash tags (`{user42}:a`). This is the same in production |
| Postgres connection refused right after start (HA) | no primary elected yet: `make infra.patroni.list`, wait for a Leader |
| Postgres `cannot execute ... in a read-only transaction` | writing to port 5433; use 5432 |
| Patroni member stuck, wrong timeline | `docker compose --profile '*' exec patroni-1 /opt/patroni/bin/patronictl -c /etc/patroni/patroni.yml reinit fupi patroni-2` |
| Migration says dirty | fix the SQL, then `docker run --rm --network fupi -v "$PWD/app/internal/adapters/postgres/migrations:/m" migrate/migrate -path=/m -database "postgres://fupi-user:fupi-pass@postgres:5432/fupidb?sslmode=disable&x-migrations-table=schema_migrations" force <version>` |
| Elasticsearch `max virtual memory areas vm.max_map_count` (Linux) | `sudo sysctl -w vm.max_map_count=262144` |
| Kibana "server is not ready" | it needs 1 to 2 minutes after Elasticsearch is healthy |
| Kibana shows no logs | create the `logs-fupi-*` data view; check `make infra.logs SERVICE=filebeat-single` (or `filebeat-ha`); only containers of the `fupi` compose project are collected |
| Grafana Elasticsearch datasource error | ELK is not running |
| An exporter target is missing in Prometheus | its stack is not running; targets are discovered by DNS |

## Local versus production

| Topic | Locally | Production |
|---|---|---|
| Kafka auth/TLS | none | SASL and TLS |
| Elasticsearch HTTP | plain HTTP, auth on | HTTPS |
| Postgres replication | asynchronous | consider synchronous |
| Redis hostnames | via `/etc/hosts` | real DNS |
| Prometheus, Grafana, Tempo | single instance | replicated (Thanos or Mimir for Prometheus) |
| Filebeat | root, Docker socket | node agent with least privilege |
| Credentials | committed defaults | secrets manager |

## Adding a service

1. Put it in the stack's file under `deploy/<stack>/compose.yml` (or a new `deploy/<name>/compose.yml` plus an
   `include:` entry in `docker-compose.yml`).
2. Give it profiles: its own `<stack>-ha` and/or `<stack>-single`, `core-*` if the app needs it, `full-*`. For a mode-specific
   variant, give the single service the alias of the HA node 1 so addresses do not change.
3. Bind published ports to `127.0.0.1`, pin the image tag, and give every credential `${VAR:-default}` with the same
   default in `.env.example`.
   Name one-shot jobs `*-init-*`, `*-setup-*` or `migrate` (with `restart: "no"` or `on-failure`): `make infra.<stack>.up`
   waits for them to exit 0, while every other service must be running and healthy.
4. Run `make infra.config`.
