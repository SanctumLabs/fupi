# Load tests

k6 scripts for the Fupi API. The plan is in [docs/superpowers/plans/2026-10-13-load-testing.md](../docs/superpowers/plans/2026-10-13-load-testing.md).

## Run locally

```bash
make infra.core.up MODE=single && make infra.app.up MODE=single   # or MODE=ha
make loadtest.seed                                                 # users + urls -> loadtest/.data/seed.json
make loadtest.smoke
```

k6 runs from the pinned `grafana/k6` image on the `fupi` Docker network (`http://fupi-app:8085`), so nothing needs installing.

## Seeding

Two seeders write the same file, so use whichever is handy:

| Command | Runs with | Notes |
|---|---|---|
| `make loadtest.seed` | Go on the host | talks to `SEED_BASE_URL` (default `http://127.0.0.1:8085`) |
| `make loadtest.seed.k6` | Docker only | talks to `LOADTEST_BASE_URL` |

Both take `USERS` (default 5) and `URLS_PER_USER` (default 20), register each user, log in once, and create their urls.
A `429` from the API is waited out using `Retry-After`. The seed file holds tokens and test passwords, so it is git-ignored.

Access tokens last 15 minutes, refresh tokens 24 hours. A refresh returns a **new** access and refresh token (the old
refresh token is not revoked: tokens are stateless JWTs). Scenarios must therefore refresh before they use the seeded
access token; `smoke.js` does so in `setup()`. Re-seed after 24 hours.

## Scenarios

```bash
make loadtest.run SCENARIO=redirect K6_ARGS="-e RATE=50 -e DURATION=5m"
make loadtest.run SCENARIO=create   K6_ARGS="-e RATE=2 -e DURATION=20m"
```

| Scenario | What it does |
|---|---|
| `smoke` | one pass over each path, 5 iterations (`make loadtest.smoke`) |
| `redirect` | public `GET /{code}` at a constant arrival rate, Zipf-weighted codes, `MISS_RATIO` (default 5%) unknown codes |
| `create` | `POST /urls` at a constant arrival rate; each VU is pinned to a seeded user and refreshes its token before it expires |
| `floor` | unauthenticated `GET /urls`, rejected before any DB or Redis call: the noise floor of generator + Docker network + HTTP server |
| `ratelimit` | one VU sends `GROUP_MAX + 20` requests for an unknown code and checks the redirect limit (see [Rate limits](#rate-limits)) |

`make loadtest.stress SCENARIO=redirect START=200 STEP=200 MAX=5000 STEP_DURATION=45s` runs a scenario at a rising rate,
one k6 run per step, prints one row per step (achieved rate, p50/p95/p99, failure %, dropped iterations) and stops at the
first step that crosses a threshold. Per-step summaries land in `loadtest/results/` (git-ignored). Add
`P95_MS=...` to change the latency threshold.

`rate_limited` counts 429s apart from other failures, `token_refreshes` counts refresh calls, and `unauthorized` counts
401s on the management routes (it must stay 0). A threshold failure makes k6, and so `make`, exit non-zero.

## Rate limits

The API limits requests per client IP in four route groups (auth, create, redirect, default), so a load run from one
generator would hit the limit and measure 429s. `make loadtest.app.up` starts the API with `LOADTEST_LIMITS`, which
raises the limits far above anything a run sends:

```bash
make loadtest.app.up MODE=single      # or MODE=ha; same as infra.app.up, with LOADTEST_LIMITS exported
make loadtest.app.up MODE=single LOADTEST_LIMITS="RATE_LIMIT_AUTH_MAX=1000 RATE_LIMIT_REDIRECT_MAX=5000"   # your own
```

The default is `RATE_LIMIT_AUTH_MAX=100000 RATE_LIMIT_CREATE_MAX=100000 RATE_LIMIT_REDIRECT_MAX=1000000 RATE_LIMIT_DEFAULT_MAX=100000`
(windows stay at their default, 60 seconds). A limit is `RATE_LIMIT_<GROUP>_MAX` requests per `RATE_LIMIT_<GROUP>_WINDOW`
seconds; the app's own defaults are 10, 30, 300 and 100 per minute. Run `make infra.app.up` afterwards to go back to them.
`RATE_LIMIT_ENABLED=false` turns the limiter off altogether.

`make loadtest.ratelimit` checks the limiter itself against the redirect group. It needs a small limit, since the
default would take a million requests, and a window much longer than the run so the bucket does not refill meanwhile.
Start the API with that limit first; the target refuses to run unless `GROUP_MAX` and `GROUP_WINDOW_S` are given:

```bash
make loadtest.app.up MODE=single LOADTEST_LIMITS="RATE_LIMIT_AUTH_MAX=100000 RATE_LIMIT_CREATE_MAX=100000 RATE_LIMIT_DEFAULT_MAX=100000 RATE_LIMIT_REDIRECT_MAX=200 RATE_LIMIT_REDIRECT_WINDOW=3600"
make loadtest.ratelimit GROUP_MAX=200 GROUP_WINDOW_S=3600
```

It sends `GROUP_MAX + EXTRA` (default 20) requests to `GET /zzzzzzzz` and checks that at least `GROUP_MAX` were allowed
(404), no more than `GROUP_MAX` plus what the window refills during the run, that some were limited (429), and that every
429 carries `Retry-After` and `X-RateLimit-Limit`. `GROUP_MAX` and `GROUP_WINDOW_S` must equal the API's
`RATE_LIMIT_REDIRECT_MAX` and `_WINDOW`. Run it on a fresh bucket: a second run inside the window finds the bucket empty,
fails `allowed >= GROUP_MAX` and exits non-zero, which shows the check detects an exhausted bucket. The bucket is per
client IP and lives in Redis, so wait out the window (or delete the `rate:redirect:ip:*` key) before running again.

## Settings

| Variable | Default | Meaning |
|---|---|---|
| `BASE_URL` | `http://127.0.0.1:8085` | API address (the make targets set it for the container) |
| `SEED_FILE` | `/loadtest/.data/seed.json` | seed file path inside the k6 container |
| `ITERATIONS` | `5` | smoke iterations |
| `RATE`, `DURATION` | `10` / `1m` (redirect), `2` / `1m` (create) | arrival rate per second and run length |
| `PRE_VUS`, `MAX_VUS` | `20`/`200` (redirect), `10`/`100` (create) | VU pool; a `dropped_iterations` failure means raise `MAX_VUS` |
| `MISS_RATIO`, `ZIPF_S` | `0.05`, `1.0` | share of unknown codes, skew of code popularity |
| `REFRESH_MARGIN_S` | `120` | refresh the access token this many seconds before it expires |

To point at another environment, set `LOADTEST_BASE_URL` (untested so far: the container must be able to reach that address, and `K6_NETWORK` may need changing).

## Reading results on a laptop

Generator, API, Postgres, Redis and Kafka share one machine and Docker Desktop's VM, so tails (p99 and up) vary a lot
between identical runs: the same 600 req/s redirect run gave p99 from 28ms to 270ms. Run `floor` at the same rate first;
a tail that is also there is not the application. Repeat a step before trusting a failure, and record the machine with
the result.

A threshold on `dropped_iterations` means k6 ran out of VUs while responses stalled: the offered load was not delivered,
so the step is invalid even when latency looks fine.

## Diagnosing a slow database

When a scenario saturates (achieved rate below the offered rate, p50 climbing) find out which side is the limit before
changing anything.

1. **Who is busy.** `make infra.stats` shows CPU per container during the run. On the create path Postgres was at
   ~120% CPU and the app at ~70%.
2. **Which statements.** `pg_stat_statements` is preloaded in both modes (single: the `-c shared_preload_libraries`
   flag, HA: `patroni.yml`) and the extension is created in the `postgres` database by `deploy/postgres/roles.sh`, so
   it covers every database but must be read as the superuser:

   ```bash
   make infra.pgstat.reset MODE=single      # just before the run
   make loadtest.run SCENARIO=create K6_ARGS="-e RATE=200 -e DURATION=60s"
   make infra.pgstat MODE=single TOP=10     # calls, total/mean ms, buffer hits/reads, WAL bytes per call
   ```

   Compare `mean_ms` with what the statement costs alone (next step): a large gap means contention, not work.
3. **What a statement costs alone.** `make infra.psql.admin` opens psql as the superuser. Wrap `EXPLAIN` in a
   transaction you roll back, because `ANALYZE` really runs the statement:

   ```sql
   BEGIN;
   EXPLAIN (ANALYZE, BUFFERS)
   INSERT INTO urls (id, user_id, short_code, original_url, status, expires_on)
   SELECT gen_random_uuid(), id, 'xx' || substr(md5(random()::text), 1, 8),
          'https://example.com/explain/' || md5(random()::text), 'ACTIVE', now() + interval '30 days'
   FROM users LIMIT 1;
   ROLLBACK;
   ```

   The plan ends with the time spent in triggers and foreign-key checks. The first run is cold (planning, cache
   misses): repeat it, or loop 300 inserts in a `DO` block and divide.
4. **What it is waiting for.** While the load runs, sample `pg_stat_activity`:

   ```sql
   SELECT coalesce(wait_event_type,'cpu') || ':' || coalesce(wait_event,'-') AS waiting_on, count(*)
   FROM pg_stat_activity WHERE datname = 'fupidb' AND state = 'active' AND pid <> pg_backend_pid()
   GROUP BY 1 ORDER BY 2 DESC;
   ```

   Run it every second or so and add up the samples. `LWLock:BufferContent` and `LWLock:LockManager` mean many
   backends fight over the same pages or locks; `IO:WalSync` is commit latency.

The application role cannot read `pg_stat_statements` (it lacks `pg_read_all_stats`); the HA targets use the same
commands through `patroni-1` and have not been tried.

## Findings: the create path (single mode, one laptop)

Measured with `make loadtest.stress SCENARIO=create`, 3 runs per step, 5% dropped-iteration budget.

| What was checked | Result |
|---|---|
| Short-code sequence | Not the bottleneck. The generator claims blocks of 1000 codes, so 61,744 creates made 64 `nextval` calls at 1.7ms each. |
| Outbox insert (`outbox_events`) | Cheap: 0.15-0.8ms mean and about 1.1KB of WAL, a small share of a create. |
| Uniqueness checks and indexes | 6 indexes (3 unique) on `urls`. Alone, an insert costs 0.07ms with the audit trigger disabled, so the indexes are not expensive by themselves. |
| `INSERT INTO urls` under load | Dominates: 3-6ms mean under load against 0.26ms alone, about 2-3.7KB of WAL per call. The gap is contention: `LWLock:BufferContent` in about half of the samples, then `LockManager`, `WalSync` and `WALWrite`. |
| Audit trigger on `urls` (`create_log_on_modify`) | Writes a ~1.7KB row to `audit_trail`, which has 5 indexes including two GIN indexes. Costs 0.19ms of the 0.26ms warm insert. Disabling it moved the passing ceiling from about 250 to about 350 creates/s (fails at 300 and 400 respectively) in one A/B run. |

So the create ceiling on this machine is about 250 creates/s with the audit trigger and about 350 without it, and the
audit write (heap plus GIN indexes on the hottest table) is the first thing to look at. An earlier run measured the ceiling
at about 150/s, so treat these as indicative: they vary between runs and with the state of the database.
Whether the trigger can be made cheaper (drop the GIN indexes, write the audit row asynchronously) is a product
decision, not made here.

## HA mode and failure injection (not run yet)

HA is for failover behaviour, not capacity: on one laptop it mostly measures Docker overhead. It needs far more memory
than single mode (3 Postgres + etcd + haproxy, 6 Redis, 3 Kafka) and uses the same ports as single mode, so stop the
single stacks first (`make infra.clean` removes their data; `infra.<stack>.down` keeps it). Plan for one run per
failure, with `mixed` at about half of its single-mode ceiling so a failover is not confused with saturation:

```bash
make infra.core.up infra.app.up MODE=ha            # app-ha lists all six Redis nodes and three Kafka brokers
make loadtest.seed USERS=20 URLS_PER_USER=100
make loadtest.run SCENARIO=mixed K6_ARGS="-e RATE=200 -e DURATION=10m"   # in one terminal
# in another, after about 2 minutes, one failure at a time:
make infra.patroni.list MODE=ha                    # find the leader
docker stop fupi-patroni-1                         # Postgres leader: expect a short burst of 5xx on create/manage, none on cached redirects
docker start fupi-patroni-1
docker stop fupi-redis-1                           # a Redis master: expect redirects to fall back to Postgres, and the rate limiter fallback
docker start fupi-redis-1
docker stop fupi-kafka-1                           # a broker: redirects must be unaffected (analytics is best-effort, ADR-0021)
docker start fupi-kafka-1
```

Record, per failure: seconds of elevated errors (`http_req_failed` over time, so add `--out` to Prometheus or read the
app's access log), whether the stack recovered without restarting the app, and the `token_refreshes` and `unauthorized`
counters (they must not move). Container names depend on the compose project; check `make infra.ps MODE=ha`.
