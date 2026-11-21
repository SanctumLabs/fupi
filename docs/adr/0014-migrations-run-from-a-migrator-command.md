---
status: accepted
---

# Migrations are applied by a migrator command, never by the API at startup

`postgres.Migrate()` applies the SQL migrations with golang-migrate (ADR-0010). Its only caller was the test helper, so nothing in the product applied migrations. `app/cmd/migrator` is now its production caller: it loads the database settings, applies `app/internal/adapters/postgres/migrations` and exits 0 (applied or "no change") or 1.

The API does not call `Migrate()` at startup. Several replicas starting together would race to migrate, a failed migration would take the API down with it, and the API's database role would need DDL rights it should not otherwise have. The migrator runs once per deploy, as its own process, and can use a different role.

## Considered options

- **An opt-in `MIGRATE_ON_START` flag on the API** — convenient for one laptop, but it puts the replica race and the DDL role back in production, so it was rejected.
- **A `migrate` subcommand on the API binary** — one image, but the API process would still carry the migration code path and the same temptation.

## Consequences

- `go run ./app/cmd/migrator` (from the repo root, or with `MIGRATIONS_PATH`) migrates the database in `DATABASE_*`; `make run.with.migrations` runs it and then the API.
- The migrator needs only the database settings, so it demands no `AUTH_SECRET`, and refuses the development database password outside development and test.
- `Migrate()` records its state in `schema_migrations`, the table `make migrate` and the compose `migrate` job already use, and keeps a `sslmode` given in the URL.
- The migrations directory must exist where the migrator runs; building it into an image is the Dockerfile slice's concern.
- The compose `migrate` job keeps using the `migrate/migrate` image until an app image exists.
- Only `up` is implemented; `make migrate MIGRATE_DIRECTION=down` remains the rollback path.
