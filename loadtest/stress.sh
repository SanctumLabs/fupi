#!/usr/bin/env bash
# Runs a scenario at a rising arrival rate and stops at the first step that crosses a threshold. Each step is run
# REPEATS times (a fresh k6 run each, same seed, so the cache stays warm) and reported as the median, because on a
# laptop a single run's tail varies a lot. A step fails when more than half of its runs cross a threshold. The k6
# container is capped at K6_CPUS so the generator does not compete with the stack for the Docker VM's cores.
#
#   SCENARIO=redirect START=200 STEP=200 MAX=5000 STEP_DURATION=45s REPEATS=3 K6_CPUS=2 ./loadtest/stress.sh
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
scenario="${SCENARIO:-redirect}"
start="${START:-100}"
step="${STEP:-100}"
max="${MAX:-5000}"
duration="${STEP_DURATION:-45s}"
repeats="${REPEATS:-3}"
export K6_CPUS="${K6_CPUS-2}"
results="$root/loadtest/results/$(date +%Y%m%d-%H%M%S)-$scenario"
mkdir -p "$results"

printf '%-7s %-9s %-8s %-8s %-8s %-8s %-8s %s\n' rate achieved p50_ms p95_ms p99_ms fail_pct dropped failed_runs
rate="$start"
while [ "$rate" -le "$max" ]; do
  # pre-allocate enough VUs for ~100ms responses at this rate and let k6 grow to 4x that
  pre=$(( rate / 10 + 10 ))
  failed=0
  for run in $(seq 1 "$repeats"); do
    summary="/loadtest/results/$(basename "$results")/step-$rate-run$run.json"
    make -s -C "$root" loadtest.run SCENARIO="$scenario" \
      K6_ARGS="-e RATE=$rate -e DURATION=$duration -e PRE_VUS=$pre -e MAX_VUS=$(( pre * 4 )) ${P95_MS:+-e P95_MS=$P95_MS} ${DROP_TOLERANCE:+-e DROP_TOLERANCE=$DROP_TOLERANCE}" \
      K6_FLAGS="--quiet --summary-export $summary --summary-trend-stats='med,p(95),p(99)'" >/dev/null 2>"$results/step-$rate-run$run.err" || failed=$(( failed + 1 ))
  done

  python3 - "$results" "$rate" "$duration" "$repeats" "$failed" <<'PY'
import glob, json, statistics, sys
results, rate, duration, repeats, failed = sys.argv[1:]
secs = float(duration[:-1]) * (60 if duration.endswith("m") else 1)
rows = []
for path in sorted(glob.glob(f"{results}/step-{rate}-run*.json")):
    try:
        m = json.load(open(path))["metrics"]
    except Exception:
        continue
    d = m["http_req_duration"]
    rows.append((m["http_reqs"]["count"] / secs, d["med"], d["p(95)"], d["p(99)"], m["http_req_failed"]["value"] * 100, m.get("dropped_iterations", {}).get("count", 0)))
if not rows:
    print(f"{rate:<7} no summaries: see {results}/step-{rate}-run*.err"); sys.exit(0)
med = [statistics.median(col) for col in zip(*rows)]
print(f"{rate:<7} {med[0]:<9.1f} {med[1]:<8.1f} {med[2]:<8.1f} {med[3]:<8.1f} {med[4]:<8.2f} {med[5]:<8.0f} {failed}/{repeats}")
PY
  if [ $(( failed * 2 )) -gt "$repeats" ]; then
    echo "stopped: thresholds failed in $failed of $repeats runs at $rate req/s (results in $results)"
    exit 0
  fi
  rate=$(( rate + step ))
done
echo "reached MAX=$max req/s without a failing step (results in $results)"
