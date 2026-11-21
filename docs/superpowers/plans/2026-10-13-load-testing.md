# Load testing plan

**Status:** proposed. **Tool:** k6. **Targets:** none yet, so the first runs establish a baseline and the SLOs are set from it.

## Goals

1. Find the throughput and latency of the redirect and create paths on the local Docker Compose stack, in `single` and `ha` mode.
2. Find the first bottleneck (Postgres pool, Redis, CPU, Safe Browsing, Kafka producer buffer) and how the system degrades past it.
3. Make the same scripts runnable in CI and against staging by changing only environment variables.

## Constraints that shape the design

- **Tokens expire after 15 minutes, refresh tokens after 24 hours.** Any run longer than 15 minutes (load, soak) must refresh. Logging in every iteration is wrong: password hashing is deliberately CPU-expensive and would measure bcrypt, not the system.
- **Redirect is public** (`GET /{code}`, no auth). Only the `/api/v1/fupi/urls` management routes need a token.
- **Redirects publish `URLAccessed` to Kafka best-effort** (ADR-0021). A full buffer drops visits instead of slowing the request, so a drop counter is a required signal, not just latency.
- **The API rate limits per client IP** (auth, create, redirect and default route groups), so one generator would be throttled. Load runs start the API with `make loadtest.app.up`, which exports `LOADTEST_LIMITS` (the `RATE_LIMIT_<GROUP>_MAX` / `_WINDOW` env overrides, set far above the load) instead of an allowlist. The `ratelimit` scenario checks the limiter itself, with a small redirect limit.
- **Safe Browsing** is called on `URLCreated` (ADR-0022). Leave `SAFE_BROWSING_API_KEY` unset (or point it at a stub) so a load run never spends real quota.
- The Compose stack publishes the API on `127.0.0.1:8085`; infra is started with `make infra.<stack>.up MODE=single|ha`.

## Layout

```
loadtest/
  README.md               how to run, env vars, how to read results
  lib/config.js           BASE_URL, scenario profile, thresholds from env
  lib/auth.js             token pool + refresh helper
  lib/data.js             Zipf-weighted pick of a short code
  seed/seed.js            k6 script (or Go cmd) that registers users and creates URLs, writes loadtest/.data/seed.json
  scenarios/redirect.js   public redirect, cache hit / miss / 404 mix
  scenarios/create.js     POST /urls
  scenarios/mixed.js      ~95% redirect, ~4% create, ~1% list/get/update
  profiles/{smoke,load,stress,soak,spike}.json   executor settings per profile
```

`loadtest/.data/` is git-ignored. The seed output is the only state shared between runs.

## Seeding and auth

1. **Seed once, before the test.** Register N users (default 50) and log each in once. Create M URLs (default 10k) spread across them, with long expiries. Record `{users: [{email, access, refresh}], codes: [...]}`.
2. **Do not seed through the load run.** Seeding is a separate `make` target so a run is repeatable against the same data set.
3. **Token handling in the run.** Each VU is pinned to one seeded user. Before every management request it checks the token's age. When under 2 minutes remain it calls `POST /auth/oauth/token?grant_type=refresh_token`. Refresh tokens last 24 hours, so a re-seed is needed only for runs near that limit. A cap on the refresh call rate is itself a metric (`token_refreshes`).
4. Redirect scenarios need no tokens, only `codes` from the seed file.

Open question: whether a refresh rotates the refresh token. The auth helper must store whatever the response returns either way.

## Scenarios

| Scenario | Executor | What it checks |
|---|---|---|
| `redirect` | `constant-arrival-rate` (open model) | Zipf-skewed codes (s≈1.0) so a few are hot. Add 5% unknown codes for the 404 path and a first-hit warm-up phase for cache misses |
| `create` | `constant-arrival-rate` | Unique `original_url` and alias per request. 201 rate, duplicate (409) handling, DB write latency |
| `mixed` | several `constant-arrival-rate` scenarios in parallel | Realistic interference: creates and updates while redirects run |
| `ratelimit` | one VU, one iteration | `GROUP_MAX + 20` requests to an unknown code. At least `GROUP_MAX` allowed (404), no more than `GROUP_MAX` plus the window's refill, some 429s, each with `Retry-After` and `X-RateLimit-Limit`. Needs the API started with a small redirect limit |

Open-model executors are used for everything so a slow server does not reduce the offered load (avoids coordinated omission).

Checks per request: expected status (302 with a `Location`, 201, 404), and for redirects that `Location` equals the stored target. Redirects are fetched with `redirects: 0`.

## Profiles

| Profile | Shape | Purpose |
|---|---|---|
| `smoke` | 1 VU, 30s | Scripts work, seed is valid. Runs in CI on every PR that touches `app/` |
| `load` | ramp to target RPS, hold 15–30 min | Latency and errors at expected peak |
| `stress` | step up (e.g. +25% every 3 min) until thresholds fail | Breaking point and bottleneck |
| `soak` | 60–70% of the load target for 4h | Leaks, connection exhaustion, queue growth, token refresh churn |
| `spike` | 10x one hot code for 1–2 min, then back | Cache stampede, buffer drops |

**Targets are unset.** The first step is to run `stress` on a single node to find the ceiling, then define `load` as a fraction of it and set thresholds from the baseline (for example p95 +20%).

## Thresholds (initial, to be replaced by measured values)

- `http_req_failed{scenario:redirect} < 0.1%`
- `http_req_duration{scenario:redirect} p(95) < 50ms` (placeholder)
- `http_req_duration{scenario:create} p(95) < 300ms` (placeholder)
- `dropped_iterations == 0` (the generator itself keeps up; otherwise the result is invalid)

Thresholds make k6 exit non-zero, which is what lets CI gate on them.

## What to observe during a run

Start `make infra.observability.up` first. Watch: API p50/p95/p99 and error rate, Postgres connections in use vs pool size and slow queries, Redis hit ratio and CPU, Kafka producer buffer drops and `url.accessed` lag, goroutines and memory (leaks in `soak`), and container CPU/memory with `make infra.stats`. Export k6 metrics to Prometheus (remote write) so runs line up with service dashboards.

Do the redirect check the first time with `ENVIRONMENT` set the way it will be in production, since the dev compose sets `ENVIRONMENT=development`.

## Running it

```
make loadtest.seed   MODE=single           # start infra, seed users and URLs
make loadtest.smoke
make loadtest.run    PROFILE=load SCENARIO=mixed
make loadtest.clean                         # drop seed data and the test users' URLs
```

- k6 runs from the `grafana/k6` image with a pinned tag, as with the other tool images in `.make/docker.mk`, so no local install is needed. Use `--network fupi` and `BASE_URL=http://fupi-app:8085` so the load generator is on the Compose network and not going through Docker Desktop's host port forwarding, which would cap throughput.
- **CI:** `smoke` only. It starts `core-single` + `app-single`, seeds, runs, tears down. Full profiles are manual (`workflow_dispatch`) because shared runners are too noisy for latency thresholds.
- **Staging:** same scripts with `BASE_URL` and seed credentials from env. The seed step must use dedicated test accounts and the clean-up step must delete only what it created.

## Caveats

- Laptop numbers are only comparable to other laptop runs. Generator and system share the same CPU, so record the machine and Docker memory limit with each result.
- `ha` mode on a laptop mostly measures Docker overhead; use it for failover behaviour (kill a Redis master or Postgres leader mid-run), not for capacity.
- Results are stored as k6 JSON summaries under `loadtest/results/` (git-ignored); a short results note per significant run goes in `docs/`.

## Milestones

1. `loadtest/` skeleton, config, seed script, `smoke` profile, `make loadtest.*` targets. **Verify:** `make loadtest.smoke` passes against `single`.
2. `redirect` and `create` scenarios with checks and token refresh. **Verify:** a 20-minute run shows token refreshes succeeding with no 401s.
3. `stress` on single mode to find the ceiling. **Output:** a baseline note, with thresholds and `load` target set from it.
4. `mixed`, `soak` and `spike` profiles; observability dashboard for the run.
5. CI `smoke` job, plus a manual workflow for the full profiles.
6. `ha` runs with failure injection.
