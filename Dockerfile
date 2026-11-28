# Builder: the pinned Go toolchain. GOTOOLCHAIN=local makes a toolchain older than go.mod fail loudly instead of
# silently downloading another one.
FROM golang:1.27-alpine@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS build

ARG VERSION=unknown
ARG GIT_COMMIT=unknown
ARG BUILD_TIME=unknown

ENV GOTOOLCHAIN=local

WORKDIR /src

# Dependencies first, so source changes do not invalidate this layer.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download && go mod verify

COPY app ./app

# go.sum is used as it is (no -mod=mod). Symbols are stripped, paths are trimmed and the version variables from
# app/pkg/version.go are injected.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    set -eu; \
    ldflags="-s -w \
      -X github.com/sanctumlabs/fupi/app/pkg.Version=${VERSION} \
      -X github.com/sanctumlabs/fupi/app/pkg.GitCommit=${GIT_COMMIT} \
      -X github.com/sanctumlabs/fupi/app/pkg.BuildTime=${BUILD_TIME}"; \
    CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags "${ldflags}" -o /out/fupi ./app/cmd; \
    CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags "${ldflags}" -o /out/migrator ./app/cmd/migrator; \
    CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags "${ldflags}" -o /out/worker ./app/cmd/worker

# Distribution: distroless static, no shell and no package manager, running as uid 65532.
FROM gcr.io/distroless/static-debian13:nonroot@sha256:8ce6dd87e9823c5b48dcfb2c8604714ccb351ce59607327bcc4957448da279b5

ARG VERSION=unknown
ARG GIT_COMMIT=unknown
ARG BUILD_TIME=unknown

LABEL org.opencontainers.image.title="fupi" \
      org.opencontainers.image.description="Fupi URL shortener API, database migrator and outbox relay worker" \
      org.opencontainers.image.source="https://github.com/SanctumLabs/fupi" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${GIT_COMMIT}" \
      org.opencontainers.image.created="${BUILD_TIME}"

WORKDIR /app

COPY --from=build /out/fupi /out/migrator /out/worker /app/
COPY app/internal/adapters/postgres/migrations /app/migrations

# A container started without ENVIRONMENT refuses the development secrets; compose overrides it for local use.
ENV ENVIRONMENT=production \
    MIGRATIONS_PATH=/app/migrations

USER 65532:65532

EXPOSE 8085

HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=3 CMD ["/app/fupi", "healthcheck"]

ENTRYPOINT ["/app/fupi"]
