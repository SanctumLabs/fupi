---
status: accepted
---

# URL and user statuses are PostgreSQL enums, not lookup tables

`urls.status` is the enum `url_status ('ACTIVE', 'SUSPENDED', 'DELETED', 'EXPIRED')`, replacing the `url_status` lookup table and the `status_id` foreign key. The redirect path and the expiry worker both filter on `status = 'ACTIVE'`. A partial index's predicate cannot reference another table, so with a lookup table those indexes were impossible. With the enum they are simple:

- `idx_urls_status ON urls (status) WHERE status = 'ACTIVE'`
- `idx_urls_expires_on ON urls (expires_on) WHERE status = 'ACTIVE'`

The enum also removes a join from every URL read and a status lookup from every write.

## Consequences

- The set of statuses is closed and changes by migration (`ALTER TYPE url_status ADD VALUE ...`). That fits, because the state machine lives only in the URL aggregate (ADR-0002) and a new status needs code changes anyway.
- sqlc generates `postgresql.UrlStatus`; adapters convert it to and from `url.URLStatus` directly.
- User status follows the same pattern for consistency: `users.status` is the enum `user_status ('ACTIVE', 'INACTIVE', 'SUSPENDED', 'DELETED')`, replacing the `user_status` table and `status_id`. Its transitions are owned by the User aggregate (ADR-0008). Status filters in queries compare against the enum directly, with no join.
