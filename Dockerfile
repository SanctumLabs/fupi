# Builder: the pinned Go toolchain. GOTOOLCHAIN=local makes a toolchain older than go.mod fail loudly instead of
# silently downloading another one.
FROM golang:1.26-alpine@sha256:8ac98ca534ac3f51e1f420a1dd2c15e74c75cfa0f23f3ad27eb5d7236c349a0c AS build

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
FROM gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3

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
