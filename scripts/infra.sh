#!/usr/bin/env bash
# Starts, stops or waits on a local infrastructure stack in a given mode (see docs/LocalInfrastructure.md).
#
# Usage: scripts/infra.sh <up|down|wait> <stack> [ha|single]
#   stacks: kafka redis postgres elk core full app worker (the mode applies) | observability legacy (no mode)
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
  kafka | redis | postgres | elk | core | full | app | worker)
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

# The values in .env end up in shell scripts, SQL, JSON and connection URLs: refuse to start when one would break them.
if [ "$action" = up ]; then
  "$(dirname "$0")/infra_env_check.sh" >&2 || { echo "fix the values above in .env (see docs/LocalInfrastructure.md), then run again" >&2; exit 1; }
fi

case "$action" in
  up)
    if [ "$stack" = app ]; then
      # The API needs Postgres (with its migrations) and, optionally, Redis. Compose cannot order services across
      # profiles, so this script brings them up and waits for them first.
      "$(dirname "$0")/infra.sh" up postgres "$mode"
      "$(dirname "$0")/infra.sh" up redis "$mode"
    fi
    if [ "$stack" = worker ]; then
      # The worker reads Postgres (with its migrations) and writes to Kafka (with its topics).
      "$(dirname "$0")/infra.sh" up postgres "$mode"
      "$(dirname "$0")/infra.sh" up kafka "$mode"
    fi
    [ -z "$other_profile" ] || run "${compose[@]}" --profile "$other_profile" rm -sf
    if [ "$stack" = app ] || [ "$stack" = worker ]; then up_args=(up -d --build); else up_args=(up -d); fi
    run "${compose[@]}" --profile "$profile" "${up_args[@]}"
    wait_ready "$profile"
    ;;
  down) run "${compose[@]}" "${both[@]}" rm -sf ;;
  wait) wait_ready "$profile" ;;
  *) usage ;;
esac
