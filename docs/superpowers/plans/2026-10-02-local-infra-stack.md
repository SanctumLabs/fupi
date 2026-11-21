# Local Infrastructure Stack Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a profile-driven Docker Compose stack (Kafka, Redis, Postgres, ELK, Prometheus/Grafana/Tempo, legacy Mongo) that runs each stack either HA or single-node, with `make infra.*` commands and documentation.

**Architecture:** The root `docker-compose.yml` `include:`s one `deploy/<stack>/compose.yml` per stack. HA and single-node variants are separate services selected by profiles (`<stack>-ha` / `<stack>-single`); single-node services carry a network alias equal to HA node 1 so client addresses never change between modes. `scripts/infra.sh` resolves profiles, removes the other mode's containers, starts the stack and waits for readiness; Make targets are one-liners over it.

**Tech Stack:** Docker Compose v2 (`include`, profiles), Bash/POSIX sh, apache/kafka (KRaft), Redis Cluster, Postgres 18 + Patroni + etcd + HAProxy, Elastic 9.x (ES/Logstash/Kibana/Filebeat) + nginx, OTel Collector, Tempo, Prometheus, Alertmanager, Grafana.

**Spec:** `docs/superpowers/specs/2026-10-01-local-infra-stack-design.md` (read it first; this plan implements §4–§9, with the refinements listed in Task 10).

## Global Constraints

- Branch `feat/local-infra-stack` (already checked out, branched from `feat/domain-url`). Commit steps run only after the user approves this plan. Commit messages end with `Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>`.
- **No Go code changes** in this slice. Nothing under `app/` is modified.
- **Pull gate:** before the first command that would pull an image (`docker compose up`, `docker run`, `docker compose run`, `docker build` of a not-yet-local base), **stop and ask the user for a go-ahead once**. The first pull is roughly 7–8 GiB. Static steps (`docker compose config`, shell tests) do not pull.
- **Memory gate:** Docker Desktop has about 7.7 GiB. Run one stack at a time, tear it down (`make infra.<stack>.down`, then volume cleanup with `echo y | make infra.clean`) before starting the next. `full-ha` is verified statically only.
- **System files:** never edit `/etc/hosts` or other system settings. When a step needs it, print the command and ask the user to run it.
- Entry point `docker-compose.yml` declares `name: fupi` and one bridge network `fupi`; each stack is included with `env_file: .env`; per-stack files live at `deploy/<stack>/compose.yml` with their configs beside them.
- Profiles are exactly: `kafka-ha kafka-single redis-ha redis-single postgres-ha postgres-single elk-ha elk-single observability legacy core-ha core-single full-ha full-single`. `core-*` = postgres + redis + kafka. `full-*` = core + elk + observability, never `legacy`.
- Per-mode service names and aliases: `kafka-single`→`kafka-1`, `redis-single`→`redis-1`, `postgres-single`→`postgres`, `es-single`→`es-1`, `logstash-single`→`logstash-1`; HAProxy (HA) also carries alias `postgres`.
- Address contract: Kafka host `localhost:19092` (HA also `29092`,`39092`), in-network `kafka-1:9092` (HA also `kafka-2:9092`,`kafka-3:9092`); Redis ports `7001..7006` (single `7001`), hostnames `redis-1..redis-6`, Redis serves logical DB 0 only; Postgres write `5432`, read `5433`, in-network `postgres:5432`; OTLP `4317` (gRPC) / `4318` (HTTP), in-network `otel-collector`.
- Every published port binds `127.0.0.1`.
- Image tags exactly as in spec §10.
- Every credential is `${VAR:-dev-default}` in compose and has the identical default in `.env.example`; values contain no quote, backslash or `$`.
- Kafka: HA replication factor 3 / `min.insync.replicas=2`; single 1 / 1; `auto.create.topics.enable=false`; heap 512m.
- Redis: `maxmemory 128mb`, `maxmemory-policy allkeys-lru`, `appendonly yes`, application ACL user `fupi-svc`.
- Elasticsearch: security on; transport TLS in HA only; HTTP TLS off; data stream `logs-fupi-default`; ILM rolls over daily and deletes after 7 days; heap 512m.
- Observability services are single-instance in every mode; Prometheus retention 7d.
- **Shell helper:** commands on a named service (`stop`, `start`, `exec`, `logs`, `ps`) need every profile visible. Define `dc() { docker compose --profile '*' "$@"; }` in each shell session and use `dc` for them. Also export `SCRATCH=/private/tmp/claude-501/-Users-lusina-Projects-SanctumLabs-fupi/061327a7-747e-4428-b372-1aeee47d71de/scratchpad` in each shell (the smoke program and temporary files live there). `docker compose --profile <p> config` is used as is.
- Make targets use the `infra.` prefix, `MODE ?= ha`, and depend on `create.envfile`. Existing image targets in `.make/docker.mk` are not touched.

## Review Focus

Failure modes the spec implies that no happy-path drill would catch, most likely first. Each has a test in the task named.

1. **Stale `.env`** (copied before these variables existed) leaves credentials empty. Expected: everything still works because compose falls back to the same defaults. → Task 1 (`infra_env_check.sh` enforces fallback == `.env.example`).
2. **Re-running `make infra.<stack>.up`** on a running stack must not re-initialise or break it (Redis cluster re-create, topic re-create, role re-create, ES re-provision). → Tasks 2, 3, 4, 6.
3. **Restarting with existing volumes** (`down` then `up`, not `clean`) must keep data and rejoin: Redis nodes keep their cluster identity despite container restarts, Patroni re-elects, ES reuses its certs. → Tasks 3, 5, 7.
4. **Switching mode** while the other mode is running must remove the other mode first rather than fail on a port clash. → Tasks 1 (dry-run test) and 2 (live).
5. **A password containing a shell/SQL-special character** must be rejected up front rather than corrupt a bootstrap script. → Task 1 (`infra_env_check.sh`).

## File Structure

| Path | Responsibility |
|---|---|
| `docker-compose.yml` | entry point: project name, network (static-IP range), `include:` of every stack |
| `deploy/<stack>/compose.yml` | the services of one stack, both modes |
| `deploy/<stack>/*` | that stack's configs and init scripts |
| `scripts/infra.sh` | profile resolution, `up`/`down`/`wait`, `DRY_RUN` |
| `scripts/infra_test.sh` | tests for `infra.sh` and `infra_env_check.sh` (no Docker needed) |
| `scripts/infra_env_check.sh` | env defaults == `.env.example`; no unsafe characters |
| `.make/docker.mk` | `infra.*` targets (appended; existing targets untouched) |
| `.env.example` | infra variables (appended) |
| `docs/LocalInfrastructure.md` | the user documentation |

## Host-side smoke program (throwaway, not committed)

Several tasks prove that an application running on the host can reach the stack. This small Go program does that. It lives in the scratchpad so `go.mod` is untouched (slice 3 adds the real clients).

Scratchpad: `/private/tmp/claude-501/-Users-lusina-Projects-SanctumLabs-fupi/061327a7-747e-4428-b372-1aeee47d71de/scratchpad` (`$SCRATCH` below).

- [ ] **Step 1: Create the module**

```bash
SCRATCH=/private/tmp/claude-501/-Users-lusina-Projects-SanctumLabs-fupi/061327a7-747e-4428-b372-1aeee47d71de/scratchpad
mkdir -p "$SCRATCH/infracheck" && cd "$SCRATCH/infracheck"
cat > main.go <<'GOEOF'
// Throwaway host-side smoke checks for the local infrastructure stack. Not part of the repository.
//
//	infracheck kafka    <broker,broker,...>
//	infracheck redis    <seed,seed,...>
//	infracheck postgres <host:port> <expect-in-recovery: true|false>
package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
	"github.com/twmb/franz-go/pkg/kgo"
)

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: infracheck kafka|redis|postgres <args>")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var err error
	switch os.Args[1] {
	case "kafka":
		err = kafkaCheck(ctx, strings.Split(os.Args[2], ","))
	case "redis":
		err = redisCheck(ctx, strings.Split(os.Args[2], ","))
	case "postgres":
		if len(os.Args) < 4 {
			err = fmt.Errorf("postgres needs <host:port> <expect-in-recovery>")
		} else {
			err = postgresCheck(ctx, os.Args[2], os.Args[3] == "true")
		}
	default:
		err = fmt.Errorf("unknown check %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}

func kafkaCheck(ctx context.Context, brokers []string) error {
	want := fmt.Sprintf("infracheck-%d", time.Now().UnixNano())
	cl, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ConsumeTopics("url.events"),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	)
	if err != nil {
		return err
	}
	defer cl.Close()

	if err := cl.ProduceSync(ctx, &kgo.Record{Topic: "url.events", Value: []byte(want)}).FirstErr(); err != nil {
		return fmt.Errorf("produce: %w", err)
	}
	for {
		fetches := cl.PollFetches(ctx)
		if errs := fetches.Errors(); len(errs) > 0 {
			return fmt.Errorf("fetch: %v", errs)
		}
		found := false
		fetches.EachRecord(func(r *kgo.Record) {
			if string(r.Value) == want {
				found = true
			}
		})
		if found {
			fmt.Println("kafka ok: produced and consumed", want)
			return nil
		}
	}
}

func redisCheck(ctx context.Context, seeds []string) error {
	c := redis.NewClusterClient(&redis.ClusterOptions{
		Addrs:    seeds,
		Username: env("REDIS_USERNAME", "fupi-svc"),
		Password: env("REDIS_PASSWORD", "fupi-svc"),
		// Cluster nodes announce hostnames (redis-1..redis-6). A real application resolves them through
		// /etc/hosts; this dialer does the same mapping so the check needs no system change.
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			if strings.HasPrefix(host, "redis-") {
				host = "127.0.0.1"
			}
			d := net.Dialer{Timeout: 3 * time.Second}
			return d.DialContext(ctx, network, net.JoinHostPort(host, port))
		},
	})
	defer c.Close()

	for i := 0; i < 50; i++ { // 50 keys spread across slots, so every shard is exercised
		key := fmt.Sprintf("infracheck:%d", i)
		if err := c.Set(ctx, key, i, time.Minute).Err(); err != nil {
			return fmt.Errorf("set %s: %w", key, err)
		}
		if got, err := c.Get(ctx, key).Int(); err != nil || got != i {
			return fmt.Errorf("get %s: got %d err %v", key, got, err)
		}
	}
	masters := 0
	if err := c.ForEachMaster(ctx, func(ctx context.Context, m *redis.Client) error { masters++; return nil }); err != nil {
		return err
	}
	fmt.Printf("redis ok: 50 keys set and read across %d master(s)\n", masters)
	return nil
}

func postgresCheck(ctx context.Context, hostPort string, wantRecovery bool) error {
	dsn := fmt.Sprintf("postgres://%s:%s@%s/%s?sslmode=disable",
		env("PG_APP_USER", "fupi-user"), env("PG_APP_PASSWORD", "fupi-pass"), hostPort, env("PG_DATABASE", "fupidb"))
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)

	var inRecovery bool
	if err := conn.QueryRow(ctx, "SELECT pg_is_in_recovery()").Scan(&inRecovery); err != nil {
		return err
	}
	if inRecovery != wantRecovery {
		return fmt.Errorf("%s: pg_is_in_recovery=%v, want %v", hostPort, inRecovery, wantRecovery)
	}
	fmt.Printf("postgres ok: %s pg_is_in_recovery=%v\n", hostPort, inRecovery)
	return nil
}
GOEOF
go mod init infracheck >/dev/null 2>&1
go get github.com/twmb/franz-go/pkg/kgo github.com/redis/go-redis/v9 github.com/jackc/pgx/v5 >/dev/null 2>&1
go build -o infracheck . && ./infracheck 2>&1 | head -2
```

Expected: `usage: infracheck kafka|redis|postgres <args>` (exit 2). If `go get` fails the program does not build; stop and fix before continuing.

---

## Task 1: Foundation — scripts, env, root compose, legacy stack, Make plumbing

**Files:**
- Create: `scripts/infra.sh`, `scripts/infra_test.sh`, `scripts/infra_env_check.sh`
- Create: `deploy/legacy/compose.yml`, and stub `deploy/{kafka,redis,postgres,elk,observability}/compose.yml`
- Modify (replace content): `docker-compose.yml`
- Modify: `.env.example` (append), `.make/docker.mk` (append)

**Interfaces:**
- Produces: `scripts/infra.sh <up|down|wait> <stack> [ha|single]` (profile resolution as in Global Constraints; `DRY_RUN=1` prints `+ <command>` lines; `wait` prints `ready: <profile>` or exits 1); `scripts/infra_env_check.sh` (exit 0/1; honours `ENV_EXAMPLE` and `COMPOSE_FILES` overrides); Make variables `MODE`, `INFRA`, `COMPOSE`, `KAFKA_SERVICE`, `REDIS_SERVICE`, `POSTGRES_SERVICE`, `ES_SERVICE`; targets `infra.{kafka,redis,postgres,elk,observability,legacy,core,full}.{up,down}`, `infra.config`, `infra.ps`, `infra.logs`, `infra.stats`, `infra.clean`, `infra.hosts`.
- Later tasks replace the stub compose files and append stack-specific helper targets.

- [ ] **Step 1: Commit the spec and this plan**

```bash
cd /Users/lusina/Projects/SanctumLabs/fupi
git add docs/superpowers
git commit -m "docs: add local infrastructure stack spec and implementation plan" \
  -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

- [ ] **Step 2: Write the failing tests**

Create `scripts/infra_test.sh`:

```bash
#!/usr/bin/env bash
# Tests for scripts/infra.sh and scripts/infra_env_check.sh. Needs no Docker: infra.sh runs with DRY_RUN=1.
# shellcheck disable=SC2016  # the fixtures below write a literal ${VAR:-default} and $ on purpose
set -uo pipefail
cd "$(dirname "$0")/.." || exit 1

failures=0
pass() { echo "ok:   $1"; }
fail() { echo "FAIL: $1"; failures=$((failures + 1)); }

expect_output() { # name expected command...
  local name="$1" expected="$2"
  shift 2
  local actual
  actual="$("$@" 2>&1)"
  if [ "$actual" = "$expected" ]; then
    pass "$name"
  else
    fail "$name"
    printf -- '--- expected\n%s\n--- actual\n%s\n' "$expected" "$actual"
  fi
}

expect_exit() { # name expected-code command...
  local name="$1" want="$2"
  shift 2
  "$@" >/dev/null 2>&1
  local got=$?
  if [ "$got" -eq "$want" ]; then pass "$name"; else fail "$name (exit $got, want $want)"; fi
}

export DRY_RUN=1 COMPOSE="docker compose"

# --- infra.sh ---------------------------------------------------------------------------------------------------
expect_output "up kafka single removes the ha containers first" \
"+ docker compose --profile kafka-ha rm -sf
+ docker compose --profile kafka-single up -d
+ wait kafka-single" \
  scripts/infra.sh up kafka single

expect_output "mode defaults to ha" \
"+ docker compose --profile kafka-single rm -sf
+ docker compose --profile kafka-ha up -d
+ wait kafka-ha" \
  scripts/infra.sh up kafka

expect_output "aggregate stacks follow the mode" \
"+ docker compose --profile core-ha rm -sf
+ docker compose --profile core-single up -d
+ wait core-single" \
  scripts/infra.sh up core single

expect_output "observability has no mode and nothing to remove" \
"+ docker compose --profile observability up -d
+ wait observability" \
  scripts/infra.sh up observability single

expect_output "down removes both modes of a stack" \
"+ docker compose --profile redis-ha --profile redis-single rm -sf" \
  scripts/infra.sh down redis

expect_output "down legacy" \
"+ docker compose --profile legacy rm -sf" \
  scripts/infra.sh down legacy

expect_output "wait resolves the profile" \
"+ wait postgres-single" \
  scripts/infra.sh wait postgres single

expect_exit "unknown stack is a usage error" 2 scripts/infra.sh up nope
expect_exit "unknown mode is a usage error" 2 scripts/infra.sh up kafka triple
expect_exit "missing arguments is a usage error" 2 scripts/infra.sh
expect_exit "unknown action is a usage error" 2 scripts/infra.sh restart kafka

# --- infra_env_check.sh -----------------------------------------------------------------------------------------
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
printf '# --- Local infrastructure\nPG_APP_PASSWORD=it%ss\n' "'" >"$tmp/unsafe-quote.env"
printf '# --- Local infrastructure\nPG_APP_PASSWORD=pa$ss\n' >"$tmp/unsafe-dollar.env"
printf '# --- Local infrastructure\nPG_APP_PASSWORD=safe\n' >"$tmp/safe.env"
printf 'x: ${PG_APP_PASSWORD:-safe}\n' >"$tmp/match.yml"
printf 'x: ${PG_APP_PASSWORD:-other}\n' >"$tmp/mismatch.yml"
printf 'x: ${PG_UNKNOWN:-v}\n' >"$tmp/unknown.yml"

expect_exit "env check rejects a quote in a value" 1 \
  env ENV_EXAMPLE="$tmp/unsafe-quote.env" COMPOSE_FILES=/dev/null scripts/infra_env_check.sh
expect_exit "env check rejects a dollar sign in a value" 1 \
  env ENV_EXAMPLE="$tmp/unsafe-dollar.env" COMPOSE_FILES=/dev/null scripts/infra_env_check.sh
expect_exit "env check accepts safe values with matching defaults" 0 \
  env ENV_EXAMPLE="$tmp/safe.env" COMPOSE_FILES="$tmp/match.yml" scripts/infra_env_check.sh
expect_exit "env check rejects a default that differs from .env.example" 1 \
  env ENV_EXAMPLE="$tmp/safe.env" COMPOSE_FILES="$tmp/mismatch.yml" scripts/infra_env_check.sh
expect_exit "env check rejects a variable missing from .env.example" 1 \
  env ENV_EXAMPLE="$tmp/safe.env" COMPOSE_FILES="$tmp/unknown.yml" scripts/infra_env_check.sh
expect_exit "env check passes on the repository" 0 scripts/infra_env_check.sh

# --- infra.sh wait (a stub `docker` supplies canned compose output) ---------------------------------------------
mkdir -p "$tmp/bin"
cat >"$tmp/bin/docker" <<'STUB'
#!/usr/bin/env bash
case "$*" in
  *"config --services"*) printf '%s\n' kafka-init-single kafka-single kafka-ui ;;
  *" ps "*) cat "$STUB_STATUS" ;;
esac
STUB
chmod +x "$tmp/bin/docker"

wait_with() { # one status line per service: service|state|health|exitcode
  printf '%s\n' "$1" >"$tmp/status"
  env -u DRY_RUN PATH="$tmp/bin:$PATH" STUB_STATUS="$tmp/status" WAIT_TIMEOUT=0 scripts/infra.sh wait kafka single
}

job_running='kafka-single|running|healthy|0
kafka-init-single|running||0
kafka-ui|running||0'
job_done='kafka-single|running|healthy|0
kafka-init-single|exited||0
kafka-ui|running||0'
job_failed='kafka-single|running|healthy|0
kafka-init-single|exited||1
kafka-ui|running||0'
broker_starting='kafka-single|running|starting|0
kafka-init-single|exited||0
kafka-ui|running||0'

expect_exit "wait: a still-running one-shot job is not ready" 1 wait_with "$job_running"
out="$(wait_with "$job_running" 2>&1)"
case "$out" in *"waiting for: kafka-init-single"*) pass "wait: the message names the pending job" ;; *) fail "wait: the message names the pending job ($out)" ;; esac
expect_exit "wait: ready when the job exited 0 and a service without a healthcheck is running" 0 wait_with "$job_done"
expect_exit "wait: a job that exited non-zero is not ready" 1 wait_with "$job_failed"
expect_exit "wait: a service still starting is not ready" 1 wait_with "$broker_starting"
job_failed_out="$(wait_with "$job_failed" 2>&1)"
case "$job_failed_out" in *"kafka-init-single exited with code 1"*) pass "wait: a failed job is reported by name with its exit code" ;; *) fail "wait: a failed job is reported by name with its exit code ($job_failed_out)" ;; esac


# --- deploy/redis/init-cluster.sh (a stub redis-cli records the cluster-changing calls) ---------------------------
cat >"$tmp/bin/redis-cli" <<'STUB'
#!/usr/bin/env bash
# Records the calls that change the cluster; reports a formed cluster once one happened, or when STUB_FORMED=1.
case "$*" in
  *"--cluster create"* | *addslotsrange*) echo "$*" >>"$STUB_LOG"; touch "$STUB_DIR/formed" ;;
  *ping*) echo PONG ;;
  *"cluster info"*)
    if [ -e "$STUB_DIR/formed" ] || [ "${STUB_FORMED:-}" = 1 ]; then echo "cluster_state:ok"; else echo "cluster_state:fail"; fi
    ;;
esac
STUB
chmod +x "$tmp/bin/redis-cli"

redis_init() { # mode already-formed(0|1); the recorded calls end up in $tmp/rstub/log
  rm -rf "$tmp/rstub"
  mkdir -p "$tmp/rstub"
  : >"$tmp/rstub/log"
  env PATH="$tmp/bin:$PATH" STUB_DIR="$tmp/rstub" STUB_LOG="$tmp/rstub/log" STUB_FORMED="$2" \
    CLUSTER_MODE="$1" REDIS_ADMIN_PASSWORD=x NET_PREFIX=172.29.0 sh deploy/redis/init-cluster.sh
}
expect_calls() { # name expected
  local actual
  actual="$(cat "$tmp/rstub/log")"
  if [ "$actual" = "$2" ]; then pass "$1"; else fail "$1"; printf -- '--- expected\n%s\n--- actual\n%s\n' "$2" "$actual"; fi
}

redis_init ha 0 >/dev/null 2>&1
expect_calls "redis init (ha): creates the cluster over the fixed addresses .11 to .16" \
  "--cluster create 172.29.0.11:7001 172.29.0.12:7002 172.29.0.13:7003 172.29.0.14:7004 172.29.0.15:7005 172.29.0.16:7006 --cluster-replicas 1 --cluster-yes"
redis_init ha 1 >/dev/null 2>&1
expect_calls "redis init (ha): does nothing when the cluster is already formed" ""
redis_init single 0 >/dev/null 2>&1
expect_calls "redis init (single): assigns every slot to the one node" "-h redis-1 -p 7001 cluster addslotsrange 0 16383"
redis_init single 1 >/dev/null 2>&1
expect_calls "redis init (single): does nothing when the slots are already assigned" ""

if [ "$failures" -ne 0 ]; then
  echo "$failures failure(s)"
  exit 1
fi
echo "all tests passed"
```

- [ ] **Step 3: Run the tests to verify they fail**

```bash
chmod +x scripts/infra_test.sh && scripts/infra_test.sh 2>&1 | tail -5
```

Expected: every case prints `FAIL:` (the scripts do not exist yet) and the run ends `N failure(s)`, exit 1.

- [ ] **Step 4: Write `scripts/infra.sh`**

```bash
#!/usr/bin/env bash
# Starts, stops or waits on a local infrastructure stack in a given mode (see docs/LocalInfrastructure.md).
#
# Usage: scripts/infra.sh <up|down|wait> <stack> [ha|single]
#   stacks: kafka redis postgres elk core full (the mode applies) | observability legacy (no mode)
# Env:    COMPOSE       compose command (default "docker compose")
#         WAIT_TIMEOUT  seconds to wait for services to become ready (default 300)
#         DRY_RUN=1     print the commands instead of running them
set -euo pipefail

cd "$(dirname "$0")/.."

COMPOSE="${COMPOSE:-docker compose}"
WAIT_TIMEOUT="${WAIT_TIMEOUT:-300}"
read -r -a compose <<<"$COMPOSE"

usage() {
  sed -n '2,9p' "$0" | sed 's/^# \{0,1\}//' >&2
  exit 2
}

run() {
  if [ -n "${DRY_RUN:-}" ]; then echo "+ $*"; else "$@"; fi
}

action="${1:-}"
stack="${2:-}"
mode="${3:-ha}"
[ -n "$action" ] && [ -n "$stack" ] || usage

case "$mode" in
  ha) other=single ;;
  single) other=ha ;;
  *) echo "unknown mode '$mode' (use ha or single)" >&2; exit 2 ;;
esac

case "$stack" in
  kafka | redis | postgres | elk | core | full)
    profile="$stack-$mode"
    other_profile="$stack-$other"
    both=(--profile "$stack-ha" --profile "$stack-single")
    ;;
  observability | legacy)
    profile="$stack"
    other_profile=""
    both=(--profile "$stack")
    ;;
  *) echo "unknown stack '$stack'" >&2; usage ;;
esac

# One-shot jobs (topic creation, cluster formation, migrations, Elasticsearch setup) are named *-init-*, *-setup-* or
# migrate. A job is ready only once it has exited 0; any other service is ready when it is running and healthy (or has
# no healthcheck). Without this a still-running job, which has no healthcheck, would count as ready.
is_job() {
  case "$1" in
    *-init-* | *-setup-* | migrate) return 0 ;;
    *) return 1 ;;
  esac
}

wait_ready() {
  local p="$1" deadline services status pending s line state health code
  if [ -n "${DRY_RUN:-}" ]; then echo "+ wait $p"; return 0; fi
  deadline=$(($(date +%s) + WAIT_TIMEOUT))
  services="$("${compose[@]}" --profile "$p" config --services | sort)"
  while :; do
    status="$("${compose[@]}" --profile "$p" ps -a --format '{{.Service}}|{{.State}}|{{.Health}}|{{.ExitCode}}')"
    pending=""
    for s in $services; do
      line="$(printf '%s\n' "$status" | grep "^$s|" | head -1 || true)"
      state="$(printf '%s' "$line" | cut -d'|' -f2)"
      health="$(printf '%s' "$line" | cut -d'|' -f3)"
      code="$(printf '%s' "$line" | cut -d'|' -f4)"
      if is_job "$s"; then
        if [ "$state" = exited ] && [ "$code" = 0 ]; then continue; fi
        # A job that failed will not run again (restart: "no"), so waiting out the timeout only delays the error.
        # migrate is the exception: it restarts on failure until the database accepts connections.
        if [ "$state" = exited ] && [ "$s" != migrate ]; then
          echo "$s exited with code $code" >&2
          "${compose[@]}" --profile "$p" logs --no-log-prefix --tail 20 "$s" >&2 || true
          return 1
        fi
      elif [ "$state" = running ] && { [ -z "$health" ] || [ "$health" = healthy ]; }; then
        continue
      fi
      pending="$pending $s"
    done
    if [ -z "$pending" ]; then echo "ready: $p"; return 0; fi
    if [ "$(date +%s)" -ge "$deadline" ]; then
      echo "timed out after ${WAIT_TIMEOUT}s waiting for:$pending" >&2
      printf '%s\n' "$status" | grep -E "^($(echo "$pending" | tr ' ' '|' | sed 's/^|//'))\|" >&2 || true
      return 1
    fi
    sleep 3
  done
}

case "$action" in
  up)
    [ -z "$other_profile" ] || run "${compose[@]}" --profile "$other_profile" rm -sf
    run "${compose[@]}" --profile "$profile" up -d
    wait_ready "$profile"
    ;;
  down) run "${compose[@]}" "${both[@]}" rm -sf ;;
  wait) wait_ready "$profile" ;;
  *) usage ;;
esac
```

Then `chmod +x scripts/infra.sh`.

- [ ] **Step 5: Run the tests; the `infra.sh` cases pass, the env-check cases still fail**

```bash
scripts/infra_test.sh 2>&1 | grep -E "^(ok|FAIL)" | sort | uniq -c | sort -rn | head -20
```

Expected: the 11 `infra.sh` cases are `ok`; the six `env check` cases are `FAIL` (script missing).

- [ ] **Step 6: Write `scripts/infra_env_check.sh`**

```bash
#!/usr/bin/env bash
# Guards the "works with an empty or stale .env" promise:
#  1. every ${VAR:-default} in the compose files must use exactly the default listed in .env.example;
#  2. values in the infrastructure section of .env.example must be safe to interpolate into shell and SQL
#     (no quotes, backslashes or dollar signs).
# Env: ENV_EXAMPLE (default .env.example), COMPOSE_FILES (default: root and deploy/*/compose.yml).
set -euo pipefail

cd "$(dirname "$0")/.."
ENV_EXAMPLE="${ENV_EXAMPLE:-.env.example}"
shopt -s nullglob
default_files=(docker-compose.yml deploy/*/compose.yml)
COMPOSE_FILES="${COMPOSE_FILES:-${default_files[*]}}"
status=0

awk '
  /^# --- Local infrastructure/ { on = 1; next }
  on && /^[A-Z0-9_]+=/ {
    key = substr($0, 1, index($0, "=") - 1)
    val = substr($0, index($0, "=") + 1)
    if (index(val, "\"") || index(val, "\\") || index(val, "$") || index(val, sprintf("%c", 39))) {
      print "unsafe character in " key
      bad = 1
    }
  }
  END { exit bad }
' "$ENV_EXAMPLE" || status=1

# shellcheck disable=SC2086
while IFS='|' read -r file var def; do
  [ -n "$var" ] || continue
  want="$(grep -E "^${var}=" "$ENV_EXAMPLE" | head -1 | cut -d= -f2- || true)"
  if [ -z "$want" ]; then
    echo "$file: \${$var:-...} has no entry in $ENV_EXAMPLE"
    status=1
  elif [ "$want" != "$def" ]; then
    echo "$file: \${$var:-$def} differs from $ENV_EXAMPLE ($want)"
    status=1
  fi
done < <(grep -o -H -E '\$\{[A-Z0-9_]+:-[^}]*\}' $COMPOSE_FILES 2>/dev/null |
  sed -E 's/^([^:]+):\$\{([A-Z0-9_]+):-([^}]*)\}$/\1|\2|\3/')

exit "$status"
```

Then `chmod +x scripts/infra_env_check.sh`.

- [ ] **Step 7: Run the tests; all pass**

```bash
scripts/infra_test.sh
```

Expected: ends `all tests passed`. (The "passes on the repository" case holds trivially: no infra section or compose defaults exist yet.)

- [ ] **Step 8: Append the infrastructure variables to `.env.example`**

`.env.example` currently ends without a newline after `REDIS_MASTER_NAME=fupi-cache`. Use the Edit tool with `old_string` = `REDIS_MASTER_NAME=fupi-cache` and `new_string` = the text below (it begins with the same line):

```text
REDIS_MASTER_NAME=fupi-cache

# --- Local infrastructure (docker compose, see docs/LocalInfrastructure.md) ---------------------------------
# Development-only defaults. Compose falls back to these same values when a variable is unset, so an older
# .env keeps working. Never reuse any of them outside local development. Values must not contain quotes,
# backslashes or dollar signs (make infra.config checks this).
# REDIS_USERNAME and REDIS_PASSWORD above are the application's Redis user.
FUPI_NET_PREFIX=172.29.0

KAFKA_CLUSTER_ID=MkU3OEVBNTcwNTJENDM2Qk
KAFKA_HEAP=512m

REDIS_ADMIN_PASSWORD=fupi-redis-admin

PG_DATABASE=fupidb
PG_APP_USER=fupi-user
PG_APP_PASSWORD=fupi-pass
PG_SUPERUSER_PASSWORD=fupi-postgres-admin
PG_REPLICATION_PASSWORD=fupi-replication
PG_EXPORTER_PASSWORD=fupi-exporter

ELASTIC_PASSWORD=fupi-elastic-dev
KIBANA_SYSTEM_PASSWORD=fupi-kibana-dev
LOGSTASH_WRITER_PASSWORD=fupi-logstash-dev
GRAFANA_READER_PASSWORD=fupi-grafana-reader
METRICS_READER_PASSWORD=fupi-metrics-dev
KIBANA_SECURITY_KEY=fupi-dev-kibana-security-key-0123456789ab
KIBANA_ENCRYPTED_OBJECTS_KEY=fupi-dev-kibana-saved-objects-key-0123456789
KIBANA_REPORTING_KEY=fupi-dev-kibana-reporting-key-0123456789ab
ES_HEAP=512m
LS_HEAP=512m

GRAFANA_ADMIN_USER=admin
GRAFANA_ADMIN_PASSWORD=fupi-grafana-dev
```

- [ ] **Step 9: Create the stub stack files**

Every included file redeclares the shared network with the same minimal definition (`name: fupi`) and no `external:` flag; the root file owns the driver and subnet. Declaring it `external: true` in an included file makes Compose treat the network as pre-existing, so it would refuse to start and ignore the subnet that the Redis static IPs depend on.

Create each of `deploy/kafka/compose.yml`, `deploy/redis/compose.yml`, `deploy/postgres/compose.yml`, `deploy/elk/compose.yml`, `deploy/observability/compose.yml` with exactly:

```yaml
# Stub: replaced by the stack's task in the implementation plan.
services: {}

networks:
  fupi:
    name: fupi
```

- [ ] **Step 10: Create `deploy/legacy/compose.yml`**

This moves the existing Mongo and standalone Redis services unchanged (image, credentials, volume names so existing data is kept), adds the `legacy` profile and the shared network, and binds ports to localhost per the spec.

```yaml
# Legacy stack: MongoDB and the standalone Redis used by the code behind the `legacy` build tag.
# Kept as it was while the code base moves to Postgres (MongoDB 4.4 is end-of-life). Start with: make infra.legacy.up
services:
  documentdb:
    image: mongo:4.4.14
    container_name: fupi-documentdb
    hostname: documentdb
    profiles: [legacy]
    ports:
      - "127.0.0.1:27017:27017"
    environment:
      MONGO_INITDB_ROOT_USERNAME: fupiUser
      MONGO_INITDB_ROOT_PASSWORD: fupiPassword
      MONGO_INITDB_DATABASE: fupidb
      MONGO_INITDB_USER: fupiUser
      MONGO_INITDB_PASSWORD: fupiPassword
    volumes:
      - docdb:/data/db
    networks:
      - fupi

  cache:
    image: redis:7.0.2
    container_name: fupi-cache
    hostname: cache
    profiles: [legacy]
    ports:
      - "127.0.0.1:6379:6379"
    volumes:
      - cache:/data
    networks:
      - fupi

volumes:
  docdb:
  cache:

networks:
  fupi:
    name: fupi
```

- [ ] **Step 11: Replace `docker-compose.yml`**

First capture the old rendering for the equivalence check: `docker compose config > "$SCRATCH/old-compose.rendered.yml"` (run before replacing). Then replace the file's content with:

```yaml
# Local infrastructure for Fupi. Nothing starts without a profile: use `make infra.<stack>.up`
# (see docs/LocalInfrastructure.md). Each stack lives in deploy/<stack>/compose.yml.
name: fupi

include:
  - path: deploy/kafka/compose.yml
    env_file: .env
  - path: deploy/redis/compose.yml
    env_file: .env
  - path: deploy/postgres/compose.yml
    env_file: .env
  - path: deploy/elk/compose.yml
    env_file: .env
  - path: deploy/observability/compose.yml
    env_file: .env
  - path: deploy/legacy/compose.yml
    env_file: .env

networks:
  fupi:
    name: fupi
    driver: bridge
    # Redis Cluster nodes persist their peers' IP addresses, so they get fixed addresses (.11-.16) from the
    # lower half of the subnet; every other container is assigned from the upper half (ip_range).
    ipam:
      config:
        - subnet: ${FUPI_NET_PREFIX:-172.29.0}.0/24
          ip_range: ${FUPI_NET_PREFIX:-172.29.0}.128/25
```

- [ ] **Step 12: Append the Make plumbing to `.make/docker.mk`**

Append (leave every existing line as is):

```make

############################################################################################################################################################################################################
## Local infrastructure (docker compose, see docs/LocalInfrastructure.md)
############################################################################################################################################################################################################
# MODE picks the topology for stacks that have one: ha (default) or single, e.g. make infra.kafka.up MODE=single
MODE ?= ha
INFRA := $(ROOT_DIR)/scripts/infra.sh
COMPOSE := docker compose --project-directory $(ROOT_DIR)
INFRA_PROFILES := kafka-ha kafka-single redis-ha redis-single postgres-ha postgres-single elk-ha elk-single observability legacy core-ha core-single full-ha full-single

# Service that answers for "node 1" of a stack in the selected mode, used by the debugging helpers
KAFKA_SERVICE := $(if $(filter single,$(MODE)),kafka-single,kafka-1)
REDIS_SERVICE := $(if $(filter single,$(MODE)),redis-single,redis-1)
POSTGRES_SERVICE := $(if $(filter single,$(MODE)),postgres-single,patroni-1)
ES_SERVICE := $(if $(filter single,$(MODE)),es-single,es-1)

.PHONY: infra.kafka.up infra.kafka.down
infra.kafka.up: create.envfile ## Start Kafka and Kafka UI (MODE=ha or single)
	@$(INFRA) up kafka $(MODE)
infra.kafka.down: ## Stop Kafka (data is kept)
	@$(INFRA) down kafka

.PHONY: infra.redis.up infra.redis.down
infra.redis.up: create.envfile ## Start the Redis cluster (MODE=ha or single)
	@$(INFRA) up redis $(MODE)
infra.redis.down: ## Stop Redis (data is kept)
	@$(INFRA) down redis

.PHONY: infra.postgres.up infra.postgres.down
infra.postgres.up: create.envfile ## Start Postgres and run migrations (MODE=ha or single)
	@$(INFRA) up postgres $(MODE)
infra.postgres.down: ## Stop Postgres (data is kept)
	@$(INFRA) down postgres

.PHONY: infra.elk.up infra.elk.down
infra.elk.up: create.envfile ## Start Elasticsearch Logstash Kibana and Filebeat (MODE=ha or single)
	@$(INFRA) up elk $(MODE)
infra.elk.down: ## Stop ELK (data is kept)
	@$(INFRA) down elk

.PHONY: infra.observability.up infra.observability.down
infra.observability.up: create.envfile ## Start Prometheus Grafana Tempo Alertmanager and the OTel Collector
	@$(INFRA) up observability
infra.observability.down: ## Stop the observability stack (data is kept)
	@$(INFRA) down observability

.PHONY: infra.legacy.up infra.legacy.down
infra.legacy.up: create.envfile ## Start the legacy MongoDB and standalone Redis
	@$(INFRA) up legacy
infra.legacy.down: ## Stop the legacy services (data is kept)
	@$(INFRA) down legacy

.PHONY: infra.core.up infra.core.down
infra.core.up: create.envfile ## Start everything the app needs - Postgres Redis Kafka (MODE=ha or single)
	@$(INFRA) up core $(MODE)
infra.core.down: ## Stop Postgres Redis and Kafka (data is kept)
	@$(INFRA) down core

.PHONY: infra.full.up infra.full.down
infra.full.up: create.envfile ## Start every stack except legacy (MODE=ha or single)
	@$(INFRA) up full $(MODE)
infra.full.down: ## Stop every stack except legacy (data is kept)
	@$(INFRA) down full

.PHONY: infra.config
infra.config: create.envfile ## Check that every compose profile renders and the env defaults agree
	@$(ROOT_DIR)/scripts/infra_env_check.sh
	@for p in $(INFRA_PROFILES); do \
		$(COMPOSE) --profile $$p config --quiet || exit 1; \
		echo "ok: $$p"; \
	done
	@$(COMPOSE) --profile '*' config --quiet && echo "ok: all profiles"

.PHONY: infra.ps
infra.ps: ## Show every infrastructure container and its health
	@$(COMPOSE) --profile '*' ps -a

.PHONY: infra.logs
infra.logs: ## Follow logs, usage - make infra.logs SERVICE=kafka-1 (omit SERVICE for everything)
	@$(COMPOSE) --profile '*' logs -f --tail=100 $(SERVICE)

.PHONY: infra.stats
infra.stats: ## Show memory and CPU of the running infrastructure containers
	@docker stats --no-stream --format "table {{.Name}}\t{{.MemUsage}}\t{{.CPUPerc}}" \
		$$(docker ps --filter label=com.docker.compose.project=fupi -q)

.PHONY: infra.clean
infra.clean: confirm ## Remove every infrastructure container AND volume (all data is lost)
	@$(COMPOSE) --profile '*' down -v --remove-orphans

.PHONY: infra.hosts
infra.hosts: ## Print the hosts-file line needed to run the app on the host against Redis HA
	@echo "Redis HA announces the hostnames redis-1 to redis-6. An app running on your host must resolve them."
	@echo "Add this line to /etc/hosts (needs sudo, not required when the app runs in a container):"
	@echo "  127.0.0.1 redis-1 redis-2 redis-3 redis-4 redis-5 redis-6"
	@if grep -q 'redis-1' /etc/hosts; then echo "(an entry for redis-1 already exists)"; fi
```

- [ ] **Step 13: Verify**

```bash
cd /Users/lusina/Projects/SanctumLabs/fupi
scripts/infra_test.sh                      # expect: all tests passed
make infra.config                          # expect: "ok: <profile>" for all 14 profiles, then "ok: all profiles"
make help | sed 's/\x1b\[[0-9;]*m//g' | grep -c '^infra\.'   # expect: 22 (16 up/down targets + config, ps, logs, stats, clean, hosts)
make infra.hosts                           # expect: the three explanatory lines and the 127.0.0.1 entry
docker compose --profile legacy config --services | sort   # expect: cache, documentdb
docker compose config --services | wc -l   # expect: 0 (no profile, nothing starts)
docker compose ps -a --format '{{.Service}}|{{.State}}|{{.Health}}|{{.ExitCode}}'   # expect: no error (empty output)
```

Legacy equivalence: compare `$SCRATCH/old-compose.rendered.yml` with `docker compose --profile legacy config`. The only differences must be the added `profiles`/`networks` blocks, the `127.0.0.1` port binding, and the network name. Image, container names, hostnames, environment and the `fupi_docdb` / `fupi_cache` volume names must be identical, so existing Mongo data is kept.

Run shellcheck: `command -v shellcheck && shellcheck scripts/*.sh || docker run --rm -v "$PWD:/mnt" koalaman/shellcheck:stable /mnt/scripts/*.sh` (pull gate applies to the fallback). Expected: no findings.

- [ ] **Step 14: Commit**

```bash
git add docker-compose.yml deploy scripts .env.example .make/docker.mk
git commit -m "feat(infra): add compose skeleton, profile runner and legacy stack" \
  -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

## Task 2: Kafka stack (HA 3 nodes and single node)

**Files:**
- Replace stub: `deploy/kafka/compose.yml`
- Create: `deploy/kafka/create-topics.sh`
- Modify: `.make/docker.mk` (append `infra.kafka.topics`)

**Interfaces:**
- Consumes: Task 1 (`infra.sh`, profiles, `.env` variables `KAFKA_CLUSTER_ID`, `KAFKA_HEAP`, network `fupi`).
- Produces: services `kafka-1..3` + `kafka-init-ha` (HA), `kafka-single` (alias `kafka-1`) + `kafka-init-single`, shared `kafka-ui` (host 8080) and `kafka-exporter` (in-network `kafka-exporter:9308`); Make variable `KAFKA_SERVICE`-based target `infra.kafka.topics`. Topics created: `url.events`, `identity.events`, `url.access`, `url.invalidations`, `security.scan.requests`, `webhook.deliveries`.

- [ ] **Step 1: Write `deploy/kafka/create-topics.sh`**

Idempotent: `--if-not-exists` makes a second run a no-op.

```bash
#!/usr/bin/env bash
# Creates the Fupi topics. Safe to run repeatedly. Retention and partition counts are local-development defaults
# taken from the v2 data architecture; production sizing is out of scope.
# Env: BOOTSTRAP (broker address), REPLICATION_FACTOR, MIN_ISR.
set -euo pipefail

BOOTSTRAP="${BOOTSTRAP:?}"
RF="${REPLICATION_FACTOR:?}"
MIN_ISR="${MIN_ISR:?}"
KT=/opt/kafka/bin/kafka-topics.sh

# name:partitions:retention_ms
TOPICS=(
  "url.events:3:604800000"
  "identity.events:3:604800000"
  "url.access:6:172800000"
  "url.invalidations:3:3600000"
  "security.scan.requests:3:86400000"
  "webhook.deliveries:3:259200000"
)

for entry in "${TOPICS[@]}"; do
  IFS=: read -r name partitions retention <<<"$entry"
  "$KT" --bootstrap-server "$BOOTSTRAP" --create --if-not-exists \
    --topic "$name" --partitions "$partitions" --replication-factor "$RF" \
    --config "min.insync.replicas=$MIN_ISR" --config "retention.ms=$retention"
done

echo "topics:"
"$KT" --bootstrap-server "$BOOTSTRAP" --list
```

- [ ] **Step 2: Write `deploy/kafka/compose.yml`**

```yaml
# Kafka in KRaft mode (no ZooKeeper).
#   HA     kafka-1..3: three combined broker+controller nodes, replication factor 3, min.insync.replicas 2.
#   single kafka-single: one node, replication factor 1. It answers to the alias kafka-1 so clients need no change.
# Listeners: INTERNAL kafka-N:9092 for the compose network, EXTERNAL localhost:19092/29092/39092 for the host.
# No SASL/TLS locally (documented difference from production).
x-kafka-image: &kafka-image apache/kafka:4.3.1

x-kafka-common: &kafka-common
  image: *kafka-image
  restart: unless-stopped
  healthcheck:
    test: ["CMD-SHELL", "/opt/kafka/bin/kafka-broker-api-versions.sh --bootstrap-server localhost:9092 >/dev/null 2>&1"]
    interval: 15s
    timeout: 10s
    retries: 10
    start_period: 30s

x-kafka-env: &kafka-env
  CLUSTER_ID: ${KAFKA_CLUSTER_ID:-MkU3OEVBNTcwNTJENDM2Qk}
  KAFKA_PROCESS_ROLES: broker,controller
  KAFKA_LISTENER_SECURITY_PROTOCOL_MAP: INTERNAL:PLAINTEXT,CONTROLLER:PLAINTEXT,EXTERNAL:PLAINTEXT
  KAFKA_INTER_BROKER_LISTENER_NAME: INTERNAL
  KAFKA_CONTROLLER_LISTENER_NAMES: CONTROLLER
  KAFKA_AUTO_CREATE_TOPICS_ENABLE: "false"
  KAFKA_UNCLEAN_LEADER_ELECTION_ENABLE: "false"
  KAFKA_GROUP_INITIAL_REBALANCE_DELAY_MS: "0"
  KAFKA_NUM_PARTITIONS: "3"
  KAFKA_LOG_DIRS: /var/lib/kafka/data
  KAFKA_HEAP_OPTS: -Xms${KAFKA_HEAP:-512m} -Xmx${KAFKA_HEAP:-512m}

x-kafka-ha-env: &kafka-ha-env
  <<: *kafka-env
  KAFKA_CONTROLLER_QUORUM_VOTERS: 1@kafka-1:9093,2@kafka-2:9093,3@kafka-3:9093
  KAFKA_DEFAULT_REPLICATION_FACTOR: "3"
  KAFKA_MIN_INSYNC_REPLICAS: "2"
  KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR: "3"
  KAFKA_TRANSACTION_STATE_LOG_REPLICATION_FACTOR: "3"
  KAFKA_TRANSACTION_STATE_LOG_MIN_ISR: "2"

services:
  kafka-1:
    <<: *kafka-common
    profiles: [kafka-ha, core-ha, full-ha]
    ports:
      - "127.0.0.1:19092:19092"
    environment:
      <<: *kafka-ha-env
      KAFKA_NODE_ID: "1"
      KAFKA_LISTENERS: INTERNAL://:9092,CONTROLLER://:9093,EXTERNAL://:19092
      KAFKA_ADVERTISED_LISTENERS: INTERNAL://kafka-1:9092,EXTERNAL://localhost:19092
    volumes:
      - kafka-1-data:/var/lib/kafka/data
    networks:
      - fupi

  kafka-2:
    <<: *kafka-common
    profiles: [kafka-ha, core-ha, full-ha]
    ports:
      - "127.0.0.1:29092:29092"
    environment:
      <<: *kafka-ha-env
      KAFKA_NODE_ID: "2"
      KAFKA_LISTENERS: INTERNAL://:9092,CONTROLLER://:9093,EXTERNAL://:29092
      KAFKA_ADVERTISED_LISTENERS: INTERNAL://kafka-2:9092,EXTERNAL://localhost:29092
    volumes:
      - kafka-2-data:/var/lib/kafka/data
    networks:
      - fupi

  kafka-3:
    <<: *kafka-common
    profiles: [kafka-ha, core-ha, full-ha]
    ports:
      - "127.0.0.1:39092:39092"
    environment:
      <<: *kafka-ha-env
      KAFKA_NODE_ID: "3"
      KAFKA_LISTENERS: INTERNAL://:9092,CONTROLLER://:9093,EXTERNAL://:39092
      KAFKA_ADVERTISED_LISTENERS: INTERNAL://kafka-3:9092,EXTERNAL://localhost:39092
    volumes:
      - kafka-3-data:/var/lib/kafka/data
    networks:
      - fupi

  kafka-single:
    <<: *kafka-common
    profiles: [kafka-single, core-single, full-single]
    ports:
      - "127.0.0.1:19092:19092"
    environment:
      <<: *kafka-env
      KAFKA_NODE_ID: "1"
      KAFKA_CONTROLLER_QUORUM_VOTERS: 1@kafka-1:9093
      KAFKA_LISTENERS: INTERNAL://:9092,CONTROLLER://:9093,EXTERNAL://:19092
      KAFKA_ADVERTISED_LISTENERS: INTERNAL://kafka-1:9092,EXTERNAL://localhost:19092
      KAFKA_DEFAULT_REPLICATION_FACTOR: "1"
      KAFKA_MIN_INSYNC_REPLICAS: "1"
      KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR: "1"
      KAFKA_TRANSACTION_STATE_LOG_REPLICATION_FACTOR: "1"
      KAFKA_TRANSACTION_STATE_LOG_MIN_ISR: "1"
      KAFKA_SHARE_COORDINATOR_STATE_TOPIC_REPLICATION_FACTOR: "1"
      KAFKA_SHARE_COORDINATOR_STATE_TOPIC_MIN_ISR: "1"
    volumes:
      - kafka-single-data:/var/lib/kafka/data
    networks:
      fupi:
        aliases: [kafka-1]

  kafka-init-ha:
    image: *kafka-image
    profiles: [kafka-ha, core-ha, full-ha]
    restart: "no"
    depends_on:
      kafka-1: {condition: service_healthy}
      kafka-2: {condition: service_healthy}
      kafka-3: {condition: service_healthy}
    environment:
      BOOTSTRAP: kafka-1:9092
      REPLICATION_FACTOR: "3"
      MIN_ISR: "2"
    entrypoint: ["/bin/bash", "/create-topics.sh"]
    volumes:
      - ./create-topics.sh:/create-topics.sh:ro
    networks:
      - fupi

  kafka-init-single:
    image: *kafka-image
    profiles: [kafka-single, core-single, full-single]
    restart: "no"
    depends_on:
      kafka-single: {condition: service_healthy}
    environment:
      BOOTSTRAP: kafka-1:9092
      REPLICATION_FACTOR: "1"
      MIN_ISR: "1"
    entrypoint: ["/bin/bash", "/create-topics.sh"]
    volumes:
      - ./create-topics.sh:/create-topics.sh:ro
    networks:
      - fupi

  # Shared by both modes: both reach the cluster through the kafka-1 name.
  kafka-ui:
    image: kafbat/kafka-ui:v1.5.0
    profiles: [kafka-ha, kafka-single, core-ha, core-single, full-ha, full-single]
    restart: unless-stopped
    ports:
      - "127.0.0.1:8080:8080"
    environment:
      KAFKA_CLUSTERS_0_NAME: fupi-local
      KAFKA_CLUSTERS_0_BOOTSTRAPSERVERS: kafka-1:9092
      DYNAMIC_CONFIG_ENABLED: "false"
    networks:
      - fupi

  kafka-exporter:
    image: danielqsj/kafka-exporter:v1.10.0
    profiles: [kafka-ha, kafka-single, core-ha, core-single, full-ha, full-single]
    restart: unless-stopped
    command: ["--kafka.server=kafka-1:9092"]
    networks:
      - fupi

volumes:
  kafka-1-data:
  kafka-2-data:
  kafka-3-data:
  kafka-single-data:

networks:
  fupi:
    name: fupi
```

- [ ] **Step 3: Append the helper target to `.make/docker.mk`**

```make

.PHONY: infra.kafka.topics
infra.kafka.topics: ## Describe the Kafka topics (MODE=ha or single)
	@$(COMPOSE) --profile '*' exec -T $(KAFKA_SERVICE) /opt/kafka/bin/kafka-topics.sh --bootstrap-server localhost:9092 --describe
```

- [ ] **Step 4: Static verification**

```bash
make infra.config                                                   # all profiles still render; env defaults agree
docker compose --profile kafka-ha config --services | sort          # kafka-1 kafka-2 kafka-3 kafka-exporter kafka-init-ha kafka-ui
docker compose --profile kafka-single config --services | sort      # kafka-exporter kafka-init-single kafka-single kafka-ui
docker compose --profile core-single config --services | grep -c kafka   # 4
```

Stale-env check: `mv .env .env.bak; printf 'ENV=development\n' > .env; docker compose --profile kafka-ha config | grep -E "CLUSTER_ID|KAFKA_HEAP_OPTS"; mv .env.bak .env`. Expected: `CLUSTER_ID: MkU3OEVBNTcwNTJENDM2Qk` and `-Xms512m -Xmx512m` even though the `.env` has none of the new variables.

- [ ] **Step 5: Runtime — HA (ask for the pull-gate go-ahead first)**

```bash
make infra.kafka.up MODE=ha
```

Expected: ends `ready: kafka-ha`; `make infra.ps` shows `kafka-1..3` healthy, `kafka-ui` and `kafka-exporter` running, `kafka-init-ha` exited 0.

```bash
make infra.kafka.topics MODE=ha | grep -E "^Topic: (url\.|identity\.|security\.|webhook\.)" | awk '{print $2, "partitions="$6, "rf="$8}' | sort
```

Expected: six topics, each with `ReplicationFactor: 3`; `url.access` has `PartitionCount: 6`.

Host-side client against the EXTERNAL listeners (proves the advertised addresses work from outside the network):

```bash
"$SCRATCH/infracheck/infracheck" kafka localhost:19092,localhost:29092,localhost:39092
```

Expected: `kafka ok: produced and consumed infracheck-<n>`.

Drill — lose one broker, still produce and consume (`acks=all` with `min.insync.replicas=2` needs 2 of 3):

```bash
dc stop kafka-2
"$SCRATCH/infracheck/infracheck" kafka localhost:19092,localhost:39092     # expect: kafka ok
dc start kafka-2; sleep 40
dc exec -T kafka-1 /opt/kafka/bin/kafka-topics.sh --bootstrap-server localhost:9092 --describe --under-replicated-partitions   # expect: no output (every partition back in sync)
```

Re-run idempotency (Review Focus 2) and restart persistence (Review Focus 3):

```bash
make infra.kafka.up MODE=ha                     # expect: ready again, kafka-init-ha exits 0, no "already exists" failure
make infra.kafka.down && make infra.kafka.up MODE=ha
make infra.kafka.topics MODE=ha | grep -c "^Topic: url.events"     # expect: 1 (the topics survived the restart)
```

Kafka UI: `curl -fsS -o /dev/null -w '%{http_code}\n' http://localhost:8080` → `200`. Exporter: `dc exec -T kafka-ui wget -qO- http://kafka-exporter:9308/metrics | grep -c '^kafka_brokers'` → `1`.

- [ ] **Step 6: Runtime — switching to single (Review Focus 4)**

```bash
make infra.kafka.up MODE=single
```

Expected: the log shows the HA containers removed first; ends `ready: kafka-single`; `dc ps` lists only `kafka-single`, `kafka-ui`, `kafka-exporter`, `kafka-init-single` (exited 0).

```bash
make infra.kafka.topics MODE=single | grep -c "ReplicationFactor: 1"     # expect: 6
"$SCRATCH/infracheck/infracheck" kafka localhost:19092                   # expect: kafka ok
```

- [ ] **Step 7: Tear down and commit**

```bash
make infra.kafka.down && echo y | make infra.clean
git add deploy/kafka .make/docker.mk
git commit -m "feat(infra): add Kafka stack with HA and single-node modes" \
  -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

## Task 3: Redis stack (HA 6-node cluster and single-node cluster)

**Files:**
- Replace stub: `deploy/redis/compose.yml`
- Create: `deploy/redis/redis.conf`, `deploy/redis/start.sh`, `deploy/redis/init-cluster.sh`
- Modify: `.make/docker.mk` (append `infra.redis.cli`)

**Interfaces:**
- Consumes: Task 1 (`FUPI_NET_PREFIX`, `REDIS_ADMIN_PASSWORD`, `REDIS_USERNAME`, `REDIS_PASSWORD`).
- Produces: services `redis-1..6` on ports `7001..7006` with static IPs `${FUPI_NET_PREFIX}.11` to `.16` + `redis-init-ha`; `redis-single` (alias `redis-1`, port 7001, IP `.11`) + `redis-init-single`; shared `redis-exporter` (in-network `redis-exporter:9121`). Every node announces its hostname (`redis-N`). Make target `infra.redis.cli`.

- [ ] **Step 1: Write `deploy/redis/redis.conf`**

```conf
# Shared by every Redis node. Per-node values (port, announced hostname) and every secret are passed by start.sh.
bind 0.0.0.0
protected-mode yes
dir /data

# Cluster. Single-node mode is a cluster of one, so clients use the same code path as in production.
cluster-enabled yes
cluster-config-file /data/nodes.conf
cluster-node-timeout 5000
cluster-require-full-coverage yes
cluster-preferred-endpoint-type hostname

# Cache behaviour from the v2 data architecture
maxmemory 128mb
maxmemory-policy allkeys-lru

appendonly yes
appendfsync everysec
```

- [ ] **Step 2: Write `deploy/redis/start.sh`**

```sh
#!/bin/sh
# Starts one Redis Cluster node. Passwords and the application ACL user come from the environment so the committed
# redis.conf holds no secrets. Env: NODE_PORT, NODE_HOST, REDIS_ADMIN_PASSWORD, REDIS_USERNAME, REDIS_PASSWORD.
set -eu

# The application user may do everything except the dangerous commands (FLUSHALL, KEYS, CONFIG ...), but a cluster
# client must be able to read the topology.
exec redis-server /usr/local/etc/redis/redis.conf \
  --port "$NODE_PORT" \
  --cluster-announce-hostname "$NODE_HOST" \
  --requirepass "$REDIS_ADMIN_PASSWORD" \
  --masterauth "$REDIS_ADMIN_PASSWORD" \
  --user "$REDIS_USERNAME" on ">$REDIS_PASSWORD" '~*' '&*' '+@all' '-@dangerous' \
  '+cluster|slots' '+cluster|shards' '+cluster|nodes'
```

- [ ] **Step 3: Write `deploy/redis/init-cluster.sh`**

```sh
#!/bin/sh
# Forms the Redis Cluster once and does nothing on later runs (the state lives in each node's volume).
#   CLUSTER_MODE=ha      three masters and three replicas over redis-1..redis-6 (ports 7001..7006)
#   CLUSTER_MODE=single  one node (redis-1:7001) owning all 16384 slots
# Env: CLUSTER_MODE, REDIS_ADMIN_PASSWORD, NET_PREFIX (HA only: first three octets of the nodes' fixed addresses).
set -eu

MODE="${CLUSTER_MODE:?}"
export REDISCLI_AUTH="$REDIS_ADMIN_PASSWORD"

cluster_state() { redis-cli -h "$1" -p "$2" cluster info 2>/dev/null | tr -d '\r' | sed -n 's/^cluster_state://p'; }

# POSIX sh has no local variables, so the helpers keep their counter in "tries" and leave the caller's loop variable alone.
wait_for_ping() {
  tries=0
  until redis-cli -h "$1" -p "$2" ping 2>/dev/null | grep -q PONG; do
    tries=$((tries + 1))
    [ "$tries" -le 60 ] || { echo "timed out waiting for $1:$2" >&2; exit 1; }
    sleep 1
  done
}

wait_for_ok() {
  tries=0
  until [ "$(cluster_state "$1" "$2")" = ok ]; do
    tries=$((tries + 1))
    [ "$tries" -le 60 ] || { echo "cluster did not reach state ok on $1:$2" >&2; exit 1; }
    sleep 1
  done
}

if [ "$MODE" = single ]; then
  wait_for_ping redis-1 7001
  if [ "$(cluster_state redis-1 7001)" = ok ]; then echo "cluster already formed"; exit 0; fi
  redis-cli -h redis-1 -p 7001 cluster addslotsrange 0 16383
  wait_for_ok redis-1 7001
  echo "single-node cluster ready"
  exit 0
fi

nodes=""
for i in 1 2 3 4 5 6; do
  wait_for_ping "redis-$i" "700$i"
  nodes="$nodes $NET_PREFIX.1$i:700$i"    # the nodes have fixed addresses .11 to .16
done

if [ "$(cluster_state redis-1 7001)" = ok ]; then echo "cluster already formed"; exit 0; fi

# shellcheck disable=SC2086  # $nodes is a space-separated list on purpose
redis-cli --cluster create $nodes --cluster-replicas 1 --cluster-yes
wait_for_ok redis-1 7001
echo "cluster ready"
```

- [ ] **Step 4: Write `deploy/redis/compose.yml`**

```yaml
# Redis Cluster.
#   HA     redis-1..6: 3 masters x 1 replica, ports 7001..7006.
#   single redis-single: a cluster of one node owning every slot; answers to the alias redis-1.
# Nodes announce hostnames (redis-1..redis-6) so cluster clients can follow MOVED redirects; an app on the host needs
# them in /etc/hosts (make infra.hosts). Nodes have fixed IPs because the cluster bus persists peer addresses.
x-redis-common: &redis-common
  image: redis:8.10.2-alpine
  restart: unless-stopped
  user: redis
  entrypoint: ["/bin/sh", "/start.sh"]
  healthcheck:
    test: ["CMD-SHELL", "redis-cli -p $$NODE_PORT ping | grep -q PONG"]
    interval: 5s
    timeout: 3s
    retries: 20

x-redis-env: &redis-env
  REDIS_ADMIN_PASSWORD: ${REDIS_ADMIN_PASSWORD:-fupi-redis-admin}
  REDISCLI_AUTH: ${REDIS_ADMIN_PASSWORD:-fupi-redis-admin}
  REDIS_USERNAME: ${REDIS_USERNAME:-fupi-svc}
  REDIS_PASSWORD: ${REDIS_PASSWORD:-fupi-svc}

services:
  redis-1:
    <<: *redis-common
    profiles: [redis-ha, core-ha, full-ha]
    environment: {<<: *redis-env, NODE_PORT: "7001", NODE_HOST: redis-1}
    ports: ["127.0.0.1:7001:7001"]
    volumes: ["redis-1-data:/data", "./redis.conf:/usr/local/etc/redis/redis.conf:ro", "./start.sh:/start.sh:ro"]
    networks:
      fupi:
        ipv4_address: ${FUPI_NET_PREFIX:-172.29.0}.11

  redis-2:
    <<: *redis-common
    profiles: [redis-ha, core-ha, full-ha]
    environment: {<<: *redis-env, NODE_PORT: "7002", NODE_HOST: redis-2}
    ports: ["127.0.0.1:7002:7002"]
    volumes: ["redis-2-data:/data", "./redis.conf:/usr/local/etc/redis/redis.conf:ro", "./start.sh:/start.sh:ro"]
    networks:
      fupi:
        ipv4_address: ${FUPI_NET_PREFIX:-172.29.0}.12

  redis-3:
    <<: *redis-common
    profiles: [redis-ha, core-ha, full-ha]
    environment: {<<: *redis-env, NODE_PORT: "7003", NODE_HOST: redis-3}
    ports: ["127.0.0.1:7003:7003"]
    volumes: ["redis-3-data:/data", "./redis.conf:/usr/local/etc/redis/redis.conf:ro", "./start.sh:/start.sh:ro"]
    networks:
      fupi:
        ipv4_address: ${FUPI_NET_PREFIX:-172.29.0}.13

  redis-4:
    <<: *redis-common
    profiles: [redis-ha, core-ha, full-ha]
    environment: {<<: *redis-env, NODE_PORT: "7004", NODE_HOST: redis-4}
    ports: ["127.0.0.1:7004:7004"]
    volumes: ["redis-4-data:/data", "./redis.conf:/usr/local/etc/redis/redis.conf:ro", "./start.sh:/start.sh:ro"]
    networks:
      fupi:
        ipv4_address: ${FUPI_NET_PREFIX:-172.29.0}.14

  redis-5:
    <<: *redis-common
    profiles: [redis-ha, core-ha, full-ha]
    environment: {<<: *redis-env, NODE_PORT: "7005", NODE_HOST: redis-5}
    ports: ["127.0.0.1:7005:7005"]
    volumes: ["redis-5-data:/data", "./redis.conf:/usr/local/etc/redis/redis.conf:ro", "./start.sh:/start.sh:ro"]
    networks:
      fupi:
        ipv4_address: ${FUPI_NET_PREFIX:-172.29.0}.15

  redis-6:
    <<: *redis-common
    profiles: [redis-ha, core-ha, full-ha]
    environment: {<<: *redis-env, NODE_PORT: "7006", NODE_HOST: redis-6}
    ports: ["127.0.0.1:7006:7006"]
    volumes: ["redis-6-data:/data", "./redis.conf:/usr/local/etc/redis/redis.conf:ro", "./start.sh:/start.sh:ro"]
    networks:
      fupi:
        ipv4_address: ${FUPI_NET_PREFIX:-172.29.0}.16

  redis-single:
    <<: *redis-common
    profiles: [redis-single, core-single, full-single]
    environment: {<<: *redis-env, NODE_PORT: "7001", NODE_HOST: redis-1}
    ports: ["127.0.0.1:7001:7001"]
    volumes: ["redis-single-data:/data", "./redis.conf:/usr/local/etc/redis/redis.conf:ro", "./start.sh:/start.sh:ro"]
    networks:
      fupi:
        aliases: [redis-1]
        ipv4_address: ${FUPI_NET_PREFIX:-172.29.0}.11

  redis-init-ha:
    image: redis:8.10.2-alpine
    profiles: [redis-ha, core-ha, full-ha]
    restart: "no"
    depends_on:
      redis-1: {condition: service_healthy}
      redis-2: {condition: service_healthy}
      redis-3: {condition: service_healthy}
      redis-4: {condition: service_healthy}
      redis-5: {condition: service_healthy}
      redis-6: {condition: service_healthy}
    environment:
      REDIS_ADMIN_PASSWORD: ${REDIS_ADMIN_PASSWORD:-fupi-redis-admin}
      CLUSTER_MODE: ha
      NET_PREFIX: ${FUPI_NET_PREFIX:-172.29.0}
    entrypoint: ["/bin/sh", "/init-cluster.sh"]
    volumes: ["./init-cluster.sh:/init-cluster.sh:ro"]
    networks: [fupi]

  redis-init-single:
    image: redis:8.10.2-alpine
    profiles: [redis-single, core-single, full-single]
    restart: "no"
    depends_on:
      redis-single: {condition: service_healthy}
    environment: {REDIS_ADMIN_PASSWORD: "${REDIS_ADMIN_PASSWORD:-fupi-redis-admin}", CLUSTER_MODE: single}
    entrypoint: ["/bin/sh", "/init-cluster.sh"]
    volumes: ["./init-cluster.sh:/init-cluster.sh:ro"]
    networks: [fupi]

  # One exporter serves both modes: in cluster mode it discovers the other nodes from redis-1.
  redis-exporter:
    image: oliver006/redis_exporter:v1.93.0-alpine
    profiles: [redis-ha, redis-single, core-ha, core-single, full-ha, full-single]
    restart: unless-stopped
    environment:
      REDIS_ADDR: redis://redis-1:7001
      REDIS_PASSWORD: ${REDIS_ADMIN_PASSWORD:-fupi-redis-admin}
      REDIS_EXPORTER_IS_CLUSTER: "true"
    networks: [fupi]

volumes:
  redis-1-data:
  redis-2-data:
  redis-3-data:
  redis-4-data:
  redis-5-data:
  redis-6-data:
  redis-single-data:

networks:
  fupi:
    name: fupi
```

- [ ] **Step 5: Append the helper target to `.make/docker.mk`**

```make

.PHONY: infra.redis.cli
infra.redis.cli: ## Open redis-cli as the application user in cluster mode (MODE=ha or single)
	@$(COMPOSE) --profile '*' exec $(REDIS_SERVICE) sh -c 'redis-cli -p "$$NODE_PORT" --user "$$REDIS_USERNAME" --pass "$$REDIS_PASSWORD" --no-auth-warning -c'
```

- [ ] **Step 6: Static verification**

```bash
make infra.config
docker compose --profile redis-ha config --services | sort        # redis-1..6 redis-exporter redis-init-ha
docker compose --profile redis-ha config | grep -E "ipv4_address" | sort -u      # 172.29.0.11 ... 172.29.0.16
docker compose --profile redis-single config --services | sort    # redis-exporter redis-init-single redis-single
sh -n deploy/redis/start.sh deploy/redis/init-cluster.sh && echo "sh syntax ok"
docker compose --profile redis-ha config | awk '/^networks:/,/^volumes:/' | grep -E "subnet|ip_range|external"
# expect: subnet 172.29.0.0/24 and ip_range 172.29.0.128/25, and NO "external" line
```

- [ ] **Step 7: Runtime — HA (pull gate)**

```bash
make infra.redis.up MODE=ha
```

Expected: ends `ready: redis-ha`; `redis-init-ha` exited 0.

```bash
dc exec -T redis-1 sh -c 'redis-cli -p 7001 cluster info | tr -d "\r" | grep -E "cluster_state|cluster_known_nodes|cluster_size"'
```

Expected: `cluster_state:ok`, `cluster_known_nodes:6`, `cluster_size:3`.

ACL: the application user can read the topology but not run dangerous commands:

```bash
dc exec -T redis-1 sh -c 'redis-cli -p 7001 --user "$REDIS_USERNAME" --pass "$REDIS_PASSWORD" --no-auth-warning cluster slots | head -3'   # expect: slot ranges, no NOPERM error
dc exec -T redis-1 sh -c 'redis-cli -p 7001 --user "$REDIS_USERNAME" --pass "$REDIS_PASSWORD" --no-auth-warning flushall'                # expect: NOPERM
```

If `cluster slots` returns `NOPERM`, the `+cluster|slots` grant in `start.sh` is not taking effect; fix it there before continuing.

Host-side cluster client (proves hostnames, redirects and the ACL user work together):

```bash
"$SCRATCH/infracheck/infracheck" redis localhost:7001     # expect: redis ok: 50 keys set and read across 3 master(s)
```

Drill — kill a master, a replica takes over, writes continue:

```bash
dc exec -T redis-1 sh -c 'redis-cli -p 7001 --user "$REDIS_USERNAME" --pass "$REDIS_PASSWORD" --no-auth-warning cluster nodes' | grep master | head -3
dc stop redis-1
sleep 15
"$SCRATCH/infracheck/infracheck" redis localhost:7002     # expect: redis ok ... across 3 master(s) (a replica was promoted)
dc start redis-1; sleep 10
dc exec -T redis-2 sh -c 'redis-cli -p 7002 cluster info | tr -d "\r" | grep cluster_state'   # expect: cluster_state:ok
```

Restart persistence with fixed IPs (Review Focus 3) and re-run idempotency (Review Focus 2):

```bash
make infra.redis.down && make infra.redis.up MODE=ha       # containers recreated; nodes.conf and AOF come back from the volumes
dc exec -T redis-1 sh -c 'redis-cli -p 7001 cluster info | tr -d "\r" | grep -E "cluster_state|cluster_known_nodes"'   # ok, 6
make infra.redis.up MODE=ha                                # expect: ready; redis-init-ha logs "cluster already formed"
dc logs redis-init-ha | tail -2
```

- [ ] **Step 8: Runtime — single (mode switch, Review Focus 4)**

```bash
make infra.redis.up MODE=single
dc exec -T redis-single sh -c 'redis-cli -p 7001 cluster info | tr -d "\r" | grep -E "cluster_state|cluster_slots_assigned"'   # ok, 16384
"$SCRATCH/infracheck/infracheck" redis localhost:7001     # expect: redis ok: 50 keys set and read across 1 master(s)
make infra.redis.up MODE=single                           # re-run: "cluster already formed"
```

- [ ] **Step 9: Tear down and commit**

```bash
make infra.redis.down && echo y | make infra.clean
git add deploy/redis .make/docker.mk
git commit -m "feat(infra): add Redis cluster stack with HA and single-node modes" \
  -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

## Task 4: Postgres single-node mode, migrations and exporter

**Files:**
- Replace stub: `deploy/postgres/compose.yml`
- Create: `deploy/postgres/roles.sh` (shared with the HA mode in Task 5; keep it executable)
- Modify: `.make/docker.mk` (append `infra.psql`, `infra.migrate`)

**Interfaces:**
- Consumes: Task 1 (`PG_*` variables, network).
- Produces: `postgres-single` (alias `postgres`; host `5432` and `5433` both map to it); shared `migrate` (one-shot, retries until the DB accepts connections, writes table `schema_migrations`) and `postgres-exporter` (in-network `postgres-exporter:9187`); `roles.sh [connection-string]` creating the app role/database (`fupi-user` / `fupidb` by default), the `exporter` role (`pg_monitor`) and the `pg_stat_statements` extension, idempotently.

- [ ] **Step 1: Write `deploy/postgres/roles.sh`**

```sh
#!/bin/sh
# Creates the application role and database and the monitoring role. Idempotent.
#   Postgres HA:     Patroni runs this after initdb and passes a connection string as $1.
#   Postgres single: the official image runs it from /docker-entrypoint-initdb.d (no argument, local socket).
# Env: PG_DATABASE, PG_APP_USER, PG_APP_PASSWORD, PG_EXPORTER_PASSWORD. Values are passed to psql as variables and
# quoted by format(%I / %L), so they are not interpolated into SQL text.
set -eu

conn="${1:-postgresql://postgres@%2Fvar%2Frun%2Fpostgresql/postgres}"

psql "$conn" -X -q -v ON_ERROR_STOP=1 \
  -v app_db="$PG_DATABASE" -v app_user="$PG_APP_USER" \
  -v app_password="$PG_APP_PASSWORD" -v exporter_password="$PG_EXPORTER_PASSWORD" <<'SQL'
SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', :'app_user', :'app_password')
 WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = :'app_user') \gexec
SELECT format('CREATE DATABASE %I OWNER %I', :'app_db', :'app_user')
 WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = :'app_db') \gexec
SELECT format('CREATE ROLE exporter LOGIN PASSWORD %L', :'exporter_password')
 WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'exporter') \gexec
GRANT pg_monitor TO exporter;
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
SQL
```

Then `chmod +x deploy/postgres/roles.sh` (git records the executable bit; the official image only executes init scripts that have it, otherwise it sources them).

- [ ] **Step 2: Write `deploy/postgres/compose.yml`**

```yaml
# Postgres 18.
#   single  postgres-single: one plain Postgres node. Host ports 5432 and 5433 both reach it, so code that splits
#           reads (5433) from writes (5432) works in both modes. It answers to the alias "postgres".
#   migrate and postgres-exporter are shared by both modes and reach the database through "postgres:5432".
x-pg-env: &pg-env
  PG_DATABASE: ${PG_DATABASE:-fupidb}
  PG_APP_USER: ${PG_APP_USER:-fupi-user}
  PG_APP_PASSWORD: ${PG_APP_PASSWORD:-fupi-pass}
  PG_EXPORTER_PASSWORD: ${PG_EXPORTER_PASSWORD:-fupi-exporter}

services:
  postgres-single:
    image: postgres:18.6
    profiles: [postgres-single, core-single, full-single]
    restart: unless-stopped
    command: ["postgres", "-c", "shared_preload_libraries=pg_stat_statements", "-c", "max_connections=200"]
    environment:
      <<: *pg-env
      POSTGRES_PASSWORD: ${PG_SUPERUSER_PASSWORD:-fupi-postgres-admin}
    ports:
      - "127.0.0.1:5432:5432"
      - "127.0.0.1:5433:5432"
    volumes:
      - postgres-single-data:/var/lib/postgresql
      - ./roles.sh:/docker-entrypoint-initdb.d/10-roles.sh:ro
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -h 127.0.0.1 -U postgres -d postgres"]
      interval: 5s
      timeout: 3s
      retries: 20
    networks:
      fupi:
        aliases: [postgres]

  # Applies ./app/internal/adapters/postgres/migrations. Retries until the database accepts connections, so it also
  # works while Patroni is still electing a primary. Re-running is a no-op ("no change").
  migrate:
    image: migrate/migrate:v4.19.1
    profiles: [postgres-ha, postgres-single, core-ha, core-single, full-ha, full-single]
    restart: "on-failure:10"
    command:
      - "-path=/migrations"
      - "-database=postgres://${PG_APP_USER:-fupi-user}:${PG_APP_PASSWORD:-fupi-pass}@postgres:5432/${PG_DATABASE:-fupidb}?sslmode=disable&x-migrations-table=schema_migrations"
      - "up"
    volumes:
      - ../../app/internal/adapters/postgres/migrations:/migrations:ro
    networks:
      - fupi

  postgres-exporter:
    image: prometheuscommunity/postgres-exporter:v0.20.1
    profiles: [postgres-ha, postgres-single, core-ha, core-single, full-ha, full-single]
    restart: unless-stopped
    environment:
      DATA_SOURCE_NAME: "postgresql://exporter:${PG_EXPORTER_PASSWORD:-fupi-exporter}@postgres:5432/postgres?sslmode=disable"
    networks:
      - fupi

volumes:
  postgres-single-data:

networks:
  fupi:
    name: fupi
```

- [ ] **Step 3: Append the helper targets to `.make/docker.mk`**

```make

.PHONY: infra.psql
infra.psql: ## Open psql as the application user on the primary (MODE=ha or single)
	@$(COMPOSE) --profile '*' exec $(POSTGRES_SERVICE) sh -c 'PGPASSWORD="$$PG_APP_PASSWORD" psql -h postgres -U "$$PG_APP_USER" "$$PG_DATABASE"'

.PHONY: infra.migrate
infra.migrate: create.envfile ## Re-run the database migrations against the running Postgres
	@$(COMPOSE) --profile '*' run --rm migrate
```

- [ ] **Step 4: Static verification**

```bash
make infra.config
docker compose --profile postgres-single config --services | sort    # migrate postgres-exporter postgres-single
sh -n deploy/postgres/roles.sh && echo "sh syntax ok"
test -x deploy/postgres/roles.sh && echo "executable ok"
```

Stale-env check (Review Focus 1): `mv .env .env.bak; printf 'ENV=development\n' > .env; docker compose --profile postgres-single config | grep -E "PG_APP_USER|POSTGRES_PASSWORD"; mv .env.bak .env`. Expected: `fupi-user` and `fupi-postgres-admin`.

- [ ] **Step 5: Runtime (pull gate)**

```bash
make infra.postgres.up MODE=single
```

Expected: ends `ready: postgres-single`; `migrate` exited 0 (`dc ps` shows it `exited (0)`).

```bash
Q() { dc exec -T postgres-single sh -c 'PGPASSWORD="$PG_APP_PASSWORD" psql -h postgres -U "$PG_APP_USER" -d "$PG_DATABASE" -tAc "$1"' _ "$1"; }
Q "select version, dirty from schema_migrations"                  # expect: <highest NNNNNN_*.up.sql number in the migrations directory>|f (2|f at the time of writing)
Q "select to_regclass('public.outbox_events') is not null"        # expect: t
"$SCRATCH/infracheck/infracheck" postgres localhost:5432 false    # expect: postgres ok: localhost:5432 pg_is_in_recovery=false
"$SCRATCH/infracheck/infracheck" postgres localhost:5433 false    # expect: ok (the read port reaches the same node in single mode)
```

Monitoring role and extension: `dc exec -T postgres-single sh -c 'PGPASSWORD="$PG_EXPORTER_PASSWORD" psql -h postgres -U exporter -d postgres -tAc "select count(*) >= 1 from pg_stat_statements"'` → `t`. Exporter: `docker run --rm --network fupi busybox wget -qO- http://postgres-exporter:9187/metrics | grep '^pg_up'` → `pg_up 1`.

Re-run idempotency (Review Focus 2): the migrations and the role script must both be safe to repeat.

```bash
make infra.postgres.up MODE=single            # expect: ready; migrate logs "no change"
dc logs migrate | tail -2
dc exec -T postgres-single /docker-entrypoint-initdb.d/10-roles.sh && echo "roles.sh re-run ok"   # expect: exit 0, no error
```

Restart persistence (Review Focus 3):

```bash
Q "create table infra_smoke(id int); insert into infra_smoke values (42)"
make infra.postgres.down && make infra.postgres.up MODE=single
Q "select id from infra_smoke"                # expect: 42
Q "drop table infra_smoke"
```

- [ ] **Step 6: Tear down and commit**

```bash
make infra.postgres.down && echo y | make infra.clean
git add deploy/postgres .make/docker.mk
git commit -m "feat(infra): add single-node Postgres, migrate job and exporter" \
  -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

## Task 5: Postgres HA (Patroni, etcd, HAProxy)

**Files:**
- Create: `deploy/postgres/Dockerfile`, `deploy/postgres/entrypoint.sh`, `deploy/postgres/patroni.yml`, `deploy/postgres/haproxy.cfg`
- Replace: `deploy/postgres/compose.yml` (final version: Task 4's services unchanged, plus the HA services)
- Modify: `.make/docker.mk` (append `infra.patroni.list`)

**Interfaces:**
- Consumes: Task 4 (`roles.sh`, `x-pg-env`, the shared `migrate` and `postgres-exporter`).
- Produces: `etcd-1..3` (metrics on `:2381`), `patroni-1..3` (REST/metrics on `:8008`, host ports `8008`..`8010`), `haproxy` (alias `postgres`; host `5432` = primary, `5433` = replicas, `8404` = stats and `/metrics`); image `fupi-patroni:18.6-4.1.5`; target `infra.patroni.list`.

- [ ] **Step 1: Write `deploy/postgres/Dockerfile`**

```dockerfile
# syntax=docker/dockerfile:1
# Patroni on top of the official Postgres image, for the local HA stack only.
FROM postgres:18.6

ARG PATRONI_VERSION=4.1.5

# hadolint ignore=DL3008
RUN apt-get update \
 && apt-get install -y --no-install-recommends python3 python3-venv curl \
 && python3 -m venv /opt/patroni \
 && /opt/patroni/bin/pip install --no-cache-dir "patroni[etcd3]==${PATRONI_VERSION}" "psycopg[binary]" \
 && rm -rf /var/lib/apt/lists/*

COPY entrypoint.sh /usr/local/bin/patroni-entrypoint.sh
COPY roles.sh /usr/local/bin/roles.sh
RUN chmod 0755 /usr/local/bin/patroni-entrypoint.sh /usr/local/bin/roles.sh

EXPOSE 5432 8008
ENTRYPOINT ["/usr/local/bin/patroni-entrypoint.sh"]
```

- [ ] **Step 2: Write `deploy/postgres/entrypoint.sh`**

```sh
#!/bin/sh
# Prepares the data directory as root, then drops to the postgres user before starting Patroni (the same pattern as the
# official postgres image). Patroni creates and owns the cluster inside DATA_DIR.
set -eu

DATA_DIR=/var/lib/postgresql/patroni-data

mkdir -p "$DATA_DIR"
chown -R postgres:postgres /var/lib/postgresql
chmod 700 "$DATA_DIR"

exec gosu postgres /opt/patroni/bin/patroni /etc/patroni/patroni.yml
```

- [ ] **Step 3: Write `deploy/postgres/patroni.yml`**

```yaml
# Shared by patroni-1..3. Node identity comes from PATRONI_NAME, PATRONI_RESTAPI_CONNECT_ADDRESS and
# PATRONI_POSTGRESQL_CONNECT_ADDRESS; credentials from PATRONI_SUPERUSER_* and PATRONI_REPLICATION_*.
scope: fupi
namespace: /service/

etcd3:
  hosts: etcd-1:2379,etcd-2:2379,etcd-3:2379

restapi:
  listen: 0.0.0.0:8008

bootstrap:
  dcs:
    ttl: 30
    loop_wait: 10
    retry_timeout: 10
    maximum_lag_on_failover: 1048576
    # Asynchronous replication by default. Set to true (and optionally synchronous_mode_strict) to trade write
    # latency for zero data loss on failover.
    synchronous_mode: false
    postgresql:
      use_pg_rewind: true
      use_slots: true
      parameters:
        wal_level: replica
        hot_standby: "on"
        wal_log_hints: "on"
        max_wal_senders: 10
        max_replication_slots: 10
        max_connections: 200
        shared_buffers: 128MB
        shared_preload_libraries: pg_stat_statements
  initdb:
    - encoding: UTF8
    - data-checksums
  pg_hba:
    - local all all trust
    - host replication replicator 0.0.0.0/0 scram-sha-256
    - host all all 0.0.0.0/0 scram-sha-256
  post_bootstrap: /usr/local/bin/roles.sh

postgresql:
  listen: 0.0.0.0:5432
  data_dir: /var/lib/postgresql/patroni-data
  pgpass: /tmp/pgpass
  parameters:
    unix_socket_directories: /var/run/postgresql
```

- [ ] **Step 4: Write `deploy/postgres/haproxy.cfg`**

```text
# Routes by role using Patroni's REST health checks: 5432 -> the current primary, 5433 -> healthy replicas.
global
    maxconn 1000

defaults
    mode tcp
    timeout connect 5s
    timeout client 30m
    timeout server 30m
    timeout check 3s

resolvers docker
    nameserver dns 127.0.0.11:53
    resolve_retries 3
    timeout resolve 1s
    timeout retry 1s
    hold valid 5s
    hold other 5s

frontend stats
    mode http
    bind *:8404
    http-request use-service prometheus-exporter if { path /metrics }
    stats enable
    stats uri /
    stats refresh 5s

# init-addr libc,none lets HAProxy start before every Patroni node has registered in DNS.
listen postgres_write
    bind *:5432
    option httpchk GET /primary
    http-check expect status 200
    default-server inter 3s fall 3 rise 2 on-marked-down shutdown-sessions resolvers docker init-addr libc,none
    server patroni-1 patroni-1:5432 check port 8008
    server patroni-2 patroni-2:5432 check port 8008
    server patroni-3 patroni-3:5432 check port 8008

listen postgres_read
    bind *:5433
    balance roundrobin
    option httpchk GET /replica
    http-check expect status 200
    default-server inter 3s fall 3 rise 2 on-marked-down shutdown-sessions resolvers docker init-addr libc,none
    server patroni-1 patroni-1:5432 check port 8008
    server patroni-2 patroni-2:5432 check port 8008
    server patroni-3 patroni-3:5432 check port 8008
```

- [ ] **Step 5: Replace `deploy/postgres/compose.yml` with the final version**

```yaml
# Postgres 18.
#   HA      patroni-1..3 (Patroni-managed Postgres) + etcd-1..3 (3-member quorum) + haproxy.
#           haproxy answers to the alias "postgres": :5432 reaches the primary, :5433 the replicas.
#   single  postgres-single: one plain Postgres node. Host ports 5432 and 5433 both reach it, so code that splits
#           reads (5433) from writes (5432) works in both modes. It answers to the alias "postgres" too.
#   migrate and postgres-exporter are shared by both modes and reach the database through "postgres:5432".
x-pg-env: &pg-env
  PG_DATABASE: ${PG_DATABASE:-fupidb}
  PG_APP_USER: ${PG_APP_USER:-fupi-user}
  PG_APP_PASSWORD: ${PG_APP_PASSWORD:-fupi-pass}
  PG_EXPORTER_PASSWORD: ${PG_EXPORTER_PASSWORD:-fupi-exporter}

x-patroni-env: &patroni-env
  <<: *pg-env
  PATRONI_SUPERUSER_USERNAME: postgres
  PATRONI_SUPERUSER_PASSWORD: ${PG_SUPERUSER_PASSWORD:-fupi-postgres-admin}
  PATRONI_REPLICATION_USERNAME: replicator
  PATRONI_REPLICATION_PASSWORD: ${PG_REPLICATION_PASSWORD:-fupi-replication}

x-etcd: &etcd
  image: quay.io/coreos/etcd:v3.6.15
  restart: unless-stopped

x-patroni: &patroni
  build:
    context: .
    dockerfile: Dockerfile
  image: fupi-patroni:18.6-4.1.5
  restart: unless-stopped
  depends_on:
    etcd-1: {condition: service_started}
    etcd-2: {condition: service_started}
    etcd-3: {condition: service_started}
  healthcheck:
    test: ["CMD-SHELL", "curl -fsS http://localhost:8008/health >/dev/null"]
    interval: 5s
    timeout: 3s
    retries: 30
    start_period: 20s

services:
  etcd-1:
    <<: *etcd
    profiles: [postgres-ha, core-ha, full-ha]
    command:
      - etcd
      - --name=etcd-1
      - --data-dir=/etcd-data
      - --listen-client-urls=http://0.0.0.0:2379
      - --advertise-client-urls=http://etcd-1:2379
      - --listen-peer-urls=http://0.0.0.0:2380
      - --initial-advertise-peer-urls=http://etcd-1:2380
      - --initial-cluster=etcd-1=http://etcd-1:2380,etcd-2=http://etcd-2:2380,etcd-3=http://etcd-3:2380
      - --initial-cluster-token=fupi-etcd
      - --initial-cluster-state=new
      - --listen-metrics-urls=http://0.0.0.0:2381
    volumes:
      - etcd-1-data:/etcd-data
    networks:
      - fupi

  etcd-2:
    <<: *etcd
    profiles: [postgres-ha, core-ha, full-ha]
    command:
      - etcd
      - --name=etcd-2
      - --data-dir=/etcd-data
      - --listen-client-urls=http://0.0.0.0:2379
      - --advertise-client-urls=http://etcd-2:2379
      - --listen-peer-urls=http://0.0.0.0:2380
      - --initial-advertise-peer-urls=http://etcd-2:2380
      - --initial-cluster=etcd-1=http://etcd-1:2380,etcd-2=http://etcd-2:2380,etcd-3=http://etcd-3:2380
      - --initial-cluster-token=fupi-etcd
      - --initial-cluster-state=new
      - --listen-metrics-urls=http://0.0.0.0:2381
    volumes:
      - etcd-2-data:/etcd-data
    networks:
      - fupi

  etcd-3:
    <<: *etcd
    profiles: [postgres-ha, core-ha, full-ha]
    command:
      - etcd
      - --name=etcd-3
      - --data-dir=/etcd-data
      - --listen-client-urls=http://0.0.0.0:2379
      - --advertise-client-urls=http://etcd-3:2379
      - --listen-peer-urls=http://0.0.0.0:2380
      - --initial-advertise-peer-urls=http://etcd-3:2380
      - --initial-cluster=etcd-1=http://etcd-1:2380,etcd-2=http://etcd-2:2380,etcd-3=http://etcd-3:2380
      - --initial-cluster-token=fupi-etcd
      - --initial-cluster-state=new
      - --listen-metrics-urls=http://0.0.0.0:2381
    volumes:
      - etcd-3-data:/etcd-data
    networks:
      - fupi

  patroni-1:
    <<: *patroni
    profiles: [postgres-ha, core-ha, full-ha]
    environment:
      <<: *patroni-env
      PATRONI_NAME: patroni-1
      PATRONI_RESTAPI_CONNECT_ADDRESS: patroni-1:8008
      PATRONI_POSTGRESQL_CONNECT_ADDRESS: patroni-1:5432
    ports:
      - "127.0.0.1:8008:8008"
    volumes:
      - pgdata-1:/var/lib/postgresql
      - ./patroni.yml:/etc/patroni/patroni.yml:ro
    networks:
      - fupi

  patroni-2:
    <<: *patroni
    profiles: [postgres-ha, core-ha, full-ha]
    environment:
      <<: *patroni-env
      PATRONI_NAME: patroni-2
      PATRONI_RESTAPI_CONNECT_ADDRESS: patroni-2:8008
      PATRONI_POSTGRESQL_CONNECT_ADDRESS: patroni-2:5432
    ports:
      - "127.0.0.1:8009:8008"
    volumes:
      - pgdata-2:/var/lib/postgresql
      - ./patroni.yml:/etc/patroni/patroni.yml:ro
    networks:
      - fupi

  patroni-3:
    <<: *patroni
    profiles: [postgres-ha, core-ha, full-ha]
    environment:
      <<: *patroni-env
      PATRONI_NAME: patroni-3
      PATRONI_RESTAPI_CONNECT_ADDRESS: patroni-3:8008
      PATRONI_POSTGRESQL_CONNECT_ADDRESS: patroni-3:5432
    ports:
      - "127.0.0.1:8010:8008"
    volumes:
      - pgdata-3:/var/lib/postgresql
      - ./patroni.yml:/etc/patroni/patroni.yml:ro
    networks:
      - fupi

  haproxy:
    image: haproxy:3.2.25-alpine
    profiles: [postgres-ha, core-ha, full-ha]
    restart: unless-stopped
    depends_on:
      patroni-1: {condition: service_started}
      patroni-2: {condition: service_started}
      patroni-3: {condition: service_started}
    ports:
      - "127.0.0.1:5432:5432"
      - "127.0.0.1:5433:5433"
      - "127.0.0.1:8404:8404"
    volumes:
      - ./haproxy.cfg:/usr/local/etc/haproxy/haproxy.cfg:ro
    networks:
      fupi:
        aliases: [postgres]

  postgres-single:
    image: postgres:18.6
    profiles: [postgres-single, core-single, full-single]
    restart: unless-stopped
    command: ["postgres", "-c", "shared_preload_libraries=pg_stat_statements", "-c", "max_connections=200"]
    environment:
      <<: *pg-env
      POSTGRES_PASSWORD: ${PG_SUPERUSER_PASSWORD:-fupi-postgres-admin}
    ports:
      - "127.0.0.1:5432:5432"
      - "127.0.0.1:5433:5432"
    volumes:
      - postgres-single-data:/var/lib/postgresql
      - ./roles.sh:/docker-entrypoint-initdb.d/10-roles.sh:ro
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -h 127.0.0.1 -U postgres -d postgres"]
      interval: 5s
      timeout: 3s
      retries: 20
    networks:
      fupi:
        aliases: [postgres]

  # Applies ./app/internal/adapters/postgres/migrations. Retries until the database accepts connections, so it also
  # works while Patroni is still electing a primary. Re-running is a no-op ("no change").
  migrate:
    image: migrate/migrate:v4.19.1
    profiles: [postgres-ha, postgres-single, core-ha, core-single, full-ha, full-single]
    restart: "on-failure:10"
    command:
      - "-path=/migrations"
      - "-database=postgres://${PG_APP_USER:-fupi-user}:${PG_APP_PASSWORD:-fupi-pass}@postgres:5432/${PG_DATABASE:-fupidb}?sslmode=disable&x-migrations-table=schema_migrations"
      - "up"
    volumes:
      - ../../app/internal/adapters/postgres/migrations:/migrations:ro
    networks:
      - fupi

  postgres-exporter:
    image: prometheuscommunity/postgres-exporter:v0.20.1
    profiles: [postgres-ha, postgres-single, core-ha, core-single, full-ha, full-single]
    restart: unless-stopped
    environment:
      DATA_SOURCE_NAME: "postgresql://exporter:${PG_EXPORTER_PASSWORD:-fupi-exporter}@postgres:5432/postgres?sslmode=disable"
    networks:
      - fupi

volumes:
  etcd-1-data:
  etcd-2-data:
  etcd-3-data:
  pgdata-1:
  pgdata-2:
  pgdata-3:
  postgres-single-data:

networks:
  fupi:
    name: fupi
```

- [ ] **Step 6: Append the helper target to `.make/docker.mk`**

```make

.PHONY: infra.patroni.list
infra.patroni.list: ## Show the Patroni cluster members and roles (Postgres MODE=ha only)
	@$(COMPOSE) --profile '*' exec -T patroni-1 /opt/patroni/bin/patronictl -c /etc/patroni/patroni.yml list
```

- [ ] **Step 7: Static verification**

```bash
make infra.config
docker compose --profile postgres-ha config --services | sort     # etcd-1..3 haproxy migrate patroni-1..3 postgres-exporter
docker compose --profile core-ha config --services | grep -c -E "patroni|etcd|haproxy"   # 7
sh -n deploy/postgres/entrypoint.sh && echo "sh syntax ok"
docker run --rm -v "$PWD/deploy/postgres/haproxy.cfg:/usr/local/etc/haproxy/haproxy.cfg:ro" \
  haproxy:3.2.25-alpine haproxy -c -f /usr/local/etc/haproxy/haproxy.cfg           # expect: Configuration file is valid
```

- [ ] **Step 8: Runtime — HA (pull gate; the first run builds the Patroni image)**

```bash
make infra.postgres.up MODE=ha
```

Expected: ends `ready: postgres-ha` (etcd running, three Patroni nodes healthy, `haproxy` running, `migrate` exited 0).

```bash
make infra.patroni.list MODE=ha
```

Expected: three members; exactly one `Leader` and two `Replica` in state `streaming`, all on the same timeline.

```bash
"$SCRATCH/infracheck/infracheck" postgres localhost:5432 false    # expect: ok: the write port reaches the primary
"$SCRATCH/infracheck/infracheck" postgres localhost:5433 true     # expect: ok: the read port reaches a replica
Q() { dc exec -T patroni-1 sh -c 'PGPASSWORD="$PG_APP_PASSWORD" psql -h postgres -U "$PG_APP_USER" -d "$PG_DATABASE" -tAc "$1"' _ "$1"; }
Q "select version, dirty from schema_migrations"                  # expect: the highest migration number, |f (migrations went through HAProxy to the primary)
```

Drill — lose the primary; a replica is promoted and `:5432` follows it:

```bash
leader="$(curl -s http://localhost:8008/cluster | jq -r '.members[] | select(.role=="leader") | .name')"; echo "leader: $leader"
dc stop "$leader"
sleep 45
case "$leader" in patroni-1) port=8009 ;; *) port=8008 ;; esac
curl -s "http://localhost:$port/cluster" | jq -r '.members[] | "\(.name) \(.role) \(.state)"'    # expect: a new leader, one replica, the stopped member absent/stopped
"$SCRATCH/infracheck/infracheck" postgres localhost:5432 false    # expect: ok: HAProxy now points at the new primary
dc start "$leader"; sleep 40
make infra.patroni.list MODE=ha                                   # expect: three members again, the old leader rejoined as Replica
```

Restart persistence and re-run idempotency (Review Focus 3 and 2):

```bash
Q "create table infra_smoke(id int); insert into infra_smoke values (7)"
make infra.postgres.down && make infra.postgres.up MODE=ha
Q "select id from infra_smoke"                                    # expect: 7 (data and etcd state survived the restart)
make infra.postgres.up MODE=ha                                    # expect: ready; migrate logs "no change"
Q "drop table infra_smoke"
```

HAProxy stats and metrics: `curl -s http://localhost:8404/metrics | grep -c '^haproxy_server_status'` → at least `6` (3 servers × 2 listeners). Patroni metrics: `curl -s http://localhost:8008/metrics | grep -E '^patroni_(primary|postgres_running)'` shows both series.

- [ ] **Step 9: Runtime — switching to single (Review Focus 4)**

```bash
make infra.postgres.up MODE=single       # the HA containers are removed first, then postgres-single answers on 5432
"$SCRATCH/infracheck/infracheck" postgres localhost:5432 false     # expect: ok
make infra.postgres.up MODE=ha           # and back: the HA volumes were kept, so the cluster comes back with its data
```

- [ ] **Step 10: Tear down and commit**

```bash
make infra.postgres.down && echo y | make infra.clean
git add deploy/postgres .make/docker.mk
git commit -m "feat(infra): add Patroni HA mode for Postgres with etcd and HAProxy" \
  -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

## Task 6: ELK single-node mode and the shared log pipeline

**Files:**
- Replace stub: `deploy/elk/compose.yml`
- Create: `deploy/elk/setup.sh`
- Create: `deploy/elk/logstash/logstash.yml`, `deploy/elk/logstash/pipelines.yml`, `deploy/elk/logstash/pipeline/10-input.conf`, `deploy/elk/logstash/pipeline/20-filter.conf`, `deploy/elk/logstash/pipeline/30-output-single.conf`
- Create: `deploy/elk/filebeat/filebeat-single.yml`
- Modify: `.make/docker.mk` (append `infra.es.health`)

**Interfaces:**
- Consumes: Task 1 (`ELASTIC_PASSWORD`, `KIBANA_*`, `LOGSTASH_WRITER_PASSWORD`, `GRAFANA_READER_PASSWORD`, `METRICS_READER_PASSWORD`, `ES_HEAP`, `LS_HEAP`).
- Produces: `es-single` (alias `es-1`, host `9200`), `elk-setup-single`, `logstash-single` (alias `logstash-1`, API `9600` in-network), `kibana-single` (host `5601`), `filebeat-single`; shared `elasticsearch-exporter` (`:9114`) and `logstash-exporter-1` (`:9198`). ES objects: users `logstash_writer`, `grafana_reader`, `metrics_reader`, `kibana_system`; ILM policy `fupi-logs`; index template `logs-fupi`; data stream `logs-fupi-default`. Log field contract: `message`, `log.level`, `service.name`, `trace.id`, `span.id`, `@timestamp`. `setup.sh` is shared with Task 7 (`ELK_MODE=ha|single`).

- [ ] **Step 1: Write `deploy/elk/setup.sh`**

```bash
#!/bin/bash
# Elastic setup job. Idempotent: safe to run on every `up`.
#   ELK_MODE=ha   first generates a CA and the node certificates into the shared certs volume
#   both modes    then wait for Elasticsearch and provision users, roles, the ILM policy and the index template
# Env: ELK_MODE (ha|single), ES_URL, ELASTIC_PASSWORD, KIBANA_SYSTEM_PASSWORD, LOGSTASH_WRITER_PASSWORD,
#      GRAFANA_READER_PASSWORD, METRICS_READER_PASSWORD
set -euo pipefail

ES="${ES_URL:?}"

if [ "${ELK_MODE:?}" = ha ]; then
  CERTS=/usr/share/elasticsearch/config/certs
  if [ ! -f "$CERTS/ca/ca.crt" ]; then
    echo "generating the certificate authority"
    bin/elasticsearch-certutil ca --silent --pem --out "$CERTS/ca.zip"
    unzip -q -o "$CERTS/ca.zip" -d "$CERTS"
  fi
  if [ ! -f "$CERTS/es-3/es-3.crt" ]; then
    echo "generating the node certificates"
    cat >"$CERTS/instances.yml" <<'YAML'
instances:
  - name: es-1
    dns: [es-1, localhost]
    ip: [127.0.0.1]
  - name: es-2
    dns: [es-2, localhost]
    ip: [127.0.0.1]
  - name: es-3
    dns: [es-3, localhost]
    ip: [127.0.0.1]
YAML
    bin/elasticsearch-certutil cert --silent --pem --in "$CERTS/instances.yml" \
      --ca-cert "$CERTS/ca/ca.crt" --ca-key "$CERTS/ca/ca.key" --out "$CERTS/certs.zip"
    unzip -q -o "$CERTS/certs.zip" -d "$CERTS"
  fi
  # Development only: the keys are world-readable so the unprivileged elasticsearch user can read them.
  chown -R root:root "$CERTS"
  find "$CERTS" -type d -exec chmod 755 {} +
  find "$CERTS" -type f -exec chmod 644 {} +
  touch "$CERTS/.ready"
fi

es() { curl -sS --fail-with-body -u "elastic:${ELASTIC_PASSWORD}" -H 'Content-Type: application/json' "$@"; }

echo "waiting for Elasticsearch at $ES"
until [ "$(curl -s -o /dev/null -w '%{http_code}' -u "elastic:${ELASTIC_PASSWORD}" "$ES/_cluster/health?wait_for_status=yellow&timeout=5s")" = 200 ]; do
  sleep 3
done

REPLICAS=0
[ "$ELK_MODE" = ha ] && REPLICAS=1

echo "provisioning roles and users"
es -X POST "$ES/_security/user/kibana_system/_password" -d "{\"password\":\"${KIBANA_SYSTEM_PASSWORD}\"}" >/dev/null

es -X PUT "$ES/_security/role/logstash_writer" -d '{
  "cluster": ["monitor"],
  "indices": [{"names": ["logs-fupi-*"], "privileges": ["create_doc", "auto_configure", "view_index_metadata"]}]
}' >/dev/null
# Grafana's datasource health check calls GET / on the cluster, which needs the cluster "monitor" privilege.
es -X PUT "$ES/_security/role/grafana_reader" -d '{
  "cluster": ["monitor"],
  "indices": [{"names": ["logs-fupi-*"], "privileges": ["read", "view_index_metadata"]}]
}' >/dev/null
es -X PUT "$ES/_security/role/metrics_reader" -d '{
  "cluster": ["monitor"],
  "indices": [{"names": ["*"], "privileges": ["monitor"]}]
}' >/dev/null

es -X PUT "$ES/_security/user/logstash_writer" -d "{\"password\":\"${LOGSTASH_WRITER_PASSWORD}\",\"roles\":[\"logstash_writer\"]}" >/dev/null
es -X PUT "$ES/_security/user/grafana_reader" -d "{\"password\":\"${GRAFANA_READER_PASSWORD}\",\"roles\":[\"grafana_reader\"]}" >/dev/null
es -X PUT "$ES/_security/user/metrics_reader" -d "{\"password\":\"${METRICS_READER_PASSWORD}\",\"roles\":[\"metrics_reader\"]}" >/dev/null

echo "installing the ILM policy and the index template"
es -X PUT "$ES/_ilm/policy/fupi-logs" -d '{
  "policy": {"phases": {
    "hot": {"actions": {"rollover": {"max_age": "1d", "max_primary_shard_size": "5gb"}}},
    "delete": {"min_age": "7d", "actions": {"delete": {}}}
  }}
}' >/dev/null

es -X PUT "$ES/_index_template/logs-fupi" -d "{
  \"index_patterns\": [\"logs-fupi-*\"],
  \"data_stream\": {},
  \"priority\": 500,
  \"template\": {
    \"settings\": {\"index.lifecycle.name\": \"fupi-logs\", \"index.number_of_replicas\": ${REPLICAS}},
    \"mappings\": {\"properties\": {
      \"@timestamp\": {\"type\": \"date\"},
      \"message\": {\"type\": \"text\"},
      \"service\": {\"properties\": {\"name\": {\"type\": \"keyword\"}}},
      \"trace\": {\"properties\": {\"id\": {\"type\": \"keyword\"}}},
      \"span\": {\"properties\": {\"id\": {\"type\": \"keyword\"}}},
      \"log\": {\"properties\": {\"level\": {\"type\": \"keyword\"}}}
    }}
  }
}" >/dev/null

echo "elk setup complete"
```

- [ ] **Step 2: Write the Logstash configuration**

`deploy/elk/logstash/logstash.yml`:

```yaml
# Bind the monitoring API to every interface so the exporter and the healthcheck can reach it (Logstash 9 name).
api.http.host: "0.0.0.0"
pipeline.ecs_compatibility: v8
# Survive a Logstash restart without losing buffered events; failed events go to the dead-letter queue.
queue.type: persisted
queue.max_bytes: 512mb
dead_letter_queue.enable: true
```

`deploy/elk/logstash/pipelines.yml`:

```yaml
- pipeline.id: main
  path.config: "/etc/logstash/conf.d/*.conf"
```

`deploy/elk/logstash/pipeline/10-input.conf`:

```text
input {
  beats {
    port => 5044
  }
}
```

`deploy/elk/logstash/pipeline/20-filter.conf`:

```text
filter {
  # The container log parser keeps each line's trailing newline.
  mutate { strip => ["message"] }

  # Applications log one JSON object per line (slog). Anything else is kept as a plain message.
  if [message] =~ /^\s*\{/ {
    json {
      source => "message"
      target => "app"
      skip_on_invalid_json => true
    }
    if [app] {
      mutate { rename => { "[app][msg]" => "message" } }
      mutate { rename => { "[app][level]" => "[log][level]" } }
      mutate { rename => { "[app][trace_id]" => "[trace][id]" } }
      mutate { rename => { "[app][span_id]" => "[span][id]" } }
      mutate { rename => { "[app][service]" => "[service][name]" } }
      if [app][time] {
        date {
          match => ["[app][time]", "ISO8601"]
          target => "@timestamp"
        }
        mutate { remove_field => ["[app][time]"] }
      }
    }
  }

  # Anything without a service name (infrastructure containers) is labelled with its compose service.
  if ![service][name] and [container][labels][com_docker_compose_service] {
    mutate { copy => { "[container][labels][com_docker_compose_service]" => "[service][name]" } }
  }
}
```

`deploy/elk/logstash/pipeline/30-output-single.conf`:

```text
output {
  elasticsearch {
    hosts => ["http://es-1:9200"]
    user => "logstash_writer"
    password => "${LOGSTASH_WRITER_PASSWORD}"
    data_stream => true
    data_stream_type => "logs"
    data_stream_dataset => "fupi"
    data_stream_namespace => "default"
  }
}
```

- [ ] **Step 3: Write `deploy/elk/filebeat/filebeat-single.yml`**

```yaml
filebeat.inputs:
  # Filebeat 9 dropped the container input: read the Docker json logs with filestream and its container parser.
  - type: filestream
    id: docker-container-logs
    paths:
      - /var/lib/docker/containers/*/*.log
    parsers:
      - container:
          stream: all

processors:
  - add_docker_metadata:
      host: "unix:///var/run/docker.sock"
      labels.dedot: true
  # Only this compose project's containers, and never Filebeat's own output (it would feed back into itself).
  - drop_event:
      when:
        not:
          equals:
            container.labels.com_docker_compose_project: fupi
  - drop_event:
      when:
        contains:
          container.name: filebeat

output.logstash:
  hosts: ["logstash-1:5044"]

logging.level: warning
```

- [ ] **Step 4: Write `deploy/elk/compose.yml` (single mode and the shared exporters)**

```yaml
# Elastic stack for logs (Filebeat -> Logstash -> Elasticsearch data stream logs-fupi-default -> Kibana).
#   single  es-single (alias es-1), logstash-single (alias logstash-1), kibana-single, filebeat-single
#   HA      added by the ELK HA task: es-1..3, logstash-1..2, kibana-1..2 + nginx, filebeat-ha
# Security is on in both modes. HTTP TLS is off locally; transport TLS is on in HA (required by multi-node clusters).
x-es-image: &es-image docker.elastic.co/elasticsearch/elasticsearch:9.5.4

x-es-env: &es-env
  cluster.name: fupi-logs
  ELASTIC_PASSWORD: ${ELASTIC_PASSWORD:-fupi-elastic-dev}
  bootstrap.memory_lock: "false"
  xpack.security.enabled: "true"
  xpack.security.http.ssl.enabled: "false"
  xpack.ml.enabled: "false"
  ES_JAVA_OPTS: -Xms${ES_HEAP:-512m} -Xmx${ES_HEAP:-512m}

x-setup-env: &setup-env
  ELASTIC_PASSWORD: ${ELASTIC_PASSWORD:-fupi-elastic-dev}
  KIBANA_SYSTEM_PASSWORD: ${KIBANA_SYSTEM_PASSWORD:-fupi-kibana-dev}
  LOGSTASH_WRITER_PASSWORD: ${LOGSTASH_WRITER_PASSWORD:-fupi-logstash-dev}
  GRAFANA_READER_PASSWORD: ${GRAFANA_READER_PASSWORD:-fupi-grafana-reader}
  METRICS_READER_PASSWORD: ${METRICS_READER_PASSWORD:-fupi-metrics-dev}

x-kibana-env: &kibana-env
  SERVER_PUBLICBASEURL: http://localhost:5601
  ELASTICSEARCH_USERNAME: kibana_system
  ELASTICSEARCH_PASSWORD: ${KIBANA_SYSTEM_PASSWORD:-fupi-kibana-dev}
  XPACK_SECURITY_ENCRYPTIONKEY: ${KIBANA_SECURITY_KEY:-fupi-dev-kibana-security-key-0123456789ab}
  XPACK_ENCRYPTEDSAVEDOBJECTS_ENCRYPTIONKEY: ${KIBANA_ENCRYPTED_OBJECTS_KEY:-fupi-dev-kibana-saved-objects-key-0123456789}
  XPACK_REPORTING_ENCRYPTIONKEY: ${KIBANA_REPORTING_KEY:-fupi-dev-kibana-reporting-key-0123456789ab}
  TELEMETRY_OPTIN: "false"
  NODE_OPTIONS: --max-old-space-size=768

services:
  es-single:
    image: *es-image
    profiles: [elk-single, full-single]
    restart: unless-stopped
    environment:
      <<: *es-env
      node.name: es-1
      discovery.type: single-node
    ports:
      - "127.0.0.1:9200:9200"
    ulimits:
      memlock: {soft: -1, hard: -1}
      nofile: {soft: 65536, hard: 65536}
    volumes:
      - es-single-data:/usr/share/elasticsearch/data
    healthcheck:
      test: ["CMD-SHELL", "curl -s -u elastic:$$ELASTIC_PASSWORD http://localhost:9200/_cluster/health | grep -Eq '\"status\":\"(green|yellow)\"'"]
      interval: 10s
      timeout: 5s
      retries: 30
      start_period: 40s
    networks:
      fupi:
        aliases: [es-1]

  elk-setup-single:
    image: *es-image
    user: "0"
    profiles: [elk-single, full-single]
    restart: "no"
    depends_on:
      es-single: {condition: service_healthy}
    environment:
      <<: *setup-env
      ELK_MODE: single
      ES_URL: http://es-1:9200
    entrypoint: ["/bin/bash", "/setup.sh"]
    volumes:
      - ./setup.sh:/setup.sh:ro
    networks:
      - fupi

  logstash-single:
    image: docker.elastic.co/logstash/logstash:9.5.4
    profiles: [elk-single, full-single]
    restart: unless-stopped
    depends_on:
      elk-setup-single: {condition: service_completed_successfully}
    environment:
      LS_JAVA_OPTS: -Xms${LS_HEAP:-512m} -Xmx${LS_HEAP:-512m}
      LOGSTASH_WRITER_PASSWORD: ${LOGSTASH_WRITER_PASSWORD:-fupi-logstash-dev}
    volumes:
      - logstash-single-data:/usr/share/logstash/data
      - ./logstash/logstash.yml:/usr/share/logstash/config/logstash.yml:ro
      - ./logstash/pipelines.yml:/usr/share/logstash/config/pipelines.yml:ro
      - ./logstash/pipeline/10-input.conf:/etc/logstash/conf.d/10-input.conf:ro
      - ./logstash/pipeline/20-filter.conf:/etc/logstash/conf.d/20-filter.conf:ro
      - ./logstash/pipeline/30-output-single.conf:/etc/logstash/conf.d/30-output.conf:ro
    healthcheck:
      test: ["CMD-SHELL", "curl -fsS http://localhost:9600 >/dev/null"]
      interval: 10s
      timeout: 5s
      retries: 30
      start_period: 40s
    networks:
      fupi:
        aliases: [logstash-1]

  kibana-single:
    image: docker.elastic.co/kibana/kibana:9.5.4
    profiles: [elk-single, full-single]
    restart: unless-stopped
    depends_on:
      elk-setup-single: {condition: service_completed_successfully}
    environment:
      <<: *kibana-env
      SERVER_NAME: kibana
      ELASTICSEARCH_HOSTS: http://es-1:9200
    ports:
      - "127.0.0.1:5601:5601"
    healthcheck:
      test: ["CMD-SHELL", "curl -s -I http://localhost:5601 | grep -q 'HTTP/1.1 302 Found'"]
      interval: 10s
      timeout: 5s
      retries: 30
      start_period: 60s
    networks:
      - fupi

  # Reads container logs, so it runs as root and mounts the Docker socket read-only: the one documented
  # least-privilege exception in this stack.
  filebeat-single:
    image: docker.elastic.co/beats/filebeat:9.5.4
    user: root
    profiles: [elk-single, full-single]
    restart: unless-stopped
    command: ["filebeat", "-e", "--strict.perms=false"]
    depends_on:
      logstash-single: {condition: service_healthy}
    volumes:
      - ./filebeat/filebeat-single.yml:/usr/share/filebeat/filebeat.yml:ro
      - filebeat-single-data:/usr/share/filebeat/data
      - /var/lib/docker/containers:/var/lib/docker/containers:ro
      - /var/run/docker.sock:/var/run/docker.sock:ro
    networks:
      - fupi

  # Shared by both modes: they reach the cluster through es-1 / logstash-1.
  elasticsearch-exporter:
    image: quay.io/prometheuscommunity/elasticsearch-exporter:v1.11.0
    profiles: [elk-ha, elk-single, full-ha, full-single]
    restart: unless-stopped
    command:
      - "--es.uri=http://metrics_reader:${METRICS_READER_PASSWORD:-fupi-metrics-dev}@es-1:9200"
      - "--es.all"
      - "--es.indices"
    networks:
      - fupi

  logstash-exporter-1:
    image: kuskoman/logstash-exporter:v1.9.1
    profiles: [elk-ha, elk-single, full-ha, full-single]
    restart: unless-stopped
    environment:
      LOGSTASH_URL: http://logstash-1:9600
    networks:
      - fupi

volumes:
  es-single-data:
  logstash-single-data:
  filebeat-single-data:

networks:
  fupi:
    name: fupi
```

- [ ] **Step 5: Append the helper target to `.make/docker.mk`**

```make

.PHONY: infra.es.health
infra.es.health: ## Show the Elasticsearch cluster health (MODE=ha or single)
	@$(COMPOSE) --profile '*' exec -T $(ES_SERVICE) sh -c 'curl -s -u elastic:"$$ELASTIC_PASSWORD" "localhost:9200/_cluster/health?pretty"'
```

- [ ] **Step 6: Static verification**

```bash
make infra.config
docker compose --profile elk-single config --services | sort
# expect: elasticsearch-exporter elk-setup-single es-single filebeat-single kibana-single logstash-exporter-1 logstash-single
bash -n deploy/elk/setup.sh && echo "bash syntax ok"
```

Validate the assembled single-mode pipeline together with our `logstash.yml` and `pipelines.yml` (a settings typo only shows up when the settings are loaded):

```bash
cat deploy/elk/logstash/pipeline/10-input.conf deploy/elk/logstash/pipeline/20-filter.conf deploy/elk/logstash/pipeline/30-output-single.conf > "$SCRATCH/single.conf"
docker run --rm -e LOGSTASH_WRITER_PASSWORD=x -v "$SCRATCH/single.conf:/c.conf:ro" -v "$PWD/deploy/elk/logstash/logstash.yml:/usr/share/logstash/config/logstash.yml:ro" -v "$PWD/deploy/elk/logstash/pipelines.yml:/usr/share/logstash/config/pipelines.yml:ro" docker.elastic.co/logstash/logstash:9.5.4 \
  logstash --config.test_and_exit -f /c.conf 2>&1 | grep -E "Configuration OK|invalid|ERROR"
```

Expected: `Configuration OK`.

- [ ] **Step 7: Runtime — single (pull gate; about 3 GiB of images)**

```bash
make infra.elk.up MODE=single
make infra.es.health MODE=single | grep -E '"status"|number_of_nodes'      # expect: "green", 1
```

Expected: `ready: elk-single` (`es-single`, `logstash-single`, `kibana-single` healthy; `elk-setup-single` exited 0; `filebeat-single` and both exporters running).

Provisioned objects:

```bash
E() { dc exec -T es-single sh -c 'curl -s -u elastic:"$ELASTIC_PASSWORD" "localhost:9200/$1"' _ "$1"; }
E "_security/user/logstash_writer,grafana_reader,metrics_reader" | jq 'keys'   # expect: all three users
E "_ilm/policy/fupi-logs" | jq -r '.["fupi-logs"].policy.phases.delete.min_age'   # expect: 7d
E "_index_template/logs-fupi" | jq -r '.index_templates[0].name'                  # expect: logs-fupi
```

End-to-end log path with a throwaway container (carries the compose project label so Filebeat keeps it):

```bash
NOW="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
docker run --rm --label com.docker.compose.project=fupi --label com.docker.compose.service=smoke busybox \
  sh -c "echo '{\"time\":\"$NOW\",\"level\":\"INFO\",\"msg\":\"hello from smoke\",\"service\":\"smoke\",\"trace_id\":\"abc123\",\"span_id\":\"def456\"}'; echo 'plain text line'; sleep 20"
sleep 5
E "logs-fupi-default/_search?q=trace.id:abc123" | jq '.hits.hits[0]._source | {message, level: .log.level, service: .service.name, trace: .trace.id, span: .span.id}'
```

Expected: `message: "hello from smoke"`, `level: "INFO"`, `service: "smoke"`, `trace: "abc123"`, `span: "def456"`. The plain line must also be there with `service.name` = `smoke` taken from the compose label and its text stored without the trailing newline. The throwaway container sleeps for 20 seconds on purpose: `docker run --rm` deletes the container's log file as soon as it exits, which can happen before Filebeat's 10-second scan has found it.

```bash
E 'logs-fupi-default/_search?q=message:plain&size=1' | jq -r '.hits.hits[0]._source | "\(.service.name) \(.message | @json)"'   # expect: smoke "plain text line" (no trailing newline)
```

If no document arrives within about 40 seconds, read `dc logs filebeat-single` and `dc logs logstash-single` before changing anything. A `permission denied` or missing-path error from Filebeat means it cannot read `/var/lib/docker/containers` (spec risk: Filebeat on Docker Desktop; that path normally lives inside the Docker VM and is mountable). In that case stop and report the exact error to the user instead of working around it.

Kibana and exporters:

```bash
curl -s -o /dev/null -w '%{http_code}\n' http://localhost:5601/login                      # expect: 200
curl -s -u elastic:fupi-elastic-dev http://localhost:5601/api/status | jq -r '.status.overall.level'   # expect: available
docker run --rm --network fupi busybox wget -qO- http://elasticsearch-exporter:9114/metrics | grep -c '^elasticsearch_cluster_health_status'   # expect: 3 (green/yellow/red series)
docker run --rm --network fupi busybox wget -qO- http://logstash-exporter-1:9198/metrics | grep -c '^logstash_'                              # expect: >= 1
```

If the exporters return no series, check `dc logs elasticsearch-exporter logstash-exporter-1` before moving on.

Re-run idempotency (Review Focus 2) and restart persistence (Review Focus 3):

```bash
make infra.elk.up MODE=single                  # expect: ready; elk-setup-single exits 0 again (PUTs are idempotent)
make infra.elk.down && make infra.elk.up MODE=single
E 'logs-fupi-default/_search?q=trace.id:abc123' | jq '.hits.total.value'     # expect: 1 (the data stream survived the restart)
```

- [ ] **Step 8: Tear down and commit**

```bash
make infra.elk.down && echo y | make infra.clean
git add deploy/elk .make/docker.mk
git commit -m "feat(infra): add single-node ELK stack and the log pipeline" \
  -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

## Task 7: ELK HA (3 Elasticsearch nodes, 2 Logstash, 2 Kibana behind nginx)

**Files:**
- Create: `deploy/elk/logstash/pipeline/30-output-ha.conf`, `deploy/elk/filebeat/filebeat-ha.yml`, `deploy/elk/nginx/kibana.conf`
- Replace: `deploy/elk/compose.yml` (final version: Task 6's services unchanged, plus the HA services)

**Interfaces:**
- Consumes: Task 6 (`setup.sh` with `ELK_MODE=ha`, the shared Logstash `10-input`/`20-filter`, `x-*` blocks, exporters).
- Produces: `elk-setup-ha` (healthy once certificates exist, then provisions and exits), `es-1..3` (host `9200`/`9201`/`9202`), `logstash-1..2`, `kibana-1..2`, `kibana-lb` (alias `kibana`, host `5601`), `filebeat-ha`, `logstash-exporter-2` (`:9198`). Certificates live in volume `es-certs`.

- [ ] **Step 1: Write the HA Logstash output `deploy/elk/logstash/pipeline/30-output-ha.conf`**

```text
output {
  elasticsearch {
    hosts => ["http://es-1:9200", "http://es-2:9200", "http://es-3:9200"]
    user => "logstash_writer"
    password => "${LOGSTASH_WRITER_PASSWORD}"
    data_stream => true
    data_stream_type => "logs"
    data_stream_dataset => "fupi"
    data_stream_namespace => "default"
  }
}
```

- [ ] **Step 2: Write `deploy/elk/filebeat/filebeat-ha.yml`**

```yaml
filebeat.inputs:
  # Filebeat 9 dropped the container input: read the Docker json logs with filestream and its container parser.
  - type: filestream
    id: docker-container-logs
    paths:
      - /var/lib/docker/containers/*/*.log
    parsers:
      - container:
          stream: all

processors:
  - add_docker_metadata:
      host: "unix:///var/run/docker.sock"
      labels.dedot: true
  # Only this compose project's containers, and never Filebeat's own output (it would feed back into itself).
  - drop_event:
      when:
        not:
          equals:
            container.labels.com_docker_compose_project: fupi
  - drop_event:
      when:
        contains:
          container.name: filebeat

# Spread events over both Logstash nodes; if one is down Filebeat uses the other.
output.logstash:
  hosts: ["logstash-1:5044", "logstash-2:5044"]
  loadbalance: true

logging.level: warning
```

- [ ] **Step 3: Write `deploy/elk/nginx/kibana.conf`**

```nginx
# Round-robins the two Kibana instances; a failed instance is skipped for 5 seconds.
upstream kibana {
    server kibana-1:5601 max_fails=1 fail_timeout=5s;
    server kibana-2:5601 max_fails=1 fail_timeout=5s;
}

server {
    listen 5601;

    location / {
        proxy_pass http://kibana;
        proxy_http_version 1.1;
        proxy_set_header Host $http_host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_next_upstream error timeout http_502 http_503 http_504;
        proxy_read_timeout 120s;
    }
}
```

- [ ] **Step 4: Replace `deploy/elk/compose.yml` with the final version**

```yaml
# Elastic stack for logs (Filebeat -> Logstash -> Elasticsearch data stream logs-fupi-default -> Kibana).
#   HA      es-1..3 (3 master+data nodes), logstash-1..2, kibana-1..2 behind nginx (kibana-lb), filebeat-ha.
#           elk-setup-ha creates the CA and node certificates (transport TLS is required by multi-node clusters).
#   single  es-single (alias es-1), logstash-single (alias logstash-1), kibana-single, filebeat-single.
# Security is on in both modes. HTTP TLS is off locally.
x-es-image: &es-image docker.elastic.co/elasticsearch/elasticsearch:9.5.4

x-es-env: &es-env
  cluster.name: fupi-logs
  ELASTIC_PASSWORD: ${ELASTIC_PASSWORD:-fupi-elastic-dev}
  bootstrap.memory_lock: "false"
  xpack.security.enabled: "true"
  xpack.security.http.ssl.enabled: "false"
  xpack.ml.enabled: "false"
  ES_JAVA_OPTS: -Xms${ES_HEAP:-512m} -Xmx${ES_HEAP:-512m}

x-es-ha-env: &es-ha-env
  <<: *es-env
  discovery.seed_hosts: es-1,es-2,es-3
  cluster.initial_master_nodes: es-1,es-2,es-3
  xpack.security.transport.ssl.enabled: "true"
  xpack.security.transport.ssl.verification_mode: certificate
  xpack.security.transport.ssl.certificate_authorities: certs/ca/ca.crt

x-es-common: &es-common
  image: *es-image
  restart: unless-stopped
  depends_on:
    elk-setup-ha: {condition: service_healthy}
  ulimits:
    memlock: {soft: -1, hard: -1}
    nofile: {soft: 65536, hard: 65536}
  healthcheck:
    test: ["CMD-SHELL", "curl -s -u elastic:$$ELASTIC_PASSWORD http://localhost:9200/_cluster/health | grep -Eq '\"status\":\"(green|yellow)\"'"]
    interval: 10s
    timeout: 5s
    retries: 30
    start_period: 40s

x-setup-env: &setup-env
  ELASTIC_PASSWORD: ${ELASTIC_PASSWORD:-fupi-elastic-dev}
  KIBANA_SYSTEM_PASSWORD: ${KIBANA_SYSTEM_PASSWORD:-fupi-kibana-dev}
  LOGSTASH_WRITER_PASSWORD: ${LOGSTASH_WRITER_PASSWORD:-fupi-logstash-dev}
  GRAFANA_READER_PASSWORD: ${GRAFANA_READER_PASSWORD:-fupi-grafana-reader}
  METRICS_READER_PASSWORD: ${METRICS_READER_PASSWORD:-fupi-metrics-dev}

x-kibana-env: &kibana-env
  SERVER_PUBLICBASEURL: http://localhost:5601
  ELASTICSEARCH_USERNAME: kibana_system
  ELASTICSEARCH_PASSWORD: ${KIBANA_SYSTEM_PASSWORD:-fupi-kibana-dev}
  XPACK_SECURITY_ENCRYPTIONKEY: ${KIBANA_SECURITY_KEY:-fupi-dev-kibana-security-key-0123456789ab}
  XPACK_ENCRYPTEDSAVEDOBJECTS_ENCRYPTIONKEY: ${KIBANA_ENCRYPTED_OBJECTS_KEY:-fupi-dev-kibana-saved-objects-key-0123456789}
  XPACK_REPORTING_ENCRYPTIONKEY: ${KIBANA_REPORTING_KEY:-fupi-dev-kibana-reporting-key-0123456789ab}
  TELEMETRY_OPTIN: "false"
  NODE_OPTIONS: --max-old-space-size=768

x-kibana-common: &kibana-common
  image: docker.elastic.co/kibana/kibana:9.5.4
  restart: unless-stopped
  depends_on:
    elk-setup-ha: {condition: service_completed_successfully}
  healthcheck:
    test: ["CMD-SHELL", "curl -s -I http://localhost:5601 | grep -q 'HTTP/1.1 302 Found'"]
    interval: 10s
    timeout: 5s
    retries: 30
    start_period: 60s

x-logstash-common: &logstash-common
  image: docker.elastic.co/logstash/logstash:9.5.4
  restart: unless-stopped
  environment:
    LS_JAVA_OPTS: -Xms${LS_HEAP:-512m} -Xmx${LS_HEAP:-512m}
    LOGSTASH_WRITER_PASSWORD: ${LOGSTASH_WRITER_PASSWORD:-fupi-logstash-dev}
  healthcheck:
    test: ["CMD-SHELL", "curl -fsS http://localhost:9600 >/dev/null"]
    interval: 10s
    timeout: 5s
    retries: 30
    start_period: 40s

services:
  # ---- HA ------------------------------------------------------------------------------------------------------
  elk-setup-ha:
    image: *es-image
    user: "0"
    profiles: [elk-ha, full-ha]
    restart: "no"
    environment:
      <<: *setup-env
      ELK_MODE: ha
      ES_URL: http://es-1:9200
    entrypoint: ["/bin/bash", "/setup.sh"]
    volumes:
      - es-certs:/usr/share/elasticsearch/config/certs
      - ./setup.sh:/setup.sh:ro
    # Healthy as soon as the certificates exist, so the nodes can start; the container then goes on to provision.
    healthcheck:
      test: ["CMD-SHELL", "test -f /usr/share/elasticsearch/config/certs/.ready"]
      interval: 2s
      timeout: 2s
      retries: 90
    networks:
      - fupi

  es-1:
    <<: *es-common
    profiles: [elk-ha, full-ha]
    environment:
      <<: *es-ha-env
      node.name: es-1
      xpack.security.transport.ssl.key: certs/es-1/es-1.key
      xpack.security.transport.ssl.certificate: certs/es-1/es-1.crt
    ports:
      - "127.0.0.1:9200:9200"
    volumes:
      - es-1-data:/usr/share/elasticsearch/data
      - es-certs:/usr/share/elasticsearch/config/certs:ro
    networks:
      - fupi

  es-2:
    <<: *es-common
    profiles: [elk-ha, full-ha]
    environment:
      <<: *es-ha-env
      node.name: es-2
      xpack.security.transport.ssl.key: certs/es-2/es-2.key
      xpack.security.transport.ssl.certificate: certs/es-2/es-2.crt
    ports:
      - "127.0.0.1:9201:9200"
    volumes:
      - es-2-data:/usr/share/elasticsearch/data
      - es-certs:/usr/share/elasticsearch/config/certs:ro
    networks:
      - fupi

  es-3:
    <<: *es-common
    profiles: [elk-ha, full-ha]
    environment:
      <<: *es-ha-env
      node.name: es-3
      xpack.security.transport.ssl.key: certs/es-3/es-3.key
      xpack.security.transport.ssl.certificate: certs/es-3/es-3.crt
    ports:
      - "127.0.0.1:9202:9200"
    volumes:
      - es-3-data:/usr/share/elasticsearch/data
      - es-certs:/usr/share/elasticsearch/config/certs:ro
    networks:
      - fupi

  logstash-1:
    <<: *logstash-common
    profiles: [elk-ha, full-ha]
    depends_on:
      elk-setup-ha: {condition: service_completed_successfully}
    volumes:
      - logstash-1-data:/usr/share/logstash/data
      - ./logstash/logstash.yml:/usr/share/logstash/config/logstash.yml:ro
      - ./logstash/pipelines.yml:/usr/share/logstash/config/pipelines.yml:ro
      - ./logstash/pipeline/10-input.conf:/etc/logstash/conf.d/10-input.conf:ro
      - ./logstash/pipeline/20-filter.conf:/etc/logstash/conf.d/20-filter.conf:ro
      - ./logstash/pipeline/30-output-ha.conf:/etc/logstash/conf.d/30-output.conf:ro
    networks:
      - fupi

  logstash-2:
    <<: *logstash-common
    profiles: [elk-ha, full-ha]
    depends_on:
      elk-setup-ha: {condition: service_completed_successfully}
    volumes:
      - logstash-2-data:/usr/share/logstash/data
      - ./logstash/logstash.yml:/usr/share/logstash/config/logstash.yml:ro
      - ./logstash/pipelines.yml:/usr/share/logstash/config/pipelines.yml:ro
      - ./logstash/pipeline/10-input.conf:/etc/logstash/conf.d/10-input.conf:ro
      - ./logstash/pipeline/20-filter.conf:/etc/logstash/conf.d/20-filter.conf:ro
      - ./logstash/pipeline/30-output-ha.conf:/etc/logstash/conf.d/30-output.conf:ro
    networks:
      - fupi

  kibana-1:
    <<: *kibana-common
    profiles: [elk-ha, full-ha]
    environment:
      <<: *kibana-env
      SERVER_NAME: kibana-1
      ELASTICSEARCH_HOSTS: '["http://es-1:9200","http://es-2:9200","http://es-3:9200"]'
    networks:
      - fupi

  kibana-2:
    <<: *kibana-common
    profiles: [elk-ha, full-ha]
    environment:
      <<: *kibana-env
      SERVER_NAME: kibana-2
      ELASTICSEARCH_HOSTS: '["http://es-1:9200","http://es-2:9200","http://es-3:9200"]'
    networks:
      - fupi

  kibana-lb:
    image: nginx:1.30.5-alpine
    profiles: [elk-ha, full-ha]
    restart: unless-stopped
    depends_on:
      kibana-1: {condition: service_healthy}
      kibana-2: {condition: service_healthy}
    ports:
      - "127.0.0.1:5601:5601"
    volumes:
      - ./nginx/kibana.conf:/etc/nginx/conf.d/default.conf:ro
    networks:
      fupi:
        aliases: [kibana]

  filebeat-ha:
    image: docker.elastic.co/beats/filebeat:9.5.4
    user: root
    profiles: [elk-ha, full-ha]
    restart: unless-stopped
    command: ["filebeat", "-e", "--strict.perms=false"]
    depends_on:
      logstash-1: {condition: service_healthy}
      logstash-2: {condition: service_healthy}
    volumes:
      - ./filebeat/filebeat-ha.yml:/usr/share/filebeat/filebeat.yml:ro
      - filebeat-ha-data:/usr/share/filebeat/data
      - /var/lib/docker/containers:/var/lib/docker/containers:ro
      - /var/run/docker.sock:/var/run/docker.sock:ro
    networks:
      - fupi

  logstash-exporter-2:
    image: kuskoman/logstash-exporter:v1.9.1
    profiles: [elk-ha, full-ha]
    restart: unless-stopped
    environment:
      LOGSTASH_URL: http://logstash-2:9600
    networks:
      - fupi

  # ---- single --------------------------------------------------------------------------------------------------
  es-single:
    image: *es-image
    profiles: [elk-single, full-single]
    restart: unless-stopped
    environment:
      <<: *es-env
      node.name: es-1
      discovery.type: single-node
    ports:
      - "127.0.0.1:9200:9200"
    ulimits:
      memlock: {soft: -1, hard: -1}
      nofile: {soft: 65536, hard: 65536}
    volumes:
      - es-single-data:/usr/share/elasticsearch/data
    healthcheck:
      test: ["CMD-SHELL", "curl -s -u elastic:$$ELASTIC_PASSWORD http://localhost:9200/_cluster/health | grep -Eq '\"status\":\"(green|yellow)\"'"]
      interval: 10s
      timeout: 5s
      retries: 30
      start_period: 40s
    networks:
      fupi:
        aliases: [es-1]

  elk-setup-single:
    image: *es-image
    user: "0"
    profiles: [elk-single, full-single]
    restart: "no"
    depends_on:
      es-single: {condition: service_healthy}
    environment:
      <<: *setup-env
      ELK_MODE: single
      ES_URL: http://es-1:9200
    entrypoint: ["/bin/bash", "/setup.sh"]
    volumes:
      - ./setup.sh:/setup.sh:ro
    networks:
      - fupi

  logstash-single:
    <<: *logstash-common
    profiles: [elk-single, full-single]
    depends_on:
      elk-setup-single: {condition: service_completed_successfully}
    volumes:
      - logstash-single-data:/usr/share/logstash/data
      - ./logstash/logstash.yml:/usr/share/logstash/config/logstash.yml:ro
      - ./logstash/pipelines.yml:/usr/share/logstash/config/pipelines.yml:ro
      - ./logstash/pipeline/10-input.conf:/etc/logstash/conf.d/10-input.conf:ro
      - ./logstash/pipeline/20-filter.conf:/etc/logstash/conf.d/20-filter.conf:ro
      - ./logstash/pipeline/30-output-single.conf:/etc/logstash/conf.d/30-output.conf:ro
    networks:
      fupi:
        aliases: [logstash-1]

  kibana-single:
    <<: *kibana-common
    profiles: [elk-single, full-single]
    depends_on:
      elk-setup-single: {condition: service_completed_successfully}
    environment:
      <<: *kibana-env
      SERVER_NAME: kibana
      ELASTICSEARCH_HOSTS: http://es-1:9200
    ports:
      - "127.0.0.1:5601:5601"
    networks:
      - fupi

  # Reads container logs, so it runs as root and mounts the Docker socket read-only: the one documented
  # least-privilege exception in this stack.
  filebeat-single:
    image: docker.elastic.co/beats/filebeat:9.5.4
    user: root
    profiles: [elk-single, full-single]
    restart: unless-stopped
    command: ["filebeat", "-e", "--strict.perms=false"]
    depends_on:
      logstash-single: {condition: service_healthy}
    volumes:
      - ./filebeat/filebeat-single.yml:/usr/share/filebeat/filebeat.yml:ro
      - filebeat-single-data:/usr/share/filebeat/data
      - /var/lib/docker/containers:/var/lib/docker/containers:ro
      - /var/run/docker.sock:/var/run/docker.sock:ro
    networks:
      - fupi

  # ---- shared by both modes: they reach the cluster through es-1 / logstash-1 -------------------------------------
  elasticsearch-exporter:
    image: quay.io/prometheuscommunity/elasticsearch-exporter:v1.11.0
    profiles: [elk-ha, elk-single, full-ha, full-single]
    restart: unless-stopped
    command:
      - "--es.uri=http://metrics_reader:${METRICS_READER_PASSWORD:-fupi-metrics-dev}@es-1:9200"
      - "--es.all"
      - "--es.indices"
    networks:
      - fupi

  logstash-exporter-1:
    image: kuskoman/logstash-exporter:v1.9.1
    profiles: [elk-ha, elk-single, full-ha, full-single]
    restart: unless-stopped
    environment:
      LOGSTASH_URL: http://logstash-1:9600
    networks:
      - fupi

volumes:
  es-certs:
  es-1-data:
  es-2-data:
  es-3-data:
  es-single-data:
  logstash-1-data:
  logstash-2-data:
  logstash-single-data:
  filebeat-ha-data:
  filebeat-single-data:

networks:
  fupi:
    name: fupi
```

- [ ] **Step 5: Static verification**

```bash
make infra.config
docker compose --profile elk-ha config --services | sort
# expect: elasticsearch-exporter elk-setup-ha es-1 es-2 es-3 filebeat-ha kibana-1 kibana-2 kibana-lb logstash-1 logstash-2 logstash-exporter-1 logstash-exporter-2
docker run --rm --add-host kibana-1:127.0.0.1 --add-host kibana-2:127.0.0.1 \
  -v "$PWD/deploy/elk/nginx/kibana.conf:/etc/nginx/conf.d/default.conf:ro" nginx:1.30.5-alpine nginx -t 2>&1 | tail -2
# expect: "syntax is ok" and "test is successful" (the --add-host flags let nginx resolve the upstream names outside the compose network)
cat deploy/elk/logstash/pipeline/10-input.conf deploy/elk/logstash/pipeline/20-filter.conf deploy/elk/logstash/pipeline/30-output-ha.conf > "$SCRATCH/ha.conf"
docker run --rm -e LOGSTASH_WRITER_PASSWORD=x -v "$SCRATCH/ha.conf:/c.conf:ro" -v "$PWD/deploy/elk/logstash/logstash.yml:/usr/share/logstash/config/logstash.yml:ro" -v "$PWD/deploy/elk/logstash/pipelines.yml:/usr/share/logstash/config/pipelines.yml:ro" docker.elastic.co/logstash/logstash:9.5.4 logstash --config.test_and_exit -f /c.conf 2>&1 | grep -E "Configuration OK|invalid|ERROR"
```

- [ ] **Step 6: Runtime — HA (pull gate; memory gate: watch `make infra.stats`)**

This is the heaviest stack (about 6.5 GiB estimated against about 7.7 GiB available). Start it alone.

```bash
make infra.elk.up MODE=ha
```

Expected: `ready: elk-ha`. If a container exits with code 137 (out of memory: `dc ps -a` shows `Exited (137)`), do not retry blindly: record the failure, lower `ES_HEAP` and `LS_HEAP` to `384m` in `.env` as an experiment, and report honestly what the Docker allocation can and cannot run. In that case verify the HA topology in two halves instead: (1) `dc up -d elk-setup-ha es-1 es-2 es-3` for the Elasticsearch cluster and TLS checks below, then stop them; (2) the Logstash/Filebeat/Kibana checks against a single-node Elasticsearch via `MODE=single`.

Cluster and transport TLS:

```bash
make infra.es.health MODE=ha | grep -E '"status"|number_of_nodes'     # expect: "green", 3
E() { dc exec -T es-1 sh -c 'curl -s -u elastic:"$ELASTIC_PASSWORD" "localhost:9200/$1"' _ "$1"; }
E "_nodes/settings?filter_path=nodes.*.settings.xpack.security.transport.ssl.enabled" | jq -r '.nodes[].settings.xpack.security.transport.ssl.enabled' | sort -u   # expect: true
E "_index_template/logs-fupi" | jq -r '.index_templates[0].index_template.template.settings.index.number_of_replicas'   # expect: 1 (HA)
```

End-to-end log path (same smoke container as Task 6), then the failure drills:

```bash
smoke() { docker run --rm --label com.docker.compose.project=fupi --label com.docker.compose.service=smoke busybox \
  sh -c "echo '{\"time\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",\"level\":\"INFO\",\"msg\":\"$1\",\"service\":\"smoke\",\"trace_id\":\"$2\"}'; sleep 20"; }
hits() { E "logs-fupi-default/_search?q=trace.id:$1" | jq '.hits.total.value'; }

smoke "ha baseline" ha0001; sleep 5; hits ha0001                    # expect: 1

dc stop es-2; sleep 20                                               # lose one Elasticsearch node
make infra.es.health MODE=ha | grep -E '"status"|number_of_nodes'    # expect: green or yellow, 2 nodes
smoke "one es node down" ha0002; sleep 5; hits ha0002               # expect: 1 (writes continue)
dc start es-2

dc stop logstash-1; sleep 5                                          # lose one Logstash node
smoke "one logstash down" ha0003; sleep 10; hits ha0003              # expect: 1 (Filebeat fails over to logstash-2)
dc start logstash-1

dc stop kibana-1; sleep 5                                            # lose one Kibana instance
for i in 1 2 3 4 5 6; do curl -s -o /dev/null -w '%{http_code} ' http://localhost:5601/login; done; echo   # expect: 200 200 200 200 200 200
dc start kibana-1
```

Restart persistence and re-run idempotency (Review Focus 3 and 2):

```bash
make infra.elk.down && make infra.elk.up MODE=ha
dc logs elk-setup-ha | grep -c "generating"        # expect: 0 (certificates were reused from the volume)
make infra.es.health MODE=ha | grep -E '"status"'  # expect: green
hits ha0001                                         # expect: 1
make infra.elk.up MODE=ha                           # expect: ready (re-run is a no-op)
```

Switch to single (Review Focus 4): `make infra.elk.up MODE=single` must remove the HA containers first and end `ready: elk-single`.

- [ ] **Step 7: Tear down and commit**

```bash
make infra.elk.down && echo y | make infra.clean
git add deploy/elk
git commit -m "feat(infra): add HA mode for ELK with 3 Elasticsearch nodes and nginx-fronted Kibana" \
  -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

## Task 8: Observability core — OTel Collector, Tempo, Prometheus, Alertmanager

**Files:**
- Replace stub: `deploy/observability/compose.yml`
- Create: `deploy/observability/otel-collector.yml`, `deploy/observability/tempo.yml`, `deploy/observability/alertmanager.yml`
- Create: `deploy/observability/prometheus/prometheus.yml`, `deploy/observability/prometheus/rules/stack.yml`, `deploy/observability/prometheus/tests/stack_test.yml`

**Interfaces:**
- Consumes: the exporter service names from Tasks 2–7 (`kafka-exporter:9308`, `redis-exporter:9121`, `postgres-exporter:9187`, `elasticsearch-exporter:9114`, `logstash-exporter-1/2:9198`, `patroni-1..3:8008`, `etcd-1..3:2381`, `haproxy:8404`).
- Produces: `otel-collector` (OTLP gRPC `4317` / HTTP `4318` on the host; app metrics re-exposed on `:8889`, its own on `:8888`), `tempo` (`3200` on the host), `prometheus` (`9090`), `alertmanager` (`9093`); named volumes `tempo-data`, `prometheus-data`, `alertmanager-data`; alert names `PatroniNoLeader`, `RedisClusterNotOk`, `KafkaUnderReplicatedPartitions`, `ElasticsearchClusterRed`, `RedirectLatencyHigh`. Grafana is added in Task 9.

- [ ] **Step 1: Write the failing alert-rule tests first**

Create `deploy/observability/prometheus/tests/stack_test.yml`. The rules file does not exist yet, so `promtool test rules` fails until Step 3.

```yaml
# Run with: promtool test rules tests/stack_test.yml (see Task 8, Step 5). Paths are relative to this file.
rule_files:
  - ../rules/stack.yml

evaluation_interval: 1m

tests:
  - name: Patroni has members but no leader
    interval: 1m
    input_series:
      - {series: 'patroni_primary{instance="patroni-1"}', values: "0 0 0 0 0"}
      - {series: 'patroni_primary{instance="patroni-2"}', values: "0 0 0 0 0"}
      - {series: 'patroni_postgres_running{instance="patroni-1"}', values: "1 1 1 1 1"}
    alert_rule_test:
      - eval_time: 4m
        alertname: PatroniNoLeader
        exp_alerts:
          - exp_labels: {severity: critical}
            exp_annotations: {summary: Patroni has members but no leader}

  - name: A Patroni leader exists, so no alert
    interval: 1m
    input_series:
      - {series: 'patroni_primary{instance="patroni-1"}', values: "1 1 1 1 1"}
      - {series: 'patroni_primary{instance="patroni-2"}', values: "0 0 0 0 0"}
      - {series: 'patroni_postgres_running{instance="patroni-1"}', values: "1 1 1 1 1"}
    alert_rule_test:
      - eval_time: 4m
        alertname: PatroniNoLeader
        exp_alerts: []

  - name: Single mode has no Patroni series at all, so no alert
    interval: 1m
    input_series:
      - {series: 'up{job="prometheus"}', values: "1 1 1 1 1"}
    alert_rule_test:
      - eval_time: 4m
        alertname: PatroniNoLeader
        exp_alerts: []

  - name: Redis cluster state not ok
    interval: 1m
    input_series:
      - {series: 'redis_cluster_state{instance="redis-exporter"}', values: "0 0 0 0 0"}
    alert_rule_test:
      - eval_time: 4m
        alertname: RedisClusterNotOk
        exp_alerts:
          - exp_labels: {severity: critical, instance: redis-exporter}
            exp_annotations: {summary: Redis cluster state is not ok}

  - name: Kafka under-replicated partitions
    interval: 1m
    input_series:
      - {series: 'kafka_topic_partition_under_replicated_partition{topic="url.events",partition="0"}', values: "1 1 1 1 1 1"}
    alert_rule_test:
      - eval_time: 5m
        alertname: KafkaUnderReplicatedPartitions
        exp_alerts:
          - exp_labels: {severity: warning}
            exp_annotations: {summary: Kafka has under-replicated partitions}

  - name: Elasticsearch cluster red
    interval: 1m
    input_series:
      - {series: 'elasticsearch_cluster_health_status{cluster="fupi-logs",color="red"}', values: "1 1 1 1 1"}
      - {series: 'elasticsearch_cluster_health_status{cluster="fupi-logs",color="green"}', values: "0 0 0 0 0"}
    alert_rule_test:
      - eval_time: 4m
        alertname: ElasticsearchClusterRed
        exp_alerts:
          - exp_labels: {severity: critical, cluster: fupi-logs, color: red}
            exp_annotations: {summary: Elasticsearch cluster status is red}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
docker run --rm --entrypoint promtool -v "$PWD/deploy/observability/prometheus:/p:ro" prom/prometheus:v3.15.0 \
  test rules /p/tests/stack_test.yml 2>&1 | tail -4
```

Expected: FAIL (`../rules/stack.yml` does not exist).

- [ ] **Step 3: Write the alert rules `deploy/observability/prometheus/rules/stack.yml`**

```yaml
groups:
  - name: fupi-stack
    rules:
      # "no leader" is only meaningful when Patroni members exist, so single mode (no Patroni) never alerts.
      - alert: PatroniNoLeader
        expr: sum(patroni_primary) == 0 and count(patroni_postgres_running) > 0
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: Patroni has members but no leader

      - alert: RedisClusterNotOk
        expr: redis_cluster_state == 0
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: Redis cluster state is not ok

      - alert: KafkaUnderReplicatedPartitions
        expr: sum(kafka_topic_partition_under_replicated_partition) > 0
        for: 2m
        labels:
          severity: warning
        annotations:
          summary: Kafka has under-replicated partitions

      - alert: ElasticsearchClusterRed
        expr: elasticsearch_cluster_health_status{color="red"} == 1
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: Elasticsearch cluster status is red

      # Inert until the application exports OpenTelemetry HTTP metrics (a later slice).
      - alert: RedirectLatencyHigh
        expr: histogram_quantile(0.99, sum by (le) (rate(http_server_request_duration_seconds_bucket{service_name="fupi"}[5m]))) > 0.1
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: p99 request latency is above 100ms
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
docker run --rm --entrypoint promtool -v "$PWD/deploy/observability/prometheus:/p:ro" prom/prometheus:v3.15.0 \
  test rules /p/tests/stack_test.yml 2>&1 | tail -4
```

Expected: `SUCCESS`. (If `exp_labels` mismatches, the output prints the labels Prometheus produced; fix the test or the rule, not both blindly.)

- [ ] **Step 5: Write the remaining configuration**

`deploy/observability/prometheus/prometheus.yml`:

```yaml
global:
  scrape_interval: 15s
  evaluation_interval: 15s

alerting:
  alertmanagers:
    - static_configs:
        - targets: ["alertmanager:9093"]

rule_files:
  - /etc/prometheus/rules/*.yml

# Everything except Prometheus itself is found with DNS service discovery on compose service names. A component that
# is not running (the other mode, or a stack that is stopped) has no DNS record, so it has no targets and cannot raise
# a false "down" alert. Trade-off: a container that crashes drops out of discovery instead of reporting up == 0, so
# stack health is alerted on from the exporters' own signals (see rules/stack.yml).
scrape_configs:
  - job_name: prometheus
    static_configs:
      - targets: ["localhost:9090"]

  - job_name: otel-collector
    dns_sd_configs:
      - names: [otel-collector]
        type: A
        port: 8889
    relabel_configs: &by_dns_name
      - source_labels: [__meta_dns_name]
        target_label: instance

  - job_name: otel-collector-internal
    dns_sd_configs:
      - names: [otel-collector]
        type: A
        port: 8888
    relabel_configs: *by_dns_name

  - job_name: tempo
    dns_sd_configs:
      - names: [tempo]
        type: A
        port: 3200
    relabel_configs: *by_dns_name

  - job_name: grafana
    dns_sd_configs:
      - names: [grafana]
        type: A
        port: 3000
    relabel_configs: *by_dns_name

  - job_name: alertmanager
    dns_sd_configs:
      - names: [alertmanager]
        type: A
        port: 9093
    relabel_configs: *by_dns_name

  - job_name: kafka
    dns_sd_configs:
      - names: [kafka-exporter]
        type: A
        port: 9308
    relabel_configs: *by_dns_name

  - job_name: redis
    dns_sd_configs:
      - names: [redis-exporter]
        type: A
        port: 9121
    relabel_configs: *by_dns_name

  - job_name: postgres
    dns_sd_configs:
      - names: [postgres-exporter]
        type: A
        port: 9187
    relabel_configs: *by_dns_name

  - job_name: patroni
    dns_sd_configs:
      - names: [patroni-1, patroni-2, patroni-3]
        type: A
        port: 8008
    relabel_configs: *by_dns_name

  - job_name: etcd
    dns_sd_configs:
      - names: [etcd-1, etcd-2, etcd-3]
        type: A
        port: 2381
    relabel_configs: *by_dns_name

  - job_name: haproxy
    dns_sd_configs:
      - names: [haproxy]
        type: A
        port: 8404
    relabel_configs: *by_dns_name

  - job_name: elasticsearch
    dns_sd_configs:
      - names: [elasticsearch-exporter]
        type: A
        port: 9114
    relabel_configs: *by_dns_name

  - job_name: logstash
    dns_sd_configs:
      - names: [logstash-exporter-1, logstash-exporter-2]
        type: A
        port: 9198
    relabel_configs: *by_dns_name
```

`deploy/observability/alertmanager.yml`:

```yaml
route:
  receiver: "null"
  group_by: [alertname]
  group_wait: 30s
  repeat_interval: 4h

receivers:
  # Alerts are only visible in the Alertmanager UI (http://localhost:9093). To be notified, replace this receiver with
  # a Slack, email or webhook receiver (see docs/LocalInfrastructure.md).
  - name: "null"
```

`deploy/observability/otel-collector.yml`:

```yaml
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317
      http:
        endpoint: 0.0.0.0:4318

processors:
  memory_limiter:
    check_interval: 1s
    limit_mib: 256
    spike_limit_mib: 64
  batch: {}

exporters:
  otlp/tempo:
    endpoint: tempo:4317
    tls:
      insecure: true
  # Prometheus scrapes this endpoint (job otel-collector); resource attributes such as service.name become labels.
  prometheus:
    endpoint: 0.0.0.0:8889
    resource_to_telemetry_conversion:
      enabled: true

extensions:
  health_check:
    endpoint: 0.0.0.0:13133

service:
  extensions: [health_check]
  telemetry:
    metrics:
      readers:
        - pull:
            exporter:
              prometheus:
                host: 0.0.0.0
                port: 8888
  pipelines:
    traces:
      receivers: [otlp]
      processors: [memory_limiter, batch]
      exporters: [otlp/tempo]
    metrics:
      receivers: [otlp]
      processors: [memory_limiter, batch]
      exporters: [prometheus]
```

`deploy/observability/tempo.yml`:

```yaml
stream_over_http_enabled: true

server:
  http_listen_port: 3200
  log_level: info

distributor:
  receivers:
    otlp:
      protocols:
        grpc:
          endpoint: 0.0.0.0:4317

storage:
  trace:
    backend: local
    wal:
      path: /var/tempo/wal
    local:
      path: /var/tempo/blocks

# Tempo 3.x keeps block retention in two places: the scheduler's provider decides what to delete and the worker
# compacts. Set both so the 72h limit does not depend on which one applies.
backend_scheduler:
  provider:
    compaction:
      compaction:
        block_retention: 72h

backend_worker:
  compaction:
    block_retention: 72h

usage_report:
  reporting_enabled: false
```

- [ ] **Step 6: Write `deploy/observability/compose.yml`**

```yaml
# Observability: the OTel Collector receives OTLP from the app and fans out (traces -> Tempo, metrics -> a Prometheus
# endpoint). Prometheus scrapes the Collector and every exporter; Alertmanager receives alerts; Grafana (added in the
# next task) reads Prometheus, Tempo and Elasticsearch. Logs go to ELK, not through the Collector.
# Single instance in every mode: production HA for these (Thanos/Mimir, replicated Grafana) is not emulated.
services:
  otel-collector:
    image: otel/opentelemetry-collector-contrib:0.161.0
    profiles: [observability, full-ha, full-single]
    restart: unless-stopped
    command: ["--config=/etc/otelcol-contrib/config.yaml"]
    ports:
      - "127.0.0.1:4317:4317"
      - "127.0.0.1:4318:4318"
    volumes:
      - ./otel-collector.yml:/etc/otelcol-contrib/config.yaml:ro
    networks:
      - fupi

  tempo:
    image: grafana/tempo:3.1.0
    profiles: [observability, full-ha, full-single]
    restart: unless-stopped
    command: ["-target=all", "-config.file=/etc/tempo.yaml"]
    ports:
      - "127.0.0.1:3200:3200"
    volumes:
      - ./tempo.yml:/etc/tempo.yaml:ro
      - tempo-data:/var/tempo
    networks:
      - fupi

  prometheus:
    image: prom/prometheus:v3.15.0
    profiles: [observability, full-ha, full-single]
    restart: unless-stopped
    command:
      - --config.file=/etc/prometheus/prometheus.yml
      - --storage.tsdb.path=/prometheus
      - --storage.tsdb.retention.time=7d
      - --web.enable-lifecycle
    ports:
      - "127.0.0.1:9090:9090"
    volumes:
      - ./prometheus/prometheus.yml:/etc/prometheus/prometheus.yml:ro
      - ./prometheus/rules:/etc/prometheus/rules:ro
      - prometheus-data:/prometheus
    networks:
      - fupi

  alertmanager:
    image: prom/alertmanager:v0.34.1
    profiles: [observability, full-ha, full-single]
    restart: unless-stopped
    command:
      - --config.file=/etc/alertmanager/alertmanager.yml
      - --storage.path=/alertmanager
    ports:
      - "127.0.0.1:9093:9093"
    volumes:
      - ./alertmanager.yml:/etc/alertmanager/alertmanager.yml:ro
      - alertmanager-data:/alertmanager
    networks:
      - fupi

volumes:
  tempo-data:
  prometheus-data:
  alertmanager-data:

networks:
  fupi:
    name: fupi
```

- [ ] **Step 7: Static verification**

```bash
make infra.config
docker compose --profile observability config --services | sort      # alertmanager otel-collector prometheus tempo
docker run --rm -v "$PWD/deploy/observability/otel-collector.yml:/c.yaml:ro" otel/opentelemetry-collector-contrib:0.161.0 validate --config=/c.yaml && echo "collector config valid"
docker run --rm --entrypoint promtool -v "$PWD/deploy/observability/prometheus:/etc/prometheus:ro" prom/prometheus:v3.15.0 check config /etc/prometheus/prometheus.yml
docker run --rm --entrypoint promtool -v "$PWD/deploy/observability/prometheus:/p:ro" prom/prometheus:v3.15.0 test rules /p/tests/stack_test.yml
docker run --rm --entrypoint amtool -v "$PWD/deploy/observability/alertmanager.yml:/a.yml:ro" prom/alertmanager:v0.34.1 check-config /a.yml
```

Expected: collector config valid; `SUCCESS` for the config check (1 rule file, N rules found) and for the rule tests; amtool `SUCCESS`. If the Collector rejects `service.telemetry.metrics.readers`, remove the `telemetry:` block and the `otel-collector-internal` Prometheus job (the Collector then exposes its own metrics only on localhost) and note it in the docs.

- [ ] **Step 8: Runtime — observability alone (pull gate; about 2 GiB)**

```bash
make infra.observability.up
```

Expected: `ready: observability`.

```bash
curl -s http://localhost:9090/-/ready                       # expect: Prometheus Server is Ready.
curl -s http://localhost:9093/-/ready                       # expect: OK
curl -s http://localhost:3200/ready                         # expect: ready (Tempo needs about 15 seconds)
curl -s http://localhost:3200/status/config | grep -c "block_retention: 72h"     # expect: >= 1
```

If Tempo exits with a configuration error naming `backend_worker`, remove that block (Tempo then keeps its 14-day default), record it in the docs, and continue.

Scrape targets exist only for what is running (the design point of DNS discovery); nothing may be reported down:

```bash
sleep 30
curl -s http://localhost:9090/api/v1/targets | jq -r '.data.activeTargets[] | "\(.labels.job) \(.health)"' | sort
# expect: alertmanager up, otel-collector up, otel-collector-internal up, prometheus up, tempo up. No kafka/redis/postgres/... jobs.
curl -s http://localhost:9090/api/v1/targets | jq '[.data.activeTargets[] | select(.health != "up")] | length'     # expect: 0
```

Synthetic telemetry through the Collector (a real OTLP client, not a mock):

```bash
TG=ghcr.io/open-telemetry/opentelemetry-collector-contrib/telemetrygen:v0.161.0
docker run --rm --network fupi "$TG" traces  --otlp-endpoint otel-collector:4317 --otlp-insecure --service fupi-smoke --traces 5
docker run --rm --network fupi "$TG" metrics --otlp-endpoint otel-collector:4317 --otlp-insecure --service fupi-smoke --metrics 5
sleep 20
curl -s "http://localhost:3200/api/search?tags=service.name%3Dfupi-smoke" | jq '.traces | length'                     # expect: >= 1
curl -s --get http://localhost:9090/api/v1/query --data-urlencode 'query=gen{service_name="fupi-smoke"}' | jq '.data.result | length'   # expect: >= 1
```

If the telemetrygen tag does not exist, use the newest tag listed at `https://github.com/open-telemetry/opentelemetry-collector-contrib/pkgs/container/opentelemetry-collector-contrib%2Ftelemetrygen`.

Container memory: cAdvisor was tried and removed. On Docker Desktop it cannot register the Docker or containerd factories (it reports "unable to create containerd client", with the Docker socket mounted and with `privileged: true`), so it exports no per-container series. `make infra.stats` is the supported way to see per-container memory and CPU, and the stack overview dashboard has no container-memory panel.

Discovery follows the stacks (start Kafka alongside; stop it and the target disappears without a "down"):

```bash
make infra.kafka.up MODE=single
sleep 45
curl -s http://localhost:9090/api/v1/targets | jq -r '.data.activeTargets[] | select(.labels.job=="kafka") | "\(.labels.instance) \(.health)"'     # expect: kafka-exporter up
curl -s --get http://localhost:9090/api/v1/query --data-urlencode 'query=kafka_brokers' | jq -r '.data.result[0].value[1]'                          # expect: 1
make infra.kafka.down; sleep 60
curl -s http://localhost:9090/api/v1/targets | jq '[.data.activeTargets[] | select(.labels.job=="kafka")] | length'                                 # expect: 0
```

Re-run idempotency and restart persistence (Review Focus 2 and 3): `make infra.observability.up` again ends `ready`; after `make infra.observability.down && make infra.observability.up`, the smoke trace is still searchable.

- [ ] **Step 9: Tear down and commit**

```bash
make infra.observability.down && echo y | make infra.clean
git add deploy/observability
git commit -m "feat(infra): add OpenTelemetry Collector, Tempo, Prometheus and Alertmanager" \
  -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

## Task 9: Grafana — provisioned datasources and two dashboards

**Files:**
- Modify: `deploy/observability/compose.yml` (add `grafana` and its volume)
- Create: `deploy/observability/grafana/provisioning/datasources/datasources.yml`, `deploy/observability/grafana/provisioning/dashboards/dashboards.yml`, `deploy/observability/grafana/dashboards/stack-overview.json`, `deploy/observability/grafana/dashboards/fupi-service.json`

**Interfaces:**
- Consumes: Task 8 (Prometheus `prometheus:9090`, Tempo `tempo:3200`), Task 6/7 (`es-1:9200` and the `grafana_reader` user), env `GRAFANA_ADMIN_USER`, `GRAFANA_ADMIN_PASSWORD`, `GRAFANA_READER_PASSWORD`.
- Produces: `grafana` (host `3000`), datasource UIDs `prometheus`, `elasticsearch`, `tempo`, dashboards `fupi-stack` ("Stack overview") and `fupi-service` ("Fupi service") in folder "Fupi".

- [ ] **Step 1: Write the provisioning files**

`deploy/observability/grafana/provisioning/datasources/datasources.yml`:

```yaml
apiVersion: 1

datasources:
  - name: Prometheus
    uid: prometheus
    type: prometheus
    access: proxy
    url: http://prometheus:9090
    isDefault: true
    editable: false

  # Logs live in Elasticsearch (ELK). Reachable only while an ELK stack is running.
  - name: Elasticsearch
    uid: elasticsearch
    type: elasticsearch
    access: proxy
    url: http://es-1:9200
    basicAuth: true
    basicAuthUser: grafana_reader
    editable: false
    jsonData:
      index: "logs-fupi-*"
      timeField: "@timestamp"
      logMessageField: message
      logLevelField: log.level
    secureJsonData:
      basicAuthPassword: $GRAFANA_READER_PASSWORD

  - name: Tempo
    uid: tempo
    type: tempo
    access: proxy
    url: http://tempo:3200
    editable: false
    jsonData:
      serviceMap:
        datasourceUid: prometheus
      # Jump from a span to the log lines that carry its trace id. "$$" is Grafana's escape for a literal "$".
      tracesToLogsV2:
        datasourceUid: elasticsearch
        customQuery: true
        query: 'trace.id:"$${__trace.traceId}"'
```

`deploy/observability/grafana/provisioning/dashboards/dashboards.yml`:

```yaml
apiVersion: 1

providers:
  - name: fupi
    folder: Fupi
    type: file
    disableDeletion: true
    allowUiUpdates: false
    options:
      path: /var/lib/grafana/dashboards
```

- [ ] **Step 2: Write the dashboards**

`deploy/observability/grafana/dashboards/stack-overview.json`:

```json
{
  "uid": "fupi-stack",
  "title": "Stack overview",
  "tags": [
    "fupi"
  ],
  "schemaVersion": 39,
  "version": 1,
  "editable": false,
  "refresh": "30s",
  "time": {
    "from": "now-1h",
    "to": "now"
  },
  "templating": {
    "list": []
  },
  "annotations": {
    "list": []
  },
  "panels": [
    {"id":1,"type":"stat","title":"Scrape targets up","gridPos":{"x":0,"y":0,"w":4,"h":4},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"refId":"A","expr":"count(up == 1) or vector(0)","legendFormat":""}],"fieldConfig":{"defaults":{},"overrides":[]}},
    {"id":2,"type":"stat","title":"Scrape targets down","gridPos":{"x":4,"y":0,"w":4,"h":4},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"refId":"A","expr":"count(up == 0) or vector(0)","legendFormat":""}],"fieldConfig":{"defaults":{"thresholds":{"mode":"absolute","steps":[{"color":"green","value":null},{"color":"red","value":1}]}},"overrides":[]}},
    {"id":3,"type":"stat","title":"Redis cluster state (1 = ok)","gridPos":{"x":8,"y":0,"w":4,"h":4},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"refId":"A","expr":"min(redis_cluster_state)","legendFormat":""}],"fieldConfig":{"defaults":{"thresholds":{"mode":"absolute","steps":[{"color":"red","value":null},{"color":"green","value":1}]}},"overrides":[]}},
    {"id":4,"type":"stat","title":"Patroni primaries","gridPos":{"x":12,"y":0,"w":4,"h":4},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"refId":"A","expr":"sum(patroni_primary)","legendFormat":""}],"fieldConfig":{"defaults":{"thresholds":{"mode":"absolute","steps":[{"color":"red","value":null},{"color":"green","value":1}]}},"overrides":[]}},
    {"id":5,"type":"stat","title":"Elasticsearch nodes","gridPos":{"x":16,"y":0,"w":4,"h":4},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"refId":"A","expr":"max(elasticsearch_cluster_health_number_of_nodes)","legendFormat":""}],"fieldConfig":{"defaults":{},"overrides":[]}},
    {"id":6,"type":"stat","title":"Kafka brokers","gridPos":{"x":20,"y":0,"w":4,"h":4},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"refId":"A","expr":"max(kafka_brokers)","legendFormat":""}],"fieldConfig":{"defaults":{},"overrides":[]}},
    {"id":7,"type":"timeseries","title":"Kafka under-replicated partitions","gridPos":{"x":0,"y":4,"w":12,"h":8},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"refId":"A","expr":"sum(kafka_topic_partition_under_replicated_partition)","legendFormat":"under-replicated"}],"fieldConfig":{"defaults":{},"overrides":[]}},
    {"id":8,"type":"timeseries","title":"Kafka messages per second by topic","gridPos":{"x":12,"y":4,"w":12,"h":8},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"refId":"A","expr":"sum by (topic) (rate(kafka_topic_partition_current_offset[5m]))","legendFormat":"{{topic}}"}],"fieldConfig":{"defaults":{"unit":"ops"},"overrides":[]}},
    {"id":9,"type":"timeseries","title":"Redis memory used","gridPos":{"x":0,"y":12,"w":12,"h":8},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"refId":"A","expr":"sum by (instance) (redis_memory_used_bytes)","legendFormat":"{{instance}}"}],"fieldConfig":{"defaults":{"unit":"bytes"},"overrides":[]}},
    {"id":10,"type":"timeseries","title":"Postgres connections","gridPos":{"x":12,"y":12,"w":12,"h":8},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"refId":"A","expr":"sum by (datname) (pg_stat_database_numbackends)","legendFormat":"{{datname}}"}],"fieldConfig":{"defaults":{},"overrides":[]}},
    {"id":11,"type":"timeseries","title":"Elasticsearch JVM heap used","gridPos":{"x":0,"y":20,"w":12,"h":8},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"refId":"A","expr":"max by (name) (elasticsearch_jvm_memory_used_bytes{area=\"heap\"})","legendFormat":"{{name}}"}],"fieldConfig":{"defaults":{"unit":"bytes"},"overrides":[]}}
  ]
}
```

`deploy/observability/grafana/dashboards/fupi-service.json` (its panels read OpenTelemetry HTTP metrics that exist only once the app is instrumented, so they show "No data" until then):

```json
{
  "uid": "fupi-service",
  "title": "Fupi service",
  "tags": [
    "fupi"
  ],
  "schemaVersion": 39,
  "version": 1,
  "editable": false,
  "refresh": "30s",
  "time": {
    "from": "now-1h",
    "to": "now"
  },
  "templating": {
    "list": []
  },
  "annotations": {
    "list": []
  },
  "panels": [
    {"id":1,"type":"stat","title":"Requests per second","gridPos":{"x":0,"y":0,"w":6,"h":4},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"refId":"A","expr":"sum(rate(http_server_request_duration_seconds_count{service_name=\"fupi\"}[5m]))","legendFormat":""}],"fieldConfig":{"defaults":{"unit":"reqps"},"overrides":[]}},
    {"id":2,"type":"stat","title":"p99 latency","gridPos":{"x":6,"y":0,"w":6,"h":4},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"refId":"A","expr":"histogram_quantile(0.99, sum by (le) (rate(http_server_request_duration_seconds_bucket{service_name=\"fupi\"}[5m])))","legendFormat":""}],"fieldConfig":{"defaults":{"unit":"s"},"overrides":[]}},
    {"id":3,"type":"stat","title":"5xx ratio","gridPos":{"x":12,"y":0,"w":6,"h":4},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"refId":"A","expr":"sum(rate(http_server_request_duration_seconds_count{service_name=\"fupi\",http_response_status_code=~\"5..\"}[5m])) / sum(rate(http_server_request_duration_seconds_count{service_name=\"fupi\"}[5m]))","legendFormat":""}],"fieldConfig":{"defaults":{"unit":"percentunit","thresholds":{"mode":"absolute","steps":[{"color":"green","value":null},{"color":"red","value":0.01}]}},"overrides":[]}},
    {"id":4,"type":"timeseries","title":"Requests per second by route","gridPos":{"x":0,"y":4,"w":12,"h":8},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"refId":"A","expr":"sum by (http_route) (rate(http_server_request_duration_seconds_count{service_name=\"fupi\"}[5m]))","legendFormat":"{{http_route}}"}],"fieldConfig":{"defaults":{"unit":"reqps"},"overrides":[]}},
    {"id":5,"type":"timeseries","title":"Latency p50 / p95 / p99","gridPos":{"x":12,"y":4,"w":12,"h":8},"datasource":{"type":"prometheus","uid":"prometheus"},"targets":[{"refId":"A","expr":"histogram_quantile(0.5, sum by (le) (rate(http_server_request_duration_seconds_bucket{service_name=\"fupi\"}[5m])))","legendFormat":"p50"},{"refId":"B","expr":"histogram_quantile(0.95, sum by (le) (rate(http_server_request_duration_seconds_bucket{service_name=\"fupi\"}[5m])))","legendFormat":"p95"},{"refId":"C","expr":"histogram_quantile(0.99, sum by (le) (rate(http_server_request_duration_seconds_bucket{service_name=\"fupi\"}[5m])))","legendFormat":"p99"}],"fieldConfig":{"defaults":{"unit":"s"},"overrides":[]}},
    {"id":6,"type":"logs","title":"Application logs (Elasticsearch)","gridPos":{"x":0,"y":12,"w":24,"h":9},"datasource":{"type":"elasticsearch","uid":"elasticsearch"},"targets":[{"refId":"A","query":"service.name:fupi","timeField":"@timestamp","metrics":[{"type":"logs","id":"1","settings":{"limit":"100"}}],"bucketAggs":[]}]},
    {"id":7,"type":"table","title":"Recent traces (Tempo)","gridPos":{"x":0,"y":21,"w":24,"h":9},"datasource":{"type":"tempo","uid":"tempo"},"targets":[{"refId":"A","queryType":"traceql","query":"{ resource.service.name = \"fupi\" }","limit":20}]}
  ]
}
```

- [ ] **Step 3: Add the `grafana` service**

Use the Edit tool on `deploy/observability/compose.yml`. `old_string`:

```text
volumes:
  tempo-data:
  prometheus-data:
  alertmanager-data:
```

`new_string`:

```text
  grafana:
    image: grafana/grafana:13.2.3
    profiles: [observability, full-ha, full-single]
    restart: unless-stopped
    environment:
      GF_SECURITY_ADMIN_USER: ${GRAFANA_ADMIN_USER:-admin}
      GF_SECURITY_ADMIN_PASSWORD: ${GRAFANA_ADMIN_PASSWORD:-fupi-grafana-dev}
      GF_USERS_ALLOW_SIGN_UP: "false"
      GF_AUTH_ANONYMOUS_ENABLED: "false"
      GRAFANA_READER_PASSWORD: ${GRAFANA_READER_PASSWORD:-fupi-grafana-reader}
    ports:
      - "127.0.0.1:3000:3000"
    volumes:
      - grafana-data:/var/lib/grafana
      - ./grafana/provisioning:/etc/grafana/provisioning:ro
      - ./grafana/dashboards:/var/lib/grafana/dashboards:ro
    healthcheck:
      test: ["CMD-SHELL", "wget -qO- http://localhost:3000/api/health | grep -q ok"]
      interval: 10s
      timeout: 5s
      retries: 20
      start_period: 20s
    networks:
      - fupi

volumes:
  tempo-data:
  prometheus-data:
  alertmanager-data:
  grafana-data:
```

- [ ] **Step 4: Static verification**

```bash
make infra.config
for f in deploy/observability/grafana/dashboards/*.json; do jq empty "$f" && echo "valid json: $f"; done
docker compose --profile observability config --services | sort      # + grafana
```

Every dashboard query must at least parse. After Step 5 starts Prometheus, run this (a syntax error in a panel makes the API return `error`, an empty result is fine):

```bash
jq -r '.panels[].targets[]?.expr // empty' deploy/observability/grafana/dashboards/*.json | while IFS= read -r q; do
  curl -s --get http://localhost:9090/api/v1/query --data-urlencode "query=$q" | jq -r .status
done | sort | uniq -c        # expect: only "success"
```

- [ ] **Step 5: Runtime — Grafana with Elasticsearch (pull gate; about 4.5 GiB together)**

```bash
make infra.elk.up MODE=single
make infra.observability.up
```

Expected: both end `ready`. Run the dashboard-query check from Step 4 now: only `success`.

Provisioning:

```bash
G() { curl -s -u admin:fupi-grafana-dev "http://localhost:3000$1"; }
G "/api/search?query=" | jq -r '.[] | select(.type=="dash-db") | "\(.uid) \(.title) [\(.folderTitle)]"' | sort
# expect: fupi-service Fupi service [Fupi] and fupi-stack Stack overview [Fupi]
for u in prometheus tempo elasticsearch; do echo -n "$u: "; G "/api/datasources/uid/$u/health" | jq -r .message; done
# expect: Successfully queried the Prometheus API. / Successfully connected to the Tempo API (or similar) / Elasticsearch data source is healthy.
G "/api/datasources/uid/tempo" | jq -r '.jsonData.tracesToLogsV2.query'     # expect: trace.id:"${__trace.traceId}"  (the $$ escape resolved to a literal $)
```

If the Elasticsearch health check fails with an index error, add `database: "logs-fupi-*"` at the datasource's top level next to `url` (older provisioning field for the same setting), restart Grafana, and re-check.

Trace-to-logs end to end: send a log line carrying a trace id (Task 6's `smoke`), then query it through Grafana's datasource proxy exactly as the trace link would:

```bash
docker run --rm --label com.docker.compose.project=fupi --label com.docker.compose.service=smoke busybox \
  sh -c "echo '{\"time\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",\"level\":\"INFO\",\"msg\":\"grafana link\",\"service\":\"smoke\",\"trace_id\":\"graf0001\"}'; sleep 20"
sleep 5
curl -s -u admin:fupi-grafana-dev -H 'Content-Type: application/x-ndjson' -X POST "http://localhost:3000/api/datasources/proxy/uid/elasticsearch/_msearch" \
  --data-binary $'{"index":"logs-fupi-*","search_type":"query_then_fetch"}\n{"size":1,"query":{"query_string":{"query":"trace.id:\\"graf0001\\""}}}\n' | jq '.responses[0].hits.total.value'     # expect: 1
```

Re-run idempotency and restart persistence: `make infra.observability.up` again is a no-op; after `make infra.observability.down && make infra.observability.up` the two dashboards are still provisioned.

- [ ] **Step 6: Tear down and commit**

```bash
make infra.observability.down && make infra.elk.down && echo y | make infra.clean
git add deploy/observability
git commit -m "feat(infra): add Grafana with provisioned datasources and dashboards" \
  -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

## Task 10: Documentation, whole-stack verification, spec refinements, memory

**Files:**
- Create: `docs/LocalInfrastructure.md`
- Modify: `README.md`, `docs/Deployment.md`, `.make/docker.mk` (append `infra.wait`), `docs/superpowers/specs/2026-10-01-local-infra-stack-design.md`

**Interfaces:**
- Consumes: every earlier task.
- Produces: the user documentation; `infra.wait STACK=<stack> MODE=<mode>`; measured memory numbers.

- [ ] **Step 1: Append `infra.wait` to `.make/docker.mk`**

```make

.PHONY: infra.wait
infra.wait: ## Wait until a stack is healthy, usage - make infra.wait STACK=kafka MODE=single
	@$(INFRA) wait $(STACK) $(MODE)
```

Verify: `make infra.wait STACK=nope` exits non-zero with `unknown stack 'nope'`; `make help | sed 's/\x1b\[[0-9;]*m//g' | grep -c '^infra\.'` now reports 29 once every task has landed (22 after Task 1; the pattern is anchored because an unrelated `buf.generate.clients` help line also contains the text "infra.").

- [ ] **Step 2: Whole-stack runtime check and memory measurement (`full-single`)**

This is the success criterion that `full-single` fits the current Docker allocation. Nothing else may be running (`make infra.ps` empty).

```bash
make infra.full.up MODE=single        # expect: ready: full-single (allow up to 5 minutes)
make infra.stats                      # record every row
docker stats --no-stream --format '{{.MemUsage}}' $(docker ps --filter label=com.docker.compose.project=fupi -q) | awk '{print $1}' | sed 's/MiB//;s/GiB/*1024/' | bc | paste -sd+ - | bc    # total MiB
```

Record the per-stack subtotals and the total. Then confirm the stack works together:

```bash
curl -s http://localhost:9090/api/v1/targets | jq -r '.data.activeTargets[] | "\(.labels.job) \(.health)"' | sort | uniq -c   # every job up: kafka redis postgres elasticsearch logstash tempo otel-collector grafana alertmanager ...
for u in prometheus tempo elasticsearch; do curl -s -u admin:fupi-grafana-dev "http://localhost:3000/api/datasources/uid/$u/health" | jq -r .status; done   # OK x3
"$SCRATCH/infracheck/infracheck" kafka localhost:19092
"$SCRATCH/infracheck/infracheck" redis localhost:7001
"$SCRATCH/infracheck/infracheck" postgres localhost:5432 false
```

If total memory exceeds roughly 7 GiB or a container is OOM-killed, record that honestly, report which profiles do not fit, and lower the heaps (`KAFKA_HEAP`, `ES_HEAP`, `LS_HEAP`) before documenting the budget.

Then stop everything: `make infra.full.down && echo y | make infra.clean`.

`full-ha` cannot run in the current Docker allocation. Verify it statically only and say so: `docker compose --profile full-ha config --services | wc -l` (41 services) and `make infra.config`.

- [ ] **Step 3: Write `docs/LocalInfrastructure.md`**

In the memory table, replace the **Estimate** values with the numbers measured in Step 2 for the profiles you ran (`core-single`, `elk-single`, `observability` are subtotals of the `full-single` run; the HA rows stay estimates and say so).

````markdown
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
make infra.clean                    # remove every container AND volume (asks for confirmation)
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
| `make infra.<stack>.up [MODE=ha\|single]` / `.down` | start / stop a stack: `kafka redis postgres elk observability legacy core full` |
| `make infra.wait STACK=kafka MODE=single` | wait until a stack is healthy |
| `make infra.ps` / `infra.stats` | status and health / live memory and CPU |
| `make infra.logs SERVICE=kafka-1` | follow logs (omit `SERVICE` for everything) |
| `make infra.config` | check that every profile renders and the env defaults agree |
| `make infra.clean` | remove all containers and volumes |
| `make infra.hosts` | print the `/etc/hosts` line needed for Redis HA from an app on your host |
| `make infra.migrate` | re-run the database migrations |
| `make infra.psql` | `psql` as the application user on the primary |
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

## The stacks

### Kafka

- HA: `kafka-1..3` in KRaft mode, each both broker and controller; replication factor 3, `min.insync.replicas=2`.
  Single: `kafka-single` (alias `kafka-1`), replication factor 1.
- Topics are created at start-up and are idempotent: `url.events`, `identity.events`, `url.access`, `url.invalidations`,
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
  shows "No data" until the app emits OpenTelemetry metrics.
- Prometheus: <http://localhost:9090>; Alertmanager: <http://localhost:9093>; Tempo: <http://localhost:3200>.
- Prometheus finds exporters by DNS, so only running components appear as targets. To get notifications, replace the
  `null` receiver in `deploy/observability/alertmanager.yml` with a Slack, email or webhook receiver and reload.
- More dashboards: import community dashboards by ID in Grafana (Dashboards, New, Import): Kafka 7589, Redis 763,
  Postgres 9628, Elasticsearch 14191, Go processes 6671.

### Legacy

`make infra.legacy.up` starts MongoDB 4.4 (end-of-life) and the old standalone Redis for the code behind the `legacy`
build tag. They are unchanged apart from binding to `127.0.0.1`.

## Credentials and ports

All credentials are development defaults from `.env.example`. Override any of them in `.env`.

| Service | Address | Login |
|---|---|---|
| Kafka (host) | `localhost:19092` (HA also `29092`, `39092`) | none |
| Kafka UI | <http://localhost:8080> | none |
| Redis | `localhost:7001`..`7006` | user `fupi-svc`, password `fupi-svc`; admin (default user) `fupi-redis-admin` |
| Postgres | `localhost:5432` write, `5433` read | `fupi-user` / `fupi-pass`, database `fupidb`; superuser `postgres` / `fupi-postgres-admin` |
| Elasticsearch | `localhost:9200` | `elastic` / `fupi-elastic-dev` |
| Kibana | <http://localhost:5601> | `elastic` / `fupi-elastic-dev` |
| Grafana | <http://localhost:3000> | `admin` / `fupi-grafana-dev` |
| Prometheus, Alertmanager, Tempo | `9090`, `9093`, `3200` | none |
| OTLP | `4317` gRPC, `4318` HTTP | none |
| Legacy MongoDB / Redis | `27017` / `6379` | `fupiUser` / `fupiPassword` / none |

Every published port is bound to `127.0.0.1`. Do not reuse these values anywhere else; passwords must not contain quotes,
backslashes or `$` (`make infra.config` checks this).

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
````

- [ ] **Step 4: Update `README.md` and `docs/Deployment.md`**

In `README.md`, use the Edit tool twice (if a multi-line `old_string` does not match, re-read the lines and match them exactly).

First edit, the Docker paragraph. `old_string`:

```text
The application is packaged & run in a Docker container, although it can be run without Docker, it uses services that can be run in Docker as specified in the [docker-compose.yml file](./docker-compose.yml). If you want to run supporting services such as [MongoDB](https://www.mongodb.com/) & [Redis](https://redis.io/) in docker containers, then you will require docker setup. If not, you can install these services locally on your development machine.
```

`new_string`:

```text
The application is packaged & run in a Docker container. The services it depends on (Postgres, Redis, Kafka, ELK, Prometheus and Grafana) run locally in Docker too, either as high-availability clusters or as single nodes, as described in [Local infrastructure](./docs/LocalInfrastructure.md). If you prefer, you can install these services directly on your development machine instead.
```

Second edit, the compose instructions. `old_string`:

````text
Next step is to run the services the application needs to communicate with; The database & the cache.

If you have installed these locally, you can run them in separate terminal sessions. If not, you can use Docker to do so(preferred option).

```bash
docker compose up
# You can optionally attach -d flag to the command like below
docker compose up -d
```

> This will run the services in docker containers, pulling the images and building the containers for use. Using the `-d` flag runs the services in the background.
````

`new_string`:

````text
Next step is to run the services the application needs to communicate with: Postgres, Redis and Kafka. If you have installed these locally, you can run them in separate terminal sessions. If not, use Docker (preferred); the lightest option is single-node mode:

```bash
make infra.core.up MODE=single
# or the high-availability topology
make infra.core.up
```

> Nothing starts without a profile, so a bare `docker compose up` does nothing. See [Local infrastructure](./docs/LocalInfrastructure.md) for every stack, mode and debugging tip. Stop the services with `make infra.core.down`.
````

For `docs/Deployment.md`, append:

```bash
printf '\n## Local infrastructure\n\nThe supporting services (Postgres, Redis, Kafka, ELK, Prometheus, Grafana) run locally in Docker in either HA or single-node mode. See [Local infrastructure](./LocalInfrastructure.md).\n' >> docs/Deployment.md
```

- [ ] **Step 5: Record the implementation refinements in the spec**

Use the Edit tool on `docs/superpowers/specs/2026-10-01-local-infra-stack-design.md`.

Edit 1, Redis. `old_string`:

```text
- Cluster state lives in a per-node volume (`nodes.conf`); init jobs are idempotent (skip when
  `cluster_state:ok`).
```

`new_string`:

```text
- Cluster state lives in a per-node volume (`nodes.conf`); init jobs are idempotent (skip when
  `cluster_state:ok`).
- Nodes have **fixed IP addresses** (`${FUPI_NET_PREFIX}.11`–`.16`, default `172.29.0`) because the cluster bus persists
  peer IPs in `nodes.conf`; after a restart with changed IPs the cluster could not re-form. The `fupi` network
  therefore has an explicit subnet, with other containers drawn from its upper half (`ip_range`).
```

Edit 2, Tempo. `old_string`: `- **Tempo** (single binary, local storage, 72h retention).` → `new_string`: `- **Tempo** (single binary, local storage, 72h retention, set under `backend_worker.compaction.block_retention`, where Tempo 3.x keeps it).`

Edit 3, layout. `old_string`:

```text
.make/docker.mk               # + infra.* targets
docs/LocalInfrastructure.md
```

`new_string`:

```text
.make/docker.mk               # + infra.* targets
scripts/infra.sh              # profile resolution, up/down/wait, DRY_RUN
scripts/infra_test.sh         # tests for infra.sh and the env check (no Docker)
scripts/infra_env_check.sh    # compose defaults == .env.example; no unsafe characters
docs/LocalInfrastructure.md
```

Then append a section:

```bash
cat >> docs/superpowers/specs/2026-10-01-local-infra-stack-design.md <<'EOF'

## 13. Implementation notes

- `make infra.<stack>.up MODE=...` removes the other mode's containers first (`docker compose rm -sf`, volumes kept) and
  waits for readiness: a service is ready when running and healthy (or without a healthcheck), or when a one-shot job
  exited 0. Shared services (Kafka UI, exporters, `migrate`) belong to both modes' profiles, so switching mode recreates
  them.
- Every included compose file redeclares the `fupi` network as `name: fupi` with no `external:` flag; the root file owns
  the driver and subnet. An `external: true` declaration in an included file merges into the root's definition, makes
  Compose treat the network as pre-existing, and drops the subnet the Redis static IPs need (found while checking the
  rendered model).
- Prometheus targets use DNS service discovery; etcd is scraped on its metrics port `2381`, Patroni on its REST port `8008`.
- Alert rules have unit tests (`prometheus/tests/stack_test.yml`, run with `promtool test rules`).
- `scripts/infra_env_check.sh` enforces that every compose fallback equals `.env.example`, which is what keeps a stale
  `.env` working.
EOF
```

- [ ] **Step 6: Final verification**

```bash
scripts/infra_test.sh                  # all tests passed
make infra.config                      # ok for 14 profiles + all
docker run --rm --entrypoint promtool -v "$PWD/deploy/observability/prometheus:/p:ro" prom/prometheus:v3.15.0 test rules /p/tests/stack_test.yml   # SUCCESS
command -v shellcheck && shellcheck scripts/*.sh deploy/*/*.sh || docker run --rm -v "$PWD:/mnt" koalaman/shellcheck:stable /mnt/scripts/*.sh /mnt/deploy/kafka/create-topics.sh /mnt/deploy/redis/start.sh /mnt/deploy/redis/init-cluster.sh /mnt/deploy/postgres/roles.sh /mnt/deploy/postgres/entrypoint.sh /mnt/deploy/elk/setup.sh
git status --short                     # clean apart from intended files
git diff --stat main...HEAD | tail -1  # no files under app/ changed
git diff --name-only main...HEAD | grep -c '^app/' || true     # expect: 0
```

Fix any shellcheck finding in the script that owns it and re-run that task's static step.

- [ ] **Step 7: Update the project-state memory**

Update `/Users/lusina/.claude/projects/-Users-lusina-Projects-SanctumLabs-fupi/memory/fupi-current-state.md`: add a short dated section recording that branch `feat/local-infra-stack` (from `feat/domain-url`) adds the local infrastructure stack (profiles, `infra.*` Make targets, `docs/LocalInfrastructure.md`), that slices 2 (Dockerfile), 3 (app connectivity), 4 (OpenTelemetry) and 5 (outbox relay) remain, and the measured memory numbers. Keep it factual and short.

- [ ] **Step 8: Commit**

```bash
git add docs README.md .make/docker.mk
git commit -m "docs(infra): document the local infrastructure stack and add infra.wait" \
  -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

Report to the user: what was verified at runtime (list each stack and mode), what was verified statically only (`full-ha`, any HA stack that could not run in the Docker allocation), measured memory, and every deviation from the spec found during implementation.

---

## Post-review changes (after the final whole-branch review)

The code blocks above are the plan as written; where the repository differs, the repository wins. The final review led
to these changes, each made test-first:

- `scripts/infra_env_check.sh`: allowlist `A-Za-z0-9._-` instead of a forbidden-character list; checks `.env` (override
  with `ENV_FILE`) and the Redis user/password; `scripts/infra.sh up` runs it first and refuses to start on a bad value.
- `deploy/elk/setup.sh` and `deploy/elk/compose.yml`: new mode `ELK_MODE=certs` run by `elk-setup-certs-ha`; `elk-setup-ha`
  provisions only and waits for `es-1`; the Elasticsearch nodes depend on `elk-setup-certs-ha` with
  `service_completed_successfully`; the provisioning wait has a deadline (`ES_WAIT_TIMEOUT`, default 240s).
- `.make/docker.mk`: every Docker-touching `infra.*` target depends on `create.envfile`; `infra.clean` skips the `legacy`
  profile and drops `--remove-orphans`; new `infra.clean.legacy`.
- `docs/LocalInfrastructure.md`: `.env` rules, first-start-only passwords, the clean targets, and the verified platform.
- Tests added to `scripts/infra_test.sh` for each of the above.
