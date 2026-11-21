#!/bin/sh
# Forms the Redis Cluster once and does nothing on later runs (the state lives in each node's volume).
#   CLUSTER_MODE=ha      three masters and three replicas over redis-1..redis-6 (ports 7001..7006)
#   CLUSTER_MODE=single  one node (redis-1:7001) owning all 16384 slots
# Env: CLUSTER_MODE, REDIS_ADMIN_PASSWORD, NET_PREFIX (HA only: first three octets of the nodes' fixed addresses).
set -eu

MODE="${CLUSTER_MODE:?}"
export REDISCLI_AUTH="$REDIS_ADMIN_PASSWORD"

cluster_state() { redis-cli -h "$1" -p "$2" cluster info 2>/dev/null | tr -d '\r' | sed -n 's/^cluster_state://p'; }

# POSIX sh has no local variables, so the helpers keep their counter in "tries" and leave the caller's loop variable alone.
wait_for_ping() {
  tries=0
  until redis-cli -h "$1" -p "$2" ping 2>/dev/null | grep -q PONG; do
    tries=$((tries + 1))
    [ "$tries" -le 60 ] || { echo "timed out waiting for $1:$2" >&2; exit 1; }
    sleep 1
  done
}

wait_for_ok() {
  tries=0
  until [ "$(cluster_state "$1" "$2")" = ok ]; do
    tries=$((tries + 1))
    [ "$tries" -le 60 ] || { echo "cluster did not reach state ok on $1:$2" >&2; exit 1; }
    sleep 1
  done
}

if [ "$MODE" = single ]; then
  wait_for_ping redis-1 7001
  if [ "$(cluster_state redis-1 7001)" = ok ]; then echo "cluster already formed"; exit 0; fi
  redis-cli -h redis-1 -p 7001 cluster addslotsrange 0 16383
  wait_for_ok redis-1 7001
  echo "single-node cluster ready"
  exit 0
fi

nodes=""
for i in 1 2 3 4 5 6; do
  wait_for_ping "redis-$i" "700$i"
  nodes="$nodes $NET_PREFIX.1$i:700$i"    # the nodes have fixed addresses .11 to .16
done

if [ "$(cluster_state redis-1 7001)" = ok ]; then echo "cluster already formed"; exit 0; fi

# shellcheck disable=SC2086  # $nodes is a space-separated list on purpose
redis-cli --cluster create $nodes --cluster-replicas 1 --cluster-yes
wait_for_ok redis-1 7001
echo "cluster ready"
