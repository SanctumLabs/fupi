---
status: accepted
---

# The URL aggregate owns all status transitions

`URLStatus` may only change through methods on `url.URL` (`MarkExpired`, `Suspend`, `Reinstate`, `MarkDeleted`); there is no status setter and adapters hydrate status only via `url.NewUrl`. Each transition validates the current state, returns `errdefs.ErrInvalidStatusTransition` otherwise, and records the matching domain event so the outbox can publish it. This keeps the state machine in one place instead of scattered across use cases, workers and SQL.

Permitted transitions: `ACTIVE → EXPIRED`, `ACTIVE → SUSPENDED`, `SUSPENDED → ACTIVE`, and `{ACTIVE, EXPIRED, SUSPENDED} → DELETED`. Only `ACTIVE` redirects (`Redirect()` also rejects an `ACTIVE` URL past its `expiresOn`). The spec diagram listed only `ACTIVE → DELETED`; allowing deletion from `EXPIRED` and `SUSPENDED` too is deliberate — an owner must be able to remove a link the system has already expired or suspended.

## Consequences

- `NewUrl` is a hydration factory: it validates value objects but not time invariants, so an `EXPIRED` row with a past expiry can be loaded. `url.Create` is the creation factory and enforces `expiresOn` in the future, sets `ACTIVE`, assigns identity and records `URLCreated`.
- `CustomAlias` has no setter on the aggregate, which is how "immutable once confirmed" is enforced.
- `URLAccessed` is not recorded by `Redirect()`; the redirect use case builds it from request context (IP, UA, GeoIP) once that layer exists.
