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

`rate_limited` counts 429s apart from other failures, `token_refreshes` counts refresh calls, and `unauthorized` counts
401s on the management routes (it must stay 0). A threshold failure makes k6, and so `make`, exit non-zero.

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
