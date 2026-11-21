# Production Image, App Stack and CI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a hardened, reproducible image of the API and the migrator, run it on the `fupi` network through a new `app` compose stack, repair the Docker lint and scan tooling, and modernize CI.

**Architecture:** A multi-stage `Dockerfile` (pinned Go toolchain image, distroless `static` `:nonroot` runtime) built from an allowlisted context; a `fupi healthcheck` subcommand for the image's `HEALTHCHECK`; `deploy/app/compose.yml` with `app-ha` and `app-single`, started by `scripts/infra.sh` after Postgres and Redis; two guard scripts (`scripts/image_test.sh`, `scripts/workflows_check.sh`) that make the hardening rules testable; and rewritten GitHub workflows with SHA-pinned actions.

**Tech Stack:** Docker (BuildKit cache mounts), Docker Compose 5.x, distroless, hadolint, Trivy, actionlint, GitHub Actions, Go 1.26 (stdlib only for the subcommand), bash.

**Spec:** `docs/superpowers/specs/2026-10-04-dockerfile-hardening-design.md` (decisions D1-D10 are binding; this plan argues from it).

## Global Constraints

- All commands run from the repo root `/Users/lusina/Projects/SanctumLabs/fupi`.
- No new Go module dependencies (`go.mod` and `go.sum` must not change).
- Commits end with the `Co-Authored-By` trailer given in the session's attribution instructions (spec D10). The `git commit` snippets below omit the trailer text; add it when you commit.
- The image: Go builder `golang:1.26-alpine`, runtime `gcr.io/distroless/static-debian13:nonroot` (use `debian12` only if `debian13` cannot be pulled), both pinned by digest; `USER 65532:65532`; `ENV ENVIRONMENT=production` and `ENV MIGRATIONS_PATH=/app/migrations`; `EXPOSE 8085`; `HEALTHCHECK` is `/app/fupi healthcheck`; no `# syntax=` directive; no `-mod=mod`.
- Build context is an allowlist: `go.mod`, `go.sum`, `app/`, minus `**/*_test.go` and `app/test`.
- The compose `app` services have **no `depends_on`**, an explicit `environment:` list (never `env_file`), `read_only: true`, `tmpfs: [/tmp]`, `cap_drop: [ALL]`, `security_opt: ["no-new-privileges:true"]`, `stop_grace_period: 20s`, port `127.0.0.1:8085:8085`.
- Every `${VAR:-default}` in a compose file must equal the default in `.env.example` (`scripts/infra_env_check.sh` enforces it).
- GitHub Actions are pinned by full commit SHA with the version in a trailing comment; every workflow has a top-level `permissions:` block; `workflow_run` workflows check out `workflow_run.head_sha` or `head_branch`. Reusable workflows from `SanctumLabs/ci-workflows` are exempt from pinning.
- Format every Go file you edit with `gofmt -w`. `go test ./...` must stay green at the end of every task.
- Do not touch `fly.toml`, `.gitlab-ci.yml`, `bitbucket-pipelines.yml`, the secret names in `deploy.yml`, or slice 1's `deploy/<stack>` files.

## Before you start: the pull gate

Building and verifying needs images that are not on this machine. **Ask the user once, before Task 1, for permission to download them** (name each, say the sizes are roughly tens to a couple of hundred MB, and that Trivy also downloads its vulnerability database on first use):

| Image | Used for |
|---|---|
| `golang:1.26-alpine` | the build stage (Tasks 2, 7) |
| `gcr.io/distroless/static-debian13:nonroot` | the runtime stage (Tasks 2, 7) |
| `aquasec/trivy:0.75.0` | `make scan.docker` (Tasks 3, 7) |
| `rhysd/actionlint:1.7.12` | `make lint.workflows` (Task 5) |

Record the answer in the ledger. If the user declines, stop: Tasks 2, 3, 5 and 7 cannot be verified without them. `hadolint/hadolint` is already local (pinned below by its digest), and `redis:8.10.2-alpine`, `postgres:18.6` and the other stack images are already local.

## Plan notes (deviations from the spec's wording)

1. **No `depends_on`.** The spec's first draft listed dependencies; the spec now says none are declared, because a dependency into a profile that is not enabled makes `docker compose config` fail (probed on Compose 5.5.1). `infra.sh up app` orders Postgres, Redis, then the app.
2. **Tool versions are fixed here**: Trivy `0.75.0`, actionlint `1.7.12`, hadolint pinned by the digest of the image already local (`hadolint/hadolint@sha256:32dac94127fd60b7b7e3fbfc65e1383b9b5e25c9bfd7b8536de7a539fe68a12d`, hadolint 2.15.1). GitHub Action pins were resolved on 2026-10-04 with `gh api`.
3. **Two guard scripts** make spec §9 testable: `scripts/image_test.sh` (image properties and the build context) and `scripts/workflows_check.sh` (pinning, permissions, checkout ref, Dependabot).
4. **`docs/Deployment.md` is rewritten**, not just extended: its Build, Environment and Running sections describe the Mongo-era app (`ENV`, `.env.sample`, `CACHE_*`).
5. **`docker.yml` fixes more than the spec lists.** Under `workflow_run`, `github.ref` and `github.sha` are the default branch, so `metadata-action`'s branch, `is_default_branch`, `sha` and `semver` tags are wrong or dead. Tags come from `workflow_run.head_branch` and `head_sha`, semver tags are dropped, and image labels come from the Dockerfile (build args), not from `metadata-action`.
6. **Two more `workflow_run` fixes**: `sentry.yml` tests `github.ref_name` (always the default branch under `workflow_run`), and `slack.yml` lists a workflow named `Test` that is really `Tests`. `codeql.yml` gains a `setup-go` step because `go.mod` needs Go 1.26 for autobuild.
7. `.gitlab-ci.yml` and `bitbucket-pipelines.yml` (the mirrors' CI) were out of the first version of the spec. On 2026-10-04 the user asked for parity with GitHub CI, so spec D11 and section 8b and Task 8 below were added.
8. **Task 3's Make code was corrected while executing it.** `docker save <repository>` exports every tag, so `scan.docker` saves one explicit reference (`DOCKER_IMAGE_REF`) and runs Trivy with `--quiet --no-progress`; the first scan also needed a `google.golang.org/grpc` bump to v1.83.2 (see the ledger rulings and spec section 12). The snippets in Task 3 below are the corrected ones.

## Review Focus

Failure modes the spec implies but no obvious test covers, most likely first. Each is pinned by a test or drill in the owning task.

1. The build context re-admits a secret or a new top-level directory (`.env`, `.git`, `.superpowers`) → Task 2 (`scripts/image_test.sh context`).
2. An image started without `ENVIRONMENT` accepts the development secrets → Task 2 (`image_test.sh image` runs the image with no environment and expects exit 1).
3. The healthcheck dials the wrong address when `SERVER_HOST` is set → Task 1 (host tests).
4. `docker compose stop` kills a draining app (grace period shorter than the drain) → Task 7 (the stop drill checks exit code 0 inside the grace period).
5. A `workflow_run` workflow builds, scans or releases the wrong commit → Task 5 (`workflows_check.sh` rule 3). **Holds for the first link of a chain only**: the final review showed that a workflow triggered by another `workflow_run` workflow gets the default branch's head (spec section 8, limitation).

---

### Task 1: The `healthcheck` subcommand

**Files:**
- Modify: `app/cmd/main.go`
- Modify: `app/cmd/main_test.go`

**Interfaces:**
- Consumes: `config.LoadServer(lookup config.Lookup) (config.ServerSettings, error)` and `config.Lookup` (slice 3); `probes.LivePath` (`/health`).
- Produces: `fupi healthcheck` (exit 0 when `GET /health` answers 200, else 1) and `healthcheck(lookup config.Lookup, out io.Writer) int`, `probeHost(host string) string`, `var healthcheckTimeout`. Task 2's image `HEALTHCHECK` and Task 4's compose health status rely on the subcommand.

**Task test command:** `go test -race -count=1 ./app/cmd/ && go build -o /dev/null app/cmd/main.go`

- [ ] **Step 1: Write the failing tests**

Replace the import block of `app/cmd/main_test.go` with:

```go
import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sanctumlabs/fupi/app/api/probes"
	"github.com/sanctumlabs/fupi/app/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)
```

Append to `app/cmd/main_test.go`:

```go
// healthServer answers GET /health with status and every other path with 404, like the API's liveness route.
func healthServer(t *testing.T, status int) (host, port string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != probes.LivePath {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)

	host, port, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	require.NoError(t, err)
	return host, port
}

func probeEnv(host, port string) config.Lookup {
	return lookupOf(map[string]string{"SERVER_HOST": host, "HTTP_PORT": port})
}

func TestHealthcheck_ExitsZeroWhenTheAPIIsAlive(t *testing.T) {
	host, port := healthServer(t, http.StatusOK)
	var out bytes.Buffer

	code := healthcheck(probeEnv(host, port), &out)

	assert.Equal(t, 0, code, out.String())
	assert.Contains(t, out.String(), "ok")
}

func TestHealthcheck_ExitsOneOnAnUnhealthyAnswer(t *testing.T) {
	host, port := healthServer(t, http.StatusServiceUnavailable)
	var out bytes.Buffer

	code := healthcheck(probeEnv(host, port), &out)

	assert.Equal(t, 1, code)
	assert.Contains(t, out.String(), "503")
}

func TestHealthcheck_ExitsOneWhenNothingListens(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	require.NoError(t, ln.Close())
	var out bytes.Buffer

	code := healthcheck(probeEnv("127.0.0.1", port), &out)

	assert.Equal(t, 1, code)
	assert.Contains(t, out.String(), "healthcheck:")
}

func TestHealthcheck_GivesUpAfterTheTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-release }))
	t.Cleanup(func() {
		close(release)
		srv.Close()
	})
	previous := healthcheckTimeout
	healthcheckTimeout = 100 * time.Millisecond
	t.Cleanup(func() { healthcheckTimeout = previous })
	host, port, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	require.NoError(t, err)
	var out bytes.Buffer

	start := time.Now()
	code := healthcheck(probeEnv(host, port), &out)

	assert.Equal(t, 1, code)
	assert.Less(t, time.Since(start), 2*time.Second)
}

// A wildcard bind address cannot be dialed, so the probe goes through loopback; any other host is dialed as configured.
func TestHealthcheck_ReachesAWildcardBindThroughLoopback(t *testing.T) {
	_, port := healthServer(t, http.StatusOK)

	for _, host := range []string{"0.0.0.0", "::", ""} {
		var out bytes.Buffer
		code := healthcheck(probeEnv(host, port), &out)
		assert.Equal(t, 0, code, "SERVER_HOST=%q: %s", host, out.String())
	}
}

func TestProbeHost(t *testing.T) {
	for host, want := range map[string]string{
		"": "127.0.0.1", "0.0.0.0": "127.0.0.1", "::": "127.0.0.1",
		"127.0.0.1": "127.0.0.1", "10.1.2.3": "10.1.2.3", "localhost": "localhost",
	} {
		assert.Equal(t, want, probeHost(host), "host %q", host)
	}
}

func TestHealthcheck_RejectsAnInvalidPort(t *testing.T) {
	var out bytes.Buffer

	code := healthcheck(lookupOf(map[string]string{"HTTP_PORT": "abc"}), &out)

	assert.Equal(t, 1, code)
	assert.Contains(t, out.String(), "HTTP_PORT")
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./app/cmd/ 2>&1 | head -8`
Expected: build failure `undefined: healthcheck`, `undefined: probeHost`, `undefined: healthcheckTimeout`.

- [ ] **Step 3: Implement the subcommand**

In `app/cmd/main.go`, add `"io"`, `"net"`, `"net/http"` and `"strconv"` to the import block (keep it sorted: `fmt`, `io`, `log/slog`, `net`, `net/http`, `os`, `os/signal`, `strconv`, `syscall`, `time`).

Add below the existing `const` block:

```go
// healthcheckTimeout bounds the container health probe. It is a variable so a test can shorten it.
var healthcheckTimeout = 2 * time.Second
```

Make `main` start with the dispatch (everything after it is unchanged):

```go
func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck(os.LookupEnv, os.Stdout))
	}

	if err := godotenv.Load(); err != nil {
```

Add after `main`:

```go
// healthcheck probes the API running on this host and returns the process exit code: 0 when GET /health answers 200,
// 1 otherwise. It reads only the server settings, so the container's HEALTHCHECK needs no database or Redis settings and
// works in an image that has no shell, curl or wget. It does not read .env: the container's environment is the contract.
func healthcheck(lookup config.Lookup, out io.Writer) int {
	server, err := config.LoadServer(lookup)
	if err != nil {
		fmt.Fprintf(out, "healthcheck: invalid configuration: %v\n", err)
		return 1
	}

	target := "http://" + net.JoinHostPort(probeHost(server.Host), strconv.Itoa(server.Port)) + probes.LivePath
	ctx, cancel := context.WithTimeout(context.Background(), healthcheckTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		fmt.Fprintf(out, "healthcheck: %v\n", err)
		return 1
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(out, "healthcheck: %v\n", err)
		return 1
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(out, "healthcheck: %s answered %d\n", target, resp.StatusCode)
		return 1
	}
	fmt.Fprintln(out, "healthcheck: ok")
	return 0
}

// probeHost turns a wildcard bind address, which cannot be dialed, into the loopback address that reaches it. Server
// binds exactly the configured host, so any other host is dialed as given.
func probeHost(host string) string {
	switch host {
	case "", "0.0.0.0", "::":
		return "127.0.0.1"
	}
	return host
}
```

- [ ] **Step 4: Run the tests and the single-file build**

Run: `gofmt -w app/cmd && go vet ./app/cmd/ && go test -race -count=1 ./app/cmd/ && go build -o /dev/null app/cmd/main.go && echo single-file build ok`
Expected: `ok  github.com/sanctumlabs/fupi/app/cmd`, then `single-file build ok`.

- [ ] **Step 5: Run the whole suite and commit**

Run: `go test ./... 2>&1 | grep -v "no test files" | grep -v "^ok" | grep -v "^ *[│┌└]" | head; echo "suite: no failing lines above expected"; git status --short go.mod go.sum`
Expected: no failing lines; `go.mod` and `go.sum` unchanged.

```bash
git add app/cmd/main.go app/cmd/main_test.go
git commit -m "feat(cmd): add the healthcheck subcommand for the container HEALTHCHECK"
```

---

### Task 2: The image: `.dockerignore`, `Dockerfile`, `hadolint.yaml`, `scripts/image_test.sh`

**Files:**
- Create: `scripts/image_test.sh` (executable)
- Rewrite: `.dockerignore`
- Rewrite: `Dockerfile`
- Rewrite: `hadolint.yaml`

**Interfaces:**
- Consumes (Task 1): the `/app/fupi healthcheck` subcommand; slice 3's `/app/migrator` binary and `MIGRATIONS_PATH`.
- Produces: an image buildable with `docker build` (build args `VERSION`, `GIT_COMMIT`, `BUILD_TIME`); `scripts/image_test.sh context` and `scripts/image_test.sh image <tag>`. Task 3's `make build.docker`/`scan.docker`, Task 4's compose build, Task 5's `docker.yml` and Task 7 use them.

**Task test command:** `scripts/image_test.sh context && docker build -t fupi-service:test . && scripts/image_test.sh image fupi-service:test && docker run --rm -i -v "$PWD/hadolint.yaml:/.config/hadolint.yaml:ro" hadolint/hadolint < Dockerfile`

- [ ] **Step 1: Pull the base images** (only after the pull gate was granted)

```bash
docker pull golang:1.26-alpine
docker pull gcr.io/distroless/static-debian13:nonroot
```
Expected: both succeed. If `static-debian13` does not exist, pull `static-debian12:nonroot` instead and use `debian12` everywhere below and in the docs. Record which one in the ledger.

- [ ] **Step 2: Write the guard script**

Create `scripts/image_test.sh`:

```bash
#!/usr/bin/env bash
# Checks the properties the production image must have (docs/superpowers/specs/2026-10-04-dockerfile-hardening-design.md).
# Needs Docker. `context` also needs the Go base image of the Dockerfile locally; the first build pulls it.
#
# Usage: scripts/image_test.sh context        the build context holds the sources and nothing else
#        scripts/image_test.sh image <tag>    a built image is hardened as designed
set -uo pipefail
cd "$(dirname "$0")/.." || exit 1

failures=0
pass() { echo "ok:   $1"; }
fail() { echo "FAIL: $1"; failures=$((failures + 1)); }
usage() { sed -n '2,7p' "$0" | sed 's/^# \{0,1\}//' >&2; exit 2; }

check_context() {
  local base files want path
  base="$(sed -n 's/^FROM \([^ ]*\) AS build$/\1/p' Dockerfile)"
  [ -n "$base" ] || { echo "no 'FROM <image> AS build' line in Dockerfile" >&2; exit 2; }

  # Copy the whole context into a throwaway image and list it: that is exactly what the builder can see.
  files="$(docker build --no-cache --progress=plain -f - . 2>&1 <<EOF | grep -oE '/ctx/[^[:space:]]+' | sort -u
FROM ${base}
COPY . /ctx
RUN find /ctx -type f
EOF
)"

  for want in /ctx/go.mod /ctx/go.sum /ctx/app/cmd/main.go /ctx/app/cmd/migrator/main.go \
    /ctx/app/internal/adapters/postgres/migrations/000001_initial_schema.up.sql; do
    if grep -qx "$want" <<<"$files"; then pass "context has ${want#/ctx/}"; else fail "context lacks ${want#/ctx/}"; fi
  done
  for path in /ctx/.env /ctx/.git/ /ctx/.superpowers/ /ctx/docs/ /ctx/deploy/ /ctx/scripts/; do
    if grep -q "^${path}" <<<"$files"; then fail "context must not contain ${path#/ctx/}"; else pass "context has no ${path#/ctx/}"; fi
  done
  if grep -qE '_test\.go$|^/ctx/app/test/' <<<"$files"; then
    fail "context must not contain test files"
  else
    pass "context has no test files"
  fi
}

check_image() {
  local image="$1" value out code cid
  docker image inspect "$image" >/dev/null 2>&1 || {
    echo "image $image not found: build it first (make build.docker DOCKER_IMAGE_TAG=$image)" >&2
    exit 2
  }

  value="$(docker image inspect --format '{{.Config.User}}' "$image")"
  if [ "$value" = "65532:65532" ]; then pass "runs as the nonroot uid 65532"; else fail "runs as the nonroot uid 65532 (User is '$value')"; fi

  value="$(docker image inspect --format '{{json .Config.Healthcheck.Test}}' "$image")"
  if [ "$value" = '["CMD","/app/fupi","healthcheck"]' ]; then pass "HEALTHCHECK runs /app/fupi healthcheck"; else fail "HEALTHCHECK runs /app/fupi healthcheck (is $value)"; fi

  value="$(docker image inspect --format '{{json .Config.Entrypoint}}' "$image")"
  if [ "$value" = '["/app/fupi"]' ]; then pass "entrypoint is /app/fupi"; else fail "entrypoint is /app/fupi (is $value)"; fi

  value="$(docker image inspect --format '{{range .Config.Env}}{{println .}}{{end}}' "$image")"
  if grep -qx 'ENVIRONMENT=production' <<<"$value"; then pass "ENVIRONMENT defaults to production"; else fail "ENVIRONMENT defaults to production"; fi
  if grep -qx 'MIGRATIONS_PATH=/app/migrations' <<<"$value"; then pass "MIGRATIONS_PATH is /app/migrations"; else fail "MIGRATIONS_PATH is /app/migrations"; fi

  value="$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.source"}}' "$image")"
  if [ "$value" = "https://github.com/SanctumLabs/fupi" ]; then pass "carries the OCI source label"; else fail "carries the OCI source label (is '$value')"; fi

  docker run --rm --entrypoint /bin/sh "$image" -c true >/dev/null 2>&1
  code=$?
  if [ "$code" -ne 0 ]; then pass "has no shell"; else fail "has no shell (/bin/sh ran)"; fi

  # No environment at all: the image default (production) must refuse the development secrets (spec D5).
  out="$(docker run --rm "$image" 2>&1)"
  code=$?
  if [ "$code" -eq 1 ] && grep -q 'AUTH_SECRET' <<<"$out" && grep -q 'DATABASE_PASSWORD' <<<"$out" &&
    grep -q 'REDIS_PASSWORD' <<<"$out"; then
    pass "refuses to start without secrets (ENVIRONMENT=production)"
  else
    fail "refuses to start without secrets (exit $code: $out)"
  fi
  if grep -qE 'fupi-secret|fupi-pass|fupi-svc' <<<"$out"; then fail "the refusal must not print a secret value"; else pass "the refusal prints no secret value"; fi

  out="$(docker run --rm --entrypoint /app/migrator "$image" 2>&1)"
  code=$?
  if [ "$code" -eq 1 ] && grep -q 'DATABASE_PASSWORD' <<<"$out"; then
    pass "the migrator is present and refuses the development password in production"
  else
    fail "the migrator is present and refuses the development password in production (exit $code: $out)"
  fi

  cid="$(docker create "$image")"
  value="$(docker cp "$cid:/app/migrations" - 2>/dev/null | tar -t 2>/dev/null | grep -c '\.up\.sql$')"
  docker rm "$cid" >/dev/null
  if [ "${value:-0}" -ge 1 ]; then pass "ships the migrations ($value up files)"; else fail "ships the migrations"; fi

  out="$(docker run --rm --read-only --cap-drop ALL --security-opt no-new-privileges:true "$image" healthcheck 2>&1)"
  code=$?
  if [ "$code" -eq 1 ] && grep -q 'healthcheck:' <<<"$out"; then
    pass "healthcheck runs read-only without capabilities and fails when nothing listens"
  else
    fail "healthcheck runs read-only without capabilities and fails when nothing listens (exit $code: $out)"
  fi

  echo "size: $(docker image inspect --format '{{.Size}}' "$image" | awk '{printf "%.1f MB", $1 / 1000000}')"
}

case "${1:-}" in
  context) check_context ;;
  image) [ -n "${2:-}" ] || usage; check_image "$2" ;;
  *) usage ;;
esac

if [ "$failures" -ne 0 ]; then
  echo "$failures failure(s)"
  exit 1
fi
echo "all checks passed"
```

Run: `chmod +x scripts/image_test.sh && bash -n scripts/image_test.sh && echo syntax ok`
Expected: `syntax ok`.

- [ ] **Step 3: See what reaches the builder today (the red for `.dockerignore`)**

The current `Dockerfile` has no `AS build` line yet, so run this one-off against the already pulled Go image and the **current empty** `.dockerignore`:

```bash
docker build --no-cache --progress=plain -f - . 2>&1 <<'EOF' | grep -oE '/ctx/[^[:space:]]+' | sort -u | grep -E '^/ctx/(\.env|\.git/|\.superpowers/)' | head
FROM golang:1.26-alpine
COPY . /ctx
RUN find /ctx -type f
EOF
```
Expected: lines such as `/ctx/.env`, `/ctx/.git/HEAD` and `/ctx/.superpowers/...`: the secrets and scratch files that reach the builder today.

- [ ] **Step 4: Write the allowlist `.dockerignore`**

Overwrite `.dockerignore` (it is empty today):

```
# Allowlist: nothing enters the build context unless it is listed below (spec D1, section 4).
*
!go.mod
!go.sum
!app
# Tests and their helpers never ship.
**/*_test.go
app/test
```

- [ ] **Step 5: Write the `Dockerfile` and `hadolint.yaml`**

Overwrite `Dockerfile`:

```dockerfile
# Builder: the pinned Go toolchain. GOTOOLCHAIN=local makes a toolchain older than go.mod fail loudly instead of
# silently downloading another one.
FROM golang:1.26-alpine AS build

ARG VERSION=unknown
ARG GIT_COMMIT=unknown
ARG BUILD_TIME=unknown

ENV GOTOOLCHAIN=local

WORKDIR /src

# Dependencies first, so source changes do not invalidate this layer.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download && go mod verify

COPY app ./app

# go.sum is used as it is (no -mod=mod). Symbols are stripped, paths are trimmed and the version variables from
# app/pkg/version.go are injected.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    set -eu; \
    ldflags="-s -w \
      -X github.com/sanctumlabs/fupi/app/pkg.Version=${VERSION} \
      -X github.com/sanctumlabs/fupi/app/pkg.GitCommit=${GIT_COMMIT} \
      -X github.com/sanctumlabs/fupi/app/pkg.BuildTime=${BUILD_TIME}"; \
    CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags "${ldflags}" -o /out/fupi ./app/cmd; \
    CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags "${ldflags}" -o /out/migrator ./app/cmd/migrator

# Distribution: distroless static, no shell and no package manager, running as uid 65532.
FROM gcr.io/distroless/static-debian13:nonroot

ARG VERSION=unknown
ARG GIT_COMMIT=unknown
ARG BUILD_TIME=unknown

LABEL org.opencontainers.image.title="fupi" \
      org.opencontainers.image.description="Fupi URL shortener API and database migrator" \
      org.opencontainers.image.source="https://github.com/SanctumLabs/fupi" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${GIT_COMMIT}" \
      org.opencontainers.image.created="${BUILD_TIME}"

WORKDIR /app

COPY --from=build /out/fupi /out/migrator /app/
COPY app/internal/adapters/postgres/migrations /app/migrations

# A container started without ENVIRONMENT refuses the development secrets; compose overrides it for local use.
ENV ENVIRONMENT=production \
    MIGRATIONS_PATH=/app/migrations

USER 65532:65532

EXPOSE 8085

HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=3 CMD ["/app/fupi", "healthcheck"]

ENTRYPOINT ["/app/fupi"]
```

Overwrite `hadolint.yaml`:

```yaml
# ref: https://github.com/hadolint/hadolint#configure
trustedRegistries:
  - docker.io
  - ghcr.io
  - gcr.io
```

- [ ] **Step 6: Run the context check, then see the image check fail, then build**

Run: `scripts/image_test.sh context`
Expected: every line `ok:`, ending `all checks passed` (`.env`, `.git/`, `.superpowers/`, docs, deploy, scripts and test files are all absent; the sources and the first migration are present).

Run: `scripts/image_test.sh image fupi-service:test; echo "exit $?"`
Expected: `image fupi-service:test not found: build it first ...` and `exit 2`. (Red: there is no image yet.)

Build it:

```bash
docker build -t fupi-service:test \
  --build-arg VERSION=test \
  --build-arg GIT_COMMIT="$(git rev-parse HEAD)" \
  --build-arg BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)" .
```
Expected: the build succeeds (`go mod verify` prints `all modules verified`). If the build fails, read the error: a missing package means an allowlist gap (add it to `.dockerignore`), a toolchain error means the base image is older than `go.mod`.

- [ ] **Step 7: Pin both base images by digest**

```bash
go_digest="$(docker image inspect golang:1.26-alpine --format '{{index .RepoDigests 0}}' | cut -d@ -f2)"
rt_digest="$(docker image inspect gcr.io/distroless/static-debian13:nonroot --format '{{index .RepoDigests 0}}' | cut -d@ -f2)"
echo "$go_digest $rt_digest"
sed -i.bak \
  -e "s#^FROM golang:1.26-alpine AS build#FROM golang:1.26-alpine@${go_digest} AS build#" \
  -e "s#^FROM gcr.io/distroless/static-debian13:nonroot#FROM gcr.io/distroless/static-debian13:nonroot@${rt_digest}#" Dockerfile
rm Dockerfile.bak
grep '^FROM' Dockerfile
```
Expected: two `sha256:` values and two `FROM` lines carrying `@sha256:...`. (Use `debian12` in the second `sed` and `docker image inspect` if you fell back.) Record both digests in the ledger.

- [ ] **Step 8: Run every check on the pinned build**

```bash
docker build -t fupi-service:test \
  --build-arg VERSION=test --build-arg GIT_COMMIT="$(git rev-parse HEAD)" --build-arg BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)" . \
  && scripts/image_test.sh context && scripts/image_test.sh image fupi-service:test \
  && docker run --rm -i -v "$PWD/hadolint.yaml:/.config/hadolint.yaml:ro" hadolint/hadolint < Dockerfile && echo "hadolint clean"
```
Expected: `all checks passed` twice, a `size:` line (record it), and `hadolint clean`. If hadolint reports a finding, fix the Dockerfile (do not add an ignore without a reason recorded in the ledger).

- [ ] **Step 9: Whole suite and commit**

Run: `go test ./... 2>&1 | grep -v "no test files" | grep -v "^ok" | grep -v "^ *[│┌└]" | head; echo "suite: no failing lines above expected"`

```bash
git add .dockerignore Dockerfile hadolint.yaml scripts/image_test.sh
git commit -m "feat(docker): build a hardened distroless image of the API and the migrator"
```

---

### Task 3: Make tooling: `lint.docker`, `scan.docker`, `build.docker`

**Files:**
- Modify: `.make/docker.mk`
- Modify: `scripts/infra_test.sh`

**Interfaces:**
- Consumes (Task 2): the `Dockerfile` and `hadolint.yaml`.
- Produces: `make lint.docker`, `make build.docker`, `make scan.docker` with the variables `HADOLINT_IMAGE`, `TRIVY_IMAGE`, `DOCKER_IMAGE_TAG`. Task 5's `lint.workflows` follows the same pattern; Task 7 runs all three.

**Task test command:** `bash scripts/infra_test.sh && make lint.docker && make build.docker && make scan.docker`

- [ ] **Step 1: Pull Trivy** (pull gate)

Run: `docker pull aquasec/trivy:0.75.0`
Expected: the pull succeeds. If that tag does not exist, list the releases with `gh api repos/aquasecurity/trivy/releases --jq '.[0:5][].tag_name'` and use the newest whose Docker Hub tag (without the `v`) pulls; update the version in this task, the pull-gate table and the ledger.

- [ ] **Step 2: Write the failing tests**

In `scripts/infra_test.sh`, insert this section immediately before the `if [ "$failures" -ne 0 ]; then` block at the end:

```bash
# --- Docker tooling (make -n prints the plan without running it) -------------------------------------------------------
lint_plan="$(make -n lint.docker 2>/dev/null)"
case "$lint_plan" in
  *"/hadolint.yaml:/.config/hadolint.yaml:ro"*) pass "lint.docker mounts hadolint.yaml by absolute path" ;;
  *) fail "lint.docker mounts hadolint.yaml by absolute path" ;;
esac
case "$lint_plan" in
  *" -v hadolint.yaml:"*) fail "lint.docker must not pass hadolint.yaml as a named volume" ;;
  *) pass "lint.docker does not pass hadolint.yaml as a named volume" ;;
esac

scan_plan="$(make -n scan.docker 2>/dev/null)"
case "$scan_plan" in *"docker save"*) pass "scan.docker scans a saved image" ;; *) fail "scan.docker scans a saved image" ;; esac
case "$scan_plan" in
  *"docker.sock"*) fail "scan.docker must not mount the Docker socket into the scanner" ;;
  *) pass "scan.docker does not mount the Docker socket" ;;
esac
case "$scan_plan" in
  *"--severity HIGH,CRITICAL"*"--ignore-unfixed"*"--exit-code 1"*) pass "scan.docker fails on fixable HIGH and CRITICAL findings" ;;
  *) fail "scan.docker fails on fixable HIGH and CRITICAL findings" ;;
esac

# `docker save <repository>` with no tag exports every tag of the repository, and Trivy rejects a tar with more than one image.
case "$scan_plan" in
  *'image.tar" fupi-service:latest'*) pass "scan.docker saves one explicit image reference" ;;
  *) fail "scan.docker saves one explicit image reference" ;;
esac
scan_tagged_plan="$(make -n scan.docker DOCKER_IMAGE_TAG=fupi-service:1.2.3 2>/dev/null)"
case "$scan_tagged_plan" in
  *'image.tar" fupi-service:1.2.3'*) pass "scan.docker keeps a tag that was given" ;;
  *) fail "scan.docker keeps a tag that was given" ;;
esac
case "$scan_plan" in *"--no-progress"*) pass "scan.docker keeps the scanner's progress bar out of the log" ;; *) fail "scan.docker keeps the scanner's progress bar out of the log" ;; esac

build_plan="$(make -n build.docker 2>/dev/null)"
for arg in VERSION GIT_COMMIT BUILD_TIME; do
  case "$build_plan" in *"--build-arg $arg="*) pass "build.docker passes $arg" ;; *) fail "build.docker passes $arg" ;; esac
done
if make -n scan.docker.image >/dev/null 2>&1; then fail "the empty scan.docker.image target is gone"; else pass "the empty scan.docker.image target is gone"; fi

```

- [ ] **Step 3: Run them to see them fail**

Run: `bash scripts/infra_test.sh 2>&1 | grep -E "^FAIL|failure"`
Expected: failures for the absolute-path mount, `docker save`, the severity flags, the three `--build-arg`s and the removed target (the named-volume and socket cases may already read as failures too).

- [ ] **Step 4: Rewrite the Docker targets**

In `.make/docker.mk`, replace everything from the line `scan.docker.image:` through the `@echo "Done building Docker image"` line of `build.docker` (inclusive) with:

```make
# Pinned tool images. hadolint is pinned by the digest of the image already on the machine.
HADOLINT_IMAGE ?= hadolint/hadolint@sha256:32dac94127fd60b7b7e3fbfc65e1383b9b5e25c9bfd7b8536de7a539fe68a12d
TRIVY_IMAGE ?= aquasec/trivy:0.75.0

# `docker save <repository>` exports every tag of the repository, and Trivy rejects a tar with more than one image, so the
# scan always names exactly one reference (a bare name means :latest).
DOCKER_IMAGE_REF = $(if $(findstring :,$(DOCKER_IMAGE_TAG)),$(DOCKER_IMAGE_TAG),$(DOCKER_IMAGE_TAG):latest)

# Build metadata for the image labels and the version variables in the binary
DOCKER_VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo unknown)
DOCKER_GIT_COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
DOCKER_BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

# See hadolint: https://github.com/hadolint/hadolint. The config is mounted by absolute path; a relative path would be a
# named volume and the rules in hadolint.yaml would never be read.
.PHONY: lint.docker
lint.docker: ## lints the Dockerfile with the rules in hadolint.yaml
	@echo "Running lint checks on Dockerfile"
	docker run --rm -i -v "$(ROOT_DIR)/hadolint.yaml:/.config/hadolint.yaml:ro" $(HADOLINT_IMAGE) < $(DOCKER_FILE)
	@echo "Done linting Dockerfile"

# Reference: https://trivy.dev/latest/getting-started/
# The image is saved to a tar and scanned from there, so the scanner never gets the Docker socket.
.PHONY: scan.docker
scan.docker: ## scans the image for fixable HIGH and CRITICAL vulnerabilities, building it first if it is missing
	@if ! docker image inspect $(DOCKER_IMAGE_REF) >/dev/null 2>&1; then $(MAKE) build.docker; fi
	@dir=$$(mktemp -d) && docker save -o "$$dir/image.tar" $(DOCKER_IMAGE_REF) && \
		docker run --rm -v "$$dir":/scan:ro -v fupi-trivy-cache:/root/.cache $(TRIVY_IMAGE) image --quiet --no-progress --input /scan/image.tar --severity HIGH,CRITICAL --ignore-unfixed --exit-code 1; \
		status=$$?; rm -rf "$$dir"; exit $$status

.PHONY: build.docker
build.docker: ## Build the Docker image with its version metadata, usage: make build.docker DOCKER_IMAGE_TAG=fupi-service
	@echo "Building Docker image"
	docker build -f $(DOCKER_FILE) -t $(DOCKER_IMAGE_TAG) \
		--build-arg VERSION=$(DOCKER_VERSION) \
		--build-arg GIT_COMMIT=$(DOCKER_GIT_COMMIT) \
		--build-arg BUILD_TIME=$(DOCKER_BUILD_TIME) .
	@echo "Done building Docker image"
```

- [ ] **Step 5: Run the tests, then the three targets**

Run: `bash scripts/infra_test.sh 2>&1 | tail -4`
Expected: `all tests passed`.

Run: `make lint.docker && make build.docker && make scan.docker; echo "exit $?"`
Expected: hadolint prints nothing; the build succeeds (cached); Trivy prints a report and exits 0. **If Trivy reports a fixable HIGH or CRITICAL finding:** read each one. A Go standard-library finding means the builder's patch release is behind: pull the newer `golang:1.26-alpine` (with the user's go-ahead), re-pin the digest (Task 2 Step 7), rebuild, rescan. An OS-package finding in distroless means the runtime digest is behind: same remedy. Do not add `--ignore-*` flags; if a finding cannot be fixed, record it and its reason in the ledger as a `Ruling:` and in the implementation notes.

- [ ] **Step 6: Whole suite and commit**

Run: `go test ./... 2>&1 | grep -v "no test files" | grep -v "^ok" | grep -v "^ *[│┌└]" | head; echo "suite: no failing lines above expected"`

```bash
git add .make/docker.mk scripts/infra_test.sh
git commit -m "fix(make): lint with hadolint.yaml, scan a saved image with Trivy, stamp build.docker with its version"
```

---

### Task 4: The `app` compose stack

**Files:**
- Create: `deploy/app/compose.yml`
- Modify: `docker-compose.yml` (include)
- Modify: `scripts/infra.sh`
- Modify: `scripts/infra_test.sh`
- Modify: `.make/docker.mk` (`INFRA_PROFILES`, `infra.app.up`/`infra.app.down`)

**Interfaces:**
- Consumes (Task 2): the `Dockerfile` (compose builds it, tag `fupi-app:local`) and its HEALTHCHECK; slice 1's stacks (service `postgres` alias, `migrate`, `redis-1`..) and `infra.sh up postgres|redis <mode>`.
- Produces: services `app-ha`/`app-single` (profiles `app-ha`/`app-single`), `infra.sh up|down|wait app [ha|single]`, `make infra.app.up|down MODE=…`. Task 7 exercises them.

**Task test command:** `bash scripts/infra_test.sh && make infra.config`

- [ ] **Step 1: Write the failing tests**

In `scripts/infra_test.sh`, add after the `"wait resolves the profile"` test (before the `expect_exit "unknown stack ..."` lines):

```bash
expect_output "up app brings up postgres and redis first, then builds and starts the app" \
"+ docker compose --profile postgres-ha rm -sf
+ docker compose --profile postgres-single up -d
+ wait postgres-single
+ docker compose --profile redis-ha rm -sf
+ docker compose --profile redis-single up -d
+ wait redis-single
+ docker compose --profile app-ha rm -sf
+ docker compose --profile app-single up -d --build
+ wait app-single" \
  scripts/infra.sh up app single

expect_output "down app stops only the app" \
"+ docker compose --profile app-ha --profile app-single rm -sf" \
  scripts/infra.sh down app

expect_output "wait resolves the app profile" \
"+ wait app-ha" \
  scripts/infra.sh wait app
```

- [ ] **Step 2: Run them to see them fail**

Run: `bash scripts/infra_test.sh 2>&1 | grep -E "^FAIL|failure" | head`
Expected: the three new cases fail (`unknown stack 'app'`).

- [ ] **Step 3: Teach `infra.sh` the `app` stack**

In `scripts/infra.sh`, change line 5 (keep the line count: `usage` prints lines 2-9) to:

```bash
#   stacks: kafka redis postgres elk core full app (the mode applies) | observability legacy (no mode)
```

Change the stack case to:

```bash
  kafka | redis | postgres | elk | core | full | app)
```

Replace the `up)` branch of the final `case "$action" in` with:

```bash
  up)
    if [ "$stack" = app ]; then
      # The API needs Postgres (with its migrations) and, optionally, Redis. Compose cannot order services across
      # profiles, so this script brings them up and waits for them first.
      "$(dirname "$0")/infra.sh" up postgres "$mode"
      "$(dirname "$0")/infra.sh" up redis "$mode"
    fi
    [ -z "$other_profile" ] || run "${compose[@]}" --profile "$other_profile" rm -sf
    if [ "$stack" = app ]; then up_args=(up -d --build); else up_args=(up -d); fi
    run "${compose[@]}" --profile "$profile" "${up_args[@]}"
    wait_ready "$profile"
    ;;
```

- [ ] **Step 4: Run the script tests**

Run: `bash scripts/infra_test.sh 2>&1 | tail -3`
Expected: `all tests passed`.

- [ ] **Step 5: Write the compose file and include it**

Create `deploy/app/compose.yml`:

```yaml
# The API container (see docs/LocalInfrastructure.md). One service per mode; both build the repository Dockerfile.
#   app-ha      REDIS_ADDRESS lists all six cluster nodes.
#   app-single  REDIS_ADDRESS is the single node (it answers to the alias redis-1).
# The services declare no depends_on: a dependency in a profile that is not enabled makes `docker compose config` fail,
# so scripts/infra.sh brings up and waits for Postgres and Redis first, and restart: unless-stopped covers a manual start.
# The environment is an explicit list, not env_file: the app must not see the other stacks' admin passwords.
x-app: &app
  build:
    context: ../..
    dockerfile: Dockerfile
  image: fupi-app:local
  restart: unless-stopped
  # longer than SHUTDOWN_TIMEOUT (15s), so a stop never kills a drain in progress
  stop_grace_period: 20s
  read_only: true
  tmpfs: [/tmp]
  cap_drop: [ALL]
  security_opt: ["no-new-privileges:true"]
  ports: ["127.0.0.1:8085:8085"]
  networks:
    fupi:
      aliases: [fupi-app]

x-app-env: &app-env
  ENVIRONMENT: development
  HTTP_PORT: "8085"
  DATABASE_HOST: postgres
  DATABASE_PORT: "5432"
  DATABASE_NAME: ${PG_DATABASE:-fupidb}
  DATABASE_USERNAME: ${PG_APP_USER:-fupi-user}
  DATABASE_PASSWORD: ${PG_APP_PASSWORD:-fupi-pass}
  REDIS_USERNAME: ${REDIS_USERNAME:-fupi-svc}
  REDIS_PASSWORD: ${REDIS_PASSWORD:-fupi-svc}
  AUTH_SECRET: ${AUTH_SECRET:-fupi-secret}

services:
  app-ha:
    <<: *app
    profiles: [app-ha]
    environment:
      <<: *app-env
      REDIS_ADDRESS: redis-1:7001,redis-2:7002,redis-3:7003,redis-4:7004,redis-5:7005,redis-6:7006

  app-single:
    <<: *app
    profiles: [app-single]
    environment:
      <<: *app-env
      REDIS_ADDRESS: redis-1:7001

networks:
  fupi:
    name: fupi
```

In `docker-compose.yml`, add this entry to `include:` after the `deploy/legacy/compose.yml` entry:

```yaml
  - path: deploy/app/compose.yml
    env_file: .env
```

- [ ] **Step 6: Wire the Make targets**

In `.make/docker.mk`, add `app-ha app-single` to `INFRA_PROFILES`:

```make
INFRA_PROFILES := kafka-ha kafka-single redis-ha redis-single postgres-ha postgres-single elk-ha elk-single observability legacy core-ha core-single full-ha full-single app-ha app-single
```

and add after the `infra.full.up infra.full.down` targets:

```make
.PHONY: infra.app.up infra.app.down
infra.app.up: create.envfile ## Build and start the API container, with Postgres and Redis brought up first (MODE=ha or single)
	@$(INFRA) up app $(MODE)
infra.app.down: create.envfile ## Stop the API container (Postgres and Redis keep running)
	@$(INFRA) down app
```

- [ ] **Step 7: Verify the stack renders**

Run: `make infra.config 2>&1 | tail -6; docker compose --profile app-single config --services; docker compose --profile app-ha config | grep -E "REDIS_ADDRESS|read_only|cap_drop|stop_grace_period|ENVIRONMENT"`
Expected: `infra.config` ends `ok: app-ha`, `ok: app-single`, `ok: all profiles` (and the env check passes: every `${VAR:-default}` matches `.env.example`); `config --services` prints `app-single`; the HA render shows the six-address `REDIS_ADDRESS`, `read_only: true`, the `cap_drop`, `stop_grace_period: 20s` and `ENVIRONMENT: development`.

- [ ] **Step 8: Whole suite and commit**

Run: `bash scripts/infra_test.sh 2>&1 | tail -2; go test ./... 2>&1 | grep -v "no test files" | grep -v "^ok" | grep -v "^ *[│┌└]" | head; echo "suite: no failing lines above expected"`

```bash
git add deploy/app docker-compose.yml scripts/infra.sh scripts/infra_test.sh .make/docker.mk
git commit -m "feat(infra): add the app stack that runs the API image on the fupi network"
```

---

### Task 5: CI: workflows, `workflows_check.sh`, Dependabot, `lint.workflows`

**Files:**
- Create: `scripts/workflows_check.sh` (executable)
- Rewrite: `.github/workflows/docker.yml`, `build_app.yml`, `deploy.yml`, `dangerci.yml`, `sentry.yml`, `slack.yml`, `bitbucket_sync.yml`, `gitlab_sync.yml`, `codeql.yml`, `release.yml`
- Modify: `.github/workflows/tests.yml`, `.github/workflows/lint.yml`, `.github/dependabot.yml`
- Modify: `.make/docker.mk` (`lint.workflows`)

**Interfaces:**
- Consumes (Task 2, Task 3): `scripts/image_test.sh image <tag>` (used by `docker.yml`), the Dockerfile and `hadolint.yaml`.
- Produces: `scripts/workflows_check.sh`, `make lint.workflows` (the guard script plus `actionlint`).

**Task test command:** `scripts/workflows_check.sh && make lint.workflows`

- [ ] **Step 1: Pull actionlint** (pull gate)

Run: `docker pull rhysd/actionlint:1.7.12`
Expected: succeeds. If the tag does not exist, take the newest `rhysd/actionlint` release that pulls and record it.

- [ ] **Step 2: Write the guard script**

Create `scripts/workflows_check.sh`:

```bash
#!/usr/bin/env bash
# Guards the CI rules this repository relies on (docs/superpowers/specs/2026-10-04-dockerfile-hardening-design.md):
#  1. every action is pinned to a full commit SHA (local actions and reusable workflows from SanctumLabs/ci-workflows are exempt);
#  2. every workflow declares top-level permissions;
#  3. in a workflow_run workflow every checkout names the triggering commit or branch (the default is the default branch);
#  4. Dependabot keeps the gomod, docker and github-actions pins current.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 1
shopt -s nullglob
status=0
fail() { echo "$1"; status=1; }

for f in .github/workflows/*.yml; do
  grep -qE '^permissions:' "$f" || fail "$f: no top-level permissions block"

  while IFS= read -r ref; do
    case "$ref" in ./* | SanctumLabs/ci-workflows/*) continue ;; esac
    grep -Eq '@[0-9a-f]{40}$' <<<"$ref" || fail "$f: $ref is not pinned to a full commit SHA"
  done < <(sed -nE 's/^[[:space:]]*(-[[:space:]]+)?uses:[[:space:]]*([^[:space:]#]+).*/\2/p' "$f")

  if grep -qE '^[[:space:]]*workflow_run:' "$f"; then
    awk -v file="$f" '
      /uses:[[:space:]]*actions\/checkout@/ { pending = 8; found = 0; next }
      pending > 0 {
        if ($0 ~ /ref:.*workflow_run\.head_/) found = 1
        pending--
        if (pending == 0 && !found) { print file ": a checkout in a workflow_run workflow does not use workflow_run.head_sha or head_branch"; bad = 1 }
      }
      END {
        if (pending > 0 && !found) { print file ": a checkout in a workflow_run workflow does not use workflow_run.head_sha or head_branch"; bad = 1 }
        exit bad
      }
    ' "$f" || status=1
  fi
done

for ecosystem in gomod docker github-actions; do
  grep -qE "package-ecosystem:[[:space:]]*\"?${ecosystem}\"?" .github/dependabot.yml || fail ".github/dependabot.yml: no $ecosystem ecosystem"
done

exit "$status"
```

Run: `chmod +x scripts/workflows_check.sh && bash -n scripts/workflows_check.sh && echo syntax ok`
Expected: `syntax ok`.

- [ ] **Step 3: Run it to see it fail**

Run: `scripts/workflows_check.sh; echo "exit $?"`
Expected: many lines (unpinned `actions/checkout@v2` and the other actions, missing `permissions` blocks, checkouts in `workflow_run` workflows without `head_sha`, missing `docker` and `github-actions` ecosystems) and `exit 1`.

- [ ] **Step 4: Add the `lint.workflows` target and see actionlint fail on the old workflows**

In `.make/docker.mk`, add after `scan.docker`'s block (before `build.docker`):

```make
ACTIONLINT_IMAGE ?= rhysd/actionlint:1.7.12

.PHONY: lint.workflows
lint.workflows: ## lints the GitHub Actions workflows (actionlint) and checks the pinning rules
	@$(ROOT_DIR)/scripts/workflows_check.sh
	docker run --rm -v "$(ROOT_DIR):/repo:ro" -w /repo $(ACTIONLINT_IMAGE) -color
```

Run (before rewriting any workflow): `docker run --rm -v "$PWD:/repo:ro" -w /repo rhysd/actionlint:1.7.12 -color 2>&1 | head -12`
Expected: findings such as `the runner of "actions/checkout@v2" action is too old to run on GitHub Actions` and a `set-output` deprecation. That is the red for the workflow rewrite.

- [ ] **Step 5: Rewrite the workflows**

The pins used below (resolved 2026-10-04 with `gh api`; the version is in the trailing comment):

| Action | Pin |
|---|---|
| `actions/checkout` | `3d3c42e5aac5ba805825da76410c181273ba90b1` # v7.0.1 |
| `actions/setup-go` | `b7ad1dad31e06c5925ef5d2fc7ad053ef454303e` # v7.0.0 |
| `actions/upload-artifact` | `043fb46d1a93c77aae656e7c1c64a875d1fc6a0a` # v7.0.1 |
| `actions/cache` | `55cc8345863c7cc4c66a329aec7e433d2d1c52a9` # v6.1.0 |
| `actions/setup-node` | `820762786026740c76f36085b0efc47a31fe5020` # v7.0.0 |
| `docker/setup-qemu-action` | `99012661954931238ded8c8b007157a8430204e1` # v4.4.0 |
| `docker/setup-buildx-action` | `f87e5991a6d7451dcb8d9637bfbc97413f497069` # v4.4.1 |
| `docker/metadata-action` | `dc802804100637a589fabce1cb79ff13a1411302` # v6.2.0 |
| `docker/login-action` | `dbcb813823bdd20940b903addbd779551569679f` # v4.6.0 |
| `docker/build-push-action` | `c3c9e263c25d99ce0380d002d59b67737d91b0dc` # v7.4.0 |
| `github/codeql-action` | `2892aa5e19bbd11bc0cff5427e3b750a04d9e3c2` # v4.38.2 |
| `aquasecurity/trivy-action` | `ed142fd0673e97e23eac54620cfb913e5ce36c25` # v0.36.0 |
| `hadolint/hadolint-action` | `06be81baf89a55ffd0e24b8f04a4185738dd3387` # v3.5.0 |
| `superfly/flyctl-actions` | `fc53c09e1bc3be6f54706524e3b82c4f462f77be` # 1.5 |
| `rtCamp/action-slack-notify` | `33ca3be66c6f378fe1610fd1d5258632dbed5e58` # v2.4.0 |
| `getsentry/action-release` | `ff07929a6537bac57790c3451cf4d364aca38528` # v3.7.0 |
| `wangchucheng/git-repo-sync` | `63782025e80e84c48b25a1ee6bb9a22a3bd570d3` # v0.1.0 |
| `danger/danger-js` | `279443b7b3fdf4488d79546b3d40dbd76c51d83a` # 14.0.6 |

Before writing, re-check the `flyctl-actions` SHA, which was copied from a tag lookup: `gh api repos/superfly/flyctl-actions/commits/1.5 --jq .sha` must print `fc53c09e1bc3be6f54706524e3b82c4f462f77be`. If any pin differs from a fresh lookup, use the fresh value and note it in the ledger.

Overwrite `.github/workflows/docker.yml`:

```yaml
# Builds the image once, lints and scans it, then publishes it to Docker Hub and GHCR.
# Reference: https://docs.docker.com/build/ci/github-actions/
name: Docker

on:
  # Runs only after the Build workflow has completed
  workflow_run:
    workflows:
      - "Build"
    branches:
      - 'main'
      - 'develop'
    types:
      - completed

permissions:
  contents: read

jobs:
  push_docker_image:
    name: Build, Scan and Push Docker Image
    runs-on: ubuntu-latest
    # Runs only when the Build workflow concluded successfully
    if: ${{ github.event.workflow_run.conclusion == 'success' }}
    permissions:
      contents: read
      packages: write

    steps:
      - name: Checkout the commit that was built
        uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          ref: ${{ github.event.workflow_run.head_sha }}
          fetch-depth: 0
          persist-credentials: false

      - name: Lint the Dockerfile
        uses: hadolint/hadolint-action@06be81baf89a55ffd0e24b8f04a4185738dd3387 # v3.5.0
        with:
          dockerfile: Dockerfile
          config: hadolint.yaml

      - name: Image version
        id: version
        run: |
          {
            echo "version=$(git describe --tags --always)"
            echo "short_sha=$(git rev-parse --short=7 HEAD)"
            echo "created=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
          } >> "$GITHUB_OUTPUT"

      - name: Set up QEMU
        uses: docker/setup-qemu-action@99012661954931238ded8c8b007157a8430204e1 # v4.4.0

      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@f87e5991a6d7451dcb8d9637bfbc97413f497069 # v4.4.1

      # A workflow_run workflow runs on the default branch, so the branch and commit come from the triggering run.
      - name: Docker tags
        id: meta
        uses: docker/metadata-action@dc802804100637a589fabce1cb79ff13a1411302 # v6.2.0
        with:
          images: |
            ${{ secrets.DOCKER_REGISTRY }}/fupi
            ghcr.io/${{ github.repository }}
          tags: |
            type=raw,value=${{ github.event.workflow_run.head_branch }}
            type=raw,value=latest,enable=${{ github.event.workflow_run.head_branch == 'main' }}
            type=raw,value=sha-${{ steps.version.outputs.short_sha }}

      - name: Build the image for scanning
        uses: docker/build-push-action@c3c9e263c25d99ce0380d002d59b67737d91b0dc # v7.4.0
        with:
          context: .
          load: true
          push: false
          tags: fupi-scan:ci
          build-args: |
            VERSION=${{ steps.version.outputs.version }}
            GIT_COMMIT=${{ github.event.workflow_run.head_sha }}
            BUILD_TIME=${{ steps.version.outputs.created }}
          cache-from: type=gha
          cache-to: type=gha,mode=max

      - name: Check the image is hardened
        run: scripts/image_test.sh image fupi-scan:ci

      - name: Scan the image for vulnerabilities
        uses: aquasecurity/trivy-action@ed142fd0673e97e23eac54620cfb913e5ce36c25 # v0.36.0
        with:
          image-ref: fupi-scan:ci
          severity: HIGH,CRITICAL
          ignore-unfixed: true
          exit-code: '1'

      - name: Log in to Docker Hub
        uses: docker/login-action@dbcb813823bdd20940b903addbd779551569679f # v4.6.0
        with:
          username: ${{ secrets.DOCKER_USERNAME }}
          password: ${{ secrets.DOCKER_PASSWORD }}

      - name: Log in to GitHub Container Registry
        uses: docker/login-action@dbcb813823bdd20940b903addbd779551569679f # v4.6.0
        with:
          registry: ghcr.io
          username: ${{ github.repository_owner }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Push the image to Docker Hub and GitHub Container Registry
        uses: docker/build-push-action@c3c9e263c25d99ce0380d002d59b67737d91b0dc # v7.4.0
        with:
          context: .
          platforms: linux/amd64,linux/arm64
          push: true
          tags: ${{ steps.meta.outputs.tags }}
          build-args: |
            VERSION=${{ steps.version.outputs.version }}
            GIT_COMMIT=${{ github.event.workflow_run.head_sha }}
            BUILD_TIME=${{ steps.version.outputs.created }}
          provenance: true
          sbom: true
          cache-from: type=gha
          cache-to: type=gha,mode=max
```

Overwrite `.github/workflows/build_app.yml`:

```yaml
name: Build

on:
  workflow_run:
    workflows:
      - "Tests"
    types:
      - completed

permissions:
  contents: read

jobs:

  build:
    name: Build
    if: ${{ github.event.workflow_run.conclusion == 'success' }}

    strategy:
      matrix:
        platform: [ubuntu-latest, macos-latest, windows-latest]
      fail-fast: false

    runs-on: ${{ matrix.platform }}

    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          ref: ${{ github.event.workflow_run.head_sha }}
          persist-credentials: false

      - name: Install Go
        uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
        with:
          go-version-file: go.mod

      - name: Run Build
        run: make build

      - name: Upload App Build
        uses: actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a # v7.0.1
        # due to possible limitations of artifact upload size limits, we can allow this failure
        continue-on-error: true
        with:
          name: fupi-${{ matrix.platform }}
          path: bin/fupi*
```

Overwrite `.github/workflows/deploy.yml` (secret names unchanged; one `flyctl secrets set` call, values passed through `env:`):

```yaml
name: Deploy

on:
  workflow_run:
    workflows:
      - "Build"
    branches:
      - 'main'
    types:
      - completed

permissions:
  contents: read

jobs:

  deploy:
    name: Deploy Application
    runs-on: ubuntu-latest
    if: ${{ github.event.workflow_run.conclusion == 'success' }}

    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          ref: ${{ github.event.workflow_run.head_sha }}
          persist-credentials: false

      # https://github.com/superfly/flyctl-actions
      - uses: superfly/flyctl-actions/setup-flyctl@fc53c09e1bc3be6f54706524e3b82c4f462f77be # 1.5

      # Secrets reach the script as environment variables, never interpolated into the script text.
      - name: Set secrets and deploy
        run: |
          flyctl secrets set \
            DATABASE_HOST="$DATABASE_HOST" \
            DATABASE="$DATABASE" \
            DATABASE_USERNAME="$DATABASE_USERNAME" \
            DATABASE_PASSWORD="$DATABASE_PASSWORD" \
            DATABASE_PORT="$DATABASE_PORT" \
            AUTH_SECRET="$AUTH_SECRET" \
            AUTH_EXPIRE="$AUTH_EXPIRE" \
            AUTH_ISSUER="$AUTH_ISSUER" \
            CACHE_HOST="$CACHE_HOST" \
            CACHE_PORT="$CACHE_PORT" \
            CACHE_USERNAME="$CACHE_USERNAME" \
            CACHE_PASSWORD="$CACHE_PASSWORD" \
            CACHE_REQUIRE_AUTH="$CACHE_REQUIRE_AUTH" \
            SENTRY_DSN="$SENTRY_DSN" \
            SENTRY_ENV="$SENTRY_ENV" \
            SENTRY_SAMPLE_RATE="$SENTRY_SAMPLE_RATE" \
            SENTRY_ENABLED="$SENTRY_ENABLED"
          flyctl deploy --remote-only
        env:
          FLY_API_TOKEN: ${{ secrets.FLY_API_TOKEN }}
          DATABASE_HOST: ${{ secrets.DATABASE_HOST }}
          DATABASE: ${{ secrets.DATABASE }}
          DATABASE_USERNAME: ${{ secrets.DATABASE_USERNAME }}
          DATABASE_PASSWORD: ${{ secrets.DATABASE_PASSWORD }}
          DATABASE_PORT: ${{ secrets.DATABASE_PORT }}
          AUTH_SECRET: ${{ secrets.AUTH_SECRET }}
          AUTH_EXPIRE: ${{ secrets.AUTH_EXPIRE }}
          AUTH_ISSUER: ${{ secrets.AUTH_ISSUER }}
          CACHE_HOST: ${{ secrets.CACHE_HOST }}
          CACHE_PORT: ${{ secrets.CACHE_PORT }}
          CACHE_USERNAME: ${{ secrets.CACHE_USERNAME }}
          CACHE_PASSWORD: ${{ secrets.CACHE_PASSWORD }}
          CACHE_REQUIRE_AUTH: ${{ secrets.CACHE_REQUIRE_AUTH }}
          SENTRY_DSN: ${{ secrets.SENTRY_DSN }}
          SENTRY_ENV: ${{ secrets.SENTRY_ENV }}
          SENTRY_SAMPLE_RATE: ${{ secrets.SENTRY_SAMPLE_RATE }}
          SENTRY_ENABLED: ${{ secrets.SENTRY_ENABLED }}
```

Overwrite `.github/workflows/dangerci.yml`:

```yaml
name: Danger CI

on:
  pull_request:
    branches:
      - 'develop'
      - 'main'

permissions:
  contents: read

jobs:
  test:
    name: Danger CI

    strategy:
      fail-fast: false
      matrix:
        node-version: [22.x]

    runs-on: ubuntu-latest

    steps:
    - name: Checkout
      uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
      with:
        persist-credentials: false

    - name: Use NodeJS ${{ matrix.node-version }}
      uses: actions/setup-node@820762786026740c76f36085b0efc47a31fe5020 # v7.0.0
      with:
        node-version: ${{ matrix.node-version }}

    - name: Get yarn cache directory path
      id: cache-dir-path
      run: echo "dir=$(yarn cache dir)" >> "$GITHUB_OUTPUT"

    - uses: actions/cache@55cc8345863c7cc4c66a329aec7e433d2d1c52a9 # v6.1.0
      id: cache # use this to check for `cache-hit` (`steps.cache.outputs.cache-hit != 'true'`)
      with:
        path: |
          **/node_modules
          ${{ steps.cache-dir-path.outputs.dir }}
        key: ${{ runner.os }}-cache-${{ hashFiles('**/yarn.lock') }}
        restore-keys: |
          ${{ runner.os }}-cache-${{ hashFiles('**/yarn.lock') }}

    - name: Install dependencies
      if: steps.cache.outputs.cache-hit != 'true'
      run: yarn add danger

    - name: Danger CI Check
      uses: danger/danger-js@279443b7b3fdf4488d79546b3d40dbd76c51d83a # 14.0.6
      env:
        CI: true
        GITHUB_TOKEN: ${{ secrets.GH_TOKEN }}
        DANGER_GITHUB_API_TOKEN: ${{ secrets.GH_TOKEN }}
```

Overwrite `.github/workflows/sentry.yml` (the release environment now follows the triggering run's branch):

```yaml
name: Sentry Release

on:
  workflow_run:
    workflows:
      - "Build"
    types:
      - completed
    branches:
      - main
      - develop

permissions:
  contents: read

jobs:
  sentryrelease:
    name: Sentry Release

    runs-on: ubuntu-latest

    steps:
    - name: Checkout
      uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
      with:
        ref: ${{ github.event.workflow_run.head_sha }}
        persist-credentials: false

    - name: Install Go
      uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
      with:
        go-version-file: go.mod

    - name: Install Dependencies
      run: make install

    - name: Run Build
      run: make build

    - name: Create Sentry Staging release
      if: github.event.workflow_run.head_branch == 'develop'
      uses: getsentry/action-release@ff07929a6537bac57790c3451cf4d364aca38528 # v3.7.0
      env:
        SENTRY_AUTH_TOKEN: ${{ secrets.SENTRY_GH_RELEASE_AUTH_TOKEN }}
        SENTRY_ORG: ${{ secrets.SENTRY_ORG }}
        SENTRY_PROJECT: ${{ secrets.SENTRY_PROJECT }}
      with:
        environment: staging

    - name: Create Sentry Production release
      if: github.event.workflow_run.head_branch == 'main'
      uses: getsentry/action-release@ff07929a6537bac57790c3451cf4d364aca38528 # v3.7.0
      env:
        SENTRY_AUTH_TOKEN: ${{ secrets.SENTRY_GH_RELEASE_AUTH_TOKEN }}
        SENTRY_ORG: ${{ secrets.SENTRY_ORG }}
        SENTRY_PROJECT: ${{ secrets.SENTRY_PROJECT }}
      with:
        environment: production
```

Overwrite `.github/workflows/slack.yml` (the workflow list now names `Tests`, which is what `tests.yml` is called):

```yaml
# Workflow only runs after the specified workflows have concluded running to completion. It then checks if they have
# been successful or failed. Appropriate messages are then sent afterwards on each event type
# Ref https://github.com/rtCamp/action-slack-notify
name: Slack Notification

on:
  workflow_run:
    workflows:
      - "Lint"
      - "Tests"
      - "Build"
      - "Release"
      - "Sentry Release"
      - "Docker"
      - "CodeQL"
      - "Gitlab RepoSync"
      - "BitBucket RepoSync"
    types:
      - completed

permissions:
  contents: read

jobs:
  onSuccess:
    name: Success Notification
    runs-on: ubuntu-latest
    if: ${{ github.event.workflow_run.conclusion == 'success' }}

    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          ref: ${{ github.event.workflow_run.head_branch }}
          persist-credentials: false

      - name: Slack Notification
        uses: rtCamp/action-slack-notify@33ca3be66c6f378fe1610fd1d5258632dbed5e58 # v2.4.0
        env:
          SLACK_CHANNEL: pipelines
          SLACK_COLOR: "good"
          SLACK_ICON: https://github.com/ratholos.png?size=48
          SLACK_ICON_EMOJI: ":large_green_circle:"
          SLACK_USERNAME: Ratholos
          SLACK_TITLE: ${{ github.repository }} - ${{ github.event.workflow_run.name }} Workflow Succeeded
          SLACK_MESSAGE: 'Success'
          SLACK_FOOTER: "Regards, Ratholos"
          SLACK_WEBHOOK: ${{ secrets.PIPELINES_SLACK_WEBHOOK }}

  onFailure:
    name: Failure Notification
    runs-on: ubuntu-latest
    if: ${{ github.event.workflow_run.conclusion == 'failure' }}

    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          ref: ${{ github.event.workflow_run.head_branch }}
          persist-credentials: false

      - name: Slack Notification
        uses: rtCamp/action-slack-notify@33ca3be66c6f378fe1610fd1d5258632dbed5e58 # v2.4.0
        env:
          SLACK_CHANNEL: pipelines
          SLACK_COLOR: "danger"
          SLACK_ICON: https://github.com/ratholos.png?size=48
          SLACK_ICON_EMOJI: ":red_circle:"
          SLACK_USERNAME: Ratholos
          SLACK_TITLE: ${{ github.repository }} - ${{ github.event.workflow_run.name }} Workflow Failed
          SLACK_MESSAGE: ':cry:'
          SLACK_FOOTER: "Regards, Ratholos"
          SLACK_WEBHOOK: ${{ secrets.PIPELINES_SLACK_WEBHOOK }}
```

Overwrite `.github/workflows/bitbucket_sync.yml`:

```yaml
# Ref: https://github.com/wangchucheng/git-repo-sync
name: BitBucket RepoSync

on:
  push:
    branches:
      - main
      - develop

permissions:
  contents: read

jobs:
  sync:
    runs-on: ubuntu-latest
    name: Bitbucket Repo Sync
    steps:

    - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
      with:
        fetch-depth: 0
        persist-credentials: false

    - name: Mirror to Bitbucket
      uses: wangchucheng/git-repo-sync@63782025e80e84c48b25a1ee6bb9a22a3bd570d3 # v0.1.0
      with:
        # Such as https://bitbucket.org/SomeUser/some-repo-sync.git
        target-url: ${{ secrets.BITBUCKET_REPO_URL }}
        # Such as SomeUser
        target-username: ${{ secrets.BITBUCKET_USERNAME }}
        # You can store token in your project's 'Setting > Secrets' and reference the name here. Such as ${{ secrets.ACCESS_TOKEN }}
        target-token: ${{ secrets.BITBUCKET_ACCESS_TOKEN }}
```

Overwrite `.github/workflows/gitlab_sync.yml`:

```yaml
# Ref: https://github.com/wangchucheng/git-repo-sync
name: Gitlab RepoSync

on:
  push:
    branches:
      - main
      - develop

permissions:
  contents: read

jobs:
  sync:
    runs-on: ubuntu-latest
    name: Gitlab Repo Sync
    steps:

    - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
      with:
        fetch-depth: 0
        persist-credentials: false

    - name: Mirror to Gitlab
      uses: wangchucheng/git-repo-sync@63782025e80e84c48b25a1ee6bb9a22a3bd570d3 # v0.1.0
      with:
        # Such as https://gitlab.com/SomeUser/some-repo-sync.git
        target-url: ${{ secrets.GITLAB_REPO_URL }}
        # Such as SomeUser
        target-username: ${{ secrets.GITLAB_USERNAME }}
        # You can store token in your project's 'Setting > Secrets' and reference the name here. Such as ${{ secrets.ACCESS_TOKEN }}
        target-token: ${{ secrets.GITLAB_ACCESS_TOKEN }}
```

Overwrite `.github/workflows/codeql.yml`:

```yaml
# reference https://github.com/github/codeql-action
name: "CodeQL"

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]
  schedule:
    # At 06:00 on Monday.
    - cron: '0 6 * * 1'

permissions:
  contents: read

jobs:
  CodeQL-Build:
    # CodeQL runs on ubuntu-latest, windows-latest, and macos-latest
    runs-on: ubuntu-latest

    permissions:
      # required for all workflows
      security-events: write

      # only required for workflows in private repositories
      actions: read
      contents: read

    steps:
      - name: Checkout repository
        uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          persist-credentials: false

      # go.mod needs Go 1.26, and Autobuild uses the Go on the runner.
      - name: Install Go
        uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
        with:
          go-version-file: go.mod

      # Initializes the CodeQL tools for scanning.
      - name: Initialize CodeQL
        uses: github/codeql-action/init@2892aa5e19bbd11bc0cff5427e3b750a04d9e3c2 # v4.38.2
        with:
          languages: go

      # Autobuild attempts to build any compiled languages. If it fails, replace it with an explicit build step.
      - name: Autobuild
        uses: github/codeql-action/autobuild@2892aa5e19bbd11bc0cff5427e3b750a04d9e3c2 # v4.38.2

      - name: Perform CodeQL Analysis
        uses: github/codeql-action/analyze@2892aa5e19bbd11bc0cff5427e3b750a04d9e3c2 # v4.38.2
```

Overwrite `.github/workflows/release.yml`:

```yaml
name: Release

on:
  workflow_run:
    workflows:
      - "Build"
    types:
      - completed
    branches:
      - main

permissions:
  contents: read

jobs:
  release:
    name: Release
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          ref: ${{ github.event.workflow_run.head_branch }}
          fetch-depth: 0

      - name: Release
        run: npx semantic-release
        env:
          GITHUB_TOKEN: ${{ secrets.GH_RELEASE_TOKEN }}
```

In `.github/workflows/tests.yml` change the three `go-version: [ '1.25' ]` matrix lines to `go-version: [ '1.26' ]`. In `.github/workflows/lint.yml` change `go-version: 1.25` to `go-version: '1.26'`.

In `.github/dependabot.yml`, append (the existing `gomod` entry stays as it is):

```yaml

  - package-ecosystem: docker
    open-pull-requests-limit: 5
    directory: "/"
    schedule:
      interval: "weekly"
      day: "saturday"
      time: "09:00"
      timezone: "Africa/Nairobi"
    labels:
      - "dependencies"
    reviewers:
      - "BrianLusina"

  - package-ecosystem: github-actions
    open-pull-requests-limit: 5
    directory: "/"
    schedule:
      interval: "weekly"
      day: "saturday"
      time: "09:00"
      timezone: "Africa/Nairobi"
    labels:
      - "dependencies"
    reviewers:
      - "BrianLusina"
```

- [ ] **Step 6: Run the guards and actionlint**

Run: `scripts/workflows_check.sh; echo "exit $?"; make lint.workflows; echo "exit $?"`
Expected: the guard prints nothing and exits 0; actionlint prints nothing and exits 0. If actionlint reports a finding (a renamed input in a newer major version, a shell issue), fix the workflow to the action's current documented inputs; do not silence the finding. If `docker/build-push-action`'s `provenance`/`sbom` inputs are rejected, check the action's README for the current syntax.

- [ ] **Step 7: Whole suite and commit**

Run: `bash scripts/infra_test.sh 2>&1 | tail -2; go test ./... 2>&1 | grep -v "no test files" | grep -v "^ok" | grep -v "^ *[│┌└]" | head; echo "suite: no failing lines above expected"`

```bash
git add scripts/workflows_check.sh .github .make/docker.mk
git commit -m "ci: pin and update every action, scan before publishing, fix the workflow_run checkouts"
```

---

### Task 6: Documentation and ADR-0016

**Files:**
- Create: `docs/adr/0016-runtime-image-is-distroless-and-defaults-to-production.md`
- Modify: `docs/LocalInfrastructure.md`
- Rewrite: `docs/Deployment.md`
- Modify: `README.md`

**Interfaces:** consumes the behavior built in Tasks 1-5; produces documentation only.

**Task test command:** `grep -c "The app in a container" docs/LocalInfrastructure.md && ls docs/adr | grep -c "^0016-" && go test ./...`

- [ ] **Step 1: Write ADR-0016**

Create `docs/adr/0016-runtime-image-is-distroless-and-defaults-to-production.md`:

```markdown
---
status: accepted
---

# The runtime image is distroless static, carries the migrator, and defaults to production

The API ships as one image built from `Dockerfile`: a pinned Go toolchain stage, then `gcr.io/distroless/static:nonroot` pinned by digest. The final stage has no shell and no package manager and runs as uid 65532. It holds `/app/fupi`, `/app/migrator` and `/app/migrations`, so one artifact can serve and migrate (ADR-0014).

Because the image has no `curl`, `wget` or shell, its `HEALTHCHECK` is a subcommand of the binary: `/app/fupi healthcheck` calls `GET /health` on the configured port. It probes liveness, not readiness, so a Postgres outage does not mark the container unhealthy and invite a restart that cannot help (ADR-0015).

The image sets `ENVIRONMENT=production`. A container started without `ENVIRONMENT` therefore refuses the development secrets instead of silently accepting them (the guard from the app-connectivity slice is off only when `ENVIRONMENT` is unset). The local compose stack overrides it with `development`.

The build context is an allowlist (`go.mod`, `go.sum`, `app/`, minus tests), so a secret or a new top-level directory can never enter the builder by accident.

## Considered options

- **Alpine, pinned, non-root** — a shell and `wget` make debugging and the health check easy, but busybox, musl and apk keep a larger surface for scanners to flag.
- **Leaving `ENVIRONMENT` unset in the image** — convenient for `docker run`, but a deployment that forgets it would run with the public development secrets.

## Consequences

- Debugging a running container uses `docker cp` or a distroless `:debug-nonroot` container on the same network; there is no `docker exec` shell.
- Every deployment must set the production secrets (see `docs/Deployment.md`) or the container exits 1 naming the missing variables.
- The digests of both base images go stale; Dependabot's `docker` ecosystem proposes the updates.
- The compose `migrate` job stays on the `migrate/migrate` image, so starting Postgres never needs an app build.
```

- [ ] **Step 2: Document the stack in `docs/LocalInfrastructure.md`**

Insert immediately before the line `## The stacks`:

````markdown
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

````

In the same file, in the "Make commands" table, change `` `kafka redis postgres elk observability legacy core full` `` to `` `kafka redis postgres elk observability legacy core full app` ``.

- [ ] **Step 3: Rewrite `docs/Deployment.md`**

Overwrite `docs/Deployment.md`:

````markdown
# Deployment

The application ships as one container image that holds the API and the database migrator.

## Build

```bash
make build.docker DOCKER_IMAGE_TAG=fupi-service    # stamps the image with its version, commit and build time
# or
docker build -t <IMAGE_NAME>:<IMAGE_TAG> --build-arg VERSION=<VERSION> --build-arg GIT_COMMIT=<SHA> --build-arg BUILD_TIME=<RFC3339> .
```

The image is built in two stages. The final stage is `gcr.io/distroless/static:nonroot`, pinned by digest: no shell, no package manager, uid 65532. It contains:

- `/app/fupi`, the API (the default entrypoint, port 8085);
- `/app/migrator`, which applies the database migrations (ADR-0014);
- `/app/migrations`, the SQL files the migrator reads (`MIGRATIONS_PATH` is preset to it).

`make lint.docker` runs hadolint with the rules in `hadolint.yaml`, and `make scan.docker` scans the image for fixable HIGH and CRITICAL vulnerabilities. CI does both before it publishes (`.github/workflows/docker.yml`).

Without Docker: `make build` writes the binary to `bin/fupi`.

## Configuration

The image sets `ENVIRONMENT=production`. In that mode the API and the migrator refuse the development secrets and exit 1 naming the variables that are missing, so a deployment must set at least:

| Variable | Meaning |
|---|---|
| `AUTH_SECRET` | the JWT signing secret |
| `DATABASE_HOST`, `DATABASE_NAME`, `DATABASE_USERNAME`, `DATABASE_PASSWORD` | Postgres (the primary, port 5432); or `DATABASE_URL` instead, which wins over those |
| `DATABASE_SSL_MODE` | defaults to `disable`; use `require` or stronger outside a private network (the API logs a warning otherwise) |
| `REDIS_ADDRESS`, `REDIS_PASSWORD` | a comma-separated `host:port` list (one address gives a plain client, several a cluster client) and the password; `REDIS_USERNAME` defaults to `fupi-svc` |

Every variable, its default and its unit is listed in the app-connectivity spec (`docs/superpowers/specs/2026-10-04-app-connectivity-design.md`, section 4); the local defaults are in `.env.example`.

## Migrations

The API never migrates at startup. Run the migrator from the same image, once per deploy, before starting the new version:

```bash
docker run --rm -e ENVIRONMENT=production -e DATABASE_HOST=... -e DATABASE_NAME=... -e DATABASE_USERNAME=... -e DATABASE_PASSWORD=... \
  --entrypoint /app/migrator <IMAGE_NAME>:<IMAGE_TAG>
```

It exits 0 when it applied the migrations or found nothing to do. If it reports a connection error because the database is still starting, run it again.

## Running

```bash
docker run -p 8085:8085 -e AUTH_SECRET=... -e DATABASE_HOST=... -e DATABASE_PASSWORD=... -e REDIS_ADDRESS=... -e REDIS_PASSWORD=... \
  --name fupi-api <IMAGE_NAME>:<IMAGE_TAG>
```

- `GET /health` is liveness (always 200 while the process runs); `GET /health/ready` is readiness (503 when Postgres is down or the process is draining).
- The image's `HEALTHCHECK` runs `/app/fupi healthcheck`, which calls `/health`.
- On SIGTERM the API stops accepting traffic and finishes in-flight requests for up to `SHUTDOWN_TIMEOUT` seconds (default 15). Give the orchestrator a stop grace period longer than that (the local compose stack uses 20 seconds).
- For hardening, also run it with a read-only root filesystem, `--cap-drop ALL` and `--security-opt no-new-privileges`, as `deploy/app/compose.yml` does.

## Local infrastructure

The supporting services (Postgres, Redis, Kafka, ELK, Prometheus, Grafana) run locally in Docker in either HA or single-node mode, and the API can run beside them as a container (`make infra.app.up`). See [Local infrastructure](./LocalInfrastructure.md).
````

- [ ] **Step 4: Update the README**

In `README.md`, append to the Docker paragraph (the one that ends `... directly on your development machine instead.`):

```markdown
 The API itself is built into a hardened image (`make build.docker`) and can run beside those services with `make infra.app.up`; see [Deployment](./docs/Deployment.md).
```
(keep it on the same paragraph: add it after the final sentence, separated by a space).

- [ ] **Step 5: Verify and commit**

Run: `grep -c "The app in a container" docs/LocalInfrastructure.md; ls docs/adr | grep -c "^0016-"; grep -c "infra.app.up" README.md docs/Deployment.md; go test ./... 2>&1 | grep -v "no test files" | grep -v "^ok" | grep -v "^ *[│┌└]" | head; echo "suite: no failing lines above expected"`
Expected: `1`, `1`, one match each in the README and Deployment.md, no failing lines.

```bash
git add docs README.md
git commit -m "docs: document the image, the app stack and production deployment; add ADR-0016"
```

---

### Task 7: Live verification, in single and HA mode

**Files:**
- Modify: `docs/superpowers/specs/2026-10-04-dockerfile-hardening-design.md` (append `## 12. Implementation notes`)

**Interfaces:** consumes everything built above; produces evidence and, if a drill fails, a fix with a test that failed first.

**Task test command:** `go test ./... && bash scripts/infra_test.sh && scripts/workflows_check.sh`

Preconditions: Docker is running; `docker ps --format '{{.Names}}'` shows no `fupi` containers (if the user's own containers are up, ask before `make infra.app.up`, which starts and may replace Postgres and Redis); port 8085 is free (`lsof -nP -iTCP:8085 -sTCP:LISTEN` prints nothing).

- [ ] **Step 1: The static checks and the image checks**

```bash
make lint.docker && make lint.workflows && make infra.config 2>&1 | tail -3
make build.docker DOCKER_IMAGE_TAG=fupi-service
scripts/image_test.sh context && scripts/image_test.sh image fupi-service
make scan.docker
```
Expected: no hadolint or actionlint findings; `ok: all profiles`; every `image_test.sh` check `ok:` with a `size:` line (record it); Trivy exits 0. Record the Trivy summary (counts per severity, including unfixed ones) for the notes.

- [ ] **Step 2: Start the stack in single mode**

```bash
make infra.app.up MODE=single 2>&1 | tail -12
```
Expected: `ready: postgres-single`, `ready: redis-single`, then the app build and `ready: app-single`.

- [ ] **Step 3: The running container**

```bash
docker inspect --format 'user={{.Config.User}} health={{.State.Health.Status}} readonly={{.HostConfig.ReadonlyRootfs}} caps={{.HostConfig.CapDrop}} sec={{.HostConfig.SecurityOpt}}' fupi-app-single-1
curl -s localhost:8085/health/ready; echo
docker logs fupi-app-single-1 2>&1 | grep -E "connected to DB|redis is|listening" | cut -c1-160
```
Expected: `user=65532:65532 health=healthy readonly=true caps=[ALL] sec=[no-new-privileges:true]`; `{"status":"ok","checks":{"postgres":"up","redis":"up"}}`; the three log lines.

- [ ] **Step 4: Behavior inside the container**

```bash
U=img-check-$(date +%s)
curl -s -o /dev/null -w 'register: [%{http_code}]\n' -X POST localhost:8085/api/v1/fupi/auth/register -H 'Content-Type: application/json' \
  -d "{\"username\":\"$U\",\"first_name\":\"Img\",\"email\":\"$U@example.com\",\"password\":\"Sup3r-secret-pw!\"}"
docker compose --profile '*' stop redis-single >/dev/null 2>&1; sleep 1; curl -s -w ' [%{http_code}]\n' localhost:8085/health/ready
docker compose --profile '*' start redis-single >/dev/null 2>&1; sleep 8; curl -s -w ' [%{http_code}]\n' localhost:8085/health/ready
```
Expected: `register: [201]`; with Redis stopped `{"status":"degraded",...,"redis":"down"} [200]`; after the restart `{"status":"ok",...} [200]` without restarting the app.

- [ ] **Step 5: Migrator from the image**

```bash
docker run --rm --network fupi -e ENVIRONMENT=development -e DATABASE_HOST=postgres --entrypoint /app/migrator fupi-app:local 2>&1 | tail -3
```
Expected: `migrate: no change` (the compose job already migrated) and exit 0.

- [ ] **Step 6: The stop drill (Review Focus 4)**

```bash
start=$(date +%s); docker compose --profile '*' stop app-single 2>&1 | tail -1; echo "stopped in $(( $(date +%s) - start ))s"
docker inspect --format 'exit={{.State.ExitCode}} oom={{.State.OOMKilled}}' fupi-app-single-1
docker logs fupi-app-single-1 2>&1 | grep -E "draining|shutting down" | cut -c1-120
```
Expected: it stops in well under 20 seconds, `exit=0 oom=false`, and the log shows `readiness: draining` then `shutting down server`. An exit code 137 means the grace period killed it: investigate (the drain, `stop_grace_period`) before going on.

- [ ] **Step 7: `app` down leaves Postgres and Redis running**

```bash
make infra.app.up MODE=single 2>&1 | tail -1
make infra.app.down; docker ps --format '{{.Names}}' | sort | tr '\n' ' '
```
Expected: `ready: app-single`; after `infra.app.down` the list contains the Postgres and Redis containers (and their exporters) but no `fupi-app-*`.

- [ ] **Step 8: HA mode**

```bash
make infra.app.up MODE=ha 2>&1 | tail -6
docker inspect --format 'health={{.State.Health.Status}}' fupi-app-ha-1
curl -s localhost:8085/health/ready; echo
docker logs fupi-app-ha-1 2>&1 | grep -E "redis is|connected to DB" | cut -c1-200
docker compose --profile '*' stop app-ha 2>&1 | tail -1; docker inspect --format 'exit={{.State.ExitCode}}' fupi-app-ha-1
```
Expected: `ready: postgres-ha`, `ready: redis-ha`, `ready: app-ha`; `health=healthy`; ready with both up; the log lists the six Redis addresses; the stop exits 0. (`make infra.app.up MODE=ha` first removes the single-mode Postgres and Redis containers, which is how `infra.sh` switches modes.)

- [ ] **Step 9: Tear down and run the full checks**

```bash
make infra.app.down; make infra.postgres.down MODE=ha; make infra.redis.down MODE=ha
docker ps --format '{{.Names}}'; echo "(nothing above = clean)"
bash scripts/infra_test.sh 2>&1 | tail -2; scripts/workflows_check.sh && echo "workflows ok"
go test -count=1 ./... 2>&1 | grep -v "no test files" | grep -v "^ok" | grep -v "^ *[│┌└]" | head; echo "suite: no failing lines above expected"
```
Expected: no containers left, `all tests passed`, `workflows ok`, no failing lines.

- [ ] **Step 10: Record the evidence and commit**

Append `## 12. Implementation notes` to `docs/superpowers/specs/2026-10-04-dockerfile-hardening-design.md` with: the resolved base image tags and digests (builder and runtime, and whether `debian13` or `debian12`), the image size, the Trivy summary and any recorded exception, one line per drill above with what was observed, the SHA pins that differed from the plan (if any), and anything that surprised you (including "none"). Then:

```bash
git add docs/superpowers/specs/2026-10-04-dockerfile-hardening-design.md
git commit -m "docs: record the live verification of the image and the app stack"
```
Expected: a clean `git status`.

---

### Task 8: Mirror CI (GitLab and Bitbucket)

Added at the user's request on 2026-10-04 (spec D11 and section 8b). Both pipelines run the same checks as GitHub; they were stale (`golang:1.18`) and failed at
their first step (`make setup-linting` is not a target).

**Files:**
- Create: `scripts/mirror_ci_check.sh` (executable)
- Rewrite: `.gitlab-ci.yml`, `bitbucket-pipelines.yml`
- Modify: `.gitlab/.gitlab-webide.yml` (`image: go:1.18` is not a valid image, use `golang:1.26`), `.make/docker.mk` (`lint.workflows` also runs the new guard), `docs/Deployment.md` (a "Continuous integration" section), the spec's implementation notes

**Interfaces:**
- Consumes: the commands GitHub runs (`tests.yml`, `build_app.yml`), `scripts/image_test.sh image <tag>` (Task 2), the `golangci/golangci-lint` version in `.make/dev.mk`, `hadolint.yaml`, Trivy flags (Task 3).
- Produces: `scripts/mirror_ci_check.sh`, run by `make lint.workflows` (Task 5).

**Task test command:** `scripts/mirror_ci_check.sh && make lint.workflows`

- [ ] **Step 1: Write the guard script and see it fail**

Create `scripts/mirror_ci_check.sh`:

```bash
#!/usr/bin/env bash
# Guards parity between the GitHub workflows and the mirror pipelines, .gitlab-ci.yml and bitbucket-pipelines.yml
# (docs/superpowers/specs/2026-10-04-dockerfile-hardening-design.md, section 8b). It checks, without running anything:
#  1. both mirror files parse as YAML;
#  2. every Go image has the same minor version as go.mod, and the golangci-lint image is the one `make lint` pins;
#  3. every command GitHub runs for the tests and the build also appears in each mirror;
#  4. the image checks (hadolint, build, scripts/image_test.sh, Trivy with the same flags) appear in each mirror;
#  5. Docker-based jobs disable Ryuk, no image is :latest, and every make target a mirror calls exists.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 1
status=0
fail() { echo "$1"; status=1; }

pipelines=(.gitlab-ci.yml bitbucket-pipelines.yml)

go_minor="$(sed -n 's/^go \([0-9][0-9]*\.[0-9][0-9]*\).*/\1/p' go.mod)"
lint_image="$(grep -oE 'golangci/golangci-lint:v[0-9.]+' .make/dev.mk | head -1)"
[ -n "$go_minor" ] || { echo "cannot read the Go version from go.mod"; exit 1; }
[ -n "$lint_image" ] || { echo "cannot read the golangci-lint image from .make/dev.mk"; exit 1; }

# What GitHub runs: the test commands (tests.yml) and the build (build_app.yml).
github_commands="$(sed -nE 's/^[[:space:]]*command:[[:space:]]*(make [A-Za-z0-9._-]+).*/\1/p' .github/workflows/tests.yml | sort -u)
$(sed -nE 's/^[[:space:]]*run:[[:space:]]*(make build)[[:space:]]*$/\1/p' .github/workflows/build_app.yml | sort -u)"

required=(
  "golangci-lint run"
  "hadolint --config hadolint.yaml Dockerfile"
  "docker build"
  "scripts/image_test.sh image"
  "trivy image"
  "--severity HIGH,CRITICAL"
  "--ignore-unfixed"
  "--exit-code 1"
  "TESTCONTAINERS_RYUK_DISABLED"
)

for f in "${pipelines[@]}"; do
  if [ ! -r "$f" ]; then fail "$f: missing"; continue; fi

  ruby -ryaml -e 'f = ARGV[0]; YAML.respond_to?(:unsafe_load_file) ? YAML.unsafe_load_file(f) : YAML.load_file(f)' "$f" >/dev/null 2>&1 ||
    fail "$f: does not parse as YAML"

  while IFS= read -r command; do
    [ -n "$command" ] || continue
    grep -qF -- "$command" "$f" || fail "$f: does not run \`$command\`, which GitHub runs"
  done <<<"$github_commands"

  grep -qF -- "$lint_image" "$f" || fail "$f: does not use $lint_image, the image make lint pins"
  for want in "${required[@]}"; do
    grep -qF -- "$want" "$f" || fail "$f: does not contain \`$want\`"
  done
  if grep -qE ':latest([[:space:]"]|$)' "$f"; then fail "$f: an image is :latest, pin a version"; fi

  while read -r _ target; do
    make -n "$target" >/dev/null 2>&1 || fail "$f: \`make $target\` is not a target of this Makefile"
  done < <(grep -oE '(^|[[:space:]])make [A-Za-z0-9._-]+' "$f" | sed -E 's/^[[:space:]]+//' | sort -u)
done

for f in "${pipelines[@]}" .gitlab/.gitlab-webide.yml; do
  [ -r "$f" ] || continue
  while IFS= read -r image; do
    [ "$image" = "golang:$go_minor" ] || fail "$f: $image does not match go.mod (go $go_minor)"
  done < <(grep -oE '(^|[[:space:]"'"'"'])go(lang)?:[0-9][0-9.]*' "$f" | sed -E 's/^[[:space:]"'"'"']//')
done

exit "$status"
```

Run: `chmod +x scripts/mirror_ci_check.sh && bash -n scripts/mirror_ci_check.sh && scripts/mirror_ci_check.sh; echo "exit $?"`
Expected: about 32 lines and `exit 1`: both files lack the test commands, the golangci-lint image, hadolint, `docker build`, Trivy and `TESTCONTAINERS_RYUK_DISABLED`; `make setup-linting` is not a target; `golang:1.18` and `go:1.18` do not match `go.mod`.

- [ ] **Step 2: Write `.gitlab-ci.yml` and fix the Web IDE image**

Overwrite `.gitlab-ci.yml`:

```yaml
# GitLab CI: the same verification as the GitHub workflows (lint, tests, build, image checks); scripts/mirror_ci_check.sh
# keeps the two in step. Publishing, releases, deploys, Sentry, Slack, CodeQL and Danger stay on GitHub, which also
# mirrors this repository here (.github/workflows/gitlab_sync.yml).
workflow:
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
    - if: $CI_COMMIT_BRANCH && $CI_OPEN_MERGE_REQUESTS
      when: never
    - if: $CI_COMMIT_BRANCH

default:
  image: golang:1.26
  interruptible: true

stages:
  - lint
  - test
  - build
  - image

variables:
  GOPATH: $CI_PROJECT_DIR/.go
  GOFLAGS: -buildvcs=false
  IMAGE_TAG: fupi-service:ci

.go-cache: &go-cache
  cache:
    key:
      files:
        - go.sum
    paths:
      - .go/pkg/mod/

# Jobs that need a Docker daemon (testcontainers, image builds) use Docker-in-Docker. TESTCONTAINERS_HOST_OVERRIDE makes
# the tests reach the containers they start on the daemon's host; Ryuk is off because the daemon is thrown away with the job.
.docker:
  services:
    - name: docker:29.8-dind
      alias: docker
  variables:
    DOCKER_HOST: tcp://docker:2375
    DOCKER_TLS_CERTDIR: ""
    TESTCONTAINERS_HOST_OVERRIDE: docker
    TESTCONTAINERS_RYUK_DISABLED: "true"

lint:
  stage: lint
  image: golangci/golangci-lint:v2.13.2
  <<: *go-cache
  script:
    - golangci-lint run -v

unit-tests:
  stage: test
  <<: *go-cache
  script:
    - make test.coverage
    - go tool cover -func=coverage.out | tail -1
  coverage: '/total:\s+\(statements\)\s+(\d+\.\d+)%/'
  artifacts:
    paths:
      - coverage.out
    expire_in: 1 week

integration-tests:
  stage: test
  extends: .docker
  <<: *go-cache
  script:
    - make test.integration

e2e-tests:
  stage: test
  extends: .docker
  <<: *go-cache
  script:
    - make test.e2e

build:
  stage: build
  <<: *go-cache
  script:
    - make build
  artifacts:
    paths:
      - bin/fupi
    expire_in: 1 week

docker-lint:
  stage: image
  image:
    name: hadolint/hadolint:v2.15.1-debian
    entrypoint: [""]
  script:
    - hadolint --config hadolint.yaml Dockerfile

docker-build:
  stage: image
  extends: .docker
  image: docker:29.8-cli
  before_script:
    - apk add --no-cache bash
  script:
    - docker build -t "$IMAGE_TAG" --build-arg VERSION="${CI_COMMIT_TAG:-$CI_COMMIT_SHORT_SHA}" --build-arg GIT_COMMIT="$CI_COMMIT_SHA" --build-arg BUILD_TIME="$CI_COMMIT_TIMESTAMP" .
    - scripts/image_test.sh image "$IMAGE_TAG"
    - docker save -o fupi-image.tar "$IMAGE_TAG"
  artifacts:
    paths:
      - fupi-image.tar
    expire_in: 1 day

# The image is scanned from the saved tar, so the scanner needs no Docker daemon and no shared paths.
docker-scan:
  stage: image
  needs:
    - docker-build
  image:
    name: aquasec/trivy:0.75.0
    entrypoint: [""]
  variables:
    TRIVY_CACHE_DIR: .trivycache
  cache:
    key: trivy-db
    paths:
      - .trivycache/
  script:
    - trivy image --quiet --no-progress --input fupi-image.tar --severity HIGH,CRITICAL --ignore-unfixed --exit-code 1
```

In `.gitlab/.gitlab-webide.yml` change `  image: go:1.18` to `  image: golang:1.26`.

- [ ] **Step 3: Write `bitbucket-pipelines.yml`**

Overwrite `bitbucket-pipelines.yml`:

```yaml
# ref https://support.atlassian.com/bitbucket-cloud/docs/configure-bitbucket-pipelinesyml/
# Bitbucket Pipelines: the same verification as the GitHub workflows (lint, tests, build, image checks);
# scripts/mirror_ci_check.sh keeps the two in step. Publishing, releases, deploys, Sentry, Slack, CodeQL and Danger stay on
# GitHub, which also mirrors this repository here (.github/workflows/bitbucket_sync.yml).
image: golang:1.26

definitions:
  caches:
    gomod: /go/pkg/mod
  services:
    docker:
      memory: 3072
  steps:
    - step: &lint
        name: Lint
        image: golangci/golangci-lint:v2.13.2
        caches:
          - gomod
        script:
          - golangci-lint run -v

    - step: &unit-tests
        name: Unit tests
        caches:
          - gomod
        script:
          - make test.coverage
          - go tool cover -func=coverage.out | tail -1
        artifacts:
          - coverage.out

    # The integration and e2e tests start their own containers (testcontainers) on the Docker service. Ryuk is off because
    # the daemon is thrown away with the step.
    - step: &integration-tests
        name: Integration tests
        size: 2x
        caches:
          - gomod
        services:
          - docker
        script:
          - export TESTCONTAINERS_RYUK_DISABLED=true
          - make test.integration

    - step: &e2e-tests
        name: E2E tests
        size: 2x
        caches:
          - gomod
        services:
          - docker
        script:
          - export TESTCONTAINERS_RYUK_DISABLED=true
          - make test.e2e

    - step: &build
        name: Build
        caches:
          - gomod
        script:
          - make build
        artifacts:
          - bin/fupi

    - step: &docker-lint
        name: Lint the Dockerfile
        image: hadolint/hadolint:v2.15.1-debian
        script:
          - hadolint --config hadolint.yaml Dockerfile

    - step: &docker-build
        name: Build and check the image
        size: 2x
        services:
          - docker
        script:
          - export IMAGE_TAG=fupi-service:ci
          - docker build -t "$IMAGE_TAG" --build-arg VERSION="$(echo "$BITBUCKET_COMMIT" | cut -c1-7)" --build-arg GIT_COMMIT="$BITBUCKET_COMMIT" --build-arg BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)" .
          - scripts/image_test.sh image "$IMAGE_TAG"
          - docker save -o fupi-image.tar "$IMAGE_TAG"
        artifacts:
          - fupi-image.tar

    # The image is scanned from the saved tar, so the scanner needs no Docker daemon and no shared paths.
    - step: &docker-scan
        name: Scan the image
        image: aquasec/trivy:0.75.0
        script:
          - trivy image --quiet --no-progress --input fupi-image.tar --severity HIGH,CRITICAL --ignore-unfixed --exit-code 1

pipelines:
  default: &verify
    - step: *lint
    - parallel:
        - step: *unit-tests
        - step: *integration-tests
        - step: *e2e-tests
    - step: *build
    - parallel:
        - step: *docker-lint
        - step: *docker-build
    - step: *docker-scan

  pull-requests:
    '**': *verify
```

- [ ] **Step 4: Wire the guard into `make lint.workflows`**

In `.make/docker.mk`, make the `lint.workflows` recipe (added in Task 5) start with both guards:

```make
lint.workflows: ## lints the GitHub workflows (actionlint, pinning rules) and checks the GitLab and Bitbucket pipelines against them
	@$(ROOT_DIR)/scripts/workflows_check.sh
	@$(ROOT_DIR)/scripts/mirror_ci_check.sh
	docker run --rm -v "$(ROOT_DIR):/repo:ro" -w /repo $(ACTIONLINT_IMAGE) -color
```

- [ ] **Step 5: Run the guard, then prove it bites**

Run: `scripts/mirror_ci_check.sh; echo "exit $?"; make lint.workflows >/dev/null 2>&1; echo "make exit $?"`
Expected: `exit 0` and `make exit 0`.

Break each file in turn, run the guard, and restore it: the Go image (`golang:1.26` to `golang:1.25`), Trivy's `--ignore-unfixed`, the golangci-lint version, a `make` target that does not exist, an `:latest` image, and `TESTCONTAINERS_RYUK_DISABLED`. Expected: the guard exits 1 each time and 0 after the restore.

- [ ] **Step 6: Run the commands the pipelines run**

```bash
make test.coverage && go tool cover -func=coverage.out | tail -1 && rm -f coverage.out
make test.integration
IMAGE_TAG=fupi-service:ci
docker build -t "$IMAGE_TAG" --build-arg VERSION="$(git rev-parse --short=7 HEAD)" --build-arg GIT_COMMIT="$(git rev-parse HEAD)" --build-arg BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)" .
scripts/image_test.sh image "$IMAGE_TAG" && make scan.docker DOCKER_IMAGE_TAG="$IMAGE_TAG"
docker rmi "$IMAGE_TAG"
```
Expected: every command exits 0 (the e2e command ran in Task 7). If a command fails, the pipeline would too: fix the cause before committing.

- [ ] **Step 7: Document it**

In `docs/Deployment.md`, insert a `## Continuous integration` section immediately before `## Local infrastructure`: the table of checks per CI (lint, unit tests with coverage, integration, e2e, build, Dockerfile lint, image build and checks, image scan), the line that publishing, releases, the Fly.io deploy, Sentry, Slack, CodeQL and Danger run on GitHub only, and the paragraph on Docker-in-Docker, the saved-tar scan and `make lint.workflows`.

- [ ] **Step 8: Whole suite and commit**

Run: `bash scripts/infra_test.sh 2>&1 | tail -1; go test ./... 2>&1 | grep -v "no test files" | grep -v "^ok" | grep -v "^ *[│┌└]" | head; echo "suite: no failing lines above expected"`

```bash
git add scripts/mirror_ci_check.sh .gitlab-ci.yml bitbucket-pipelines.yml .gitlab/.gitlab-webide.yml .make/docker.mk docs
git commit -m "ci: run the same lint, tests, build and image checks on GitLab and Bitbucket as on GitHub"
```
