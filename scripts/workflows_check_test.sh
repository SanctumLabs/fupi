#!/usr/bin/env bash
# Tests for the action-version rule of scripts/workflows_check.sh: an action must be referenced by an exact version (v1.2.3,
# 14.0.6, 1.5) or a full commit SHA, never by a branch, "latest", a floating major version (v4) or nothing at all.
# Needs no Docker and no network: the script runs on a throwaway copy of the repository layout.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 1

failures=0
pass() { echo "ok:   $1"; }
fail() { echo "FAIL: $1"; failures=$((failures + 1)); }

sandbox="$(mktemp -d)"
trap 'rm -rf "$sandbox"' EXIT
mkdir -p "$sandbox/scripts" "$sandbox/.github/workflows"
cp scripts/workflows_check.sh "$sandbox/scripts/"
cat >"$sandbox/.github/dependabot.yml" <<'YAML'
updates:
  - package-ecosystem: gomod
  - package-ecosystem: docker
  - package-ecosystem: github-actions
YAML

check_ref() { # name expected-exit uses-value
  local name="$1" want="$2" uses="$3"
  cat >"$sandbox/.github/workflows/w.yml" <<YAML
name: t
on: push
permissions:
  contents: read
jobs:
  j:
    runs-on: ubuntu-latest
    steps:
      - uses: $uses
YAML
  local out code
  out="$(bash "$sandbox/scripts/workflows_check.sh" 2>&1)"
  code=$?
  if [ "$code" -eq "$want" ]; then pass "$name"; else fail "$name (exit $code, wanted $want: $out)"; fi
}

check_ref "an exact v-prefixed version is accepted" 0 "actions/checkout@v7.0.1"
check_ref "an exact version without a v is accepted" 0 "danger/danger-js@14.0.6"
check_ref "a two-part version is accepted" 0 "superfly/flyctl-actions/setup-flyctl@1.5"
check_ref "a pre-release version is accepted" 0 "actions/checkout@v7.1.0-rc.1"
check_ref "a full commit SHA is accepted" 0 "actions/checkout@0123456789abcdef0123456789abcdef01234567 # v7.0.1"
check_ref "a local action is exempt" 0 "./.github/actions/setup"
check_ref "a branch is rejected" 1 "actions/checkout@main"
check_ref "a floating major version is rejected" 1 "actions/checkout@v7"
check_ref "latest is rejected" 1 "actions/checkout@latest"
check_ref "a short SHA is rejected" 1 "actions/checkout@0123456"
check_ref "no ref at all is rejected" 1 "actions/checkout"

[ "$failures" -eq 0 ] && echo "all tests passed"
exit "$failures"
