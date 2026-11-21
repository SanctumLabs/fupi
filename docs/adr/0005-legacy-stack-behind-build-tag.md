---
status: accepted
---

# Pre-v2 MongoDB stack is kept behind the `legacy` build tag

The original layered app (`app/api`, `app/server`, `app/internal/core/{contracts,entities,urlsvc,usersvc}`, `app/internal/repositories`, `app/internal/services`, `app/test/{mocks,utils,data}`, `app/cmd/legacy_main.go`) stopped compiling once the URL half was removed on this branch. Rather than delete it, every file carries `//go:build legacy` so it stays in the tree for reference during the MongoDB → PostgreSQL migration but is excluded from the default build, vet and tests. It does **not** compile under `-tags legacy` either — `entities.URL`, `urlrepo` and `services/cache` are gone — so the tag is an exclusion marker, not a working configuration. Delete the tagged files once the v2 application and HTTP layers replace them (Phase 4, MongoDB decommission).

`app/config`, `app/tools` and `app/internal/adapters/redis` are untagged because they compile and the redis adapter still depends on the first two.
