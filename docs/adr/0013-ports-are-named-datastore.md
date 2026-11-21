---
status: accepted
---

# Persistence ports and adapters are named "Datastore"

Persistence ports in every context are named `<Aggregate>{Read,Write}Datastore`, for example `identity.UserReadDatastore` and `url.UrlWriteDatastore`. Their PostgreSQL adapters are `<aggregate>{Read,Write}DatastoreAdapter` in `<context>datastore` packages such as `identitydatastore` and `urldatastore`. The URL context previously used "Repository" while Identity used "Datastore"; one name removes the question of whether they are different concepts.

The generic base interfaces in `core/ports/repository` (`ReadRepositoryPort[T]`, `WriteRepositoryPort[T]`) are shared with the legacy stack and keep their names until it is deleted (ADR-0005). The `Fetch*` method naming from ADR-0004 is unchanged.
