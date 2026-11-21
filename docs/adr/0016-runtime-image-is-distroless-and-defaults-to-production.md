---
status: accepted
---

# The runtime image is distroless static, carries the migrator, and defaults to production

The API ships as one image built from `Dockerfile`: a pinned Go toolchain stage, then `gcr.io/distroless/static-debian13:nonroot` pinned by digest. The final stage has no shell and no package manager and runs as uid 65532. It holds `/app/fupi`, `/app/migrator` and `/app/migrations`, so one artifact can serve and migrate (ADR-0014).

Because the image has no `curl`, `wget` or shell, its `HEALTHCHECK` is a subcommand of the binary: `/app/fupi healthcheck` calls `GET /health` on the configured port. It probes liveness, not readiness, so a Postgres outage does not mark the container unhealthy and invite a restart that cannot help (ADR-0015).

The image sets `ENVIRONMENT=production`. A container started without `ENVIRONMENT` therefore refuses the development secrets instead of silently accepting them (the guard from the app-connectivity slice is off for `development` and `test`, and when `ENVIRONMENT` is unset or empty). The local compose stack overrides it with `development`.

The build context is an allowlist (`go.mod`, `go.sum`, `app/`, minus tests), so a secret or a new top-level directory can never enter the builder by accident.

## Considered options

- **Alpine, pinned, non-root** — a shell and `wget` make debugging and the health check easy, but busybox, musl and apk keep a larger surface for scanners to flag.
- **Leaving `ENVIRONMENT` unset in the image** — convenient for `docker run`, but a deployment that forgets it would run with the public development secrets.

## Consequences

- Debugging a running container uses `docker cp` or a distroless `:debug-nonroot` container on the same network; there is no `docker exec` shell.
- Every deployment must set the production secrets (see `docs/Deployment.md`) or the container exits 1 naming the missing variables.
- The digests of both base images go stale; Dependabot's `docker` ecosystem proposes the updates.
- The compose `migrate` job stays on the `migrate/migrate` image, so starting Postgres never needs an app build.
