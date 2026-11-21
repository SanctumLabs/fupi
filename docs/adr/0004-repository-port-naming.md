---
status: accepted
---

# Repository ports use the `Fetch*` naming already in the code

The v2 spec named the URL repository methods `FindByShortCode`, `FindByID`, `FindExpiredActive`, etc. The code had already settled on `FetchById`, `FetchByShortCode`, `FetchByUserId` via the generic `repository.ReadRepositoryPort[T]` / `WriteRepositoryPort[T]`, shared by the URL and Identity contexts. We keep the code's naming rather than rename across both contexts and their mocks. `FetchExpiredActive(ctx, before, limit)` was added to `url.UrlReadDatastore` (then `UrlReadRepository`; see ADR-0013) for the expiry worker.

Identity keeps the `User` shape in the code (username, first/last name, email, verification, status, metadata) rather than the narrower spec'd table; the schema follows the code.
