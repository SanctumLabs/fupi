# HTTP rate limiter — design

Status: draft for review · Date: 2026-10-13

## 1. Intent

The API has no rate limiting. `app/pkg/infra/server/middleware/limiter.go` is a stub (fiber's limiter, 100 requests a minute per IP, in
memory, an empty config struct), wired globally in `server.NewServer`. It is a starting point, not something to ship.

Three kinds of abuse drive this:

- **Scraping and hammering redirects.** `GET /{code}` is public and the hottest path.
- **Mass URL creation.** `POST /api/v1/fupi/urls` needs a token, but every create fans out into an outbox event and a Safe Browsing scan
  (ADR-0022), so a flood costs real work and quota downstream.
- **Login abuse.** Brute force and credential stuffing against the auth endpoints. Password hashing is deliberately CPU-expensive, which
  also makes login the cheapest way to exhaust the API's CPU.

The k6 load tests being added in `loadtest/` must run through the real limiter, and one scenario must prove that it limits.

**Success criteria**

1. With defaults, 11 login attempts from one IP in quick succession (well under the 6s refill) get 10 answers and one `429` with
   `Retry-After` and `{"message":"too many requests"}`. After that, attempts are held to the steady rate of one every 6s.
2. Two API instances sharing Redis enforce one shared count: N concurrent requests for one key across both instances let through exactly
   the limit, no more (refill during the burst aside).
3. With Redis stopped, the API keeps serving, every request still passes through a limiter (per instance), no request waits on Redis
   timeouts, and a dashboard shows the fallback is active. When Redis returns, the limiter goes back to Redis without a restart.
4. `/health`, `/health/ready` and `/docs` are never limited.
5. Every limit can be changed by env var without a code change. The load-test stack raises them so throughput runs are not throttled, and a
   dedicated k6 scenario shows the limit trips.
6. Limiting decisions are visible in OTel metrics, on the request span, and in logs.

## 2. Goals, non-goals, anti-goals

**Goals:** per-route-group limits, shared and atomic across instances via Redis, graceful per-instance fallback, configurable with sensible
defaults, observable, tested, documented (ADR + Architecture.md).

**Non-goals**

- Limiting by account (e.g. per email on login to catch distributed credential stuffing). Keys are client IP or user ID only.
- Per-plan or per-customer quotas, allowlists, ban lists.
- Limiting the outbox relay or the worker; this is HTTP only.
- Anything specific to a hosting provider. v1 ran on Fly.io; v2 may or may not. Nothing may assume Fly.

**Anti-goals** (would be a failure even if it technically works)

- The API falling over, or stalling on Redis timeouts, when Redis is down.
- Probes getting 429s, so an orchestrator restarts or drains healthy instances.
- Behind a proxy, every client sharing the proxy's IP and therefore one bucket.
- Client IPs or user IDs as metric labels (unbounded cardinality).

## 3. Decisions made with the user

| # | Decision |
|---|---|
| D1 | Use what fiber provides: `github.com/gofiber/fiber/v2/middleware/limiter` with its `Config` (`Next`, `Max`, `Expiration`, `KeyGenerator`, `LimitReached`) and its `LimiterMiddleware` hook for a custom algorithm. |
| D2 | Counting in Redis is atomic, using `github.com/go-redis/redis_rate/v10` (GCRA, one Lua call per check) on the existing go-redis v9 `UniversalClient`. **`gofiber/storage` is not used**: its `Storage` interface is get/set only and cannot be atomic. |
| D3 | When Redis is unreachable, fall back to in-memory limiting per instance. With N instances the effective limit becomes up to N× during an outage. Accepted. |
| D4 | Different limits per route group (table in §4.1), each configurable by env var, with the defaults below. |
| D5 | Client IP comes from a configurable trusted header and trusted-proxy list; by default the connection IP. Nothing provider-specific. |
| D6 | Load-test stack sets its own (higher) limits via env vars; every load-test run goes through the limiter. A dedicated scenario checks 429s. |
| D7 | Observability uses the existing OTel meter provider, the request span from `OTelMiddleware`, and slog. |
| D8 | Approximate behaviour is acceptable in principle since limits are configurable; atomic Redis counting (D2) is chosen because it costs little. |
| D9 | "max per window" means a steady rate with a burst of `max` (GCRA via `redis_rate`), not a hard cap per fixed window. See §4.3. |

## 4. Behaviour

### 4.1 Route groups and defaults

| Group | Matches | Key | Default max | Default window |
|---|---|---|---|---|
| `auth` | any method, path under `{baseURI}/auth/` (register, login, oauth/token, verify) | client IP | 10 | 1m |
| `create` | `POST {baseURI}/urls` | authenticated user ID | 30 | 1m |
| `redirect` | `GET` matching `urlapi.RedirectPathPattern` | client IP | 300 | 1m |
| `default` | everything else | client IP | 100 | 1m |
| exempt | `probes.LivePath`, `probes.ReadyPath`, prefix `/docs` | — | — | — |

The defaults are the builder's starting values, agreed as reasonable; they are expected to be tuned from load-test results.

Keys are namespaced by group so groups never share a bucket: e.g. `ratelimit:auth:ip:203.0.113.7`, `ratelimit:create:user:<id>`. If the
`create` group somehow has no user ID in `Locals`, it falls back to the IP key.

`infra/server/middleware` must not import `app/api/...`. The route-group matching (base URI, redirect pattern, probe paths) is passed in from
`cmd/main.go`, the same way `AuthConfig` receives `PublicPaths` and `PublicPatterns`.

### 4.2 Placement

The limiter moves out of `server.NewServer` and runs **after** `AuthMiddleware`, so the `create` group can key on
`apimiddleware.UserIDContextKey`.

It must also run **before** `IdempotencyMiddleware`. Fiber's idempotency middleware caches whatever status the chain returns for 30
minutes, so a rate-limited POST carrying `X-Idempotency-Key` would otherwise replay its 429 for 30 minutes, ignoring `Retry-After`.
*Builder's call (from the review):* `server.ServerConfig` takes a list of request-guard handlers (auth, then the limiter) that `NewServer`
registers before Helmet and Idempotency. `cmd/main.go` passes them in instead of calling `srv.Use` for auth afterwards. Add a test that a
429 is not replayed for an idempotency key. Consequence: a request with a missing or invalid token on a protected route is
rejected with 401 by the auth middleware before it reaches the limiter. That is acceptable: JWT verification is cheap, and the expensive
endpoints (login, register, token) are public and so are limited.

### 4.3 Algorithm (D2)

A custom `limiter.LimiterHandler` whose `New(limiter.Config)` returns a handler that, per request:

1. Calls `redis_rate.Limiter.Allow(ctx, key, redis_rate.Limit{Rate: max, Burst: max, Period: window})` with a short timeout.
2. Sets `X-RateLimit-Limit`, `X-RateLimit-Remaining`, `X-RateLimit-Reset` (seconds) on the response, matching fiber's header names.
3. If not allowed: `Retry-After` (whole seconds, rounded up from `RetryAfter`) and `config.LimitReached`.

**Semantics (D9): a steady rate with a burst, not a hard cap per window.** With `Rate = Burst = max` a fresh client can spend the whole
allowance at once, then earns one request back every `window/max` (auth defaults: 10 at once, then one every 6s). There are no clock
windows. Any 60s span can let through up to about `2×max − 1`: the full burst plus a window's refill. That is accepted. For login the slow
refill is the brake that matters, and a patient client is held to the steady rate.

### 4.4 Redis unavailable (D3)

- Each Redis call is bounded by `RATE_LIMIT_REDIS_TIMEOUT` (default 50ms) via the request context, independent of the go-redis client's
  own (multi-second) timeouts.
- On an error the request is decided by the in-memory fallback, and the limiter stops trying Redis for `RATE_LIMIT_REDIS_RETRY_AFTER`
  (default 5s). After that the next request tries Redis again; success switches back.
- The fallback is fiber's own fixed-window limiter (`limiter.FixedWindow{}.New(cfg)`, default in-memory storage), built once per group at
  startup, so no new limiting code is written for it.
- A Redis error never fails the request and never lets it through unlimited.
- Redis stays a non-required health check (ADR-0015): losing it weakens limiting, it does not break the API.

### 4.5 Response when limited

`429 Too Many Requests`, `Retry-After`, the `X-RateLimit-*` headers, and body `{"message":"too many requests"}`, the same shape as the
existing `errorResponseDto`.

### 4.6 Client IP (D5)

Two settings:

- `SERVER_PROXY_HEADER` (default unset): header to read the client IP from, e.g. `X-Forwarded-For`, `X-Real-IP`, `Fly-Client-IP`.
- `SERVER_TRUSTED_PROXIES` (default unset): comma-separated IPs/CIDRs of the proxies in front of the API.

Unset means the connection's IP. Setting a header with no trusted proxies is refused by the config loader.

*Builder's call (from the review):* fiber's `ProxyHeader` is not used, because for `X-Forwarded-For` it returns the leftmost entry, which
the client controls, so a client could get a fresh bucket per request. The limiter's key function extracts the IP itself:

- The header is honoured only when the connection's peer is a trusted proxy; otherwise the peer IP is used.
- For a list header (`X-Forwarded-For`), walk from the right and take the first entry that is not a trusted proxy.
- For a single-value header, take it as is. Anything unparsable falls back to the peer IP.

The extraction lives in one small function with table tests, including spoofed leftmost entries.

### 4.7 Configuration

Read through the existing strict loader in `app/config`, in a new settings struct on `config.App`:

| Variable | Default |
|---|---|
| `RATE_LIMIT_ENABLED` | `true` (when `false`, the middleware is not registered at all) |
| `RATE_LIMIT_AUTH_MAX` / `RATE_LIMIT_AUTH_WINDOW` | `10` / `1m` |
| `RATE_LIMIT_CREATE_MAX` / `RATE_LIMIT_CREATE_WINDOW` | `30` / `1m` |
| `RATE_LIMIT_REDIRECT_MAX` / `RATE_LIMIT_REDIRECT_WINDOW` | `300` / `1m` |
| `RATE_LIMIT_DEFAULT_MAX` / `RATE_LIMIT_DEFAULT_WINDOW` | `100` / `1m` |
| `RATE_LIMIT_REDIS_TIMEOUT` | `50ms` |
| `RATE_LIMIT_REDIS_RETRY_AFTER` | `5s` |
| `SERVER_PROXY_HEADER`, `SERVER_TRUSTED_PROXIES` | unset |

Max must be > 0 and windows > 0; invalid values are reported by the loader like other settings. Add them to `.env.example`.

## 5. Observability (D7)

Instruments from `otel.GetMeterProvider()` (or an injected provider in tests), following `urlexpiry/metrics.go`:

| Instrument | Kind | Attributes | Purpose |
|---|---|---|---|
| `http.ratelimit.requests` | Int64Counter | `group`, `outcome` (`allowed`/`limited`), `backend` (`redis`/`memory`) | Who is being limited, and where |
| `http.ratelimit.backend.errors` | Int64Counter | `group` | Redis failures |
| `http.ratelimit.fallback.active` | Int64ObservableGauge | — | 1 while in fallback, 0 otherwise |

On the request span (`trace.SpanFromContext(c.UserContext())`): `ratelimit.group`, `ratelimit.limited`, `ratelimit.remaining`,
`ratelimit.backend`, and `ratelimit.key` (IP or user ID is fine on spans, never on metrics).

Logs: one `Warn` when switching to fallback (with the error), one `Info` when back on Redis. Never one per request. Limited requests already
show in the access log as 429.

## 6. Tests

- **Unit** (default build): group resolution and exemptions; key derivation (IP, user ID, fallback to IP); 429 body and headers; switching
  to the fallback on a Redis error and back after the retry interval, using a fake for the Redis-side interface; metrics recorded (OTel SDK
  `ManualReader`); config loader defaults and validation; proxy header ignored from an untrusted peer.
- **Integration** (behind the integration build tag, ADR-0006, using the testcontainers Redis module already in `go.mod`): real
  `redis_rate`; N concurrent requests over two separate fiber apps sharing one Redis allow exactly `max` (use a long window, e.g. 10 per
hour, so refill during the test is nil); stopping the container switches to
  fallback without stalling, restarting it switches back.
- Existing tests that build the server or run e2e flows must not become flaky from the new limits (e.g. e2e registering many users from one
  IP). Raise limits or disable in those tests explicitly.

## 7. Load testing (D6)

- The app container used by the load tests gets higher limits via env so seeding (`USERS` registrations and logins from one IP) and the
  throughput scenarios are not throttled. In throughput scenarios a 429 is an unexpected status and fails the checks, so a mis-set limit is
  obvious.
- New `loadtest/scenarios/ratelimit.js` and `make loadtest.ratelimit`: reads the configured limit for one group from k6 env, sends
  `max + N` requests from one VU as fast as it can, and checks that the number allowed is `max` plus at most the refill earned during the
  run (`elapsed × max / window`, rounded up), and that every 429 carries `Retry-After` and `X-RateLimit-*`. Pick the group or limit so the
  refill during the burst is small. It empties that bucket, so it runs on its own, not inside `mixed`.
- Update `docs/superpowers/plans/2026-10-13-load-testing.md` where it says no limiter exists, and `loadtest/README.md`.

## 8. Documentation

- New ADR `docs/adr/0023-...md`: rate limiting is atomic in Redis with a per-instance in-memory fallback; why `redis_rate` over
  `gofiber/storage`; why Redis stays optional; the N× limit during an outage.
- `docs/Architecture.md`: a "Rate limiting" section covering groups, keys, placement after auth, fallback and the signals to watch.
- Env vars documented wherever the existing ones are (`.env.example`, Deployment/LocalInfrastructure docs as applicable).

## 9. Left to the builder

- File and package layout (e.g. the `redis_rate` handler in `middleware/` or its own package), and how the group dispatcher is structured
  (one middleware choosing among per-group handlers vs one `limiter.New` per group with `Next`).
- How the go-redis `UniversalClient` is exposed from `pkg/infra/cache/redis` to the limiter (it is currently private behind `CacheClient`).
- The exact load-test limit values and how the load-test stack's env is supplied.
- Metric and span attribute naming details, as long as they follow existing conventions.
