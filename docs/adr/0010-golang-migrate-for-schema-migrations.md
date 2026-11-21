---
status: accepted
---

# Schema migrations use golang-migrate, not goose

The v2 spec names goose and a `cmd/migrator` binary. The code already runs migrations with golang-migrate: `pkg/infra/database/postgres/migrator.go` applies the `up`/`down` SQL pairs in `internal/adapters/postgres/migrations`, `make migrate` drives the same files, and the integration and e2e suites migrate their testcontainers databases through it. We keep golang-migrate rather than swap tools for no functional gain.

## Consequences

- Migrations are plain paired `NNNNNN_name.up.sql` / `.down.sql` files; there are no Go migrations.
- `sql/schema.sql` is sqlc's input and must stay identical to the applied migrations (ignoring comments).
- Until v2 first ships, `000001_initial_schema` is edited in place. After that, every change is a new numbered migration.
