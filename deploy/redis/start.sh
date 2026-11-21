#!/bin/sh
# Starts one Redis Cluster node. Passwords and the application ACL user come from the environment so the committed
# redis.conf holds no secrets. Env: NODE_PORT, NODE_HOST, REDIS_ADMIN_PASSWORD, REDIS_USERNAME, REDIS_PASSWORD.
set -eu

# The application user may do everything except the dangerous commands (FLUSHALL, KEYS, CONFIG ...), but a cluster
# client must be able to read the topology.
exec redis-server /usr/local/etc/redis/redis.conf \
  --port "$NODE_PORT" \
  --cluster-announce-hostname "$NODE_HOST" \
  --requirepass "$REDIS_ADMIN_PASSWORD" \
  --masterauth "$REDIS_ADMIN_PASSWORD" \
  --user "$REDIS_USERNAME" on ">$REDIS_PASSWORD" '~*' '&*' '+@all' '-@dangerous' \
  '+cluster|slots' '+cluster|shards' '+cluster|nodes'
