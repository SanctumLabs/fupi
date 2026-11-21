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

expect_output "up worker brings up postgres and kafka first, then builds and starts the worker" \
"+ docker compose --profile postgres-ha rm -sf
+ docker compose --profile postgres-single up -d
+ wait postgres-single
+ docker compose --profile kafka-ha rm -sf
+ docker compose --profile kafka-single up -d
+ wait kafka-single
+ docker compose --profile worker-ha rm -sf
+ docker compose --profile worker-single up -d --build
+ wait worker-single" \
  scripts/infra.sh up worker single

expect_output "down worker stops only the worker" \
"+ docker compose --profile worker-ha --profile worker-single rm -sf" \
  scripts/infra.sh down worker

expect_output "wait resolves the worker profile" \
"+ wait worker-ha" \
  scripts/infra.sh wait worker

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

# --- the developer's own .env is checked too, against an allowlist ----------------------------------------------------
cat >"$tmp/example2.env" <<'EOF'
REDIS_USERNAME=fupi-svc
REDIS_PASSWORD=fupi-svc
LOG_FORMAT="json"

# --- Local infrastructure
PG_APP_PASSWORD=safe
EOF
env_check_with() { # contents of the developer's .env
  printf '%s\n' "$1" >"$tmp/dev.env"
  ENV_EXAMPLE="$tmp/example2.env" ENV_FILE="$tmp/dev.env" COMPOSE_FILES=/dev/null scripts/infra_env_check.sh
}
for bad in 'pa$word' 'p#ss' 'x@y' 'p ss' 'a/b' 'a:b' 'a%41b' 'a?b' 'a"b'; do
  expect_exit "env check rejects '$bad' in .env" 1 env_check_with "PG_APP_PASSWORD=$bad"
done
expect_exit "env check accepts a safe .env value" 0 env_check_with "PG_APP_PASSWORD=Pa55.w_rd-1"
expect_exit "env check ignores keys outside the infrastructure section (quotes in LOG_FORMAT are fine)" 0 env_check_with 'LOG_FORMAT="json"'
expect_exit "env check covers the Redis application password" 1 env_check_with "REDIS_PASSWORD=a\$b"
expect_exit "env check accepts a missing .env" 0 \
  env ENV_EXAMPLE="$tmp/example2.env" ENV_FILE="$tmp/does-not-exist" COMPOSE_FILES=/dev/null scripts/infra_env_check.sh
printf '# --- Local infrastructure\nPG_APP_PASSWORD=a@b\n' >"$tmp/unsafe-at.env"
expect_exit "env check also applies the allowlist to .env.example" 1 \
  env ENV_EXAMPLE="$tmp/unsafe-at.env" ENV_FILE=/dev/null COMPOSE_FILES=/dev/null scripts/infra_env_check.sh
printf 'PG_APP_PASSWORD=pa$word\n' >"$tmp/bad.env"
expect_exit "infra.sh up refuses to start with an unsafe .env" 1 env ENV_FILE="$tmp/bad.env" scripts/infra.sh up kafka single

# --- deploy/elk/setup.sh gives up instead of waiting forever for Elasticsearch ------------------------------------
cat >"$tmp/bin/curl" <<'STUB'
#!/usr/bin/env bash
echo 000
STUB
chmod +x "$tmp/bin/curl"
setup_without_es() { # the alarm turns a hang into a failure instead of blocking the test run
  perl -e 'alarm 15; exec @ARGV' env PATH="$tmp/bin:$PATH" ELK_MODE=single ES_URL=http://es-1:9200 ELASTIC_PASSWORD=x ES_WAIT_TIMEOUT=1 \
    bash deploy/elk/setup.sh
}
expect_exit "elk setup: exits 1 when Elasticsearch never becomes ready" 1 setup_without_es
out="$(setup_without_es 2>&1)"
case "$out" in *"did not become ready"*) pass "elk setup: says why it gave up" ;; *) fail "elk setup: says why it gave up ($out)" ;; esac

# --- Make targets (make -n prints the plan without running it) -------------------------------------------------------
infra_targets="$(make help 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g' | awk '{print $1}' | grep '^infra\.')"
missing_env=""
for t in $infra_targets; do
  plan="$(make -n "$t" STACK=kafka 2>/dev/null)"
  case "$plan" in
    *"docker compose"* | *"scripts/infra.sh"* | *"docker stats"*)
      case "$plan" in *"cp .env.example .env"*) ;; *) missing_env="$missing_env $t" ;; esac
      ;;
  esac
done
if [ -z "$missing_env" ]; then pass "every infra target that talks to Docker creates .env first"; else fail "targets without the .env prerequisite:$missing_env"; fi

clean_plan="$(make -n infra.clean 2>/dev/null)"
case "$clean_plan" in
  *"--profile legacy"* | *"--profile '*'"* | *"remove-orphans"*) fail "infra.clean must leave the legacy MongoDB and Redis data alone" ;;
  *) pass "infra.clean leaves the legacy MongoDB and Redis data alone" ;;
esac
case "$clean_plan" in *"down -v"*) pass "infra.clean still removes the volumes of the other stacks" ;; *) fail "infra.clean still removes the volumes of the other stacks" ;; esac
legacy_plan="$(make -n infra.clean.legacy 2>/dev/null)"
case "$legacy_plan" in
  *"--profile legacy"*"down -v"*) pass "infra.clean.legacy removes the legacy containers and volumes" ;;
  *) fail "infra.clean.legacy removes the legacy containers and volumes" ;;
esac

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

if [ "$failures" -ne 0 ]; then
  echo "$failures failure(s)"
  exit 1
fi
echo "all tests passed"
