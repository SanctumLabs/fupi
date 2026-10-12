#!/usr/bin/env bash
# Guards the CI rules this repository relies on (docs/superpowers/specs/2026-10-04-dockerfile-hardening-design.md):
#  1. every action is pinned to an exact version (v1.2.3, 14.0.6, 1.5) or a full commit SHA, never a branch, "latest" or a floating
#     major such as v4 (local actions and reusable workflows from SanctumLabs/ci-workflows are exempt);
#  2. every workflow declares top-level permissions;
#  3. in a workflow_run workflow every checkout names the triggering commit or branch (the default is the default branch);
#  4. Dependabot keeps the gomod, docker and github-actions pins current;
#  5. a workflow_run workflow checks workflow_run.conclusion, so it does not run when the workflow it follows failed;
#  6. only slack.yml uses workflow_run: a workflow triggered by another workflow_run workflow runs against the default branch's
#     head, not the tested commit, so the build, publish and release jobs are chained with `needs:` in ci.yml instead.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 1
shopt -s nullglob
status=0
fail() { echo "$1"; status=1; }

for f in .github/workflows/*.yml; do
  grep -qE '^permissions:' "$f" || fail "$f: no top-level permissions block"

  while IFS= read -r ref; do
    case "$ref" in ./* | SanctumLabs/ci-workflows/*) continue ;; esac
    grep -Eq '@([0-9a-f]{40}|v?[0-9]+\.[0-9]+(\.[0-9]+)?([-+][0-9A-Za-z.]+)?)$' <<<"$ref" ||
      fail "$f: $ref is not pinned to an exact version or a full commit SHA"
  done < <(sed -nE 's/^[[:space:]]*(-[[:space:]]+)?uses:[[:space:]]*([^[:space:]#]+).*/\2/p' "$f")

  if grep -qE '^[[:space:]]*workflow_run:' "$f"; then
    [ "$f" = .github/workflows/slack.yml ] || fail "$f: only slack.yml may use workflow_run, chain the jobs with needs: in ci.yml"
    grep -qF 'workflow_run.conclusion' "$f" ||
      fail "$f: a workflow_run workflow does not check workflow_run.conclusion, so it runs even when the workflow it follows failed"
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
