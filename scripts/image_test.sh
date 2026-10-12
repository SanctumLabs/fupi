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

  for want in /ctx/go.mod /ctx/go.sum /ctx/app/cmd/main.go /ctx/app/cmd/migrator/main.go /ctx/app/cmd/worker/main.go \
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
    grep -q 'REDIS_PASSWORD' <<<"$out" && grep -q 'SHORT_CODE_KEY' <<<"$out"; then
    pass "refuses to start without secrets (ENVIRONMENT=production)"
  else
    fail "refuses to start without secrets (exit $code: $out)"
  fi
  if grep -qE 'fupi-secret|fupi-pass|fupi-svc|fupi-short-code-key' <<<"$out"; then fail "the refusal must not print a secret value"; else pass "the refusal prints no secret value"; fi

  out="$(docker run --rm --entrypoint /app/migrator "$image" 2>&1)"
  code=$?
  if [ "$code" -eq 1 ] && grep -q 'DATABASE_PASSWORD' <<<"$out"; then
    pass "the migrator is present and refuses the development password in production"
  else
    fail "the migrator is present and refuses the development password in production (exit $code: $out)"
  fi

  out="$(docker run --rm --entrypoint /app/worker "$image" 2>&1)"
  code=$?
  if [ "$code" -eq 1 ] && grep -q 'DATABASE_PASSWORD' <<<"$out"; then
    pass "the worker is present and refuses the development password in production"
  else
    fail "the worker is present and refuses the development password in production (exit $code: $out)"
  fi

  out="$(docker run --rm --read-only --cap-drop ALL --security-opt no-new-privileges:true --entrypoint /app/worker "$image" healthcheck 2>&1)"
  code=$?
  if [ "$code" -eq 1 ] && grep -q 'healthcheck:' <<<"$out"; then
    pass "the worker's healthcheck runs read-only without capabilities and fails when nothing listens"
  else
    fail "the worker's healthcheck runs read-only without capabilities and fails when nothing listens (exit $code: $out)"
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
