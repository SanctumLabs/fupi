# Deployment

The application ships as one container image that holds the API and the database migrator.

## Build

```bash
make build.docker DOCKER_IMAGE_TAG=fupi-service    # stamps the image with its version, commit and build time
# or
docker build -t <IMAGE_NAME>:<IMAGE_TAG> --build-arg VERSION=<VERSION> --build-arg GIT_COMMIT=<SHA> --build-arg BUILD_TIME=<RFC3339> .
```

The image is built in two stages. The final stage is `gcr.io/distroless/static-debian13:nonroot`, pinned by digest: no shell, no package manager, uid 65532. It contains:

- `/app/fupi`, the API (the default entrypoint, port 8085);
- `/app/migrator`, which applies the database migrations (ADR-0014);
- `/app/migrations`, the SQL files the migrator reads (`MIGRATIONS_PATH` is preset to it).

`make lint.docker` runs hadolint with the rules in `hadolint.yaml`, and `make scan.docker` scans the image for fixable HIGH and CRITICAL vulnerabilities. CI does both before it publishes (`.github/workflows/ci.yml`).

Without Docker: `make build` writes the binary to `bin/fupi`.

## Configuration

The image sets `ENVIRONMENT=production`. In that mode the API, the migrator and the worker refuse the development secrets and exit 1 naming the variables that are missing, so a deployment must set at least:

| Variable | Meaning |
|---|---|
| `AUTH_SECRET` | the JWT signing secret |
| `SHORT_CODE_KEY` | keys the permutation that makes short codes non-sequential; at least 32 characters, and **never change it once codes have been issued** ([ADR-0019](adr/0019-short-codes-are-a-keyed-permutation-of-the-counter.md)) |
| `DATABASE_HOST`, `DATABASE_NAME`, `DATABASE_USERNAME`, `DATABASE_PASSWORD` | Postgres (the primary, port 5432); or `DATABASE_URL` instead, which wins over those |
| `DATABASE_SSL_MODE` | defaults to `disable`; use `require` or stronger outside a private network (the API logs a warning otherwise) |
| `REDIS_ADDRESS`, `REDIS_PASSWORD` | a comma-separated `host:port` list (one address gives a plain client, several a cluster client) and the password; `REDIS_USERNAME` defaults to `fupi-svc` |

Every variable, its default and its unit is listed in the app-connectivity spec (`docs/superpowers/specs/2026-10-04-app-connectivity-design.md`, section 4); the local defaults are in `.env.example`.

Optional telemetry and logging variables (the defaults suit the local stack; none is required in production):

| Variable | Meaning |
|---|---|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | the OpenTelemetry Collector's OTLP/gRPC address, for example `http://otel-collector:4317` (default `http://localhost:4317`); an `https://` address uses TLS |
| `OTEL_SERVICE_NAME` | the service name on traces, metrics and log lines (default `fupi`) |
| `OTEL_TRACES_SAMPLER`, `OTEL_TRACES_SAMPLER_ARG` | default `parentbased_always_on`, which is too much for production traffic: set for example `parentbased_traceidratio` with `0.05`. Keep a parent-based sampler: the readiness checks run under an unsampled parent so they leave no spans |
| `OTEL_METRIC_EXPORT_INTERVAL` | milliseconds between metric exports (default 15000) |
| `OTEL_SDK_DISABLED` | `true` turns tracing and metrics off |
| `LOG_LEVEL` | `debug`, `info` (default), `warn` or `error` |
| `LOG_FORMAT` | `json` (default; one object per line with `time`, `level`, `msg`, `service`, and `trace_id`/`span_id` inside a request) or `text` |

A collector that is unreachable never stops the API or slows a request; the API logs a `telemetry export failed` line for each distinct error and signal at most once a minute. On SIGTERM the API exports its last spans and metrics after it has drained, waiting at most five seconds.

## Migrations

The API never migrates at startup. Run the migrator from the same image, once per deploy, before starting the new version:

```bash
docker run --rm -e ENVIRONMENT=production -e DATABASE_HOST=... -e DATABASE_NAME=... -e DATABASE_USERNAME=... -e DATABASE_PASSWORD=... \
  --entrypoint /app/migrator <IMAGE_NAME>:<IMAGE_TAG>
```

It exits 0 when it applied the migrations or found nothing to do. If it reports a connection error because the database is still starting, run it again.

## Running

```bash
docker run -p 8085:8085 -e AUTH_SECRET=... -e SHORT_CODE_KEY=... -e DATABASE_HOST=... -e DATABASE_NAME=... -e DATABASE_USERNAME=... -e DATABASE_PASSWORD=... -e REDIS_ADDRESS=... -e REDIS_PASSWORD=... \
  --name fupi-api <IMAGE_NAME>:<IMAGE_TAG>
```

- `GET /health` is liveness (always 200 while the process runs); `GET /health/ready` is readiness (503 when Postgres is down or the process is draining).
- The image's `HEALTHCHECK` runs `/app/fupi healthcheck`, which calls `/health`.
- On SIGTERM the API stops accepting traffic and finishes in-flight requests for up to `SHUTDOWN_TIMEOUT` seconds (default 15). Give the orchestrator a stop grace period longer than that plus the five seconds the API may spend exporting its last telemetry (the local compose stack uses 25 seconds).
- For hardening, also run it with a read-only root filesystem, `--cap-drop ALL` and `--security-opt no-new-privileges`, as `deploy/app/compose.yml` does.

## The outbox relay worker

The image also contains `/app/worker`, which delivers the transactional outbox to Kafka. Run it as its own container from the same image, after the migrator and with the same database settings as the API (it never migrates):

```bash
docker run -p 8086:8086 -e DATABASE_HOST=... -e DATABASE_NAME=... -e DATABASE_USERNAME=... -e DATABASE_PASSWORD=... -e KAFKA_BROKERS=... \
  --no-healthcheck --entrypoint /app/worker --name fupi-worker <IMAGE_NAME>:<IMAGE_TAG>
```

| Variable | Default | Meaning |
|---|---|---|
| `KAFKA_BROKERS` | `localhost:19092` | comma-separated `host:port` list; set it |
| `KAFKA_CLIENT_ID` | `fupi-worker` | |
| `KAFKA_PUBLISH_TIMEOUT` | `10` | seconds a record may take to be acknowledged before it counts as a transient failure |
| `OUTBOX_POLL_INTERVAL_MS` | `100` | milliseconds between cycles when nothing was claimed (at least 10) |
| `OUTBOX_BATCH_SIZE` | `100` | rows claimed per destination per cycle (1 to 1000) |
| `OUTBOX_MAX_ATTEMPTS` | `3` | permanent rejections before an event is parked (at least 1) |
| `OUTBOX_STANDBY_INTERVAL` | `5` | seconds between a standby's attempts to become the active relay |
| `OUTBOX_RETENTION_DAYS` | `7` | days sent rows are kept; `0` keeps them forever |
| `WORKER_HTTP_PORT` | `8086` | the health listener |
| `SAFE_BROWSING_API_KEY` | unset | Google Safe Browsing key; when set, the worker scans every created url and suspends flagged ones (ADR-0022), and then also needs the `REDIS_*` and `REDIRECT_CACHE_*` settings. Unset, it does not scan |

The database, logging and telemetry variables are the API's; the worker's service name defaults to `fupi-worker`.

- Run more than one for failover: one is active (it holds a Postgres advisory lock on a connection of its own), the others stand by. A second worker is not extra capacity. The lock connection sets `idle_session_timeout` (60 seconds, kept busy by the active relay's checks), so a leader that vanishes without closing its connection loses the lock within a minute; this needs PostgreSQL 14 or newer.
- The topics must exist (the relay does not create them); an event for a missing topic waits and shows in the backlog alert.
- `GET /health` is liveness: 503 when the relay loop has not completed a cycle or a standby attempt for 30 seconds, so a wedged worker is restarted. `GET /health/ready` is 503 when Postgres or Kafka is down or the worker is draining; neither is a reason to restart. The image's built-in `HEALTHCHECK` is the API's (`/app/fupi healthcheck`, port 8085), which is why the command above passes `--no-healthcheck`: give the worker its own, either `GET :8086/health` or `/app/worker healthcheck` (the binary has no shell, so use the exec form, as `deploy/worker/compose.yml` does).
- On SIGTERM the worker finishes the batch it has in flight (bounded by `KAFKA_PUBLISH_TIMEOUT` and `SHUTDOWN_TIMEOUT`), releases the lock, exports its last telemetry and exits 0. Give it the same stop grace period as the API (25 seconds locally).
- Delivery is at least once: consumers must de-duplicate on the `event_id` header. See ADR-0018 and the operations notes in `docs/LocalInfrastructure.md` (parked events, purge, failure behaviour).

## Continuous integration

GitHub Actions runs the full pipeline. GitLab CI (`.gitlab-ci.yml`) and Bitbucket Pipelines (`bitbucket-pipelines.yml`) run the same verification on the mirrors that `.github/workflows/gitlab_sync.yml` and `bitbucket_sync.yml` keep in step:

| Check | Command | GitHub | GitLab | Bitbucket |
|---|---|---|---|---|
| Lint | `golangci-lint run` (v2.13.2, the version `make lint` pins) | yes | yes | yes |
| Unit tests with coverage | `make test.coverage` | yes | yes | yes |
| Integration tests | `make test.integration` (testcontainers) | yes | yes | yes |
| End-to-end tests | `make test.e2e` (testcontainers) | yes | yes | yes |
| Binary build | `make build` | Linux, macOS, Windows | Linux | Linux |
| Dockerfile lint | `hadolint --config hadolint.yaml Dockerfile` | yes | yes | yes |
| Image build and checks | `docker build`, then `scripts/image_test.sh image <tag>` | yes | yes | yes |
| Image scan | `trivy image --severity HIGH,CRITICAL --ignore-unfixed --exit-code 1` | yes | yes | yes |

Publishing the image, releases, the Fly.io deploy, Sentry releases, Slack notifications, CodeQL and Danger run on GitHub only.

The jobs that need Docker (the integration and end-to-end tests and the image build) use Docker-in-Docker on GitLab (`docker:29.8-dind`) and the `docker` service on Bitbucket, with `TESTCONTAINERS_RYUK_DISABLED=true`. The image is scanned from a saved tar, so the scanner needs no Docker daemon. `make lint.workflows` runs `scripts/workflows_check.sh`, `actionlint` and `scripts/mirror_ci_check.sh`, which fails when a mirror drifts from the GitHub workflows (a Go version that differs from `go.mod`, a missing command, an unpinned image). It can only check the files, not run them: the first run of a mirror pipeline happens when GitHub pushes to it.

## Local infrastructure

The supporting services (Postgres, Redis, Kafka, ELK, Prometheus, Grafana) run locally in Docker in either HA or single-node mode, and the API can run beside them as a container (`make infra.app.up`). See [Local infrastructure](./LocalInfrastructure.md).
