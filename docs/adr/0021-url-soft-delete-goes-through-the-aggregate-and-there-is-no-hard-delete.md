---
status: accepted
---

# URL soft delete goes through the aggregate, and there is no hard delete

`UrlWriteDatastore.SoftDelete(ctx, id)` loads the URL and its keywords, calls `MarkDeleted()` and persists the result exactly as `Update` does (status, `deleted_at`, keywords, `url.deleted` in the outbox), all in one transaction. A plain `UPDATE ... SET deleted_at` would have left `status = ACTIVE` and emitted no event, bypassing ADR-0002. `urlapp.Service.Delete` checks ownership and then calls `SoftDelete`, so there is one delete path. An unknown id is `NotFound`; an already deleted URL is a `Conflict`.

The URL port does not declare the hard `Delete` of `repository.WriteRepositoryPort`. Nothing in the product purges a URL, and a hard delete would remove the row and its keywords by cascade without any event, so consumers (analytics, caches) would never learn the URL is gone. `UrlWriteDatastore` therefore lists `Save`, `Update` and `SoftDelete` itself. If erasure requests ever need a purge, add it then with an explicit event (for example `url.purged`) written to the outbox in the same transaction.

No method may exist only to return `errdefs.NotImplemented`: implement it or do not declare it.
