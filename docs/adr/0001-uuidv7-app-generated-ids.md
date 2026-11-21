---
status: accepted
---

# UUIDv7 app-generated identifiers

Aggregates need identities before they are persisted (domain events reference the aggregate ID) and IDs should sort by creation time. The v2 design spec proposed xid strings in `VARCHAR(20)` columns, but the schema, sqlc output, `entity.ID` and every adapter mapper were already built on UUID. We use UUIDv7 (`entity.NewID()` → `uuid.NewV7()`): it gives time ordering and in-process generation with no DB round-trip, while keeping the UUID columns and pgx plumbing unchanged. `users.id` and `urls.id` have no `DEFAULT gen_random_uuid()` so the application is the only source of aggregate IDs; other tables keep their DB default until their own adapters exist.

## Considered options

- **xid `VARCHAR(20)`** (as spec'd) — same properties, but would require a schema change, regenerating sqlc and rewriting every mapper and test for no functional gain.
- **UUIDv4 with DB default** (previous behaviour) — no time ordering; the ID only exists after the INSERT returns, so events couldn't be recorded before persistence.
