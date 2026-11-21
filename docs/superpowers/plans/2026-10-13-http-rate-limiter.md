# HTTP Rate Limiter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Per-route-group HTTP rate limiting, atomic across instances in Redis, with a per-instance in-memory fallback, configurable by env, observable, and checked by a k6 scenario.

**Architecture:** One fiber middleware dispatches each request to a per-group `limiter.New(...)` handler. Each group's handler is a custom `limiter.LimiterHandler` that asks `redis_rate` (GCRA, one Lua call) and, on a Redis error, delegates to fiber's own `limiter.FixedWindow` in memory, skipping Redis for a cooldown. The middleware runs after auth, so `create` can key on the user ID, and before idempotency, so a 429 is never cached.

**Tech Stack:** Go, fiber v2.52.15 (`middleware/limiter`), `github.com/go-redis/redis_rate/v10` v10.0.1 on go-redis v9, OTel metric/trace API, slog, testify, testcontainers Redis module, k6.

**Spec:** [docs/superpowers/specs/2026-10-13-http-rate-limiter-design.md](../specs/2026-10-13-http-rate-limiter-design.md)

## Global Constraints

- `app/pkg/infra/...` never imports `app/api/...` or `app/config`; route knowledge is passed in from `app/api` / `app/cmd`.
- Default limits: auth 10/60s, create 30/60s, redirect 300/60s, default 100/60s. `redis_rate.Limit{Rate: max, Burst: max, Period: window}`.
- 429 body is exactly `{"message":"too many requests"}`, with `Retry-After` (whole seconds, rounded up) and `X-RateLimit-Limit`, `X-RateLimit-Remaining`, `X-RateLimit-Reset`.
- Never limited: `/health`, `/health/ready` (`probes.LivePath`, `probes.ReadyPath`), and anything with prefix `/docs`.
- A Redis error never fails a request and never lets it through unlimited.
- Client IP or user ID never appears as a metric attribute; spans only.
- Env durations follow the repo's integer-unit convention (like `REDIRECT_CACHE_TTL`): windows and retry-after in seconds, the Redis timeout in milliseconds. This replaces the spec's `1m`-style examples.
- The user commits their own in-progress work. Commit only files a task creates or that have no uncommitted user changes at the start of the task (check `git status`). Files known to carry the user's uncommitted work: `loadtest/**`, `.make/loadtest.mk`, `docs/superpowers/plans/2026-10-13-load-testing.md`, `docs/api/api_collections/*`. Edit those when a task says so, but leave them uncommitted and list them in the task report. `app/pkg/infra/server/middleware/limiter.go` and the one-line `server.go` change are the user's stub, which this plan replaces, so committing them is fine.
- Match the surrounding style: doc comments on exported identifiers, `slog` with context, test names `TestThing_Behaviour`.

## Review Focus

1. **Redis that hangs rather than refuses** (paused, overloaded, network blackhole): every request must still answer within about `RATE_LIMIT_REDIS_TIMEOUT_MS`, not the go-redis read timeout. Test owned by Task 5 (`DEBUG SLEEP`).
2. **A spoofed `X-Forwarded-For` from a client behind a trusted proxy**: the key must be the rightmost untrusted address, and garbage entries must fall back to the peer. Test owned by Task 2.
3. **A POST with `X-Idempotency-Key` that is limited, then retried after refill**: it must succeed, not replay the 429. Test owned by Task 6.
4. **Concurrent requests during the fallback switch**: exactly one Warn log on entering fallback and one Info log on leaving, however many requests race. Test owned by Task 4.
5. **Routes that look like a redirect but aren't** (`/docs`, `/health`, a 3–12 char path on the wrong method): they must go to their own group or be exempt, not the redirect bucket. Test owned by Task 6.

---

### Task 1: Configuration

**Files:**
- Create: `app/config/ratelimit.go`
- Modify: `app/config/loader.go` (add `boolean` reader; add `RateLimit` to `App` and to `Load`), `app/config/app.go` (`ServerSettings` + `LoadServer`)
- Modify: `.env.example`, `docs/Deployment.md` (env table)
- Test: `app/config/ratelimit_test.go`, `app/config/app_test.go`

**Interfaces:**
- Produces:
  - `type RateLimitRule struct { Max int; Window time.Duration }`
  - `type RateLimitSettings struct { Enabled bool; Auth, Create, Redirect, Default RateLimitRule; RedisTimeout, RedisRetryAfter time.Duration }`
  - `func LoadRateLimit(lookup Lookup) (RateLimitSettings, error)`
  - `App.RateLimit RateLimitSettings`
  - `ServerSettings.ProxyHeader string`, `ServerSettings.TrustedProxies []netip.Prefix` (a bare IP becomes a /32 or /128)
  - `func (r *reader) boolean(key string, def bool) bool` (`strconv.ParseBool`; bad value → `r.fail("%s must be true or false", key)`)

Env vars: `RATE_LIMIT_ENABLED` (true), `RATE_LIMIT_{AUTH,CREATE,REDIRECT,DEFAULT}_MAX` (10/30/300/100), `RATE_LIMIT_{…}_WINDOW` seconds (60 each), `RATE_LIMIT_REDIS_TIMEOUT_MS` (50), `RATE_LIMIT_REDIS_RETRY_AFTER` seconds (5), `SERVER_PROXY_HEADER` (unset), `SERVER_TRUSTED_PROXIES` (unset, comma list of IPs/CIDRs).

- [ ] **Step 1: Write the failing tests** in `ratelimit_test.go`:
  - `TestLoadRateLimit_Defaults`: `LoadRateLimit(lookupOf(nil))` equals `{Enabled: true, Auth: {10, 60s}, Create: {30, 60s}, Redirect: {300, 60s}, Default: {100, 60s}, RedisTimeout: 50ms, RedisRetryAfter: 5s}`.
  - `TestLoadRateLimit_Overrides`: every variable set to a non-default value is read.
  - `TestLoadRateLimit_RejectsInvalidValues`: table of `MAX=0`, `MAX=-1`, `WINDOW=0`, `REDIS_TIMEOUT_MS=0`, `REDIS_RETRY_AFTER=0`, `ENABLED=maybe`; each returns an error naming the variable.

  And in `app_test.go`:
  - `TestLoadServer_TrustedProxies`: `"10.0.0.1, 192.168.0.0/16, ::1"` → prefixes `10.0.0.1/32`, `192.168.0.0/16`, `::1/128`.
  - `TestLoadServer_RejectsProxySettings`: a header with no trusted proxies is an error, and `"not-an-ip"` is an error.
- [ ] **Step 2: Run** `go test ./app/config/...`. Expected: FAIL to compile (`LoadRateLimit` undefined).
- [ ] **Step 3: Implement** `ratelimit.go`, `boolean`, the `ServerSettings` fields and validation in `LoadServer`, and wire `LoadRateLimit` into `Load` like `LoadRedirectCache`. Add the variables to `.env.example` with their defaults, so `TestLoad_EnvExampleDocumentsTheDefaults` keeps passing. Add rows to the env table in `docs/Deployment.md`.
- [ ] **Step 4: Run** `go test ./app/config/...`. Expected: PASS, including the existing `TestLoad_*`.
- [ ] **Step 5: Commit** `feat(config): rate limit and trusted proxy settings`.

---

### Task 2: Client IP extraction

**Files:**
- Create: `app/pkg/infra/server/middleware/client_ip.go`
- Test: `app/pkg/infra/server/middleware/client_ip_test.go`

**Interfaces:**
- Produces:
  - `type ClientIPConfig struct { Header string; TrustedProxies []netip.Prefix }`
  - `func ClientIP(c *fiber.Ctx, cfg ClientIPConfig) string`

Rules: the peer is `c.Context().RemoteIP()`. Return the peer unless `cfg.Header != ""` and the peer is inside a trusted prefix. Otherwise split the header value on `,` and walk from the right. Return the first entry that parses as an IP (`netip.ParseAddr` after trimming spaces) and is not trusted. If none qualifies, or any entry walked over doesn't parse, return the peer. A single-value header is just a one-entry list.

- [ ] **Step 1: Write the failing table test** `TestClientIP`. Drive it through a fiber app with `app.Test(req)`, setting the peer with `req.RemoteAddr` and returning `ClientIP` as the body. Cases (trusted = `10.0.0.0/8`):

| case | peer | header cfg | header value | want |
|---|---|---|---|---|
| no header configured | 203.0.113.7 | "" | — | 203.0.113.7 |
| untrusted peer ignores header | 203.0.113.7 | X-Forwarded-For | 1.2.3.4 | 203.0.113.7 |
| trusted peer, single value | 10.0.0.2 | X-Real-IP | 198.51.100.9 | 198.51.100.9 |
| XFF rightmost untrusted | 10.0.0.2 | X-Forwarded-For | `6.6.6.6, 198.51.100.9, 10.0.0.3` | 198.51.100.9 |
| spoofed leftmost ignored | 10.0.0.2 | X-Forwarded-For | `1.1.1.1, 198.51.100.9` | 198.51.100.9 |
| garbage entry → peer | 10.0.0.2 | X-Forwarded-For | `198.51.100.9, nonsense` | 10.0.0.2 |
| all trusted → peer | 10.0.0.2 | X-Forwarded-For | `10.0.0.5` | 10.0.0.2 |
| header absent → peer | 10.0.0.2 | X-Forwarded-For | — | 10.0.0.2 |
| IPv6 | 10.0.0.2 | X-Forwarded-For | `2001:db8::1` | 2001:db8::1 |

- [ ] **Step 2: Run** `go test ./app/pkg/infra/server/middleware/ -run TestClientIP`. Expected: FAIL (undefined).
- [ ] **Step 3: Implement** `ClientIP`.
- [ ] **Step 4: Run** it again. Expected: PASS.
- [ ] **Step 5: Commit** `feat(server): trusted-proxy aware client IP`.

---

### Task 3: Rate limiter middleware (groups, keys, 429, in-memory)

Replaces the stub in `limiter.go`. In this task there is no Redis: a group's handler is plain fiber `limiter.New` with the default fixed window in memory. Task 4 swaps the algorithm in.

**Files:**
- Modify (rewrite): `app/pkg/infra/server/middleware/limiter.go`
- Test: `app/pkg/infra/server/middleware/limiter_test.go`

**Interfaces:**
- Consumes: `ClientIP`, `ClientIPConfig` (Task 2).
- Produces:
  - `type RateLimitRule struct { Max int; Window time.Duration }`
  - `type RateLimitGroup struct { Name string; Rule RateLimitRule; Match func(*fiber.Ctx) bool; Key func(*fiber.Ctx) string }`. `Match == nil` matches all. `Key` returns e.g. `"user:<id>"`; `nil` or `""` means `"ip:" + ClientIP(...)`.
  - `type RateLimiterConfig struct { Groups []RateLimitGroup; ExemptPaths []string; ExemptPrefixes []string; ClientIP ClientIPConfig; Redis RateAllower; RedisTimeout time.Duration; RedisRetryAfter time.Duration; MeterProvider metric.MeterProvider }`. The `Redis*` and `MeterProvider` fields are declared here and used from Task 4 and Task 5 on.
  - `type RateAllower interface { Allow(ctx context.Context, key string, limit redis_rate.Limit) (*redis_rate.Result, error) }`. `*redis_rate.Limiter` satisfies it. Add the module with `go get github.com/go-redis/redis_rate/v10@v10.0.1`.
  - `func RateLimiterMiddleware(cfg RateLimiterConfig) (fiber.Handler, error)`
  - `const LimitedMessage = "too many requests"`

Behaviour: exempt paths and prefixes pass straight to `c.Next()`. The first matching group handles the request; if nothing matches, the request passes unlimited. Each group's limiter key is `<group name>:<key>`. `LimitReached` writes 429 with `{"message": LimitedMessage}`; fiber's fixed window already sets `Retry-After` and the `X-RateLimit-*` headers.

- [ ] **Step 1: Write failing tests** (each builds a fiber app with the middleware and a few routes; requests come from a fixed `RemoteAddr`):
  - `TestRateLimiter_LimitsAfterMax`: group `{Name: "auth", Rule: {2, time.Minute}}`. Requests 1 and 2 → 200, request 3 → 429. The body JSON-decodes to `message == "too many requests"`. `Retry-After` is a positive integer; `X-RateLimit-Limit == "2"`.
  - `TestRateLimiter_ExemptPathsAreNeverLimited`: max 1, `ExemptPaths: {"/health"}`, `ExemptPrefixes: {"/docs"}`. Five requests to each of `/health` and `/docs/index.html` → all 200, no `X-RateLimit-Limit` header.
  - `TestRateLimiter_GroupsHaveSeparateBuckets`: groups `a` (matches `/a`, max 1) and `b` (catch-all, max 1). `/a` 200, `/b` 200, `/a` 429, `/b` 429.
  - `TestRateLimiter_KeysSeparateClients`: max 1. Two different `RemoteAddr`s each get 200.
  - `TestRateLimiter_GroupKeyFunc`: `Key` returns `"user:" + header("X-User")`. Same IP, users u1 and u2 → both 200; u1 again → 429. When `Key` returns `""`, the request falls back to the IP bucket.
- [ ] **Step 2: Run** `go test ./app/pkg/infra/server/middleware/ -run TestRateLimiter`. Expected: FAIL.
- [ ] **Step 3: Implement** `RateLimiterMiddleware`. Build one `limiter.New(limiter.Config{Max, Expiration: Rule.Window, KeyGenerator, LimitReached})` per group at construction time, never per request.
- [ ] **Step 4: Run** the tests. Expected: PASS. Also run `go build ./...`. It fails at `server.go`, where the call site uses the old signature. Fix the call site minimally by deleting the `app.Use(middleware.RateLimiterMiddleware(...))` line, since Task 6 rewires it.
- [ ] **Step 5: Commit** `feat(server): per-group rate limiter middleware`.

---

### Task 4: Redis-backed algorithm with fallback

**Files:**
- Create: `app/pkg/infra/server/middleware/limiter_redis.go`
- Test: `app/pkg/infra/server/middleware/limiter_redis_test.go`

**Interfaces:**
- Consumes: `RateLimiterConfig`, `RateAllower` (Task 3).
- Produces:
  - `type redisRateLimiter struct { …; backend *rateLimitBackend }`, which implements `limiter.LimiterHandler`.
  - `type rateLimitBackend struct`. It is shared by all groups, so one Redis outage flips them all, and it is built once in `RateLimiterMiddleware`. Methods:
    - `useRedis(now time.Time) bool`
    - `failed(ctx context.Context, now time.Time, err error)`, which logs Warn only on the healthy→fallback transition
    - `succeeded(ctx context.Context)`, which logs Info only on the fallback→healthy transition
    - `active() bool`
  - An unexported `now func() time.Time` field on `RateLimiterConfig` (default `time.Now`) for tests.
- When `cfg.Redis != nil`, `RateLimiterMiddleware` sets `LimiterMiddleware: &redisRateLimiter{…}` on each group's `limiter.Config`. When it is nil, groups keep the Task 3 behaviour.

Per request, `redisRateLimiter.New(cfg)` returns a handler. The handler builds `fallback := limiter.FixedWindow{}.New(cfg)` once, in `New`, not per request. Then:
1. If `!backend.useRedis(now)`, return `fallback(c)`.
2. Call `Allow(ctx, key, redis_rate.Limit{Rate: Max, Burst: Max, Period: Expiration})`, where `ctx` is `context.WithTimeout(c.UserContext(), RedisTimeout)`. On error: `backend.failed(...)`, then `return fallback(c)`. On success: `backend.succeeded(...)`.
3. Set `X-RateLimit-Limit` = Max, `X-RateLimit-Remaining` = `res.Remaining`, `X-RateLimit-Reset` = `ceil(res.ResetAfter.Seconds())`.
4. If `res.Allowed == 0`: set `Retry-After` = `ceil(res.RetryAfter.Seconds())` with a minimum of 1, and `return cfg.LimitReached(c)`. Otherwise `return c.Next()`.

The Warn/Info transitions use `atomic.Bool` `CompareAndSwap`, so concurrent requests log once.

- [ ] **Step 1: Write failing tests** using `fakeAllower`. It returns a scripted `*redis_rate.Result` or error, counts calls, and blocks on a channel when told to.
  - `TestRedisLimiter_AllowedSetsHeaders`: result `{Allowed: 1, Remaining: 4, ResetAfter: 1500ms}` → 200, `X-RateLimit-Remaining: 4`, `X-RateLimit-Reset: 2`. The fake saw key `auth:ip:203.0.113.7` and `Limit{Rate: 5, Burst: 5, Period: time.Minute}`.
  - `TestRedisLimiter_DeniedReturns429WithRetryAfter`: `{Allowed: 0, RetryAfter: 2100ms}` → 429, `Retry-After: 3`, body message `too many requests`.
  - `TestRedisLimiter_ErrorFallsBackToMemory`: the fake returns an error, max 2. Three requests → 200, 200, 429 (the in-memory fixed window). The fake was called once: the later requests skip Redis during the cooldown.
  - `TestRedisLimiter_RetriesRedisAfterCooldown`: after an error, advance the test clock past `RedisRetryAfter`. The fake now succeeds; the next request calls it, and `backend.active()` is false.
  - `TestRedisLimiter_HungRedisDoesNotStall`: the fake blocks until its ctx is done, with `RedisTimeout: 20ms`. The request returns 200 within 200ms.
  - `TestRedisLimiter_LogsTransitionsOnce` (Review Focus 4): use `logTo` from `access_log_test.go`. 50 concurrent requests while the fake errors give exactly 1 Warn line with msg `rate limiter falling back to memory`. After the cooldown and a success, exactly 1 Info line `rate limiter back on redis`.
- [ ] **Step 2: Run** `go test ./app/pkg/infra/server/middleware/ -run TestRedisLimiter`. Expected: FAIL.
- [ ] **Step 3: Implement** `limiter_redis.go` and wire it into `RateLimiterMiddleware`.
- [ ] **Step 4: Run** `go test ./app/pkg/infra/server/middleware/...`. Expected: PASS, with Task 3's tests still green.
- [ ] **Step 5: Commit** `feat(server): atomic redis_rate limiting with in-memory fallback`.

---

### Task 5: Observability, Redis client accessor, integration tests

**Files:**
- Create: `app/pkg/infra/server/middleware/limiter_metrics.go`
- Modify: `limiter.go`, `limiter_redis.go` (record the decision and span attributes), `app/pkg/infra/cache/redis/client.go`
- Test: `app/pkg/infra/server/middleware/limiter_metrics_test.go`, `app/pkg/infra/server/middleware/limiter_integration_test.go` (`//go:build integration`)

**Interfaces:**
- Produces:
  - Metrics on the meter `github.com/sanctumlabs/fupi/app/pkg/infra/server/middleware` (follow `urlexpiry/metrics.go`):
    - `http.ratelimit.requests` (Int64Counter; attributes `group`, `outcome` = `allowed`/`limited`, `backend` = `redis`/`memory`)
    - `http.ratelimit.backend.errors` (Int64Counter; attribute `group`)
    - `http.ratelimit.fallback.active` (Int64ObservableGauge, 1/0 from `backend.active()`)
  - Attributes on `trace.SpanFromContext(c.UserContext())`: `ratelimit.group`, `ratelimit.key`, `ratelimit.backend`, `ratelimit.limited` (bool), `ratelimit.remaining` (int)
  - `func UniversalClient(client cache.CacheClient) (redisGo.UniversalClient, bool)` in `app/pkg/infra/cache/redis/client.go`. It type-asserts the package's own `*redisClient`.

The outcome on the fallback path: wrap `LimitReached` to set a `Locals` flag before writing the 429, and read the flag after the fallback handler returns. Read `ratelimit.remaining` on the fallback path from the `X-RateLimit-Remaining` response header.

- [ ] **Step 1: Write failing unit tests** with an SDK `metric.NewManualReader()` provider and an `sdktrace` provider with `tracetest.NewSpanRecorder()`, plus a parent span on `c.UserContext()` set by a tiny test middleware.
  - `TestRateLimiterMetrics_CountsDecisions`: with the fake allowing once then denying, `http.ratelimit.requests` has `{group=auth, outcome=allowed, backend=redis}: 1` and `{…limited…}: 1`. No attribute key is `key` or any IP.
  - `TestRateLimiterMetrics_FallbackSignals`: with the fake erroring, `backend.errors{group=auth} == 1`, the gauge reads 1, and the request is counted with `backend=memory`.
  - `TestRateLimiterSpan_Attributes`: the recorded span has `ratelimit.group=auth`, `ratelimit.key=auth:ip:203.0.113.7`, `ratelimit.limited=false`, `ratelimit.remaining=4`.
- [ ] **Step 2: Run** them. Expected: FAIL.
- [ ] **Step 3: Implement** the metrics, the span attributes and `UniversalClient`.
- [ ] **Step 4: Run** `go test ./app/pkg/infra/...`. Expected: PASS.
- [ ] **Step 5: Write the integration tests**, following `startRedis` in `cache/redis/client_integration_test.go`, with `redis_rate.NewLimiter(universal)`:
  - `TestRateLimiterRedis_SharedAcrossInstances`: two separate fiber apps share one Redis; rule 10 per hour. 40 concurrent requests, split across both apps, from the same IP → exactly 10 return 200.
  - `TestRateLimiterRedis_HungRedisFallsBackAndRecovers` (Review Focus 1): `RedisTimeout: 50ms`, `RedisRetryAfter: 1s`. In a goroutine, `container.Exec(ctx, []string{"redis-cli", "DEBUG", "SLEEP", "2"})`; wait about 100ms. A request returns 200 in under 500ms and `fallback.active` is 1. After the sleep ends plus the retry interval, a request is served by Redis: `backend=redis` is counted and the gauge reads 0.
- [ ] **Step 6: Run** `go test -tags integration ./app/pkg/infra/server/middleware/...`. Expected: PASS. Docker is required.
- [ ] **Step 7: Commit** `feat(server): rate limiter metrics, spans and redis integration tests`.

---

### Task 6: Wiring into the server and main

**Files:**
- Modify: `app/pkg/infra/server/config.go` (`ServerConfig.Guards []fiber.Handler`), `app/pkg/infra/server/server.go`
- Create: `app/api/middleware/ratelimit.go`
- Modify: `app/cmd/main.go`
- Test: `app/api/middleware/ratelimit_test.go`, `app/pkg/infra/server/server_test.go` (create it if absent)

**Interfaces:**
- Consumes: `RateLimiterMiddleware`, `RateLimitGroup`, `RateLimitRule`, `RateLimiterConfig`, `ClientIPConfig` (Tasks 2–5); `config.RateLimitSettings`, `ServerSettings.ProxyHeader/TrustedProxies` (Task 1); `cacheredis.UniversalClient` (Task 5).
- Produces:
  - `ServerConfig.Guards []fiber.Handler`. `NewServer` registers them, in order, right after `HelmetMiddleware()` and before `IdempotencyMiddleware()`.
  - In `app/api/middleware`: `type RateLimitRules struct { Auth, Create, Redirect, Default servermw.RateLimitRule }` and `func RateLimitGroups(baseURI string, redirect *regexp.Regexp, rules RateLimitRules) []servermw.RateLimitGroup`. The groups, in order:
    - `auth`: path has prefix `baseURI + "/auth/"`
    - `create`: `POST` and path `== baseURI + "/urls"`. `Key` is `"user:" + Locals(UserIDContextKey).(string)` when non-empty.
    - `redirect`: `GET` and `redirect.MatchString(path)`
    - `default`: catch-all

`main.go`:
- Build `limit, err := servermw.RateLimiterMiddleware(...)` from `cfg.RateLimit`, converting each `config.RateLimitRule` to `servermw.RateLimitRule` (same fields). Exempt `probes.LivePath`, `probes.ReadyPath` and the `/docs` prefix; set `ClientIP` from `cfg.Server`. When the client is Redis, set `Redis: redis_rate.NewLimiter(universal)`.
- Move the existing `AuthMiddleware(...)` construction into `Guards: []fiber.Handler{auth, limit}`, and drop it from the guards when `!cfg.RateLimit.Enabled`. Remove the later `srv.Use(AuthMiddleware…)`.
- Log one Info line at startup: `rate limiting`, with `enabled` and the four rules.

- [ ] **Step 1: Write failing tests.**
  - `TestRateLimitGroups_Routing` (Review Focus 5), a table over `(method, path) → group name`:
    - `POST /api/v1/fupi/auth/login` → auth
    - `GET /api/v1/fupi/auth/verify` → auth
    - `POST /api/v1/fupi/urls` → create
    - `GET /api/v1/fupi/urls` → default
    - `GET /abc123` → redirect
    - `POST /abc123` → default
    - `GET /docs` → default (exemption is the middleware's job, tested in Task 3)
    - `GET /api/v1/fupi/urls/xyz` → default

    Assert by the first `Match` that returns true.
  - `TestRateLimitGroups_CreateKeysOnUser`: with `Locals(UserIDContextKey, "u-1")`, `Key` returns `"user:u-1"`; with no local, it returns `""`.
  - `TestServer_GuardsRunBeforeIdempotency` (Review Focus 3): `NewServer` with `Guards` = a rate limiter of max 1 per minute, using a fake `RateAllower` whose verdict the test flips, and a `POST /things` route returning 201.
    1. `POST` with `X-Idempotency-Key: <uuid A>` → 201.
    2. The fake denies. `POST` with `<uuid B>` → 429.
    3. The fake allows. `POST` with `<uuid B>` → 201, not a replayed 429.
- [ ] **Step 2: Run** `go test ./app/api/middleware/ ./app/pkg/infra/server/`. Expected: FAIL.
- [ ] **Step 3: Implement** `Guards`, `RateLimitGroups` and the `main.go` wiring.
- [ ] **Step 4: Run** `go test ./...` and `go vet ./...`. Expected: PASS. Then `go test -tags integration ./app/cmd/...`. Expected: PASS; with Redis down, the API still serves.
- [ ] **Step 5: Smoke by hand.** `make infra.core.up MODE=single`, then run the API, then fire 11 `POST /api/v1/fupi/auth/login` in a loop with `curl -s -o /dev/null -w '%{http_code}\n'`. Expected: ten non-429 statuses, then `429`.
- [ ] **Step 6: Commit** `feat(api): wire rate limiting after auth and before idempotency`.

---

### Task 7: Load-test support

**Files:**
- Modify: `deploy/app/compose.yml` (`x-app-env`: pass through every `RATE_LIMIT_*` as `${RATE_LIMIT_X:-}`; empty counts as unset in the loader)
- Modify (user's uncommitted files; do not commit): `.make/loadtest.mk`, `loadtest/README.md`, `docs/superpowers/plans/2026-10-13-load-testing.md`
- Create (do not commit; it lives in the user's uncommitted `loadtest/`): `loadtest/scenarios/ratelimit.js`

**Interfaces:**
- `make loadtest.app.up MODE=single|ha` starts the API with `LOADTEST_LIMITS` exported. The default is `RATE_LIMIT_AUTH_MAX=100000 RATE_LIMIT_CREATE_MAX=100000 RATE_LIMIT_REDIRECT_MAX=1000000 RATE_LIMIT_DEFAULT_MAX=100000`; override with `make loadtest.app.up LOADTEST_LIMITS="…"`.
- `make loadtest.ratelimit` runs `ratelimit.js` with `-e GROUP_MAX=… -e GROUP_WINDOW_S=…` (default 100000 and 60, matching the redirect group under `LOADTEST_LIMITS`, overridable).

`ratelimit.js`: one VU, one iteration. It sends `GROUP_MAX + EXTRA` requests (`EXTRA` default 20) to an unknown short code, `GET /zzzzzzzz`, which is in the redirect group and costs no DB hit beyond the absent-cache lookup. The redirect group's expected statuses are 404 and 429. It records the elapsed time, then checks:
- `allowed >= GROUP_MAX`
- `allowed <= GROUP_MAX + ceil(elapsed_s × GROUP_MAX / GROUP_WINDOW_S)`
- at least one 429
- every 429 has `Retry-After` and `X-RateLimit-Limit`

Its thresholds are `checks: ['rate==1']`. A large `GROUP_MAX` takes a while; the README tells people to run it with a smaller redirect limit, e.g. `make loadtest.app.up LOADTEST_LIMITS="… RATE_LIMIT_REDIRECT_MAX=200 RATE_LIMIT_REDIRECT_WINDOW=3600"` with `GROUP_MAX=200 GROUP_WINDOW_S=3600`, so the refill is negligible.

- [ ] **Step 1: Make the compose and make changes.** Run `docker compose -f deploy/app/compose.yml config` with `RATE_LIMIT_AUTH_MAX=7` exported. Expected: the app env shows `RATE_LIMIT_AUTH_MAX: "7"`.
- [ ] **Step 2: Write `ratelimit.js`.** Bring up the stack with the small redirect limit above and run `make loadtest.ratelimit GROUP_MAX=200 GROUP_WINDOW_S=3600`. Expected: all checks pass and k6 exits 0. Then run it again immediately. Expected: it fails `allowed >= GROUP_MAX`, which proves it detects an exhausted bucket. Note this in the README.
- [ ] **Step 3: Update docs.**
  - In `loadtest/README.md`: a "Rate limits" section on `loadtest.app.up`, `LOADTEST_LIMITS` and `loadtest.ratelimit`.
  - In the load-testing plan: replace the "No HTTP rate limiter was found" constraint with the env-override approach and add `ratelimit` to the scenarios table.
- [ ] **Step 4: Commit** only `deploy/app/compose.yml`: `feat(deploy): pass rate limit settings to the app container`. In the report, list the uncommitted load-test files for the user.

---

### Task 8: Architecture docs and ADR

**Files:**
- Create: `docs/adr/0023-rate-limiting-is-atomic-in-redis-with-a-per-instance-fallback.md`
- Modify: `docs/Architecture.md`

The ADR follows the format of `docs/adr/0021-*.md`: front matter `status: accepted`, a decision-as-title, a body, then Consequences. It covers:
- GCRA via `redis_rate` instead of `gofiber/storage`, and why: get/set can't be atomic.
- Steady-rate-with-burst semantics, with the up-to-`2×max−1`-per-window note.
- Redis stays optional, per ADR-0015: the fallback is per instance, so N instances allow up to N× during an outage, and the signals are the gauge and the log lines.
- The middleware order (after auth, before idempotency) and why.
- Client IP from the rightmost untrusted address, not fiber's `ProxyHeader`.

`Architecture.md`: add a `## Rate limiting` section after `## Cache`. It holds the group table (route, key, default), where the middleware sits, the fallback behaviour, the env-var prefix, and the three metrics, linking ADR-0023 and the spec.

- [ ] **Step 1: Write both.** Check every route, env var and metric name against the code from Tasks 1–6 with `grep`.
- [ ] **Step 2: Commit** `docs: rate limiting ADR-0023 and architecture section`.
