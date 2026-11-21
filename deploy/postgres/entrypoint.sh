#!/bin/sh
# Prepares the data directory as root, then drops to the postgres user before starting Patroni (the same pattern as the
# official postgres image). Patroni creates and owns the cluster inside DATA_DIR.
set -eu

DATA_DIR=/var/lib/postgresql/patroni-data

mkdir -p "$DATA_DIR"
chown -R postgres:postgres /var/lib/postgresql
chmod 700 "$DATA_DIR"

exec gosu postgres /opt/patroni/bin/patroni /etc/patroni/patroni.yml
