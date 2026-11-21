#!/usr/bin/env bash
# Guards the "works with an empty or stale .env" promise:
#  1. every ${VAR:-default} in the compose files must use exactly the default listed in .env.example;
#  2. the infrastructure values (the whole "Local infrastructure" section of .env.example plus the Redis application
#     user and password) must only contain letters, digits, '.', '_' and '-', in .env.example AND in the developer's .env.
#     These values are interpolated into shell scripts, SQL, JSON bodies and connection URLs, where anything else
#     (quotes, $, #, @, /, :, %, ?, spaces) silently changes the meaning or breaks the bootstrap.
# Env: ENV_EXAMPLE (default .env.example), ENV_FILE (default .env, skipped when missing),
#      COMPOSE_FILES (default: root and deploy/*/compose.yml).
set -euo pipefail

cd "$(dirname "$0")/.."
ENV_EXAMPLE="${ENV_EXAMPLE:-.env.example}"
ENV_FILE="${ENV_FILE:-.env}"
shopt -s nullglob
default_files=(docker-compose.yml deploy/*/compose.yml)
COMPOSE_FILES="${COMPOSE_FILES:-${default_files[*]}}"
status=0

keys="$(awk '/^# --- Local infrastructure/ { on = 1; next } on && /^[A-Z0-9_]+=/ { print substr($0, 1, index($0, "=") - 1) }' "$ENV_EXAMPLE") REDIS_USERNAME REDIS_PASSWORD"

check_values() { # file
  [ -r "$1" ] || return 0
  local key value
  for key in $keys; do
    value="$(grep -E "^${key}=" "$1" | tail -1 | cut -d= -f2- || true)"
    if ! printf '%s\n' "$value" | grep -Eq '^[A-Za-z0-9._-]*$'; then
      echo "$1: $key may only contain letters, digits, '.', '_' and '-'"
      status=1
    fi
  done
}
check_values "$ENV_EXAMPLE"
check_values "$ENV_FILE"

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
