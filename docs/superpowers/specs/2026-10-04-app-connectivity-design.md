# App connectivity: config, Redis, health, shutdown, migrator — design

Status: draft for review · Date: 2026-10-04 · Slice 3 of 5

## 1. Intent

Slice 1 built the local infrastructure stack. The API process (`app/cmd/main.go`) does not yet fit it: it defaults
to the wrong Postgres port, ships Mongo-era settings, has no health endpoint, cannot shut down cleanly, and carries
a Redis client that cannot connect reliably. There is also no production path that applies database migrations;
`postgres.Migrate()` is called only by the test helper.

This slice makes the API start against the stack in either mode (HA or single), report whether it can serve
traffic, drain and exit cleanly on SIGTERM, and gives migrations a real entry point.

**Success criteria**

1. With `make infra.core.up MODE=single` (or HA) running, `go run ./app/cmd` starts with no `.env` edits and
   `GET /health/ready` reports Postgres and Redis `up`.
2. Stopping Redis leaves the API ready (HTTP 200, `degraded`); stopping Postgres makes it not ready (503).
3. SIGTERM lets in-flight requests finish, then closes Redis and Postgres and exits 0.
4. `go run ./app/cmd/migrator` applies the migrations to the stack's Postgres and a second run is a no-op.
5. With `ENVIRONMENT=production` the API and the migrator refuse to start on a development default secret.
6. A user registered through the API produces an `outbox_events` row (end-to-end proof of connectivity).

## 2. Scope

This is **slice 3** of five (slice table from `2026-10-01-local-infra-stack-design.md`, updated):

| # | Slice | Change in this spec |
|---|-------|---------------------|
| 1 | Local infrastructure stack | done (PR SanctumLabs/fupi#342) |
| 2 | Dockerfile hardening + `app` compose profile | now **after** slice 3, so it can use this env contract, `/health` and the migrator |
| 3 | **This spec** | config, Redis client, health, graceful shutdown, migrator |
| 4 | OpenTelemetry | unchanged |
| 5 | Outbox relay worker | **now also owns the Kafka client** (moved out of slice 3, see D1) |

**In scope:** `app/config` (typed loader), `app/cmd/main.go` (`run` function, signals), `app/cmd/migrator`,
`app/pkg/infra/cache/redis`, `app/pkg/infra/monitoring/health`, `app/pkg/infra/server` (shutdown), `app/pkg/infra/database/postgres`
(DSN builder and `Migrate()` fixes), auth middleware public paths, `.env.example` app section, `.make/dev.mk`
(`run.with.migrations`), `docs/LocalInfrastructure.md`.

**Out of scope:** Kafka client or any Kafka code (slice 5), a Postgres read pool, the Dockerfile and compose `app`
profile (slice 2), OpenTelemetry (slice 4), the broken `-tags legacy` build, `fly.toml` (see §11), email transport,
any schema change.

## 3. Decisions and deviations

| # | Decision | Why |
|---|----------|-----|
| D1 | The Kafka client moves to slice 5. | With the single outbox (ADR-0011) the API only writes `outbox_events` in Postgres; only the relay worker publishes. Keeping Kafka out of the API means a Kafka outage cannot affect it. Confirmed by the user. |
| D2 | Redis is optional: the API starts and stays ready without it; readiness reports it `down` but returns 200. Only Postgres is required. | Redis will be a cache, not the source of truth. Confirmed by the user. |
| D3 | One Postgres pool, pointed at the primary (`:5432`). | Nothing reads from replicas yet; a read pool is speculative until the URL read paths exist. The old `5433` default was the HA read port, so a write would have failed in HA. |
| D4 | Migrations are applied by a new `app/cmd/migrator` binary that calls `postgres.Migrate()`. The API never migrates at startup. | The user wants `Migrate()` used for real migrations, not only tests. The v2 layout already names `cmd/migrator` (ADR-0010). Several API replicas racing to migrate at boot is avoided, and the migrator can later run with a DDL-capable role. **Assumption** recorded for review: if you wanted an opt-in flag on the API instead, say so. |
| D5 | A typed `config.Load` over a small strict reader (`Lookup`, satisfied by `os.LookupEnv`); no new config library. | About 25 variables; a library adds a dependency without removing code. The existing `env.EnvConfig` getters swallow parse errors, which §4 requires to be errors, so they are not reused. |
| D6 | Production safety is opt-out: any `ENVIRONMENT` other than `development` or `test` rejects development default secrets. | A forgotten variable in a real deployment must fail at boot, not run with `fupi-secret`. Values like `release` (used by `fly.toml`) and `staging` are protected too. |
| D7 | The public readiness response carries `up`/`down` only, never error text. | The endpoint is unauthenticated. Errors are logged server-side. |
| D8 | The legacy-tagged code (`app/api/health`, `app/config` legacy types) is not touched. | `go vet -tags legacy ./app/...` already fails in `userepo/mapper.go`; unrelated to this work. |
| D9 | The compose `migrate` job (slice 1) keeps using the `migrate/migrate` image. | It is verified and independent. Slice 2 can switch it to the app's migrator image once that image exists. |

## 4. Configuration contract

`config.Load(lookup Lookup) (App, error)` returns `App{Server, Database, Redis, Auth, Shutdown}` and validates it.
Each section also has its own loader (`LoadDatabase`, `LoadRedis`, `LoadAuth`, `LoadServer`, `LoadMigrations`) so the migrator loads
only the database section and does not need `AUTH_SECRET`. Existing variable names and their integer unit
conventions are kept (for example `DATABASE_MAX_CONN_LIFETIME` is in hours) so existing `.env` files keep working.

Defaults are the stack's development values, so a host app works with no `.env`.

| Variable | Default | Notes |
|---|---|---|
| `ENVIRONMENT` | `development` | `development` and `test` allow default secrets; anything else enforces D6 |
| `HTTP_PORT` | `8085` | |
| `SERVER_HOST` / `SERVER_HEADER` / `SERVER_NAME` / `SERVER_VERSION` | `0.0.0.0` / `Fupi` / `Fupi` / `1.0.0` | unchanged |
| `APP_BASE_URL` | `http://localhost:8085` | unchanged |
| `SHUTDOWN_TIMEOUT` | `15` (seconds) | drain deadline for in-flight requests |
| `DATABASE_URL` | empty | when set it **wins** over the parts below (today it is passed in and silently ignored) |
| `DATABASE_HOST` / `DATABASE_PORT` | `localhost` / **`5432`** | primary; was `5433` (HA read port) |
| `DATABASE_NAME` / `DATABASE_USERNAME` / `DATABASE_PASSWORD` | `fupidb` / `fupi-user` / `fupi-pass` | matches `PG_*` in `.env.example` |
| `DATABASE_SSL_MODE` | `disable` | today ignored by the DSN builder; now honored |
| `DATABASE_MAX_CONNS` / `MIN_CONNS` / `MAX_CONN_LIFETIME` / `MAX_CONN_IDLE_TIME` / `CONN_TIMEOUT` / `QUERY_TIMEOUT` / `OPERATION_TIMEOUT` | `30` / `5` / `1` h / `30` min / `30` s / `10` s / `30` s | unchanged |
| `MIGRATIONS_PATH` | `app/internal/adapters/postgres/migrations` | migrator only; resolved to an absolute `file://` URL |
| `REDIS_ADDRESS` | `localhost:7001` | comma-separated `host:port` list; one entry = plain client, several = cluster client (HA: `localhost:7001`..`7006`) |
| `REDIS_USERNAME` / `REDIS_PASSWORD` | `fupi-svc` / `fupi-svc` | the stack's application ACL user |
| `REDIS_DATABASE` | `0` | cluster mode serves database 0 only; any other value is rejected |
| `AUTH_SECRET` | `fupi-secret` (dev only) | |
| `AUTH_ISSUER` / `AUTH_EXPIRE_DELTA` / `AUTH_REFRESH_EXPIRE_DELTA` | `fupi` / `15` / `24` | unchanged (minutes / hours) |

**Validation (returns an error, so the process exits 1 with a clear message):**

- A numeric variable that does not parse is an error. Today `EnvIntOr` logs and silently falls back.
- `HTTP_PORT` and `DATABASE_PORT` must be 1..65535; pool sizes must satisfy `MIN <= MAX`; `REDIS_DATABASE` must be 0.
- `REDIS_ADDRESS` entries must each be `host:port`.
- Outside development/test (D6): `AUTH_SECRET == "fupi-secret"` or empty, `DATABASE_PASSWORD == "fupi-pass"` or empty,
  and `REDIS_PASSWORD == "fupi-svc"` are errors. `DATABASE_SSL_MODE=disable` only logs a warning, because the
  compose network is not TLS either.
- Passwords are never logged. Startup logs the resolved host, port and database name only.

`.env.example`'s application section is rewritten to this contract. The Mongo-era variables (`DATABASE_PORT=27017`,
`DATABASE_USES_SRV`, `REDIS_HOST`, `REDIS_PORT`, `REDIS_MASTER_NAME`) are removed, and the names the code actually
reads (`HTTP_PORT`, `ENVIRONMENT`, `AUTH_EXPIRE_DELTA`) replace the ones it does not (`PORT`, `ENV`, `AUTH_EXPIRE`).
The legacy-tagged `config.Config`, `DatabaseConfig` and `CacheConfig` types stay as they are (D8).

## 5. Postgres

- `buildConnectionString` becomes an exported `ConnectionString(PostgresDatabaseConfig)`. It returns `Url` when set;
  otherwise it builds the DSN with `net/url` (`url.UserPassword`, so a password containing `@`, `/` or `:` no longer
  corrupts it) and includes `sslmode`. Pool parameters are appended as today. The API and the migrator both use it.
- `Migrate()` changes:
  - table `schema_migrations` instead of `bid_schema_migrations`, matching `make migrate` and the compose job;
  - it keeps the `sslmode` already in the URL instead of forcing `disable` (default `disable` only if absent);
  - the unused `inDocker` parameter stays (no caller churn).
- `app/test` keeps calling `Migrate()`, so tests exercise the production path.
- The stale `DATABASE_SCHEMA=bid` default in `PostgresDatabaseConfig` (never read) is removed.

## 6. Redis client

`NewRedisClient(RedisClientConfig)` today pings in a loop that `break`s on the first failure and keeps sleeping while
pings succeed. New behaviour:

- It builds a go-redis `UniversalClient` from `Address` (one address gives a plain client, several a cluster client)
  and performs **no I/O**. It returns an error only for an invalid configuration (empty address list).
- `Ping(ctx) error` and `Close() error` are added to `cache.CacheClient`. go-redis reconnects by itself, so Redis
  coming up after the API is picked up without a restart.
- `main.go` pings once at startup with a short timeout and logs `redis up` or `redis down (continuing without it)`.
- The `WithConnAttempts` option and its retry loop are removed (nothing calls them).
- `client_integration_test.go` imports `parksys/...` and does not compile under `-tags integration`. It is rewritten
  against testcontainers (already in `go.mod`): set/get/delete/exists, TTL expiry, ping, and a ping that fails after
  the container stops.
- Cluster behaviour against the real 6-node cluster (including whether the `fupi-svc` ACL allows the `CLUSTER SLOTS`
  call the cluster client makes) is verified live, not in a unit test (see §10).

## 7. Health

A small registry in `pkg/infra/monitoring/health` (the existing `MonitoringHealthClient` interface there has no
implementation and is left alone):

```go
type Check struct {
    Name     string
    Required bool
    Fn       func(ctx context.Context) error
}
type Registry struct { /* checks, draining flag */ }
func (r *Registry) Add(Check)
func (r *Registry) SetDraining()
func (r *Registry) Run(ctx context.Context) Report   // checks in parallel, 2s timeout each
```

Fiber handlers (new file in a package without the `legacy` tag, not the legacy `app/api/health`):

| Request | Result |
|---|---|
| `GET /health` | `200 {"status":"ok"}` — liveness; always 200 while the process runs (keeps `fly.toml`'s check working) |
| `GET /health/ready` | all checks up: `200 {"status":"ok","checks":{"postgres":"up","redis":"up"}}` |
| | only an optional check down: `200 {"status":"degraded","checks":{"postgres":"up","redis":"down"}}` |
| | a required check down: `503 {"status":"unavailable",...}` |
| | draining: `503 {"status":"draining",...}` |

Checks: `postgres` (required, `dbClient.HealthCheck`) and `redis` (optional, `Ping`). No error text in the body (D7);
failures are logged with the error. `/health` and `/health/ready` are added to the auth middleware's public paths.
Checks run in parallel, so the endpoint's latency is bounded by the slowest check, at most 2s.

## 8. Graceful shutdown

`main()` becomes: load `.env`, `config.Load`, then `run(ctx, cfg) error` with
`ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)`. `run` is testable
in-process.

Sequence in `run`:

1. Build clients (Postgres required: failure returns an error, exit 1; Redis: ping once, log, continue).
2. Register the health checks, build the server, start `Listen` in a goroutine, send its error on a channel.
3. Wait for `ctx.Done()` or a listen error.
4. On signal: `registry.SetDraining()` so readiness turns 503, then `srv.ShutdownWithTimeout(cfg.Shutdown.Timeout)`
   (new method wrapping Fiber's `ShutdownWithTimeout`; Fiber is v2.52). In-flight requests finish or hit the deadline.
5. Close Redis, then Postgres; return nil, exit 0. A listen error or a missed deadline returns an error, exit 1.

Closing the data clients only after the server has drained avoids failing the requests still in flight.

## 9. Migrator

`app/cmd/migrator/main.go`:

1. `godotenv.Load()` (warn if absent), `config.LoadDatabase`, `config.LoadMigrations` (path), apply D6 for the password.
2. `postgres.Migrate(postgres.ConnectionString(cfg), "file://"+absPath, false)`.
3. Exit 0 on success or "no change", 1 with the error otherwise. `Migrate()` retries the connection 5 times, one second
   apart. That rides out Postgres a few seconds from ready but not a Patroni election (about 12 seconds, section 13); if the
   migrator reports a connection error, run it again.

Only `up` is implemented. `make migrate MIGRATE_DIRECTION=down` (the `migrate/migrate` image) remains the way to roll back.
`.make/dev.mk` `run.with.migrations` is stale (`cd cmd && go run -tags migrate main.go`); it becomes
`go run ./app/cmd/migrator && go run ./app/cmd`. A `make migrate.app` target is not added (YAGNI); the command is in the docs.

The migrations directory must exist at runtime. On the host that is the repo path; slice 2 copies it into the image
(or embeds it). That is a slice 2 decision.

## 10. Verification plan

Written test-first. Commands run with the suite green before each commit.

**Unit tests**

- `config`: defaults equal the stack's `.env.example` values; every override; unparseable numbers, bad ports,
  `MIN > MAX`, `REDIS_DATABASE != 0` and malformed `REDIS_ADDRESS` return errors; D6 rejects each default secret when
  `ENVIRONMENT=production` and `release`, and accepts them for `development` and `test`; passwords never appear in the
  error text.
- `postgres.ConnectionString`: `Url` wins; a password with `@/:?#%` round-trips through `pgxpool.ParseConfig`;
  `sslmode` is present.
- `health`: required vs optional outcomes, timeout (a hanging check does not block past 2s), parallelism, draining.
- health handlers: every row of the §7 table, JSON shape, no error text in the body.
- `run`: starts on port 0, `/health` is 200, cancelling the context makes readiness 503 and `run` return nil; a Postgres
  failure at startup returns an error; a slow in-flight handler completes before shutdown returns.

**Integration tests (`-tags integration`)**: the rewritten Redis client test (testcontainers); `Migrate()` against a
Postgres testcontainer, twice (second run is "no change"), reusing the existing helper.

**Live, on the host against the real stack, in both modes (`MODE=single` and `MODE=ha`):**

1. `make infra.core.up`, `go run ./app/cmd/migrator`, run it again (no change), `go run ./app/cmd`.
2. `curl /health`, `/health/ready` give `ok`.
3. Stop Redis: ready is 200 `degraded`; start it: back to `ok` without restarting the API.
4. Stop Postgres: ready is 503 (in HA, stop the primary and confirm recovery after Patroni elects a new one).
5. Send SIGTERM during a slow request: the request completes, the process exits 0, readiness was 503 while draining.
6. `POST` a registration, then `make infra.psql` and confirm the `outbox_events` row.
7. HA Redis from the host: with `make infra.hosts` applied, the cluster client connects and a set/get works; if the
   `fupi-svc` ACL blocks a cluster command, fix the ACL in `deploy/redis/start.sh` (slice 1 file) and record the change.
8. `ENVIRONMENT=production go run ./app/cmd` exits 1 naming the offending variable.

## 11. Risks and open items

- **D4 assumption** (migrator binary rather than an API flag) is for your review.
- `fly.toml` sets `ENV=release` and `PORT=8085`, but the code reads `ENVIRONMENT` and `HTTP_PORT`, so those two settings are
  ignored today. Not changed here (deployment config); worth fixing together with the Fly deployment.
- Redis down at startup is only logged, so a mis-set `REDIS_ADDRESS` shows as `degraded` rather than a crash. That is the
  chosen policy (D2); the log line at startup and the readiness body are the signal.
- The cluster client needs `redis-1..6` in `/etc/hosts` from a host app (documented, `make infra.hosts`).
- `Migrate()` has no context or timeout parameter; a hung connection is bounded only by its 5 attempts.
- `-tags legacy` stays broken (D8).

## 12. Documentation

`docs/LocalInfrastructure.md` gains "Running the app against the stack": the commands in §10 steps 1 to 2, the env
contract summary with a pointer to `.env.example`, the readiness semantics, and the migrator. The README's run
instructions reference it.

## 13. Implementation notes

Live verification, 2026-10-04, Docker Desktop for Mac (arm64). Binaries were built from the branch and run from a scratch
directory with no `.env`, so every result below is "defaults only". Only the Postgres and Redis stacks were started (the API
does not use Kafka, ADR-0015).

Single mode (`make infra.postgres.up MODE=single`, `make infra.redis.up MODE=single`):

- Migrator: on a fresh database the first run logged `migrate: up success`, the second `migrate: no change`, both exit 0;
  `schema_migrations` held version 2, `dirty = f`; `outbox_events` existed.
- API start: `/health` 200; `/health/ready` `{"status":"ok","checks":{"postgres":"up","redis":"up"}}`.
- Redis stopped: `degraded` with HTTP 200; started again: `ok` within 2 seconds, no API restart.
- Postgres stopped: `unavailable` with HTTP 503 while `/health` stayed 200; started again: `ok` within 2 seconds.
- Registration returned 201 and wrote one unsent outbox row, `user.registered` to `identity.events`.
- SIGTERM with a half-sent request: the request was answered (401) during the drain, the log shows
  `readiness: draining` and then the Postgres pool closing, and the process exited 0. An idle SIGTERM exits 0.
- Second signal: with default signal dispositions (as from a terminal) a SIGINT during a stuck drain ends the process at once
  (killed by SIGINT, 0.0s); without it the drain waits for the stuck request and exits 0. A non-interactive shell starts
  background jobs with SIGINT ignored and Go restores that after `signal.Stop`, so the drill must start the process with default
  dispositions to be meaningful.
- `ENVIRONMENT=production`: the API named `DATABASE_PASSWORD`, `REDIS_PASSWORD` and `AUTH_SECRET`, the migrator only
  `DATABASE_PASSWORD`; both exited 1 and printed no value.

HA mode (`MODE=ha`), API and migrator run in a container on the `fupi` network because `redis-1..6` in `/etc/hosts` needs sudo
(the host-side path with that line was not exercised):

- Migrator: `no change`, exit 0 (the compose job had already migrated).
- API: ready with the six-address `REDIS_ADDRESS`; the cluster client worked as `fupi-svc` with no `NOPERM`, so the Redis
  ACL needed no change. 30 keys across slots (set, get, delete through the cache client) had 0 failures.
- Redis master `redis-1` stopped: 25 readiness polls gave 24 `ok` and 1 `degraded` (a random-node `PING` can land on the dead
  node during the failover window), never a 503; `redis-5` was promoted, set/get kept working, and `redis-1` rejoined as a replica.
- Patroni leader `patroni-2` stopped: readiness went 503 at +2s and returned to `ok` at +12s with no API restart; `patroni-1`
  became leader (timeline 2) and a registration afterwards returned 201.

Findings:

- The repo's local `.env` was an untouched copy of the old `.env.example`. Its Mongo-era values override the new defaults, so the
  first migrator run dialed `localhost:27017`. The file was not modified; `docs/LocalInfrastructure.md` now says how to refresh it.
- `logger.New` (zap) builds with an empty `SamplingConfig`, which drops every line, so the server's own logger has never printed.
  `Serve`/`ServeListener` therefore log through `slog`. The logger itself is a separate follow-up.
- `.github/workflows/deploy.yml` sets an `AUTH_EXPIRE` secret and `fly.toml` sets `ENV` and `PORT`; the code reads `AUTH_EXPIRE_DELTA`,
  `ENVIRONMENT` and `HTTP_PORT`. Deployment config, not changed here.
