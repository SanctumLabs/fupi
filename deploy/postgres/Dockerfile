# syntax=docker/dockerfile:1
# Patroni on top of the official Postgres image, for the local HA stack only.
FROM postgres:18.6

ARG PATRONI_VERSION=4.1.5

# hadolint ignore=DL3008
RUN apt-get update \
 && apt-get install -y --no-install-recommends python3 python3-venv curl \
 && python3 -m venv /opt/patroni \
 && /opt/patroni/bin/pip install --no-cache-dir "patroni[etcd3]==${PATRONI_VERSION}" "psycopg[binary]" \
 && rm -rf /var/lib/apt/lists/*

COPY entrypoint.sh /usr/local/bin/patroni-entrypoint.sh
COPY roles.sh /usr/local/bin/roles.sh
RUN chmod 0755 /usr/local/bin/patroni-entrypoint.sh /usr/local/bin/roles.sh

EXPOSE 5432 8008
ENTRYPOINT ["/usr/local/bin/patroni-entrypoint.sh"]
