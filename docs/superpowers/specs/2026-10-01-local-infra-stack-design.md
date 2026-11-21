# Local infrastructure stack (HA and single-node) — design

Status: draft for review · Date: 2026-10-01 · Slice 1 of 5

## 1. Intent

Fupi is being built as a scalable URL shortener (target: 1B URLs, 100M DAU, ~11.6K redirects/s).
Its production topology (see `architecture-v2.md`, "Infrastructure stack") is Postgres with Patroni,
a 6-node Redis Cluster, a 3-broker Kafka cluster, and Prometheus/Grafana/OpenTelemetry. Developers
need to run that topology locally so HA behaviour (failover, replication, quorum) can be exercised
before it reaches production, and also need a **lightweight single-node mode** for day-to-day work on
a laptop with limited Docker memory.

**Success criteria**

1. `make infra.<stack>.up MODE=ha|single` brings up any stack in either mode with no manual setup.
2. In HA mode, each stack survives the loss of one node and the documented drill proves it.
3. Switching mode never changes the addresses application code uses (see §5.3).
4. A developer who has never seen the setup can follow `docs/LocalInfrastructure.md` to start,
   use, and debug every stack.
5. `full-single` fits in the current Docker Desktop allocation (~7.7 GiB).

## 2. Scope

This spec is **slice 1** of five. Each later slice gets its own spec → plan → implementation cycle.

| # | Slice | Depends on |
|---|-------|------------|
| 1 | **This spec** — compose stack, Makefile targets, docs | — |
| 2 | Dockerfile hardening (+ `app` compose profile) | 1 |
| 3 | App connectivity — config cleanup, Redis/Kafka clients, `/health`, graceful shutdown | 1 |
| 4 | OpenTelemetry — SDK, HTTP/pgx/redis/kafka instrumentation, log↔trace correlation | 1, 3 |
| 5 | Outbox relay worker (`outbox_events` → Kafka) | 3 |

**In scope here:** `docker-compose.yml` (entry point) plus per-stack files and configs under `deploy/`,
`.make/docker.mk` targets, `.env.example` additions, `docs/LocalInfrastructure.md`, README update.

**Out of scope here:** the `app` service/profile, any Go code, dashboards for app-specific metrics
that do not exist yet, production deployment manifests (Kubernetes), ClickHouse (Analytics context,
later phase), Mongo changes.

## 3. Decisions and deviations

Settled with the requester during brainstorming:

- **Postgres 18 + Patroni**, not CockroachDB. The audit-trigger function uses statement-level triggers,
  `txid_current()`, `inet_client_addr()` and `current_query()`, which CockroachDB does not support, and CI
  already runs Postgres 18.
- **Kafka scope:** client **and** outbox relay (slices 3 and 5).
- **Two modes per stack:** HA and single-node (requested at spec review).
- **ELK for logs; Mongo kept** in a `legacy` profile while the code base transitions.

Decisions made in this spec — **please confirm or push back**:

| # | Decision | Why | Deviation |
|---|----------|-----|-----------|
| D1 | **Tempo** for traces | Native Grafana integration, single binary, trace→log links via the ES datasource | v2 spec says Jaeger; Loki is replaced by ELK (requested) |
| D2 | Redis **single mode is a cluster of one** (cluster mode on, one node owning all 16384 slots) | Same client code path and same `CROSSSLOT` behaviour as production; address config does not change | A plain standalone Redis is the usual "single node" |
| D3 | Postgres **single mode is plain `postgres:18`**, no Patroni/etcd/HAProxy | The point of single mode is a light footprint (1 container vs 8) | Primary only; no replica to test read routing against |
| D4 | Kafka uses **PLAINTEXT**, no SASL, locally | SASL per broker adds large bootstrap for little local value | Production will use auth; app config must make it optional (slice 3) |
| D5 | ES **HTTP TLS off**, transport TLS and auth **on** | Transport TLS is mandatory for multi-node; HTTP TLS would need the CA mounted into ~6 consumers | Documented as a local/production difference |
| D6 | All published ports bind to **127.0.0.1** | Dev credentials are defaults; do not expose them on the LAN | — |
| D7 | Make targets use the **`infra.` prefix** (`infra.kafka.up`) | The existing `.make/docker.mk` verbs (`build.docker`, `scan.docker`) concern the app image; separating avoids confusion | — |
| D8 | Prometheus, Grafana, Alertmanager, Tempo, Collector run **single-instance** in every mode | Observability HA is a production concern (Thanos/Mimir); not emulated | Documented |
| D9 | The **`app` service is deferred** to slice 2/3 | The image does not build today and `main.go` does not wire Redis/Kafka yet | `docker compose up` for the app comes later |

## 4. Layout

```text
docker-compose.yml            # entry point: name, network, include: of every stack
deploy/
  kafka/      compose.yml  create-topics.sh
  redis/      compose.yml  redis.conf  init-cluster.sh
  postgres/   compose.yml  Dockerfile  patroni.yml  haproxy.cfg  entrypoint.sh  roles.sh
  elk/        compose.yml  setup.sh  logstash/{logstash.yml,pipelines.yml,pipeline/*.conf}
              filebeat/{filebeat-ha.yml,filebeat-single.yml}  nginx/kibana.conf
  observability/ compose.yml  otel-collector.yml  tempo.yml
              prometheus/{prometheus.yml,rules/*.yml}  alertmanager.yml
              grafana/{provisioning/,dashboards/*.json}
  legacy/     compose.yml
.make/docker.mk               # + infra.* targets
scripts/infra.sh              # profile resolution, up/down/wait, DRY_RUN
scripts/infra_test.sh         # tests for infra.sh, the env check and the Redis init script (no Docker)
scripts/infra_env_check.sh    # compose defaults == .env.example; no unsafe characters
docs/LocalInfrastructure.md
```

The root file declares `name: fupi` and a single bridge network `fupi`, then `include:`s each
stack with `env_file: .env` (verified: `include` needs an explicit `env_file` to see the root
`.env`, a missing `.env` is an error, shell variables override it, and relative bind mounts resolve
against the included file's directory). Every `infra.*` target therefore depends on `create.envfile`.
Every credential also has a `${VAR:-dev-default}` fallback in compose, so an empty `.env` still works.

## 5. Modes and profiles

### 5.1 Profiles

One profile per stack **per mode**, plus aggregates. A service lists every profile that should start it.

| Profile | Starts |
|---|---|
| `kafka-ha` / `kafka-single` | Kafka brokers, topic init, Kafka UI, Kafka exporter |
| `redis-ha` / `redis-single` | Redis nodes, cluster init, Redis exporter |
| `postgres-ha` / `postgres-single` | Postgres (Patroni+etcd+HAProxy, or one node), migrate job, Postgres exporter |
| `elk-ha` / `elk-single` | Elasticsearch, Logstash, Kibana, Filebeat, setup job, exporters |
| `observability` | OTel Collector, Prometheus, Alertmanager, Grafana, Tempo |
| `legacy` | Mongo + the old standalone Redis, unchanged |
| `core-ha` / `core-single` | `postgres` + `redis` + `kafka` in that mode (all the app needs) |
| `full-ha` / `full-single` | `core` + `elk` + `observability` in that mode (never `legacy`) |

Stacks are independent: `kafka-ha` with `postgres-single` is valid. **Do not activate both modes of one
stack** — they claim the same ports and aliases. The Make targets enforce this by stopping the other
mode's services first; raw `docker compose` users are warned in the docs. With no profile active,
`docker compose up` starts nothing (the README currently says otherwise and is updated).

### 5.2 Naming: per-mode services, stable aliases

HA and single variants are separate services (different configs, replication factors, listeners). The
single-mode service carries a **network alias equal to node 1 of the HA topology**, so in-network
addresses are identical in both modes.

| Stack | HA services | Single service (alias) | Shared by both |
|---|---|---|---|
| kafka | `kafka-1..3`, `kafka-init-ha` | `kafka-single` (`kafka-1`), `kafka-init-single` | `kafka-ui`, `kafka-exporter` |
| redis | `redis-1..6`, `redis-init-ha` | `redis-single` (`redis-1`), `redis-init-single` | `redis-exporter` |
| postgres | `patroni-1..3`, `etcd-1..3`, `haproxy` (`postgres`) | `postgres-single` (`postgres`) | `migrate`, `postgres-exporter` |
| elk | `elk-setup-ha`, `es-1..3`, `logstash-1..2`, `kibana-1..2`, `kibana-lb` (nginx), `filebeat-ha`, `logstash-exporter-2` | `elk-setup-single`, `es-single` (`es-1`), `logstash-single` (`logstash-1`), `kibana-single`, `filebeat-single` | `elasticsearch-exporter`, `logstash-exporter-1` |

### 5.3 Address contract (what slice 3 consumes)

Identical in both modes except the lists get shorter.

| Concern | From the host (app via `air`) | From the compose network |
|---|---|---|
| Kafka bootstrap | HA `localhost:19092,localhost:29092,localhost:39092` · single `localhost:19092` | HA `kafka-1:9092,kafka-2:9092,kafka-3:9092` · single `kafka-1:9092` |
| Redis seeds | HA `localhost:7001..7006` · single `localhost:7001` | HA `redis-1:7001..redis-6:7006` · single `redis-1:7001` |
| Postgres write | `localhost:5432` (HA: current primary via HAProxy) | `postgres:5432` |
| Postgres read | `localhost:5433` (HA: replicas; single: same node) | HA `postgres:5433`; single: use `postgres:5432` |
| OTLP | `http://localhost:4317` | `http://otel-collector:4317` |

Redis in HA mode announces **hostnames** (`redis-1`..`redis-6`) so a cluster client can follow `MOVED`
redirects. A host-run app must resolve those names: `make infra.hosts` prints the one-line `/etc/hosts`
entry (`127.0.0.1 redis-1 … redis-6`); a containerised app needs nothing. Redis in either mode serves
only logical DB 0 (cluster mode), so `REDIS_DATABASE` must be `0`.

## 6. Per-stack design

Image tags are pinned exactly (verified against the registries on 2026-10-01); see §10.

### 6.1 Kafka

- Official `apache/kafka` (KRaft; no ZooKeeper). Bitnami images are not used (free catalog withdrawn).
- **HA:** 3 nodes, each `broker,controller`, quorum `1@kafka-1:9093,2@kafka-2:9093,3@kafka-3:9093`.
  Defaults: replication factor 3, `min.insync.replicas=2`, `unclean.leader.election.enable=false`,
  `auto.create.topics.enable=false`, internal-topic replication 3. Heap 512 MB.
- **Single:** 1 node, same image; replication factor 1, `min.insync.replicas=1`, internal topics RF 1.
- **Listeners:** `INTERNAL` (`kafka-N:9092`, for in-network clients and inter-broker traffic),
  `CONTROLLER` (`:9093`), `EXTERNAL` (`localhost:19092/29092/39092` published to the host).
- **Topics** (`kafka-init-*` runs `kafka-topics.sh --create --if-not-exists`, idempotent), from `data-architecture.md`:

  | Topic | Retention | Local partitions | Notes |
  |---|---|---|---|
  | `url.events` | 7d | 3 | |
  | `identity.events` | 7d | 3 | named in ADR-0011 |
  | `url.access` | 2d | 6 | keyed by hash(shortCode) |
  | `url.invalidations` | 1h | 3 | |
  | `security.scan.requests` | 1d | 3 | |
  | `webhook.deliveries` | 3d | 3 | |

  Partition counts are local-dev defaults, not production sizing.
- **UI:** `kafbat/kafka-ui` (provectus is unmaintained), pointed at `kafka-1:9092`, port 8080.
- **Health:** `kafka-broker-api-versions.sh` per broker.

### 6.2 Redis

- Official `redis` image, one `redis.conf`: `cluster-enabled yes`, `cluster-node-timeout 5000`,
  `appendonly yes`, `maxmemory 128mb`, `maxmemory-policy allkeys-lru` (per v2 spec),
  `cluster-announce-hostname` + `cluster-preferred-endpoint-type hostname`.
- ACL: default user locked with a password (also used for `masterauth`/replication); app user
  `fupi-svc` with the permissions the app needs. Credentials from env.
- **HA:** `redis-1..6` on ports 7001–7006 (client port; bus port = +10000, in-network only).
  `redis-init-ha` runs `redis-cli --cluster create … --cluster-replicas 1` (3 shards × 1 replica).
- **Single:** `redis-single` on 7001; `redis-init-single` runs `CLUSTER ADDSLOTSRANGE 0 16383`.
- Cluster state lives in a per-node volume (`nodes.conf`); init jobs are idempotent (skip when
  `cluster_state:ok`).
- Nodes have **fixed IP addresses** (`${FUPI_NET_PREFIX}.11`–`.16`, default `172.29.0`) because the cluster bus persists
  peer IPs in `nodes.conf`; after a restart with changed IPs the cluster could not re-form. The `fupi` network
  therefore has an explicit subnet, with other containers drawn from its upper half (`ip_range`).

### 6.3 Postgres

- **HA:** `deploy/postgres/Dockerfile` builds Patroni 4.1.5 (etcd3) on `postgres:18`, runs as the
  `postgres` user, initdb with data checksums. `patroni-1..3` + `etcd-1..3` (3-member quorum) +
  `haproxy`.
  - **HAProxy** (`haproxy` image): `:5432` → the leader (`GET /primary` on Patroni's 8008),
    `:5433` → replicas (`GET /replica`); connections are shut down when a backend is marked down.
    Prometheus metrics on `:8404/metrics`.
  - **Replication:** async by default; `synchronous_mode` documented as a one-line toggle.
    `use_pg_rewind: true`; `pg_stat_statements` preloaded.
  - **Roles** (created by a bootstrap SQL script shared with single mode): `fupi-user` owning
    database `fupidb` (current app defaults), `exporter` with `pg_monitor`, `replicator`.
- **Single:** `postgres:18` official image, same roles via `/docker-entrypoint-initdb.d/`.
  Host ports `5432` and `5433` both map to the one node, so read/write-split code works in both modes.
- **`migrate`** (shared): `migrate/migrate` against `postgres:5432` with `./app/internal/adapters/postgres/migrations`
  mounted, table `schema_migrations` (matches the Makefile; the app's own migrator currently uses
  `bid_schema_migrations` — reconciled in slice 3). Restarts on failure until the DB accepts connections.
- Patroni REST ports 8008–8010 are published to localhost for `curl`/`patronictl` debugging.

### 6.4 ELK

- Elastic 9.x for Elasticsearch, Logstash, Kibana and Filebeat, all on the same version.
- **Log path:** Filebeat (container input + `add_docker_metadata`, reads `/var/lib/docker/containers`,
  excludes its own container) → Logstash (beats input) → Elasticsearch **data stream**
  `logs-fupi-default` with an ILM policy (roll over daily, delete after 7 days). One log store; no Loki.
- **Logstash pipeline:** parse a JSON `message` when present (the app's format), otherwise keep the raw
  line; map to `service.name`, `trace.id`, `span.id`, `log.level`; persistent queue and dead-letter queue.
- **One-shot setup jobs** (idempotent): in HA, `elk-setup-certs-ha` generates a CA and node certs with
  `elasticsearch-certutil` into a shared volume and exits (the Elasticsearch nodes wait for it to complete). In both modes
  `elk-setup-{ha,single}` then waits for Elasticsearch (with a deadline), sets `kibana_system`'s password, creates
  `logstash_writer` (write to `logs-fupi-*` only), `grafana_reader` and `metrics_reader` (read-only, plus cluster
  `monitor` for Grafana's health check) and installs the ILM policy and index template.
- **HA:** `es-1..3` (all master+data, quorum of 3, heap 512 MB, `bootstrap.memory_lock=false`;
  `es-1/2/3` published on 9200/9201/9202), `logstash-1..2` (Filebeat load-balances across both),
  `kibana-1..2` behind `kibana-lb` (nginx, round-robin, published on 5601) sharing the same encryption keys.
- **Single:** `es-single` (`discovery.type=single-node`, security on, no transport TLS), `logstash-single`,
  `kibana-single` (5601 directly), `filebeat-single`.
- Linux hosts need `vm.max_map_count=262144` (Docker Desktop already sets it); documented.
- Filebeat runs as root to read container logs and mounts `docker.sock` read-only. This is the one
  documented least-privilege exception in the stack.

### 6.5 Observability (same in both modes)

- **OTel Collector** (contrib): OTLP gRPC/HTTP in; `memory_limiter` + `batch`; traces → Tempo,
  metrics → a Prometheus exporter endpoint (`:8889`). Health extension on `:13133`.
- **Tempo** (single binary, local storage, 72h retention, set in both places Tempo 3.x keeps it:
  `backend_scheduler.provider.compaction.compaction.block_retention` and `backend_worker.compaction.block_retention`).
- **Prometheus:** 7-day retention, rules loaded from `rules/`. Targets use **DNS service discovery**
  (`dns_sd_configs`) on service names, so components that exist only in HA mode (Patroni, etcd,
  HAProxy, `logstash-exporter-2`) and stacks that are not running produce no targets and no false
  `up == 0` alerts. Trade-off: a crashed container disappears from discovery rather than reporting
  `up == 0`; rules for stack health therefore use the exporters' own signals.
- **Exporters** (each carries its stack's profiles so it starts with the stack): Kafka (consumer lag,
  under-replicated partitions), Redis (cluster mode), Postgres, Elasticsearch, Logstash; Patroni, etcd,
  HAProxy, Collector, Tempo and Grafana expose native `/metrics`. Per-container CPU and memory come from
  `make infra.stats`; cAdvisor was tried and dropped because it cannot see containers on Docker Desktop.
- **Alertmanager** with a small rule set — Patroni has nodes but no leader, Redis `cluster_state` not ok,
  Kafka under-replicated partitions, ES cluster red, p99 redirect latency high (inert until the app
  emits it) — and a stub receiver; the docs show how to add Slack/email.
- **Grafana:** admin password from env, anonymous access off, provisioned datasources Prometheus, Tempo
  and Elasticsearch (read-only user), with Tempo's trace→logs linked on `trace.id`. Two dashboards ship
  in this slice: **Stack overview** and **Fupi service** (the latter fills in as slice 4 lands). Community
  dashboard IDs for deeper per-component views are listed in the docs.

### 6.6 Legacy

`documentdb` (Mongo 4.4.14) and `cache` (Redis 7.0.2) move **unchanged** into `deploy/legacy/` under
the `legacy` profile, keeping current ports, credentials and volume names. Mongo 4.4 is end-of-life;
that is called out in the docs and left as is by request.

## 7. Makefile interface (`.make/docker.mk`)

`MODE ?= ha`. Targets are idempotent and print what they started.

| Target | Purpose |
|---|---|
| `infra.{kafka,redis,postgres,elk,observability,legacy,core,full}.up` | start a stack (`MODE=ha\|single`); stops the other mode of that stack first |
| `infra.{…}.down` | stop (volumes kept) |
| `infra.clean` | `down -v` for every stack except `legacy`, behind the existing `confirm` prompt |
| `infra.clean.legacy` | `down -v` for the legacy MongoDB and Redis only (their data predates this slice), behind `confirm` |
| `infra.ps` · `infra.logs SERVICE=x` · `infra.stats` | status, logs, live memory/CPU |
| `infra.config` | render-validate every profile combination (`docker compose config`) |
| `infra.hosts` | print the `/etc/hosts` line for host-run apps against Redis HA |
| `infra.wait` | block until the active services report healthy |
| `infra.migrate` | run the `migrate` job |
| `infra.psql` · `infra.redis.cli` · `infra.kafka.topics` · `infra.patroni.list` · `infra.es.health` | one-line debugging helpers |

Existing image targets are untouched. (Noted for slice 2: `run.docker` depends on `create.dockerEnvFile`,
but the target is named `create.dockerenvfile`.)

## 8. Documentation (`docs/LocalInfrastructure.md`)

Prerequisites and Docker memory settings · quick start for both modes · profile and mode matrix ·
per-stack sections (what runs, ports, credentials, how to connect, how to inspect) · the address
contract (§5.3) · **RAM budget per profile (measured, replacing the estimates below)** · failure drills
(what to kill, what to expect) · a troubleshooting playbook per stack (symptom → check → fix) ·
local-vs-production differences (D4, D5, D8, async replication, Redis hostnames, Filebeat privileges) ·
how to add a service. README's Docker section and `docs/Deployment.md` link to it; the README text that
says `docker compose up` starts MongoDB and Redis is corrected.

## 9. Verification plan

Static (every profile combination, no containers): `docker compose config`, `haproxy -c`, `nginx -t`,
`promtool check config|rules`, `amtool check-config`, `otelcol validate`, `logstash -t`, `shellcheck`
on the scripts.

Runtime, on this machine (Docker Desktop ~7.7 GiB, 10 CPUs), one stack at a time:

| Stack | HA acceptance drill | Single acceptance |
|---|---|---|
| Kafka | create topics (RF 3); stop `kafka-2`; produce and consume still succeed; restart, ISR recovers | topics exist (RF 1); produce/consume |
| Redis | cluster `ok`, 3 masters/3 replicas; kill a master, replica promoted, cluster client keeps writing | one node owns 16384 slots; `SET/GET` |
| Postgres | `patronictl list` shows leader + 2 replicas; migrations apply via `postgres:5432`; stop the leader, new leader elected, `:5432` follows, `:5433` serves reads | migrations apply; both host ports reach the node |
| ELK | cluster green; stop `es-2`, stays yellow/green and logs keep flowing; stop `logstash-1`, Filebeat fails over; stop `kibana-1`, UI still reachable; a log line appears in `logs-fupi-default` | log line reaches Kibana |
| Observability | all datasources healthy in Grafana; Prometheus targets present for the running stacks; a synthetic OTLP span reaches Tempo and is visible from Grafana | same |

`full-single` is brought up together and its measured memory recorded. `full-ha` (est. ~12 GiB) **cannot
run in the current Docker allocation**, so HA stacks are verified individually and `full-ha` is verified
statically only; I will say so rather than claim otherwise. The first run pulls several GiB of images and
is done with the requester's go-ahead.

## 10. Image versions (pinned, registry-verified 2026-10-01)

| Component | Image | Tag |
|---|---|---|
| Kafka | `apache/kafka` | `4.3.1` |
| Kafka UI | `kafbat/kafka-ui` | `v1.5.0` |
| Redis | `redis` | `8.10.2-alpine` |
| Postgres | `postgres` | `18.6` (HA image adds Patroni `4.1.5`) |
| etcd | `quay.io/coreos/etcd` | `v3.6.15` |
| HAProxy | `haproxy` | `3.2.25-alpine` (LTS line) |
| nginx | `nginx` | `1.30.5-alpine` |
| Elasticsearch / Logstash / Kibana / Filebeat | `docker.elastic.co/…` | `9.5.4` |
| Prometheus | `prom/prometheus` | `v3.15.0` |
| Alertmanager | `prom/alertmanager` | `v0.34.1` |
| Grafana | `grafana/grafana` | `13.2.3` |
| Tempo | `grafana/tempo` | `3.1.0` |
| OTel Collector | `otel/opentelemetry-collector-contrib` | `0.161.0` |
| redis_exporter | `oliver006/redis_exporter` | `v1.93.0-alpine` |
| postgres_exporter | `prometheuscommunity/postgres-exporter` | `v0.20.1` |
| kafka_exporter | `danielqsj/kafka-exporter` | `v1.10.0` |
| logstash_exporter | `kuskoman/logstash-exporter` | `v1.9.1` |
| elasticsearch_exporter | `quay.io/prometheuscommunity/elasticsearch-exporter` | `v1.11.0` |
| golang-migrate | `migrate/migrate` | `v4.19.1` (matches `go.mod`) |
| Mongo / legacy Redis | `mongo` / `redis` | `4.4.14` / `7.0.2` (unchanged) |

## 11. Memory (measured)

Measured with `make infra.stats` on idle stacks, Docker Desktop on Apple silicon, 7.75 GiB allocated. Estimates in the
first revision of this spec were ~1.3 / ~3.2 GiB (core), ~2.4 / ~6.5 GiB (elk), ~2 GiB (observability), ~5.7 / ~12 GiB (full).

| Profile | Single | HA |
|---|---|---|
| `core` (postgres + redis + kafka + Kafka UI) | 0.75 GiB | 2.0 GiB |
| `elk` | 3.0 GiB | 6.2 GiB |
| `observability` | 0.5 GiB | 0.5 GiB |
| `full` | **4.2 GiB** (measured, 22 services) | **8.7 GiB** (sum of the measured parts; does not fit 7.75 GiB, needs ≥ 12 GiB) |

Heaps are sized small (512 MB for Kafka/ES/Logstash).

## 12. Risks and open items

| Risk | Handling |
|---|---|
| `redis-cli --cluster create` may require IPs rather than hostnames | resolve with `getent` in the init script; verify first thing |
| Patroni image build size/time; `pg_rewind` and bootstrap ordering | build once, cached; the drill in §9 is the acceptance test |
| Tempo 3.x config format differs from older docs | start from Tempo's current example; validate by running it |
| cAdvisor is unreliable on Docker Desktop for Mac | **happened**: it cannot see containers there (no Docker or containerd factory), so it was dropped; `make infra.stats` covers per-container memory |
| Filebeat reading `/var/lib/docker/containers` on Docker Desktop | verify early; fall back to the `docker` log driver input if needed |
| ES in HA may exhaust the Docker VM's memory | verify alone; if it OOMs, report and lower heaps or document the minimum |
| Redis 8 is tri-licensed (RSALv2/SSPL/AGPLv3) | fine for local dev; Valkey is a drop-in alternative if licensing matters |
| `include` needs `.env` to exist | every `infra.*` target depends on `create.envfile`; documented for raw `docker compose` |

## 13. Implementation notes

- `make infra.<stack>.up MODE=...` removes the other mode's containers first (`docker compose rm -sf`, volumes kept) and
  waits for readiness. One-shot jobs are recognised by name (`*-init-*`, `*-setup-*`, `migrate`) and are ready only once
  they have exited 0; every other service must be running and healthy (or have no healthcheck). A failed job (other than
  `migrate`, which restarts until the database is up) fails the wait immediately with its logs. Shared services (Kafka UI,
  exporters, `migrate`) belong to both modes' profiles, so switching mode recreates them.
- Every included compose file redeclares the `fupi` network as `name: fupi` with no `external:` flag; the root file owns
  the driver and subnet. An `external: true` declaration in an included file merges into the root's definition, makes
  Compose treat the network as pre-existing, and drops the subnet the Redis static IPs need.
- Prometheus targets use DNS service discovery; etcd is scraped on its metrics port `2381`, Patroni on its REST port `8008`.
- Alert rules have unit tests (`prometheus/tests/stack_test.yml`, run with `promtool test rules`).
- `scripts/infra_env_check.sh` enforces that every compose fallback equals `.env.example`, which is what keeps a stale
  `.env` working.
- Version-specific settings found by running the stack: Logstash 9 uses `api.http.host`; Filebeat 9 needs the
  `filestream` input with the `container` parser; Kibana wants a JSON array for a multi-host `ELASTICSEARCH_HOSTS`;
  Grafana's Elasticsearch health check needs the cluster `monitor` privilege.
- Measured failover: Patroni promotes within seconds after a graceful stop and about 30 seconds after a crash; HAProxy then
  needs about 6 seconds of health checks to route `:5432` to the new primary.
- Post-review changes: (1) the one setup job that turned healthy and then exited was split into `elk-setup-certs-ha` (nodes
  depend on its completion) and `elk-setup-ha` (depends on `es-1` being healthy): re-running `up` on a running HA stack
  failed 2 times in 5 with "dependency failed to start: container ... exited (0)" and now passes 10 of 10; (2) the
  setup job's wait for Elasticsearch has a 240s deadline, because Compose waits for a job with no timeout of its own;
  (3) the unsafe-character check became an allowlist (`A-Za-z0-9._-`), covers the Redis application user and password,
  and runs on the developer's `.env` as well, at the start of every `infra.sh up`; (4) every Docker-touching `infra.*`
  target now depends on `create.envfile`, as §4 requires; (5) `infra.clean` no longer deletes the legacy volumes.
