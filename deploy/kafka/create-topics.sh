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
  "url.accessed:6:172800000"
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
