#!/bin/sh
# Creates the application role and database and the monitoring role. Idempotent.
#   Postgres HA:     Patroni runs this after initdb and passes a connection string as $1.
#   Postgres single: the official image runs it from /docker-entrypoint-initdb.d (no argument, local socket).
# Env: PG_DATABASE, PG_APP_USER, PG_APP_PASSWORD, PG_EXPORTER_PASSWORD. Values are passed to psql as variables and
# quoted by format(%I / %L), so they are not interpolated into SQL text.
set -eu

conn="${1:-postgresql://postgres@%2Fvar%2Frun%2Fpostgresql/postgres}"

psql "$conn" -X -q -v ON_ERROR_STOP=1 \
  -v app_db="$PG_DATABASE" -v app_user="$PG_APP_USER" \
  -v app_password="$PG_APP_PASSWORD" -v exporter_password="$PG_EXPORTER_PASSWORD" <<'SQL'
SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', :'app_user', :'app_password')
 WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = :'app_user') \gexec
SELECT format('CREATE DATABASE %I OWNER %I', :'app_db', :'app_user')
 WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = :'app_db') \gexec
SELECT format('CREATE ROLE exporter LOGIN PASSWORD %L', :'exporter_password')
 WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'exporter') \gexec
GRANT pg_monitor TO exporter;
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
SQL
