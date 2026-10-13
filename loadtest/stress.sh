#!/usr/bin/env bash
# Runs a scenario at a rising arrival rate, one k6 run per step, and stops at the first step that crosses a threshold.
# Each step is a fresh k6 run against the same seed, so the cache stays warm between steps. Prints one row per step.
#
#   SCENARIO=redirect START=100 STEP=100 MAX=5000 STEP_DURATION=45s ./loadtest/stress.sh
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
scenario="${SCENARIO:-redirect}"
start="${START:-100}"
step="${STEP:-100}"
max="${MAX:-5000}"
duration="${STEP_DURATION:-45s}"
results="$root/loadtest/results/$(date +%Y%m%d-%H%M%S)-$scenario"
mkdir -p "$results"

printf '%-8s %-10s %-9s %-9s %-9s %-9s %-8s %s\n' rate achieved p50_ms p95_ms p99_ms fail_pct dropped result
rate="$start"
while [ "$rate" -le "$max" ]; do
  summary="/loadtest/results/$(basename "$results")/step-$rate.json"
  # pre-allocate enough VUs for ~100ms responses at this rate and let k6 grow to 4x that
  pre=$(( rate / 10 + 10 ))
  status=ok
  make -s -C "$root" loadtest.run SCENARIO="$scenario" \
    K6_ARGS="-e RATE=$rate -e DURATION=$duration -e PRE_VUS=$pre -e MAX_VUS=$(( pre * 4 )) ${P95_MS:+-e P95_MS=$P95_MS}" \
    K6_FLAGS="--quiet --summary-export $summary --summary-trend-stats='med,p(95),p(99)'" >/dev/null 2>"$results/step-$rate.err" || status=THRESHOLD_CROSSED

  python3 - "$results/step-$rate.json" "$rate" "$duration" "$status" <<'PY'
import json, sys
path, rate, duration, status = sys.argv[1:]
try:
    m = json.load(open(path))["metrics"]
except Exception:
    print(f"{rate:<8} no summary: see {path.replace('.json', '.err')}"); sys.exit(0)
secs = float(duration.rstrip("s")) if duration.endswith("s") else float(duration.rstrip("m")) * 60
d = m["http_req_duration"]
print(f"{rate:<8} {m['http_reqs']['count']/secs:<10.1f} {d['med']:<9.1f} {d['p(95)']:<9.1f} {d['p(99)']:<9.1f} {m['http_req_failed']['value']*100:<9.2f} {m.get('dropped_iterations',{}).get('count',0):<8} {status}")
PY
  [ "$status" = ok ] || { echo "stopped: a threshold failed at $rate req/s (results in $results)"; exit 0; }
  rate=$(( rate + step ))
done
echo "reached MAX=$max req/s without crossing a threshold (results in $results)"
