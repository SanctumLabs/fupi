#!/usr/bin/env bash
# Guards parity between the GitHub workflows and the mirror pipelines, .gitlab-ci.yml and bitbucket-pipelines.yml
# (docs/superpowers/specs/2026-10-04-dockerfile-hardening-design.md, section 8b). It checks, without running anything:
#  1. both mirror files parse as YAML;
#  2. every Go image has the same minor version as go.mod, and the golangci-lint image is the one `make lint` pins;
#  3. every command GitHub runs for the tests and the build also appears in each mirror;
#  4. the image checks (hadolint, build, scripts/image_test.sh, Trivy with the same flags) appear in each mirror;
#  5. Docker-based jobs disable Ryuk, no image is :latest, and every make target a mirror calls exists;
#  6. Bitbucket turns BuildKit on (its docker service uses the classic builder otherwise, and the Dockerfile uses cache mounts).
set -uo pipefail
cd "$(dirname "$0")/.." || exit 1
status=0
fail() { echo "$1"; status=1; }

pipelines=(.gitlab-ci.yml bitbucket-pipelines.yml)

go_minor="$(sed -n 's/^go \([0-9][0-9]*\.[0-9][0-9]*\).*/\1/p' go.mod)"
lint_image="$(grep -oE 'golangci/golangci-lint:v[0-9.]+' .make/dev.mk | head -1)"
[ -n "$go_minor" ] || { echo "cannot read the Go version from go.mod"; exit 1; }
[ -n "$lint_image" ] || { echo "cannot read the golangci-lint image from .make/dev.mk"; exit 1; }

# What GitHub runs: the test commands and the build (ci.yml).
github_commands="$(sed -nE 's/^[[:space:]]*command:[[:space:]]*(make [A-Za-z0-9._-]+).*/\1/p' .github/workflows/ci.yml | sort -u)
$(sed -nE 's/^[[:space:]]*run:[[:space:]]*(make build)[[:space:]]*$/\1/p' .github/workflows/ci.yml | sort -u)"

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
  if [ "$f" = bitbucket-pipelines.yml ]; then
    grep -qF 'DOCKER_BUILDKIT=1' "$f" || fail "$f: does not set DOCKER_BUILDKIT=1, so docker build uses the classic builder and rejects RUN --mount"
  fi

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
