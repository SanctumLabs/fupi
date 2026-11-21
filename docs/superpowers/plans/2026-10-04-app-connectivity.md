# App Connectivity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the Fupi API start against the local infrastructure stack in either mode, report readiness, drain and exit cleanly on SIGTERM, and give database migrations a real entry point (`cmd/migrator`).

**Architecture:** A strict typed config loader (`config.Load`) replaces the scattered `EnvOr` calls; a Redis client that builds without I/O and exposes `Ping`/`Close`; a small health registry (Postgres required, Redis optional) served at `/health` and `/health/ready`; `Server.Serve` drains in-flight requests on context cancel; `main()` becomes `run(ctx, cfg)`; and `app/cmd/migrator` is the production caller of `postgres.Migrate()`.

**Tech Stack:** Go 1.26, Fiber v2.52, go-redis v9, pgx v5 / golang-migrate, testify, testcontainers (Postgres, Redis). No new module dependencies.

**Spec:** `docs/superpowers/specs/2026-10-04-app-connectivity-design.md` (decisions D1–D9 are binding; this plan argues from it).

## Global Constraints

- Go `1.26.0` (`go.mod`). **No new module dependencies**; if `go.mod`/`go.sum` change, something is wrong (testcontainers Redis/Postgres, testify, godotenv, go-redis v9, Fiber v2.52 and golang-migrate are already required).
- All commands run from the repo root `/Users/lusina/Projects/SanctumLabs/fupi`.
- Environment variable names, defaults and integer unit conventions are exactly the spec §4 table (`DATABASE_MAX_CONN_LIFETIME` in hours, `DATABASE_MAX_CONN_IDLE_TIME` in minutes, the other durations in seconds).
- Development defaults equal the stack: Postgres `localhost:5432` `fupi-user`/`fupi-pass` `fupidb`; Redis `localhost:7001` `fupi-svc`/`fupi-svc`; `AUTH_SECRET=fupi-secret`.
- Any `ENVIRONMENT` other than `development` or `test` rejects the three development defaults (spec D6). Error text names variables, **never values**.
- Public health responses carry `up`/`down` only, never error text (D7).
- Unit tests are untagged; Docker-backed tests carry `//go:build integration` (ADR-0006). Use testify `require`/`assert`. `go test ./...` must be green at the end of every task.
- `make run` and the README use `go run app/cmd/main.go` (one file), so the whole of `main`/`run` stays in `app/cmd/main.go`.
- Do not touch the legacy-tagged code (`-tags legacy` is already broken, D8), `fly.toml`, or slice 1's `deploy/` files (exception: spec §10 step 7 if the live Redis ACL check fails).
- Format with `gofmt -w` on every Go file you create or edit.

## Plan notes (deviations from the spec's wording)

1. `config.Load` takes a `Lookup` (`func(string) (string, bool)`, satisfied by `os.LookupEnv`) instead of `*env.EnvConfig`. `EnvConfig`'s getters swallow parse errors, and spec §4 requires them to be errors, so the loader has its own strict reader. The spec's D5 line is updated in the same commit as this plan.
2. The graceful-shutdown logic lives in `Server.Serve`/`Server.ServeListener` (testable without a database); `run` in `main.go` composes it. Spec §8's sequence is unchanged.
3. The Fiber probe handlers live in a new package `app/api/probes` (spec §7 asks for a package without the `legacy` tag, distinct from `app/api/health`).
4. Config types are `config.App`, `ServerSettings`, `DatabaseSettings`, `MigrationSettings`; Postgres and Redis settings reuse `postgres.PostgresDatabaseConfig` and `redis.RedisClientConfig` (no import cycle: neither imports `app/config`).
5. The `.env.example` rewrite moves into Task 2, because a test there pins the file to the loader's defaults.

## Review Focus

Failure modes the spec implies but no obvious test covers, most likely first. Each has a pinning test or drill in the owning task.

1. A database password with `@ / : ? # %` or a space corrupts the DSN → Task 1 (`TestConnectionString_EscapesCredentialsSoTheyRoundTrip`).
2. `.env` lines like `REDIS_PASSWORD=` (set but empty) must mean "unset", not an empty secret → Task 2 (`TestLoad_AnEmptyValueCountsAsUnset`).
3. A dependency that hangs (ignores its context) must not hang the readiness endpoint → Task 4 (`TestRegistry_AHangingCheckCannotHangTheReport`).
4. In-flight requests that outlast `SHUTDOWN_TIMEOUT` must make shutdown return an error (exit 1), not hang → Task 5 (`TestServe_ReturnsAnErrorWhenInFlightRequestsOutlastTheTimeout`).
5. A second Ctrl-C during a stuck drain must end the process immediately → Task 5 wiring in `main()`, pinned by the Task 8 live drill.

---

### Task 1: Postgres DSN builder and `Migrate()` fixes

**Files:**

- Modify: `app/pkg/infra/database/postgres/utils.go` (replace `buildConnectionString`)
- Modify: `app/pkg/infra/database/postgres/postgres_client.go:44`
- Modify: `app/pkg/infra/database/postgres/migrator.go`
- Modify: `app/pkg/infra/database/postgres/config.go` (drop `Schema`)
- Modify: `app/test/test_database.go:67` (drop the `Schema:` line of `DefaultPostgresDatabaseConfig`)
- Create: `app/pkg/infra/database/postgres/utils_test.go`
- Create: `app/pkg/infra/database/postgres/migrator_test.go`
- Create: `app/pkg/infra/database/postgres/migrator_integration_test.go`

**Interfaces:**

- Consumes: nothing from earlier tasks.
- Produces: `postgres.ConnectionString(config PostgresDatabaseConfig) string` (exported; returns `config.Url` when set); `PostgresDatabaseConfig` **without** a `Schema` field; `postgres.Migrate(databaseURL, migrationPath string, inDocker bool) error` keeps its signature but uses table `schema_migrations`, keeps a `sslmode` already in the URL and drops `pool_*` query parameters before handing the URL to golang-migrate. Task 2 reads `PostgresDatabaseConfig`; Task 5 and Task 6 call `ConnectionString` / `Migrate`.

**Task test command:** `go test ./app/pkg/infra/database/postgres/ && go test -tags integration -count=1 -run TestMigrate ./app/pkg/infra/database/postgres/`

- [ ] **Step 1: Write the failing unit tests**

Create `app/pkg/infra/database/postgres/utils_test.go`:

```go
package postgres

import (
 "testing"

 "github.com/jackc/pgx/v5/pgxpool"
 "github.com/stretchr/testify/assert"
 "github.com/stretchr/testify/require"
)

func TestConnectionString_UrlWinsWhenSet(t *testing.T) {
 cfg := PostgresDatabaseConfig{Url: "postgres://u:p@h:1/d?sslmode=require", Host: "ignored", Port: "5432"}

 assert.Equal(t, "postgres://u:p@h:1/d?sslmode=require", ConnectionString(cfg))
}

func TestConnectionString_EscapesCredentialsSoTheyRoundTrip(t *testing.T) {
 password := "p@ss/w:rd?#%x y"
 cfg := PostgresDatabaseConfig{
  Host: "db.internal", Port: "5432", Name: "fupidb",
  Username: "fupi user", Password: password,
  SslMode: "disable", MaxConns: 30, MinConns: 5,
 }

 parsed, err := pgxpool.ParseConfig(ConnectionString(cfg))

 require.NoError(t, err)
 assert.Equal(t, password, parsed.ConnConfig.Password)
 assert.Equal(t, "fupi user", parsed.ConnConfig.User)
 assert.Equal(t, "db.internal", parsed.ConnConfig.Host)
 assert.Equal(t, uint16(5432), parsed.ConnConfig.Port)
 assert.Equal(t, "fupidb", parsed.ConnConfig.Database)
 assert.Equal(t, int32(30), parsed.MaxConns)
 assert.Equal(t, int32(5), parsed.MinConns)
}

func TestConnectionString_HonoursSslMode(t *testing.T) {
 base := PostgresDatabaseConfig{Host: "h", Port: "5432", Name: "d", Username: "u", Password: "p", MaxConns: 2, MinConns: 1}

 disabled := base
 disabled.SslMode = "disable"
 parsed, err := pgxpool.ParseConfig(ConnectionString(disabled))
 require.NoError(t, err)
 assert.Nil(t, parsed.ConnConfig.TLSConfig, "sslmode=disable must not negotiate TLS")

 required := base
 required.SslMode = "require"
 parsed, err = pgxpool.ParseConfig(ConnectionString(required))
 require.NoError(t, err)
 assert.NotNil(t, parsed.ConnConfig.TLSConfig, "sslmode=require must negotiate TLS")
}
```

Create `app/pkg/infra/database/postgres/migrator_test.go`:

```go
package postgres

import (
 "net/url"
 "testing"

 "github.com/stretchr/testify/assert"
 "github.com/stretchr/testify/require"
)

func TestMigrationURL_UsesTheSharedTableAndDefaultsSslModeOff(t *testing.T) {
 got, err := migrationURL("postgres://u:p@h:5432/d")
 require.NoError(t, err)

 query := mustQuery(t, got)
 assert.Equal(t, "schema_migrations", query.Get("x-migrations-table"))
 assert.Equal(t, "disable", query.Get("sslmode"))
}

func TestMigrationURL_KeepsAnSslModeThatIsAlreadySet(t *testing.T) {
 got, err := migrationURL("postgres://u:p@h:5432/d?sslmode=require")
 require.NoError(t, err)

 assert.Equal(t, "require", mustQuery(t, got).Get("sslmode"))
}

func TestMigrationURL_DropsPoolParametersTheMigrationDriverDoesNotKnow(t *testing.T) {
 got, err := migrationURL("postgres://u:p@h:5432/d?pool_max_conns=30&pool_min_conns=5&sslmode=require")
 require.NoError(t, err)

 query := mustQuery(t, got)
 assert.False(t, query.Has("pool_max_conns"))
 assert.False(t, query.Has("pool_min_conns"))
 assert.Equal(t, "require", query.Get("sslmode"))
}

func TestMigrationURL_RejectsAMalformedURL(t *testing.T) {
 _, err := migrationURL("://bad")

 assert.Error(t, err)
}

func mustQuery(t *testing.T, rawURL string) url.Values {
 t.Helper()
 parsed, err := url.Parse(rawURL)
 require.NoError(t, err)
 return parsed.Query()
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./app/pkg/infra/database/postgres/`
Expected: build failure `undefined: ConnectionString` and `undefined: migrationURL`.

- [ ] **Step 3: Write the failing integration test**

Create `app/pkg/infra/database/postgres/migrator_integration_test.go`:

```go
//go:build integration

package postgres_test

import (
 "context"
 "testing"

 "github.com/jackc/pgx/v5/pgxpool"
 "github.com/sanctumlabs/fupi/app/test"
 "github.com/stretchr/testify/assert"
 "github.com/stretchr/testify/require"
)

// Migrations are applied through Migrate, record their state in schema_migrations (the table `make migrate` and the
// compose migrate job use) and a second run changes nothing.
func TestMigrate_AppliesTheMigrationsOnceInSchemaMigrations(t *testing.T) {
 ctx := context.Background()
 container, err := test.TestPostgresDatabaseContainer(ctx, test.DefaultTestDatabaseConfig())
 require.NoError(t, err)
 t.Cleanup(func() { _ = container.Terminate(context.Background()) })
 connectionString, err := container.ConnectionString(ctx, "sslmode=disable")
 require.NoError(t, err)

 require.NoError(t, test.RunDatabaseMigration(ctx, connectionString))
 require.NoError(t, test.RunDatabaseMigration(ctx, connectionString), "a second run must be a no-op")

 pool, err := pgxpool.New(ctx, connectionString)
 require.NoError(t, err)
 t.Cleanup(pool.Close)

 var version int64
 var dirty bool
 require.NoError(t, pool.QueryRow(ctx, "SELECT version, dirty FROM schema_migrations").Scan(&version, &dirty))
 assert.GreaterOrEqual(t, version, int64(1))
 assert.False(t, dirty)

 var legacyTable, outboxTable *string
 require.NoError(t, pool.QueryRow(ctx, "SELECT to_regclass('bid_schema_migrations')::text").Scan(&legacyTable))
 require.NoError(t, pool.QueryRow(ctx, "SELECT to_regclass('outbox_events')::text").Scan(&outboxTable))
 assert.Nil(t, legacyTable, "the old bid_schema_migrations table must not be created")
 assert.NotNil(t, outboxTable, "the migrations must have created outbox_events")
}
```

- [ ] **Step 4: Run it to verify it fails for the right reason**

The unit-test files from Step 1 do not compile yet, so move them aside for this one run (Docker must be running):

```bash
ASIDE=$(mktemp -d)
mv app/pkg/infra/database/postgres/utils_test.go app/pkg/infra/database/postgres/migrator_test.go "$ASIDE"/
go test -tags integration -count=1 -run TestMigrate ./app/pkg/infra/database/postgres/ 2>&1 | tail -15
mv "$ASIDE"/utils_test.go "$ASIDE"/migrator_test.go app/pkg/infra/database/postgres/
```

Expected: `FAIL` with `relation "schema_migrations" does not exist` (today's `Migrate` writes `bid_schema_migrations`). The two files are back in place afterwards.

- [ ] **Step 5: Implement `ConnectionString`**

In `app/pkg/infra/database/postgres/utils.go`, replace the whole `buildConnectionString` function with:

```go
// ConnectionString returns the DSN for a database config. DATABASE_URL (Url) wins when set; otherwise the DSN is
// built from the parts with the credentials escaped, so a password containing @ / : ? # or % cannot corrupt it.
func ConnectionString(config PostgresDatabaseConfig) string {
 if config.Url != "" {
  return config.Url
 }

 query := url.Values{}
 if config.SslMode != "" {
  query.Set("sslmode", config.SslMode)
 }
 query.Set("pool_max_conns", strconv.Itoa(int(config.MaxConns)))
 query.Set("pool_min_conns", strconv.Itoa(int(config.MinConns)))

 dsn := url.URL{
  Scheme:   "postgres",
  User:     url.UserPassword(config.Username, config.Password),
  Host:     net.JoinHostPort(config.Host, config.Port),
  Path:     "/" + config.Name,
  RawQuery: query.Encode(),
 }
 return dsn.String()
}
```

Replace the import block of `utils.go` with (adds `net`, `net/url`, `strconv`; `fmt` stays because `WithTransactionRetry` uses it — keep every import the rest of the file already uses):

```go
import (
 "context"
 "fmt"
 "log/slog"
 "net"
 "net/url"
 "strconv"

 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgconn"
 "github.com/jackc/pgx/v5/pgxpool"
 postgresql "github.com/sanctumlabs/fupi/app/internal/adapters/postgres/sql"
 "github.com/sanctumlabs/fupi/app/pkg/infra/database"
 recoveryutils "github.com/sanctumlabs/fupi/app/pkg/utils/recover"
)
```

In `postgres_client.go` change line 44 from `connStr := buildConnectionString(config)` to `connStr := ConnectionString(config)`.

- [ ] **Step 6: Implement `migrationURL` and use it in `Migrate`**

In `app/pkg/infra/database/postgres/migrator.go`, add above `Migrate`:

```go
// migrationsTable is the table golang-migrate records its state in. `make migrate` and the compose migrate job use
// the same name, so every way of running the migrations agrees on what has been applied.
const migrationsTable = "schema_migrations"

// migrationURL prepares a database URL for golang-migrate: it selects the migrations table, defaults sslmode to
// disable only when the URL does not say otherwise, and drops the pool_* parameters that pgxpool understands but the
// migration driver would send to the server as unknown settings.
func migrationURL(databaseURL string) (string, error) {
 parsed, err := url.Parse(databaseURL)
 if err != nil {
  return "", err
 }

 query := parsed.Query()
 if query.Get("sslmode") == "" {
  query.Set("sslmode", "disable")
 }
 for key := range query {
  if strings.HasPrefix(key, "pool_") {
   query.Del(key)
  }
 }
 query.Set("x-migrations-table", migrationsTable)

 parsed.RawQuery = query.Encode()
 return parsed.String(), nil
}
```

Replace the block in `Migrate` that starts at `// Parse the database URL and properly append sslmode parameter` and ends at `databaseURL = parsedURL.String()` with:

```go
 databaseURL, urlErr := migrationURL(databaseURL)
 if urlErr != nil {
  slog.ErrorContext(ctx, "migrate: invalid DATABASE_URL", "error", urlErr)
  return urlErr
 }
```

Add `"strings"` to the import block of `migrator.go` (keep `net/url`, which `migrationURL` uses).

- [ ] **Step 7: Remove the unused `Schema` field**

In `config.go` delete the line `Schema   string \`env-description:"Database Schema" yaml:"schema" env:"DATABASE_SCHEMA" env-default:"bid"\``. In`app/test/test_database.go` delete the line `Schema:      TEST_DATABASE_SCHEMA,` inside `DefaultPostgresDatabaseConfig` (line 67; leave the `TestDatabaseConfig.Schema` field and its other uses alone).

- [ ] **Step 8: Run the tests**

Run: `gofmt -w app/pkg/infra/database/postgres app/test && go build ./... && go test ./app/pkg/infra/database/postgres/ && go test -tags integration -count=1 -run TestMigrate ./app/pkg/infra/database/postgres/`
Expected: unit tests `ok`; the integration test `ok` (it was RED in Step 4).

- [ ] **Step 9: Run the whole suite and commit**

Run: `go test ./... 2>&1 | grep -v "no test files" | tail -30`
Expected: every package `ok`, no `FAIL`.

```bash
git add app/pkg/infra/database/postgres app/test/test_database.go
git commit -m "$(cat <<'EOF'
fix(postgres): escape DSN credentials, honour DATABASE_URL and sslmode, migrate into schema_migrations

EOF
)"
```

---

### Task 2: Typed config loader and the `.env.example` contract

**Files:**

- Create: `app/config/loader.go`
- Create: `app/config/app.go`
- Create: `app/config/app_test.go`
- Modify: `.env.example` (application section)

**Interfaces:**

- Consumes (Task 1): `postgres.PostgresDatabaseConfig` (no `Schema`); `redis.RedisClientConfig{Address, Username, Password, Database}` (existing).
- Produces:
  - `type Lookup func(key string) (string, bool)`
  - `type ServerSettings struct{ Host, Header, Name, Version, BaseURL string; Port int }`
  - `type DatabaseSettings struct{ Postgres postgres.PostgresDatabaseConfig; OperationTimeout time.Duration }`
  - `type MigrationSettings struct{ Path string }`
  - `type App struct{ Environment string; Server ServerSettings; Database DatabaseSettings; Redis redis.RedisClientConfig; Auth AuthConfig; ShutdownTimeout time.Duration }`
  - `func Load(lookup Lookup) (App, error)`, `LoadServer`, `LoadDatabase`, `LoadRedis`, `LoadAuth` (returns the existing `AuthConfig`), `LoadMigrations` — each `func(Lookup) (T, error)`.
  - Tasks 5 and 6 consume these.

**Task test command:** `go test ./app/config/ && make infra.config && bash scripts/infra_test.sh`

- [ ] **Step 1: Write the failing tests**

Create `app/config/app_test.go`:

```go
package config

import (
 "testing"
 "time"

 "github.com/joho/godotenv"
 "github.com/stretchr/testify/assert"
 "github.com/stretchr/testify/require"
)

func lookupOf(env map[string]string) Lookup {
 return func(key string) (string, bool) {
  value, ok := env[key]
  return value, ok
 }
}

func TestLoad_DefaultsMatchTheLocalStack(t *testing.T) {
 app, err := Load(lookupOf(nil))
 require.NoError(t, err)

 assert.Equal(t, "development", app.Environment)
 assert.Equal(t, ServerSettings{
  Host: "0.0.0.0", Port: 8085, Header: "Fupi", Name: "Fupi", Version: "1.0.0", BaseURL: "http://localhost:8085",
 }, app.Server)
 assert.Equal(t, 15*time.Second, app.ShutdownTimeout)

 pg := app.Database.Postgres
 assert.Equal(t, "localhost", pg.Host)
 assert.Equal(t, "5432", pg.Port, "writes go to the primary on 5432; 5433 is the HA read port")
 assert.Equal(t, "fupidb", pg.Name)
 assert.Equal(t, "fupi-user", pg.Username)
 assert.Equal(t, "fupi-pass", pg.Password)
 assert.Equal(t, "disable", pg.SslMode)
 assert.Empty(t, pg.Url)
 assert.Equal(t, int32(30), pg.MaxConns)
 assert.Equal(t, int32(5), pg.MinConns)
 assert.Equal(t, time.Hour, pg.MaxConnLifetime)
 assert.Equal(t, 30*time.Minute, pg.MaxConnIdleTime)
 assert.Equal(t, 30*time.Second, pg.ConnTimeout)
 assert.Equal(t, 10*time.Second, pg.QueryTimeout)
 assert.Equal(t, 30*time.Second, app.Database.OperationTimeout)

 assert.Equal(t, []string{"localhost:7001"}, app.Redis.Address)
 assert.Equal(t, "fupi-svc", app.Redis.Username)
 assert.Equal(t, "fupi-svc", app.Redis.Password)
 assert.Equal(t, 0, app.Redis.Database)

 assert.Equal(t, "fupi-secret", app.Auth.Secret)
 assert.Equal(t, "fupi", app.Auth.Issuer)
 assert.Equal(t, 15, app.Auth.ExpireDelta)
 assert.Equal(t, 24, app.Auth.RefreshExpireDelta)
}

// .env.example is what a new developer copies to .env. If it drifts from the loader's defaults, the documented
// setup stops matching the stack.
func TestLoad_EnvExampleDocumentsTheDefaults(t *testing.T) {
 example, err := godotenv.Read("../../.env.example")
 require.NoError(t, err)

 defaults, err := Load(lookupOf(nil))
 require.NoError(t, err)
 fromExample, err := Load(lookupOf(example))
 require.NoError(t, err)

 assert.Equal(t, defaults, fromExample)
}

func TestLoad_Overrides(t *testing.T) {
 app, err := Load(lookupOf(map[string]string{
  "ENVIRONMENT": "test", "HTTP_PORT": "9000", "SERVER_HOST": "127.0.0.1", "SERVER_HEADER": "H", "SERVER_NAME": "N",
  "SERVER_VERSION": "2.0.0", "APP_BASE_URL": "https://fupi.test", "SHUTDOWN_TIMEOUT": "30",
  "DATABASE_HOST": "db", "DATABASE_PORT": "6432", "DATABASE_NAME": "x", "DATABASE_USERNAME": "u",
  "DATABASE_PASSWORD": "p", "DATABASE_SSL_MODE": "require", "DATABASE_URL": "postgres://u:p@h:1/d",
  "DATABASE_MAX_CONNS": "10", "DATABASE_MIN_CONNS": "2", "DATABASE_MAX_CONN_LIFETIME": "2",
  "DATABASE_MAX_CONN_IDLE_TIME": "5", "DATABASE_CONN_TIMEOUT": "7", "DATABASE_QUERY_TIMEOUT": "8",
  "DATABASE_OPERATION_TIMEOUT": "9",
  "REDIS_ADDRESS":              "r1:7001, r2:7002", "REDIS_USERNAME": "ru", "REDIS_PASSWORD": "rp",
  "AUTH_SECRET": "s", "AUTH_ISSUER": "i", "AUTH_EXPIRE_DELTA": "5", "AUTH_REFRESH_EXPIRE_DELTA": "6",
 }))
 require.NoError(t, err)

 assert.Equal(t, "test", app.Environment)
 assert.Equal(t, ServerSettings{Host: "127.0.0.1", Port: 9000, Header: "H", Name: "N", Version: "2.0.0", BaseURL: "https://fupi.test"}, app.Server)
 assert.Equal(t, 30*time.Second, app.ShutdownTimeout)
 pg := app.Database.Postgres
 assert.Equal(t, "db", pg.Host)
 assert.Equal(t, "6432", pg.Port)
 assert.Equal(t, "postgres://u:p@h:1/d", pg.Url)
 assert.Equal(t, int32(10), pg.MaxConns)
 assert.Equal(t, int32(2), pg.MinConns)
 assert.Equal(t, 2*time.Hour, pg.MaxConnLifetime)
 assert.Equal(t, 5*time.Minute, pg.MaxConnIdleTime)
 assert.Equal(t, 7*time.Second, pg.ConnTimeout)
 assert.Equal(t, 8*time.Second, pg.QueryTimeout)
 assert.Equal(t, 9*time.Second, app.Database.OperationTimeout)
 assert.Equal(t, []string{"r1:7001", "r2:7002"}, app.Redis.Address)
 assert.Equal(t, "ru", app.Redis.Username)
 assert.Equal(t, 5, app.Auth.ExpireDelta)
}

// A value that is set but empty means "not set": a .env line like REDIS_PASSWORD= must not become an empty secret.
func TestLoad_AnEmptyValueCountsAsUnset(t *testing.T) {
 defaults, err := Load(lookupOf(nil))
 require.NoError(t, err)

 app, err := Load(lookupOf(map[string]string{
  "REDIS_PASSWORD": "", "HTTP_PORT": "  ", "DATABASE_PASSWORD": "", "AUTH_SECRET": "",
 }))

 require.NoError(t, err)
 assert.Equal(t, defaults, app)
}

func TestLoad_RejectsInvalidValues(t *testing.T) {
 cases := map[string]struct {
  env  map[string]string
  want string
 }{
  "http port not a number":         {map[string]string{"HTTP_PORT": "abc"}, "HTTP_PORT"},
  "http port zero":                 {map[string]string{"HTTP_PORT": "0"}, "HTTP_PORT"},
  "http port too large":            {map[string]string{"HTTP_PORT": "70000"}, "HTTP_PORT"},
  "database port not a number":     {map[string]string{"DATABASE_PORT": "x"}, "DATABASE_PORT"},
  "database port too large":        {map[string]string{"DATABASE_PORT": "65536"}, "DATABASE_PORT"},
  "max conns not a number":         {map[string]string{"DATABASE_MAX_CONNS": "many"}, "DATABASE_MAX_CONNS"},
  "min conns above max conns":      {map[string]string{"DATABASE_MIN_CONNS": "9", "DATABASE_MAX_CONNS": "3"}, "DATABASE_MIN_CONNS"},
  "max conns below one":            {map[string]string{"DATABASE_MAX_CONNS": "0", "DATABASE_MIN_CONNS": "0"}, "DATABASE_MAX_CONNS"},
  "conn timeout not a number":      {map[string]string{"DATABASE_CONN_TIMEOUT": "30s"}, "DATABASE_CONN_TIMEOUT"},
  "database url malformed":         {map[string]string{"DATABASE_URL": "://bad"}, "DATABASE_URL"},
  "redis database not zero":        {map[string]string{"REDIS_DATABASE": "1"}, "REDIS_DATABASE"},
  "redis address without a port":   {map[string]string{"REDIS_ADDRESS": "localhost"}, "REDIS_ADDRESS"},
  "redis address second entry bad": {map[string]string{"REDIS_ADDRESS": "a:7001,b"}, "REDIS_ADDRESS"},
  "redis port out of range":        {map[string]string{"REDIS_ADDRESS": "a:99999"}, "REDIS_ADDRESS"},
  "shutdown timeout zero":          {map[string]string{"SHUTDOWN_TIMEOUT": "0"}, "SHUTDOWN_TIMEOUT"},
  "auth expiry zero":               {map[string]string{"AUTH_EXPIRE_DELTA": "0"}, "AUTH_EXPIRE_DELTA"},
 }

 for name, tc := range cases {
  t.Run(name, func(t *testing.T) {
   _, err := Load(lookupOf(tc.env))

   require.Error(t, err)
   assert.ErrorContains(t, err, tc.want)
  })
 }
}

func TestLoad_ReportsEveryProblemAtOnce(t *testing.T) {
 _, err := Load(lookupOf(map[string]string{"HTTP_PORT": "abc", "REDIS_DATABASE": "3", "AUTH_EXPIRE_DELTA": "-1"}))

 require.Error(t, err)
 assert.ErrorContains(t, err, "HTTP_PORT")
 assert.ErrorContains(t, err, "REDIS_DATABASE")
 assert.ErrorContains(t, err, "AUTH_EXPIRE_DELTA")
}

func TestLoad_RefusesDevelopmentSecretsOutsideDevelopmentAndTest(t *testing.T) {
 for _, environment := range []string{"production", "release", "staging"} {
  t.Run(environment, func(t *testing.T) {
   _, err := Load(lookupOf(map[string]string{"ENVIRONMENT": environment}))

   require.Error(t, err)
   for _, variable := range []string{"AUTH_SECRET", "DATABASE_PASSWORD", "REDIS_PASSWORD"} {
    assert.ErrorContains(t, err, variable)
   }
   for _, secret := range []string{"fupi-secret", "fupi-pass", "fupi-svc"} {
    assert.NotContains(t, err.Error(), secret, "an error must name the variable, never print a value")
   }
  })
 }
}

func TestLoad_AllowsDevelopmentSecretsInDevelopmentAndTest(t *testing.T) {
 for _, environment := range []string{"development", "test"} {
  _, err := Load(lookupOf(map[string]string{"ENVIRONMENT": environment}))
  assert.NoError(t, err, environment)
 }
}

func TestLoad_AcceptsOverriddenSecretsInProduction(t *testing.T) {
 _, err := Load(lookupOf(map[string]string{
  "ENVIRONMENT": "production", "AUTH_SECRET": "a-real-secret", "DATABASE_PASSWORD": "a-real-password", "REDIS_PASSWORD": "a-real-redis-password",
 }))

 assert.NoError(t, err)
}

// When DATABASE_URL is set the password lives in the URL, so that is where the development default is rejected.
func TestLoadDatabase_ChecksThePasswordInsideDatabaseURL(t *testing.T) {
 _, err := LoadDatabase(lookupOf(map[string]string{
  "ENVIRONMENT": "production", "DATABASE_URL": "postgres://fupi-user:fupi-pass@db:5432/fupidb",
 }))
 require.Error(t, err)
 assert.ErrorContains(t, err, "DATABASE_URL")
 assert.NotContains(t, err.Error(), "fupi-pass")

 _, err = LoadDatabase(lookupOf(map[string]string{
  "ENVIRONMENT": "production", "DATABASE_URL": "postgres://fupi-user:a-real-password@db:5432/fupidb",
 }))
 assert.NoError(t, err)
}

// The migrator loads only the database section, so it must not demand an AUTH_SECRET.
func TestLoadDatabase_DoesNotNeedAnAuthSecret(t *testing.T) {
 _, err := LoadDatabase(lookupOf(map[string]string{"ENVIRONMENT": "production", "DATABASE_PASSWORD": "a-real-password"}))

 assert.NoError(t, err)
}

func TestLoadMigrations(t *testing.T) {
 defaults, err := LoadMigrations(lookupOf(nil))
 require.NoError(t, err)
 assert.Equal(t, "app/internal/adapters/postgres/migrations", defaults.Path)

 custom, err := LoadMigrations(lookupOf(map[string]string{"MIGRATIONS_PATH": "/migrations"}))
 require.NoError(t, err)
 assert.Equal(t, "/migrations", custom.Path)
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./app/config/ 2>&1 | head -8`
Expected: build failure `undefined: Lookup`, `undefined: Load` (and the others).

- [ ] **Step 3: Implement the strict reader**

Create `app/config/loader.go`:

```go
package config

import (
 "errors"
 "fmt"
 "net"
 "strconv"
 "strings"
 "time"
)

const (
 environmentDevelopment = "development"
 environmentTest        = "test"

 // The development defaults. They match the local infrastructure stack and are refused outside development and test.
 devAuthSecret       = "fupi-secret"
 devDatabasePassword = "fupi-pass"
 devRedisPassword    = "fupi-svc"
)

// Lookup reads one environment variable. os.LookupEnv satisfies it; tests pass a map.
type Lookup func(key string) (string, bool)

// reader reads typed values through a Lookup and remembers every problem, so a bad environment is reported in full
// instead of one variable at a time. A value that is set but empty counts as unset, because `FOO=` in a .env file
// means "not set".
type reader struct {
 lookup Lookup
 errs   []error
}

func newReader(lookup Lookup) *reader { return &reader{lookup: lookup} }

func (r *reader) raw(key string) (string, bool) {
 value, ok := r.lookup(key)
 value = strings.TrimSpace(value)
 return value, ok && value != ""
}

func (r *reader) str(key, def string) string {
 if value, ok := r.raw(key); ok {
  return value
 }
 return def
}

func (r *reader) integer(key string, def int) int {
 value, ok := r.raw(key)
 if !ok {
  return def
 }
 number, err := strconv.Atoi(value)
 if err != nil {
  r.fail("%s must be an integer", key)
  return def
 }
 return number
}

// units reads a whole number of unit, so DATABASE_CONN_TIMEOUT=30 with time.Second is thirty seconds.
func (r *reader) units(key string, def int, unit time.Duration) time.Duration {
 return time.Duration(r.integer(key, def)) * unit
}

// port records a failure unless value is a TCP port number.
func (r *reader) port(key, value string) {
 number, err := strconv.Atoi(value)
 if err != nil || number < 1 || number > 65535 {
  r.fail("%s must be a port between 1 and 65535", key)
 }
}

// enforceSecrets is true for every environment except development and test (spec D6), so a forgotten variable in
// a real deployment fails at boot instead of running with a development default.
func (r *reader) enforceSecrets() bool {
 environment := r.str("ENVIRONMENT", environmentDevelopment)
 return environment != environmentDevelopment && environment != environmentTest
}

func (r *reader) fail(format string, args ...any) {
 r.errs = append(r.errs, fmt.Errorf(format, args...))
}

func (r *reader) err() error { return errors.Join(r.errs...) }

// splitList turns "a:1, b:2" into ["a:1", "b:2"], dropping empty entries.
func splitList(value string) []string {
 var items []string
 for _, item := range strings.Split(value, ",") {
  if item = strings.TrimSpace(item); item != "" {
   items = append(items, item)
  }
 }
 return items
}

// validAddress reports whether entry is host:port with a usable port.
func validAddress(entry string) bool {
 host, port, err := net.SplitHostPort(entry)
 if err != nil || host == "" {
  return false
 }
 number, err := strconv.Atoi(port)
 return err == nil && number >= 1 && number <= 65535
}
```

- [ ] **Step 4: Implement the loaders**

Create `app/config/app.go`:

```go
package config

import (
 "errors"
 "net/url"
 "time"

 "github.com/sanctumlabs/fupi/app/pkg/infra/cache/redis"
 "github.com/sanctumlabs/fupi/app/pkg/infra/database/postgres"
)

const defaultMigrationsPath = "app/internal/adapters/postgres/migrations"

// ServerSettings configures the HTTP server.
type ServerSettings struct {
 Host    string
 Port    int
 Header  string
 Name    string
 Version string
 // BaseURL is the externally visible address, used to build links in emails.
 BaseURL string
}

// DatabaseSettings configures Postgres: the client settings plus the per-operation timeout the datastores use.
type DatabaseSettings struct {
 Postgres         postgres.PostgresDatabaseConfig
 OperationTimeout time.Duration
}

// MigrationSettings says where the SQL migrations live. Only the migrator reads it.
type MigrationSettings struct {
 Path string
}

// App is everything the API process reads from its environment.
type App struct {
 Environment     string
 Server          ServerSettings
 Database        DatabaseSettings
 Redis           redis.RedisClientConfig
 Auth            AuthConfig
 ShutdownTimeout time.Duration
}

// Load reads and validates the whole application configuration. Every problem is reported, not just the first.
func Load(lookup Lookup) (App, error) {
 r := newReader(lookup)
 app := App{
  Environment:     r.str("ENVIRONMENT", environmentDevelopment),
  ShutdownTimeout: r.units("SHUTDOWN_TIMEOUT", 15, time.Second),
 }
 if app.ShutdownTimeout <= 0 {
  r.fail("SHUTDOWN_TIMEOUT must be greater than zero")
 }

 errs := []error{r.err()}
 var err error
 app.Server, err = LoadServer(lookup)
 errs = append(errs, err)
 app.Database, err = LoadDatabase(lookup)
 errs = append(errs, err)
 app.Redis, err = LoadRedis(lookup)
 errs = append(errs, err)
 app.Auth, err = LoadAuth(lookup)
 errs = append(errs, err)

 return app, errors.Join(errs...)
}

// LoadServer reads the HTTP server settings.
func LoadServer(lookup Lookup) (ServerSettings, error) {
 r := newReader(lookup)
 settings := ServerSettings{
  Host:    r.str("SERVER_HOST", "0.0.0.0"),
  Port:    r.integer("HTTP_PORT", 8085),
  Header:  r.str("SERVER_HEADER", "Fupi"),
  Name:    r.str("SERVER_NAME", "Fupi"),
  Version: r.str("SERVER_VERSION", "1.0.0"),
  BaseURL: r.str("APP_BASE_URL", "http://localhost:8085"),
 }
 if settings.Port < 1 || settings.Port > 65535 {
  r.fail("HTTP_PORT must be a port between 1 and 65535")
 }
 return settings, r.err()
}

// LoadDatabase reads the Postgres settings. The migrator uses only this loader, so it needs no AUTH_SECRET.
func LoadDatabase(lookup Lookup) (DatabaseSettings, error) {
 r := newReader(lookup)
 pg := postgres.PostgresDatabaseConfig{
  Host:            r.str("DATABASE_HOST", "localhost"),
  Port:            r.str("DATABASE_PORT", "5432"),
  Name:            r.str("DATABASE_NAME", "fupidb"),
  Username:        r.str("DATABASE_USERNAME", "fupi-user"),
  Password:        r.str("DATABASE_PASSWORD", devDatabasePassword),
  Url:             r.str("DATABASE_URL", ""),
  SslMode:         r.str("DATABASE_SSL_MODE", "disable"),
  MaxConns:        int32(r.integer("DATABASE_MAX_CONNS", 30)),
  MinConns:        int32(r.integer("DATABASE_MIN_CONNS", 5)),
  MaxConnLifetime: r.units("DATABASE_MAX_CONN_LIFETIME", 1, time.Hour),
  MaxConnIdleTime: r.units("DATABASE_MAX_CONN_IDLE_TIME", 30, time.Minute),
  ConnTimeout:     r.units("DATABASE_CONN_TIMEOUT", 30, time.Second),
  QueryTimeout:    r.units("DATABASE_QUERY_TIMEOUT", 10, time.Second),
 }
 settings := DatabaseSettings{
  Postgres:         pg,
  OperationTimeout: r.units("DATABASE_OPERATION_TIMEOUT", 30, time.Second),
 }

 r.port("DATABASE_PORT", pg.Port)
 if pg.MaxConns < 1 {
  r.fail("DATABASE_MAX_CONNS must be at least 1")
 }
 if pg.MinConns > pg.MaxConns {
  r.fail("DATABASE_MIN_CONNS must not exceed DATABASE_MAX_CONNS")
 }

 password := pg.Password
 if pg.Url != "" {
  parsed, err := url.Parse(pg.Url)
  if err != nil || parsed.Scheme == "" || parsed.Host == "" {
   // The parse error text can contain the URL, and with it the password, so it is not repeated.
   r.fail("DATABASE_URL is not a valid database URL")
  } else if parsed.User != nil {
   password, _ = parsed.User.Password()
  } else {
   password = ""
  }
 }
 if r.enforceSecrets() && password == devDatabasePassword {
  if pg.Url != "" {
   r.fail("DATABASE_URL must not use the development password when ENVIRONMENT is not development or test")
  } else {
   r.fail("DATABASE_PASSWORD must be set to a non-default value when ENVIRONMENT is not development or test")
  }
 }

 return settings, r.err()
}

// LoadRedis reads the Redis settings. REDIS_ADDRESS is a comma-separated host:port list: one entry gives a plain
// client, several give a cluster client.
func LoadRedis(lookup Lookup) (redis.RedisClientConfig, error) {
 r := newReader(lookup)
 cfg := redis.RedisClientConfig{
  Address:  splitList(r.str("REDIS_ADDRESS", "localhost:7001")),
  Username: r.str("REDIS_USERNAME", "fupi-svc"),
  Password: r.str("REDIS_PASSWORD", devRedisPassword),
  Database: r.integer("REDIS_DATABASE", 0),
 }

 if len(cfg.Address) == 0 {
  r.fail("REDIS_ADDRESS must list at least one host:port")
 }
 for _, entry := range cfg.Address {
  if !validAddress(entry) {
   r.fail("REDIS_ADDRESS entry %q must be host:port with a port between 1 and 65535", entry)
  }
 }
 if cfg.Database != 0 {
  r.fail("REDIS_DATABASE must be 0 (the Redis Cluster serves database 0 only)")
 }
 if r.enforceSecrets() && cfg.Password == devRedisPassword {
  r.fail("REDIS_PASSWORD must be set to a non-default value when ENVIRONMENT is not development or test")
 }

 return cfg, r.err()
}

// LoadAuth reads the JWT settings.
func LoadAuth(lookup Lookup) (AuthConfig, error) {
 r := newReader(lookup)
 cfg := AuthConfig{Jwt: Jwt{
  Secret:             r.str("AUTH_SECRET", devAuthSecret),
  Issuer:             r.str("AUTH_ISSUER", "fupi"),
  ExpireDelta:        r.integer("AUTH_EXPIRE_DELTA", 15),
  RefreshExpireDelta: r.integer("AUTH_REFRESH_EXPIRE_DELTA", 24),
 }}

 if cfg.ExpireDelta < 1 {
  r.fail("AUTH_EXPIRE_DELTA must be at least 1")
 }
 if cfg.RefreshExpireDelta < 1 {
  r.fail("AUTH_REFRESH_EXPIRE_DELTA must be at least 1")
 }
 if r.enforceSecrets() && cfg.Secret == devAuthSecret {
  r.fail("AUTH_SECRET must be set to a non-default value when ENVIRONMENT is not development or test")
 }

 return cfg, r.err()
}

// LoadMigrations reads where the SQL migrations live.
func LoadMigrations(lookup Lookup) (MigrationSettings, error) {
 r := newReader(lookup)
 return MigrationSettings{Path: r.str("MIGRATIONS_PATH", defaultMigrationsPath)}, r.err()
}
```

- [ ] **Step 5: Run the tests; expect only the `.env.example` test to fail**

Run: `gofmt -w app/config; go test ./app/config/ 2>&1 | tail -30`
Expected: every test passes except `TestLoad_EnvExampleDocumentsTheDefaults` (the current `.env.example` has `DATABASE_PORT=27017`, `AUTH_SECRET=<AUTH_SECRET>` and `REDIS_ADDRESS=localhost`). That failure is the RED for Step 6.

- [ ] **Step 6: Rewrite the application section of `.env.example`**

Edit `.env.example`. Replace the lines from `ENV=development` through `PORT=8085` (the first four lines) with:

```
ENVIRONMENT=development
LOG_LEVEL=debug
LOG_JSON_OUTPUT=true
HTTP_PORT=8085
APP_BASE_URL=http://localhost:8085
SHUTDOWN_TIMEOUT=15
```

Replace the `# Authentication configuration` block (the comment plus the four `AUTH_*` lines) with:

```
# Authentication configuration. Any ENVIRONMENT other than development or test refuses these development defaults.
AUTH_SECRET=fupi-secret
AUTH_ISSUER=fupi
AUTH_EXPIRE_DELTA=15
AUTH_REFRESH_EXPIRE_DELTA=24
```

Replace the five `DATABASE_*` lines (`DATABASE_HOST` through `DATABASE_USES_SRV`) with:

```
# Postgres: the primary (writes) on 5432 in both modes. DATABASE_URL, when set, wins over the parts below.
# DATABASE_USERNAME/DATABASE_PASSWORD must match PG_APP_USER/PG_APP_PASSWORD further down.
DATABASE_HOST=localhost
DATABASE_PORT=5432
DATABASE_NAME=fupidb
DATABASE_USERNAME=fupi-user
DATABASE_PASSWORD=fupi-pass
DATABASE_SSL_MODE=disable
# DATABASE_URL=postgres://fupi-user:fupi-pass@localhost:5432/fupidb?sslmode=disable
```

Replace the seven `REDIS_*` lines (`REDIS_ADDRESS` through `REDIS_MASTER_NAME`) with:

```
# Redis: a comma-separated host:port list. One address gives a plain client; HA needs all six seed nodes and
# /etc/hosts entries for redis-1..6 (run `make infra.hosts`):
#   REDIS_ADDRESS=localhost:7001,localhost:7002,localhost:7003,localhost:7004,localhost:7005,localhost:7006
REDIS_ADDRESS=localhost:7001
REDIS_USERNAME=fupi-svc
REDIS_PASSWORD=fupi-svc
REDIS_DATABASE=0
```

Leave `SENTRY_*`, the second `LOG_*` block, `METRICS_*` and the whole "Local infrastructure" section as they are.

- [ ] **Step 7: Run the tests and the stack's own checks**

Run: `gofmt -w app/config; go test ./app/config/ && make infra.config 2>&1 | tail -3 && bash scripts/infra_test.sh 2>&1 | tail -3`
Expected: `ok  github.com/sanctumlabs/fupi/app/config`; `ok: all profiles`; `all tests passed` (the env check and compose config must still accept the edited `.env.example`).

- [ ] **Step 8: Run the whole suite and commit**

Run: `go build ./... && go test ./... 2>&1 | grep -v "no test files" | tail -30`
Expected: every package `ok`.

```bash
git add app/config .env.example
git commit -m "$(cat <<'EOF'
feat(config): load and validate the application config strictly; align .env.example with the stack
EOF
)"
```

---

### Task 3: Redis client that builds without I/O, with `Ping` and `Close`

**Files:**

- Modify: `app/pkg/infra/cache/cache_client.go` (add `Ping`, `Close`)
- Modify: `app/pkg/infra/cache/redis/client.go` (constructor rewrite)
- Modify: `app/pkg/infra/cache/redis/config.go` (drop `Host`, `Port`)
- Modify: `app/pkg/infra/cache/redis/options.go` (drop `WithConnAttempts`)
- Regenerate: `app/pkg/infra/cache/mocks/cache_client_mock.go`
- Create: `app/pkg/infra/cache/redis/client_test.go`
- Replace: `app/pkg/infra/cache/redis/client_integration_test.go`

**Interfaces:**

- Consumes (Task 2): `config.LoadRedis` produces `redis.RedisClientConfig{Address, Username, Password, Database}`.
- Produces: `cache.CacheClient` gains `Ping(ctx context.Context) error` and `Close() error`; `redis.NewRedisClient(RedisClientConfig) (cache.CacheClient, error)` performs no I/O and errors only on an empty address list; `RedisClientConfig` loses `Host`/`Port`; the mock `mockcache.MockCacheClient` has `Ping`/`Close`. Task 5 calls `Ping`/`Close`.

**Task test command:** `go test ./app/pkg/infra/cache/... && go test -tags integration -count=1 ./app/pkg/infra/cache/redis/`

- [ ] **Step 1: Write the failing unit tests**

Create `app/pkg/infra/cache/redis/client_test.go`:

```go
package redis

import (
 "context"
 "testing"
 "time"

 redisGo "github.com/redis/go-redis/v9"
 "github.com/stretchr/testify/assert"
 "github.com/stretchr/testify/require"
)

func TestNewRedisClient_RejectsAnEmptyAddressList(t *testing.T) {
 _, err := NewRedisClient(RedisClientConfig{})

 require.Error(t, err)
}

// The client dials lazily and go-redis reconnects by itself, so building it must succeed while Redis is down.
func TestNewRedisClient_BuildsWithoutIOAndPingReportsAnUnreachableRedis(t *testing.T) {
 client, err := NewRedisClient(RedisClientConfig{Address: []string{"127.0.0.1:1"}})
 require.NoError(t, err)

 ctx, cancel := context.WithTimeout(context.Background(), time.Second)
 defer cancel()

 assert.Error(t, client.Ping(ctx))
 assert.NoError(t, client.Close())
}

func TestNewRedisClient_PicksTheClientTypeFromTheAddressCount(t *testing.T) {
 single, err := NewRedisClient(RedisClientConfig{Address: []string{"localhost:7001"}})
 require.NoError(t, err)
 assert.IsType(t, &redisGo.Client{}, single.(*redisClient).client)

 cluster, err := NewRedisClient(RedisClientConfig{Address: []string{"localhost:7001", "localhost:7002"}})
 require.NoError(t, err)
 assert.IsType(t, &redisGo.ClusterClient{}, cluster.(*redisClient).client)
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./app/pkg/infra/cache/redis/ -run TestNewRedisClient 2>&1 | head -10`
Expected: `client.Ping undefined` (the interface has no `Ping`) and, for the unreachable case, the old constructor returning an error. Note the package's existing integration test is tagged, so it does not interfere.

- [ ] **Step 3: Extend the interface**

In `app/pkg/infra/cache/cache_client.go` add before the closing brace of `CacheClient`:

```go

 // Ping checks that the cache is reachable
 Ping(ctx context.Context) error

 // Close releases the connections held by the client
 Close() error
```

- [ ] **Step 4: Rewrite the client constructor**

Replace the whole of `app/pkg/infra/cache/redis/client.go` with:

```go
package redis

import (
 "context"
 "errors"

 "github.com/google/wire"
 redisGo "github.com/redis/go-redis/v9"
 "github.com/sanctumlabs/fupi/app/pkg/infra/cache"
)

const _statsEnabled = true

// redisClient is a wrapper around a go-redis universal client
type redisClient struct {
 // statsEnabled sets enabling stats to true
 statsEnabled bool

 // marshalFunc a marshaling function that marshals/serializes a value into a byte slice
 marshalFunc func(any) ([]byte, error)

 // unmarshalFunc un-marshals a byte slice into a given payload type
 unmarshalFunc func([]byte, any) error

 // client
 client redisGo.UniversalClient
}

var (
 _             cache.CacheClient = (*redisClient)(nil)
 RedisCacheSet                   = wire.NewSet(NewRedisClient)
)

// NewRedisClient builds a client for the configured addresses: one address gives a plain client, several give a
// cluster client. It performs no I/O. go-redis dials lazily and reconnects by itself, so Redis may come up after the
// caller and the client picks it up; use Ping to check reachability.
func NewRedisClient(config RedisClientConfig) (cache.CacheClient, error) {
 if len(config.Address) == 0 {
  return nil, errors.New("redis: at least one address is required")
 }

 return &redisClient{
  client: redisGo.NewUniversalClient(&redisGo.UniversalOptions{
   Addrs:      config.Address,
   Username:   config.Username,
   Password:   config.Password,
   DB:         config.Database,
   MasterName: config.MasterName,
  }),
 }, nil
}

func (p *redisClient) Configure(opts ...Option) cache.CacheClient {
 for _, opt := range opts {
  opt(p)
 }

 return p
}

// Ping checks that Redis answers
func (rc *redisClient) Ping(ctx context.Context) error {
 return rc.client.Ping(ctx).Err()
}

// Close closes the connections held by the client
func (rc *redisClient) Close() error {
 return rc.client.Close()
}

// Set adds an item with a given key to the cache
func (rc *redisClient) Set(ctx context.Context, item cache.CacheItem, options ...cache.CacheItemOption) error {
 // apply optional options for caching item
 for _, option := range options {
  option(&item)
 }

 // cache the item
 statusCmd := rc.client.Set(ctx, item.Key, item.Value, item.TTL)

 if statusCmd.Err() != nil {
  return statusCmd.Err()
 }

 return nil
}

// Get retrieves a value from the cache with a given key
func (rc *redisClient) Get(ctx context.Context, key string) (cache.CacheItem, error) {
 statusCmd := rc.client.Get(ctx, key)
 err := statusCmd.Err()
 if err != nil {
  return cache.CacheItem{}, err
 }

 statusCmd.Val()

 item := cache.CacheItem{
  Key:   key,
  Value: statusCmd.Val(),
 }

 return item, nil
}

// Exists checks if a value for a given key exists in the cache
func (rc *redisClient) Exists(ctx context.Context, key string) bool {
 statusCmd := rc.client.Exists(ctx, key)
 return statusCmd.Val() > 0
}

// Delete deletes a value from the cache with a given key
func (rc *redisClient) Delete(ctx context.Context, key string) error {
 statusCmd := rc.client.Del(ctx, key)
 return statusCmd.Err()
}
```

In `config.go` delete the two fields `Host string ...` and `Port int ...` of `RedisClientConfig` (the log line that used them is gone). In `options.go` delete the `WithConnAttempts` function.

- [ ] **Step 5: Regenerate the mock**

Run from the `app` directory, with the same command recorded in the mock's header:

```bash
(cd app && mockgen -destination=pkg/infra/cache/mocks/cache_client_mock.go -package=mockcache -source=pkg/infra/cache/cache_client.go)
git diff --stat app/pkg/infra/cache/mocks
```

Expected: the diff only adds `Ping` and `Close` mock methods and recorders.

- [ ] **Step 6: Run the unit tests**

Run: `gofmt -w app/pkg/infra/cache; go build ./... && go test ./app/pkg/infra/cache/...`
Expected: `ok` for `.../cache/redis` and the other cache packages.

- [ ] **Step 7: Replace the stale integration test**

Overwrite `app/pkg/infra/cache/redis/client_integration_test.go` (it imports another project's `parksys/...` and does not compile) with:

```go
//go:build integration

package redis_test

import (
 "context"
 "strings"
 "testing"
 "time"

 "github.com/sanctumlabs/fupi/app/pkg/infra/cache"
 cacheredis "github.com/sanctumlabs/fupi/app/pkg/infra/cache/redis"
 "github.com/stretchr/testify/assert"
 "github.com/stretchr/testify/require"
 redisContainer "github.com/testcontainers/testcontainers-go/modules/redis"
)

func startRedis(t *testing.T) (cache.CacheClient, *redisContainer.RedisContainer) {
 t.Helper()
 ctx := context.Background()

 container, err := redisContainer.Run(ctx, "redis:7.4-alpine")
 require.NoError(t, err)
 t.Cleanup(func() { _ = container.Terminate(context.Background()) })

 uri, err := container.ConnectionString(ctx)
 require.NoError(t, err)

 client, err := cacheredis.NewRedisClient(cacheredis.RedisClientConfig{Address: []string{strings.TrimPrefix(uri, "redis://")}})
 require.NoError(t, err)
 t.Cleanup(func() { _ = client.Close() })

 return client, container
}

func TestRedisClient_SetGetExistsDelete(t *testing.T) {
 ctx := context.Background()
 client, _ := startRedis(t)

 require.NoError(t, client.Ping(ctx))
 require.NoError(t, client.Set(ctx, cache.CacheItem{Key: "short:abc", Value: "https://example.com"}))

 assert.True(t, client.Exists(ctx, "short:abc"))
 item, err := client.Get(ctx, "short:abc")
 require.NoError(t, err)
 assert.Equal(t, "https://example.com", item.Value)

 require.NoError(t, client.Delete(ctx, "short:abc"))
 assert.False(t, client.Exists(ctx, "short:abc"))
 _, err = client.Get(ctx, "short:abc")
 assert.Error(t, err, "a missing key is an error, not an empty item")
}

func TestRedisClient_ItemsExpireAfterTheirTTL(t *testing.T) {
 ctx := context.Background()
 client, _ := startRedis(t)

 require.NoError(t, client.Set(ctx, cache.CacheItem{Key: "k", Value: "v"}, cache.WithTTL(time.Second)))
 require.True(t, client.Exists(ctx, "k"))

 require.Eventually(t, func() bool { return !client.Exists(ctx, "k") }, 5*time.Second, 100*time.Millisecond)
}

func TestRedisClient_PingReportsAStoppedRedis(t *testing.T) {
 ctx := context.Background()
 client, container := startRedis(t)
 require.NoError(t, client.Ping(ctx))

 require.NoError(t, container.Stop(ctx, nil))

 pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
 defer cancel()
 assert.Error(t, client.Ping(pingCtx))
}
```

- [ ] **Step 8: Run the integration tests**

Run: `gofmt -w app/pkg/infra/cache; go vet -tags integration ./app/pkg/infra/cache/... && go test -tags integration -count=1 ./app/pkg/infra/cache/redis/`
Expected: `ok` (Docker must be running; the first run pulls `redis:7.4-alpine`).

- [ ] **Step 9: Run the whole suite and commit**

Run: `go test ./... 2>&1 | grep -v "no test files" | tail -30; git status --short go.mod go.sum`
Expected: every package `ok`; `go.mod`/`go.sum` unchanged.

```bash
git add app/pkg/infra/cache
git commit -m "$(cat <<'EOF'
fix(cache): build the Redis client without I/O and add Ping and Close

EOF
)"
```

---

### Task 4: Health registry and the probe endpoints

**Files:**

- Create: `app/pkg/infra/monitoring/health/registry.go`
- Create: `app/pkg/infra/monitoring/health/registry_test.go`
- Create: `app/api/probes/router.go`
- Create: `app/api/probes/router_test.go`

**Interfaces:**

- Consumes: `router.Router`, `router.NewGetRoute` (existing, `app/pkg/infra/server/router`).
- Produces:
  - `health.Check{Name string; Required bool; Fn func(ctx context.Context) error}`
  - `health.Status` (`StatusOK`, `StatusDegraded`, `StatusUnavailable`, `StatusDraining`), `health.Report{Status Status; Checks map[string]string}` with `Ready() bool`
  - `health.NewRegistry(timeout time.Duration) *Registry`, `(*Registry).Add(Check)`, `.SetDraining()`, `.Run(ctx) Report`, `health.DefaultCheckTimeout = 2 * time.Second`
  - `probes.NewRouter(registry *health.Registry) router.Router`, `probes.LivePath = "/health"`, `probes.ReadyPath = "/health/ready"`
  - Task 5 consumes all of these.

**Task test command:** `go test -race ./app/pkg/infra/monitoring/health/ ./app/api/probes/`

- [ ] **Step 1: Write the failing registry tests**

Create `app/pkg/infra/monitoring/health/registry_test.go`:

```go
package health

import (
 "context"
 "errors"
 "sync/atomic"
 "testing"
 "time"

 "github.com/stretchr/testify/assert"
 "github.com/stretchr/testify/require"
)

func up(context.Context) error   { return nil }
func down(context.Context) error { return errors.New("connection refused") }

func TestRegistry_StatusFollowsWhichChecksAreDown(t *testing.T) {
 cases := map[string]struct {
  checks     []Check
  wantStatus Status
  wantChecks map[string]string
  wantReady  bool
 }{
  "no checks":              {nil, StatusOK, map[string]string{}, true},
  "all up":                 {[]Check{{"postgres", true, up}, {"redis", false, up}}, StatusOK, map[string]string{"postgres": "up", "redis": "up"}, true},
  "optional down":          {[]Check{{"postgres", true, up}, {"redis", false, down}}, StatusDegraded, map[string]string{"postgres": "up", "redis": "down"}, true},
  "required down":          {[]Check{{"postgres", true, down}, {"redis", false, up}}, StatusUnavailable, map[string]string{"postgres": "down", "redis": "up"}, false},
  "required and optional":  {[]Check{{"postgres", true, down}, {"redis", false, down}}, StatusUnavailable, map[string]string{"postgres": "down", "redis": "down"}, false},
 }

 for name, tc := range cases {
  t.Run(name, func(t *testing.T) {
   registry := NewRegistry(time.Second)
   for _, check := range tc.checks {
    registry.Add(check)
   }

   report := registry.Run(context.Background())

   assert.Equal(t, tc.wantStatus, report.Status)
   assert.Equal(t, tc.wantChecks, report.Checks)
   assert.Equal(t, tc.wantReady, report.Ready())
  })
 }
}

// A dependency that never answers and ignores its context must not hang the readiness endpoint.
func TestRegistry_AHangingCheckCannotHangTheReport(t *testing.T) {
 release := make(chan struct{})
 t.Cleanup(func() { close(release) })
 registry := NewRegistry(50 * time.Millisecond)
 registry.Add(Check{Name: "postgres", Required: true, Fn: func(context.Context) error { <-release; return nil }})

 start := time.Now()
 report := registry.Run(context.Background())

 assert.Less(t, time.Since(start), 2*time.Second)
 assert.Equal(t, StatusUnavailable, report.Status)
 assert.Equal(t, "down", report.Checks["postgres"])
}

// The checks run in parallel: each waits until the other has started, so a sequential runner would time out.
func TestRegistry_RunsTheChecksInParallel(t *testing.T) {
 started := make(chan struct{}, 2)
 release := make(chan struct{})
 waiting := func(ctx context.Context) error {
  started <- struct{}{}
  select {
  case <-release:
   return nil
  case <-ctx.Done():
   return ctx.Err()
  }
 }
 registry := NewRegistry(5 * time.Second)
 registry.Add(Check{Name: "a", Required: true, Fn: waiting})
 registry.Add(Check{Name: "b", Required: false, Fn: waiting})

 reports := make(chan Report, 1)
 go func() { reports <- registry.Run(context.Background()) }()

 for range 2 {
  select {
  case <-started:
  case <-time.After(2 * time.Second):
   t.Fatal("the second check did not start while the first was still running")
  }
 }
 close(release)

 assert.Equal(t, StatusOK, (<-reports).Status)
}

func TestRegistry_DrainingReportsUnavailableWithoutRunningTheChecks(t *testing.T) {
 var calls atomic.Int32
 registry := NewRegistry(time.Second)
 registry.Add(Check{Name: "postgres", Required: true, Fn: func(context.Context) error { calls.Add(1); return nil }})

 registry.SetDraining()
 report := registry.Run(context.Background())

 assert.Equal(t, StatusDraining, report.Status)
 assert.False(t, report.Ready())
 assert.Zero(t, calls.Load(), "a draining process must not spend time probing its dependencies")
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./app/pkg/infra/monitoring/health/ 2>&1 | head -6`
Expected: build failure `undefined: Check`, `undefined: NewRegistry`, and so on.

- [ ] **Step 3: Implement the registry**

Create `app/pkg/infra/monitoring/health/registry.go`:

```go
package health

import (
 "context"
 "log/slog"
 "sync"
 "sync/atomic"
 "time"
)

// DefaultCheckTimeout bounds each dependency check, so a hung dependency cannot hang the readiness endpoint.
const DefaultCheckTimeout = 2 * time.Second

// Status is the overall readiness verdict.
type Status string

const (
 // StatusOK means every check is up.
 StatusOK Status = "ok"
 // StatusDegraded means only optional checks are down; the process can still serve traffic.
 StatusDegraded Status = "degraded"
 // StatusUnavailable means a required check is down.
 StatusUnavailable Status = "unavailable"
 // StatusDraining means the process is shutting down and wants no new traffic.
 StatusDraining Status = "draining"
)

// Check probes one dependency. A Required check that fails makes the process not ready; an optional one only
// degrades it.
type Check struct {
 Name     string
 Required bool
 Fn       func(ctx context.Context) error
}

// Report is the readiness result. Checks maps a check name to "up" or "down". It never carries error text: the
// endpoint is public, and failures are logged server-side instead.
type Report struct {
 Status Status            `json:"status"`
 Checks map[string]string `json:"checks"`
}

// Ready reports whether traffic should be sent to the process.
func (r Report) Ready() bool { return r.Status == StatusOK || r.Status == StatusDegraded }

// Registry runs the registered checks. Add every check before serving; Add is not safe for concurrent use.
type Registry struct {
 timeout  time.Duration
 checks   []Check
 draining atomic.Bool
}

// NewRegistry creates a registry whose checks each get at most timeout.
func NewRegistry(timeout time.Duration) *Registry {
 return &Registry{timeout: timeout}
}

// Add registers a check.
func (r *Registry) Add(check Check) {
 r.checks = append(r.checks, check)
}

// SetDraining flips readiness to "draining" for the rest of the process's life. Call it when shutdown begins.
func (r *Registry) SetDraining() {
 slog.Info("readiness: draining, no longer accepting new traffic")
 r.draining.Store(true)
}

// Run executes every check in parallel, each bounded by the registry's timeout.
func (r *Registry) Run(ctx context.Context) Report {
 if r.draining.Load() {
  return Report{Status: StatusDraining, Checks: map[string]string{}}
 }

 errs := make([]error, len(r.checks))
 var wg sync.WaitGroup
 for i, check := range r.checks {
  wg.Add(1)
  go func() {
   defer wg.Done()
   errs[i] = r.runOne(ctx, check)
  }()
 }
 wg.Wait()

 report := Report{Status: StatusOK, Checks: make(map[string]string, len(r.checks))}
 for i, check := range r.checks {
  if errs[i] == nil {
   report.Checks[check.Name] = "up"
   continue
  }
  report.Checks[check.Name] = "down"
  slog.Warn("health check failed", "check", check.Name, "required", check.Required, "error", errs[i])
  if check.Required {
   report.Status = StatusUnavailable
  } else if report.Status == StatusOK {
   report.Status = StatusDegraded
  }
 }
 return report
}

// runOne runs a check in its own goroutine and gives up after the timeout even if the check ignores its context. A
// check that never returns leaks its goroutine; that is the price of a bounded readiness endpoint.
func (r *Registry) runOne(ctx context.Context, check Check) error {
 ctx, cancel := context.WithTimeout(ctx, r.timeout)
 defer cancel()

 done := make(chan error, 1)
 go func() { done <- check.Fn(ctx) }()

 select {
 case err := <-done:
  return err
 case <-ctx.Done():
  return ctx.Err()
 }
}
```

- [ ] **Step 4: Run the registry tests**

Run: `gofmt -w app/pkg/infra/monitoring/health; go test -race ./app/pkg/infra/monitoring/health/`
Expected: `ok`.

- [ ] **Step 5: Write the failing probe tests**

Create `app/api/probes/router_test.go`:

```go
package probes

import (
 "context"
 "encoding/json"
 "errors"
 "io"
 "net/http/httptest"
 "testing"
 "time"

 "github.com/gofiber/fiber/v2"
 "github.com/sanctumlabs/fupi/app/pkg/infra/monitoring/health"
 "github.com/stretchr/testify/assert"
 "github.com/stretchr/testify/require"
)

func appWith(registry *health.Registry) *fiber.App {
 app := fiber.New()
 for _, route := range NewRouter(registry).Routes() {
  app.Add(route.Method(), route.Path(), route.Handler())
 }
 return app
}

func get(t *testing.T, app *fiber.App, path string) (int, string) {
 t.Helper()
 resp, err := app.Test(httptest.NewRequest("GET", path, nil))
 require.NoError(t, err)
 defer resp.Body.Close()
 body, err := io.ReadAll(resp.Body)
 require.NoError(t, err)
 return resp.StatusCode, string(body)
}

func registryWith(postgres, redis error) *health.Registry {
 registry := health.NewRegistry(time.Second)
 registry.Add(health.Check{Name: "postgres", Required: true, Fn: func(context.Context) error { return postgres }})
 registry.Add(health.Check{Name: "redis", Required: false, Fn: func(context.Context) error { return redis }})
 return registry
}

func TestLiveness_IsAlwaysOkWhileTheProcessRuns(t *testing.T) {
 registry := registryWith(errors.New("down"), errors.New("down"))
 registry.SetDraining()

 status, body := get(t, appWith(registry), LivePath)

 assert.Equal(t, 200, status)
 assert.JSONEq(t, `{"status":"ok"}`, body)
}

func TestReadiness_ReportsEachDependency(t *testing.T) {
 cases := map[string]struct {
  registry   *health.Registry
  wantStatus int
  wantBody   string
 }{
  "all up":        {registryWith(nil, nil), 200, `{"status":"ok","checks":{"postgres":"up","redis":"up"}}`},
  "redis down":    {registryWith(nil, errors.New("x")), 200, `{"status":"degraded","checks":{"postgres":"up","redis":"down"}}`},
  "postgres down": {registryWith(errors.New("x"), nil), 503, `{"status":"unavailable","checks":{"postgres":"down","redis":"up"}}`},
 }

 for name, tc := range cases {
  t.Run(name, func(t *testing.T) {
   status, body := get(t, appWith(tc.registry), ReadyPath)

   assert.Equal(t, tc.wantStatus, status)
   assert.JSONEq(t, tc.wantBody, body)
  })
 }
}

func TestReadiness_ReturnsServiceUnavailableWhileDraining(t *testing.T) {
 registry := registryWith(nil, nil)
 registry.SetDraining()

 status, body := get(t, appWith(registry), ReadyPath)

 assert.Equal(t, 503, status)
 assert.JSONEq(t, `{"status":"draining","checks":{}}`, body)
}

// The endpoint is public, so a failing dependency's error text (addresses, user names) must stay in the log.
func TestReadiness_NeverLeaksErrorText(t *testing.T) {
 leak := errors.New("dial tcp 10.0.0.5:5432: password authentication failed for user fupi-user")

 _, body := get(t, appWith(registryWith(leak, leak)), ReadyPath)

 assert.NotContains(t, body, "10.0.0.5")
 assert.NotContains(t, body, "password")
 assert.NotContains(t, body, "fupi-user")
 var decoded map[string]any
 require.NoError(t, json.Unmarshal([]byte(body), &decoded))
}
```

- [ ] **Step 6: Run them to verify they fail, then implement the router**

Run: `go test ./app/api/probes/ 2>&1 | head -6`
Expected: build failure `undefined: NewRouter`, `undefined: LivePath`.

Create `app/api/probes/router.go`:

```go
// Package probes exposes the liveness and readiness endpoints that orchestrators and load balancers poll.
package probes

import (
 "github.com/gofiber/fiber/v2"
 "github.com/sanctumlabs/fupi/app/pkg/infra/monitoring/health"
 "github.com/sanctumlabs/fupi/app/pkg/infra/server/router"
)

const (
 // LivePath answers 200 while the process runs.
 LivePath = "/health"
 // ReadyPath answers 200 when the process can serve traffic and 503 when it cannot or is draining.
 ReadyPath = "/health/ready"
)

type probesRouter struct {
 registry *health.Registry
 routes   []router.Route
}

// NewRouter creates the liveness and readiness routes. Both are unauthenticated, so list them as public paths in the
// auth middleware.
func NewRouter(registry *health.Registry) router.Router {
 r := &probesRouter{registry: registry}
 r.routes = []router.Route{
  router.NewGetRoute(LivePath, r.live),
  router.NewGetRoute(ReadyPath, r.ready),
 }
 return r
}

func (r *probesRouter) Routes() []router.Route { return r.routes }

func (r *probesRouter) live(ctx *fiber.Ctx) error {
 return ctx.JSON(fiber.Map{"status": "ok"})
}

func (r *probesRouter) ready(ctx *fiber.Ctx) error {
 report := r.registry.Run(ctx.UserContext())
 status := fiber.StatusOK
 if !report.Ready() {
  status = fiber.StatusServiceUnavailable
 }
 return ctx.Status(status).JSON(report)
}
```

- [ ] **Step 7: Run the tests, the whole suite, and commit**

Run: `gofmt -w app/api/probes app/pkg/infra/monitoring; go test -race ./app/pkg/infra/monitoring/health/ ./app/api/probes/ && go test ./... 2>&1 | grep -v "no test files" | tail -30`
Expected: `ok` everywhere.

```bash
git add app/pkg/infra/monitoring/health/registry.go app/pkg/infra/monitoring/health/registry_test.go app/api/probes
git commit -m "$(cat <<'EOF'
feat(health): add a readiness registry and the /health and /health/ready endpoints
EOF
)"
```

---

### Task 5: Graceful serve and the `run` function

**Files:**

- Modify: `app/pkg/infra/server/server.go` (add `Serve`, `ServeListener`)
- Modify: `app/pkg/infra/server/server_test.go` (add shutdown tests)
- Rewrite: `app/cmd/main.go`
- Create: `app/cmd/main_test.go`
- Create: `app/cmd/main_integration_test.go`

**Interfaces:**

- Consumes: Task 2 `config.Load`/`config.App`; Task 3 `cache.CacheClient.Ping/Close` and `redis.NewRedisClient`; Task 4 `health.NewRegistry/Check/DefaultCheckTimeout`, `probes.NewRouter/LivePath/ReadyPath`; Task 1 `postgres.NewPostgresClient` (already uses `ConnectionString`).
- Produces: `(*server.Server).Serve(ctx, timeout, onDrain) error`, `(*server.Server).ServeListener(ctx, ln net.Listener, timeout, onDrain) error`; `run(ctx context.Context, cfg config.App) error` in `package main`.

**Task test command:** `go test ./app/pkg/infra/server/ ./app/cmd/ && go test -tags integration -count=1 -run TestRun ./app/cmd/`

- [ ] **Step 1: Write the failing server tests**

Append to `app/pkg/infra/server/server_test.go` (add the imports `context`, `io`, `net`, `net/http`, `sync/atomic`, `time` to the existing import block):

```go
func listenOnFreePort(t *testing.T) net.Listener {
 t.Helper()
 ln, err := net.Listen("tcp", "127.0.0.1:0")
 require.NoError(t, err)
 return ln
}

func waitFor(t *testing.T, ch <-chan struct{}, what string) {
 t.Helper()
 select {
 case <-ch:
 case <-time.After(5 * time.Second):
  t.Fatalf("timed out waiting for %s", what)
 }
}

// Cancelling the context calls onDrain first, lets the in-flight request finish, and only then returns.
func TestServe_FinishesInFlightRequestsBeforeReturning(t *testing.T) {
 srv := NewServer(ServerConfig{AppName: "fupi-test"})
 started := make(chan struct{})
 release := make(chan struct{})
 srv.RegisterHandlers([]router.Router{stubRouter{routes: []router.Route{
  router.NewGetRoute("/slow", func(c *fiber.Ctx) error {
   close(started)
   <-release
   return c.SendString("done")
  }),
 }}})
 ln := listenOnFreePort(t)

 ctx, cancel := context.WithCancel(context.Background())
 defer cancel()
 var drained atomic.Bool
 served := make(chan error, 1)
 go func() {
  served <- srv.ServeListener(ctx, ln, 5*time.Second, func() { drained.Store(true) })
 }()

 type response struct {
  body string
  err  error
 }
 responses := make(chan response, 1)
 go func() {
  resp, err := http.Get("http://" + ln.Addr().String() + "/slow")
  if err != nil {
   responses <- response{err: err}
   return
  }
  defer resp.Body.Close()
  body, _ := io.ReadAll(resp.Body)
  responses <- response{body: string(body)}
 }()

 waitFor(t, started, "the request to reach its handler")
 cancel() // the SIGTERM

 require.Eventually(t, drained.Load, 2*time.Second, 5*time.Millisecond, "onDrain must run when the context is cancelled")
 select {
 case err := <-served:
  t.Fatalf("Serve returned (%v) while a request was still in flight", err)
 case <-time.After(150 * time.Millisecond):
 }

 close(release)
 got := <-responses
 require.NoError(t, got.err)
 assert.Equal(t, "done", got.body)
 require.NoError(t, <-served)
}

// A request that never finishes must not hold the process forever: shutdown gives up at the timeout and says so.
func TestServe_ReturnsAnErrorWhenInFlightRequestsOutlastTheTimeout(t *testing.T) {
 srv := NewServer(ServerConfig{AppName: "fupi-test"})
 started := make(chan struct{})
 release := make(chan struct{})
 t.Cleanup(func() { close(release) })
 srv.RegisterHandlers([]router.Router{stubRouter{routes: []router.Route{
  router.NewGetRoute("/stuck", func(c *fiber.Ctx) error {
   close(started)
   <-release
   return nil
  }),
 }}})
 ln := listenOnFreePort(t)

 ctx, cancel := context.WithCancel(context.Background())
 defer cancel()
 served := make(chan error, 1)
 go func() { served <- srv.ServeListener(ctx, ln, 100*time.Millisecond, nil) }()
 go func() { _, _ = http.Get("http://" + ln.Addr().String() + "/stuck") }()

 waitFor(t, started, "the request to reach its handler")
 cancel()

 select {
 case err := <-served:
  assert.Error(t, err)
 case <-time.After(5 * time.Second):
  t.Fatal("Serve did not give up at the shutdown timeout")
 }
}

func TestServe_ReturnsTheListenerError(t *testing.T) {
 srv := NewServer(ServerConfig{AppName: "fupi-test"})
 ln := listenOnFreePort(t)
 require.NoError(t, ln.Close())

 err := srv.ServeListener(context.Background(), ln, time.Second, nil)

 assert.Error(t, err)
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./app/pkg/infra/server/ 2>&1 | head -6`
Expected: build failure `srv.ServeListener undefined`.

- [ ] **Step 3: Implement `Serve` and `ServeListener`**

In `app/pkg/infra/server/server.go` add `"context"`, `"net"` and `"time"` to the imports, and add after the existing `Listen` method (leave `Listen` and `Shutdown` as they are):

```go
// Serve listens on the configured port and blocks until ctx is cancelled or the listener fails. See ServeListener.
func (srv *Server) Serve(ctx context.Context, timeout time.Duration, onDrain func()) error {
 ln, err := net.Listen("tcp", fmt.Sprintf(":%d", srv.cfg.Port))
 if err != nil {
  return fmt.Errorf("listen on port %d: %w", srv.cfg.Port, err)
 }

 srv.log.Infow("Listening on port", "port", srv.cfg.Port)
 return srv.ServeListener(ctx, ln, timeout, onDrain)
}

// ServeListener serves on ln until ctx is cancelled or the listener fails. When ctx is cancelled it calls onDrain
// (use it to turn readiness to 503), stops accepting connections, and waits up to timeout for in-flight requests to
// finish. It returns nil after a clean drain and an error if the listener failed or the deadline passed first.
func (srv *Server) ServeListener(ctx context.Context, ln net.Listener, timeout time.Duration, onDrain func()) error {
 listenErr := make(chan error, 1)
 go func() { listenErr <- srv.app.Listener(ln) }()

 select {
 case err := <-listenErr:
  return err
 case <-ctx.Done():
 }

 if onDrain != nil {
  onDrain()
 }
 srv.log.Infow("shutting down server", "timeout", timeout.String())
 if err := srv.app.ShutdownWithTimeout(timeout); err != nil {
  return fmt.Errorf("shut down within %s: %w", timeout, err)
 }
 return nil
}
```

- [ ] **Step 4: Run the server tests**

Run: `gofmt -w app/pkg/infra/server; go test -race -count=1 ./app/pkg/infra/server/`
Expected: `ok`. If `TestServe_ReturnsTheListenerError` fails because `ServeListener` returned nil, Fiber reports no error for a closed listener: stop and debug with `superpowers:systematic-debugging` (do not weaken the test).

- [ ] **Step 5: Write the failing `run` test**

Create `app/cmd/main_test.go`:

```go
package main

import (
 "context"
 "testing"
 "time"

 "github.com/sanctumlabs/fupi/app/config"
 "github.com/stretchr/testify/assert"
 "github.com/stretchr/testify/require"
)

func lookupOf(env map[string]string) config.Lookup {
 return func(key string) (string, bool) {
  value, ok := env[key]
  return value, ok
 }
}

// Postgres is the one required dependency: with it unreachable the process must exit with an error, not hang or serve.
func TestRun_ReturnsAnErrorWhenPostgresIsUnreachable(t *testing.T) {
 if testing.Short() {
  t.Skip("takes about three seconds of connection retries")
 }
 cfg, err := config.Load(lookupOf(map[string]string{
  "DATABASE_PORT": "1", "DATABASE_CONN_TIMEOUT": "1", "REDIS_ADDRESS": "127.0.0.1:1",
 }))
 require.NoError(t, err)

 ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
 defer cancel()

 err = run(ctx, cfg)

 require.Error(t, err)
 assert.ErrorContains(t, err, "postgres")
}
```

- [ ] **Step 6: Run it to verify it fails**

Run: `go test ./app/cmd/ 2>&1 | head -6`
Expected: build failure `undefined: run`.

- [ ] **Step 7: Rewrite `app/cmd/main.go`**

Replace the whole file with:

```go
// Command fupi runs the Fupi HTTP API.
//
// Only the Identity bounded context is wired up so far; the URL context follows once its
// application layer exists.
package main

import (
 "context"
 "fmt"
 "log/slog"
 "os"
 "os/signal"
 "syscall"
 "time"

 "github.com/joho/godotenv"
 "github.com/sanctumlabs/fupi/app/api/probes"
 apimiddleware "github.com/sanctumlabs/fupi/app/api/middleware"
 identityapi "github.com/sanctumlabs/fupi/app/api/v1/identity"
 "github.com/sanctumlabs/fupi/app/config"
 "github.com/sanctumlabs/fupi/app/internal/adapters/jwtauth"
 "github.com/sanctumlabs/fupi/app/internal/adapters/notifications"
 identitydatastore "github.com/sanctumlabs/fupi/app/internal/adapters/postgres/identity"
 identityapp "github.com/sanctumlabs/fupi/app/internal/application/identity"
 cacheredis "github.com/sanctumlabs/fupi/app/pkg/infra/cache/redis"
 "github.com/sanctumlabs/fupi/app/pkg/infra/database"
 "github.com/sanctumlabs/fupi/app/pkg/infra/database/postgres"
 "github.com/sanctumlabs/fupi/app/pkg/infra/monitoring/health"
 "github.com/sanctumlabs/fupi/app/pkg/infra/server"
 "github.com/sanctumlabs/fupi/app/pkg/infra/server/router"
 "github.com/sanctumlabs/fupi/app/pkg/jwt"
 recoveryutils "github.com/sanctumlabs/fupi/app/pkg/utils/recover"
)

const (
 baseURI = "/api/v1/fupi"

 // redisStartupPingTimeout bounds the one ping that only decides which startup line is logged.
 redisStartupPingTimeout = 2 * time.Second
)

func main() {
 if err := godotenv.Load(); err != nil {
  slog.Warn("no .env file found, relying on the environment", "error", err)
 }

 cfg, err := config.Load(os.LookupEnv)
 if err != nil {
  slog.Error("invalid configuration", "error", err)
  os.Exit(1)
 }

 ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
 go func() {
  <-ctx.Done()
  // Hand signal handling back to the runtime as soon as the first signal arrives, so a second Ctrl-C ends a
  // stuck drain immediately instead of being swallowed.
  stop()
 }()

 err = run(ctx, cfg)
 stop()
 if err != nil {
  slog.Error("fupi stopped", "error", err)
  os.Exit(1)
 }
}

// run builds the API from cfg and serves it until ctx is cancelled, then drains in-flight requests and closes the
// data clients. Postgres is required: failing to reach it is an error. Redis is optional: failing to reach it is
// logged and the API carries on (readiness reports it as down).
func run(ctx context.Context, cfg config.App) error {
 dbClient, err := postgres.NewPostgresClient(cfg.Database.Postgres)
 if err != nil {
  return fmt.Errorf("connect to postgres: %w", err)
 }
 defer dbClient.Close()

 cache, err := cacheredis.NewRedisClient(cfg.Redis)
 if err != nil {
  return fmt.Errorf("create redis client: %w", err)
 }
 defer func() {
  if closeErr := cache.Close(); closeErr != nil {
   slog.Warn("closing redis", "error", closeErr)
  }
 }()

 pingCtx, cancel := context.WithTimeout(ctx, redisStartupPingTimeout)
 if pingErr := cache.Ping(pingCtx); pingErr != nil {
  slog.Warn("redis is down, continuing without it", "addresses", cfg.Redis.Address, "error", pingErr)
 } else {
  slog.Info("redis is up", "addresses", cfg.Redis.Address)
 }
 cancel()

 registry := health.NewRegistry(health.DefaultCheckTimeout)
 registry.Add(health.Check{Name: "postgres", Required: true, Fn: dbClient.HealthCheck})
 registry.Add(health.Check{Name: "redis", Required: false, Fn: cache.Ping})

 dbConfig := database.Config{
  OperationTimeout: cfg.Database.OperationTimeout,
  RetryConfig:      recoveryutils.DefaultRetryConfig,
 }

 tokenService := jwtauth.NewTokenService(cfg.Auth, jwt.New())

 // No email transport is configured yet, so verification links are logged rather than sent.
 notifier := notifications.NewEmailNotifier(cfg.Server.BaseURL, notifications.NewLoggingEmailSender())

 identityService := identityapp.NewService(
  identitydatastore.NewUserDatastoreAdapter(dbClient, dbConfig),
  tokenService,
  notifier,
 )

 srv := server.NewServer(server.ServerConfig{
  Header:      cfg.Server.Header,
  Host:        cfg.Server.Host,
  Port:        cfg.Server.Port,
  AppName:     cfg.Server.Name,
  Version:     cfg.Server.Version,
  Environment: cfg.Environment,
 })

 // Everything is authenticated unless it is listed here. Registration, login, token refresh
 // and email verification must be reachable without a token, by definition, and so must the probes.
 srv.Use(apimiddleware.AuthMiddleware(apimiddleware.AuthConfig{
  TokenService: tokenService,
  PublicPaths: []string{
   baseURI + "/auth/register",
   baseURI + "/auth/login",
   baseURI + "/auth/oauth/token",
   baseURI + "/auth/verify",
   probes.LivePath,
   probes.ReadyPath,
   "/metrics",
  },
  PublicPrefixes: []string{"/docs/"},
 }))

 srv.RegisterHandlers([]router.Router{
  probes.NewRouter(registry),
  identityapi.NewRouter(baseURI, identityService),
 })

 return srv.Serve(ctx, cfg.ShutdownTimeout, registry.SetDraining)
}
```

- [ ] **Step 8: Run the unit tests and build**

Run: `gofmt -w app/cmd; go build ./... && go vet ./app/cmd/ && go test -count=1 ./app/pkg/infra/server/ ./app/cmd/`
Expected: build and vet clean; both packages `ok` (the `app/cmd` test takes about three seconds).

- [ ] **Step 9: Write and run the integration test for the success path**

Create `app/cmd/main_integration_test.go`:

```go
//go:build integration

package main

import (
 "context"
 "encoding/json"
 "fmt"
 "net"
 "net/http"
 "strconv"
 "testing"
 "time"

 "github.com/sanctumlabs/fupi/app/config"
 "github.com/sanctumlabs/fupi/app/test"
 "github.com/stretchr/testify/assert"
 "github.com/stretchr/testify/require"
)

func freePort(t *testing.T) int {
 t.Helper()
 ln, err := net.Listen("tcp", "127.0.0.1:0")
 require.NoError(t, err)
 defer ln.Close()
 return ln.Addr().(*net.TCPAddr).Port
}

// With Postgres up and Redis down the API serves, reports itself degraded, and stops cleanly when its context ends.
func TestRun_ServesReadinessAndStopsCleanlyOnCancel(t *testing.T) {
 ctx := context.Background()
 container, err := test.TestPostgresDatabaseContainer(ctx, test.DefaultTestDatabaseConfig())
 require.NoError(t, err)
 t.Cleanup(func() { _ = container.Terminate(context.Background()) })
 connectionString, err := container.ConnectionString(ctx, "sslmode=disable")
 require.NoError(t, err)
 require.NoError(t, test.RunDatabaseMigration(ctx, connectionString))

 port := freePort(t)
 cfg, err := config.Load(lookupOf(map[string]string{
  "DATABASE_URL":  connectionString,
  "HTTP_PORT":     strconv.Itoa(port),
  "REDIS_ADDRESS": "127.0.0.1:1",
 }))
 require.NoError(t, err)

 runCtx, cancel := context.WithCancel(ctx)
 defer cancel()
 done := make(chan error, 1)
 go func() { done <- run(runCtx, cfg) }()

 base := fmt.Sprintf("http://127.0.0.1:%d", port)
 require.Eventually(t, func() bool {
  resp, err := http.Get(base + "/health/ready")
  if err != nil {
   return false
  }
  defer resp.Body.Close()
  return resp.StatusCode == http.StatusOK
 }, 30*time.Second, 200*time.Millisecond, "the API never became ready")

 resp, err := http.Get(base + "/health/ready")
 require.NoError(t, err)
 var body struct {
  Status string            `json:"status"`
  Checks map[string]string `json:"checks"`
 }
 require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
 resp.Body.Close()
 assert.Equal(t, "degraded", body.Status)
 assert.Equal(t, "up", body.Checks["postgres"])
 assert.Equal(t, "down", body.Checks["redis"])

 live, err := http.Get(base + "/health")
 require.NoError(t, err)
 live.Body.Close()
 assert.Equal(t, http.StatusOK, live.StatusCode)

 cancel()
 select {
 case err := <-done:
  require.NoError(t, err)
 case <-time.After(20 * time.Second):
  t.Fatal("run did not return after its context was cancelled")
 }
}
```

Run: `gofmt -w app/cmd; go vet -tags integration ./app/cmd/ && go test -tags integration -count=1 -run TestRun ./app/cmd/`
Expected: `ok`.

- [ ] **Step 10: Run the whole suite and commit**

Run: `go test ./... 2>&1 | grep -v "no test files" | tail -30`
Expected: every package `ok`.

```bash
git add app/pkg/infra/server app/cmd/main.go app/cmd/main_test.go app/cmd/main_integration_test.go
git commit -m "$(cat <<'EOF'
feat(server): drain in-flight requests on SIGTERM, wire config, Redis and the health probes into run
EOF
)"
```

---

### Task 6: The migrator command

**Files:**

- Create: `app/cmd/migrator/main.go`
- Create: `app/cmd/migrator/main_test.go`
- Modify: `.make/dev.mk` (`run.with.migrations`)

**Interfaces:**

- Consumes: Task 1 `postgres.ConnectionString`, `postgres.Migrate`; Task 2 `config.Lookup`, `config.LoadDatabase`, `config.LoadMigrations`.
- Produces: the `migrator` binary (`go run ./app/cmd/migrator`); `run(lookup config.Lookup, migrate migrateFunc) error` in its `package main`.

**Task test command:** `go test ./app/cmd/migrator/ && make -n run.with.migrations`

- [ ] **Step 1: Write the failing tests**

Create `app/cmd/migrator/main_test.go`:

```go
package main

import (
 "errors"
 "testing"

 "github.com/sanctumlabs/fupi/app/config"
 "github.com/stretchr/testify/assert"
 "github.com/stretchr/testify/require"
)

func lookupOf(env map[string]string) config.Lookup {
 return func(key string) (string, bool) {
  value, ok := env[key]
  return value, ok
 }
}

type recorder struct {
 calls []call
 err   error
}

type call struct {
 url, path string
 inDocker  bool
}

func (r *recorder) migrate(databaseURL, migrationPath string, inDocker bool) error {
 r.calls = append(r.calls, call{databaseURL, migrationPath, inDocker})
 return r.err
}

func TestRun_MigratesTheConfiguredDatabase(t *testing.T) {
 dir := t.TempDir()
 rec := &recorder{}

 err := run(lookupOf(map[string]string{
  "DATABASE_HOST": "db.internal", "DATABASE_PORT": "6432", "MIGRATIONS_PATH": dir,
 }), rec.migrate)

 require.NoError(t, err)
 require.Len(t, rec.calls, 1)
 assert.Contains(t, rec.calls[0].url, "postgres://fupi-user:fupi-pass@db.internal:6432/fupidb?")
 assert.Equal(t, "file://"+dir, rec.calls[0].path)
 assert.False(t, rec.calls[0].inDocker)
}

func TestRun_RefusesTheDevelopmentPasswordInProduction(t *testing.T) {
 rec := &recorder{}

 err := run(lookupOf(map[string]string{"ENVIRONMENT": "production", "MIGRATIONS_PATH": t.TempDir()}), rec.migrate)

 require.Error(t, err)
 assert.ErrorContains(t, err, "DATABASE_PASSWORD")
 assert.Empty(t, rec.calls, "nothing may be migrated with a development password in production")
}

// The migrator needs only the database section; it must not demand an AUTH_SECRET.
func TestRun_DoesNotNeedAnAuthSecret(t *testing.T) {
 rec := &recorder{}

 err := run(lookupOf(map[string]string{
  "ENVIRONMENT": "production", "DATABASE_PASSWORD": "a-real-password", "MIGRATIONS_PATH": t.TempDir(),
 }), rec.migrate)

 require.NoError(t, err)
 assert.Len(t, rec.calls, 1)
}

func TestRun_FailsWhenTheMigrationsDirectoryIsMissing(t *testing.T) {
 rec := &recorder{}

 err := run(lookupOf(map[string]string{"MIGRATIONS_PATH": "/does/not/exist"}), rec.migrate)

 require.Error(t, err)
 assert.ErrorContains(t, err, "/does/not/exist")
 assert.Empty(t, rec.calls)
}

func TestRun_ReturnsTheMigrationError(t *testing.T) {
 rec := &recorder{err: errors.New("dirty database version 2")}

 err := run(lookupOf(map[string]string{"MIGRATIONS_PATH": t.TempDir()}), rec.migrate)

 assert.ErrorContains(t, err, "dirty database version 2")
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./app/cmd/migrator/ 2>&1 | head -6`
Expected: build failure `undefined: run` (and the package has no non-test file yet).

- [ ] **Step 3: Implement the migrator**

Create `app/cmd/migrator/main.go`:

```go
// Command migrator applies the SQL migrations in app/internal/adapters/postgres/migrations to the configured
// database and exits. It is the production caller of postgres.Migrate. The API never migrates at startup, so several
// replicas cannot race to migrate at boot, and the migrator can run with its own database role.
//
// Run it from the repository root (or set MIGRATIONS_PATH):
//
// go run ./app/cmd/migrator
package main

import (
 "fmt"
 "log/slog"
 "os"
 "path/filepath"

 "github.com/joho/godotenv"
 "github.com/sanctumlabs/fupi/app/config"
 "github.com/sanctumlabs/fupi/app/pkg/infra/database/postgres"
)

type migrateFunc func(databaseURL, migrationPath string, inDocker bool) error

func main() {
 if err := godotenv.Load(); err != nil {
  slog.Warn("no .env file found, relying on the environment", "error", err)
 }

 if err := run(os.LookupEnv, postgres.Migrate); err != nil {
  slog.Error("migration failed", "error", err)
  os.Exit(1)
 }
}

func run(lookup config.Lookup, migrate migrateFunc) error {
 database, err := config.LoadDatabase(lookup)
 if err != nil {
  return fmt.Errorf("invalid configuration: %w", err)
 }
 migrations, err := config.LoadMigrations(lookup)
 if err != nil {
  return fmt.Errorf("invalid configuration: %w", err)
 }

 dir, err := filepath.Abs(migrations.Path)
 if err != nil {
  return fmt.Errorf("resolve the migrations path %q: %w", migrations.Path, err)
 }
 if _, err := os.Stat(dir); err != nil {
  return fmt.Errorf("migrations directory %s: %w", dir, err)
 }

 pg := database.Postgres
 slog.Info("applying migrations", "host", pg.Host, "port", pg.Port, "database", pg.Name, "path", dir)
 return migrate(postgres.ConnectionString(pg), "file://"+dir, false)
}
```

- [ ] **Step 4: Run the tests**

Run: `gofmt -w app/cmd/migrator; go vet ./app/cmd/migrator/ && go test -count=1 ./app/cmd/migrator/`
Expected: `ok`.

- [ ] **Step 5: Repair the stale Make target**

In `.make/dev.mk` replace

```
.PHONY: runWithMigrations
run.with.migrations: ## Runs the project applying migrations
 @echo "${GREEN} Running application ${NC}"
 cd cmd && CGO_ENABLED=0 go run -tags migrate main.go
```

with

```
.PHONY: run.with.migrations
run.with.migrations: ## Applies the migrations with the migrator command, then runs the project
 @echo "${GREEN} Applying migrations, then running application ${NC}"
 CGO_ENABLED=0 go run ./app/cmd/migrator && CGO_ENABLED=0 go run app/cmd/main.go
```

Run: `make -n run.with.migrations`
Expected: it prints the echo and the two `go run` commands, with no `cd cmd`.

- [ ] **Step 6: Run the whole suite and commit**

Run: `go build ./... && go test ./... 2>&1 | grep -v "no test files" | tail -30`
Expected: every package `ok`.

```bash
git add app/cmd/migrator .make/dev.mk
git commit -m "$(cat <<'EOF'
feat(migrator): add the migrator command that applies migrations through postgres.Migrate
EOF
)"
```

---

### Task 7: ADRs, documentation and README

**Files:**

- Create: `docs/adr/0014-migrations-run-from-a-migrator-command.md`
- Create: `docs/adr/0015-postgres-is-the-only-required-dependency.md`
- Modify: `docs/LocalInfrastructure.md` (new section before `## The stacks`)
- Modify: `README.md` (after the `make run` block)

**Interfaces:** consumes the behaviour built in Tasks 1–6; produces documentation only.

**Task test command:** `grep -c "Running the app against the stack" docs/LocalInfrastructure.md && ls docs/adr | grep -c "^001[45]-" && go test ./...`

- [ ] **Step 1: Write ADR-0014**

Create `docs/adr/0014-migrations-run-from-a-migrator-command.md`:

```markdown
---
status: accepted
---

# Migrations are applied by a migrator command, never by the API at startup

`postgres.Migrate()` applies the SQL migrations with golang-migrate (ADR-0010). Its only caller was the test helper, so nothing in the product applied migrations. `app/cmd/migrator` is now its production caller: it loads the database settings, applies `app/internal/adapters/postgres/migrations` and exits 0 (applied or "no change") or 1.

The API does not call `Migrate()` at startup. Several replicas starting together would race to migrate, a failed migration would take the API down with it, and the API's database role would need DDL rights it should not otherwise have. The migrator runs once per deploy, as its own process, and can use a different role.

## Considered options

- **An opt-in `MIGRATE_ON_START` flag on the API** — convenient for one laptop, but it puts the replica race and the DDL role back in production, so it was rejected.
- **A `migrate` subcommand on the API binary** — one image, but the API process would still carry the migration code path and the same temptation.

## Consequences

- `go run ./app/cmd/migrator` (from the repo root, or with `MIGRATIONS_PATH`) migrates the database in `DATABASE_*`; `make run.with.migrations` runs it and then the API.
- The migrator needs only the database settings, so it demands no `AUTH_SECRET`, and refuses the development database password outside development and test.
- `Migrate()` records its state in `schema_migrations`, the table `make migrate` and the compose `migrate` job already use, and keeps a `sslmode` given in the URL.
- The migrations directory must exist where the migrator runs; building it into an image is the Dockerfile slice's concern.
- The compose `migrate` job keeps using the `migrate/migrate` image until an app image exists.
- Only `up` is implemented; `make migrate MIGRATE_DIRECTION=down` remains the rollback path.
```

- [ ] **Step 2: Write ADR-0015**

Create `docs/adr/0015-postgres-is-the-only-required-dependency.md`:

```markdown
---
status: accepted
---

# Postgres is the only dependency the API requires; Redis is optional and Kafka is not used

`GET /health/ready` answers 503 when Postgres is down or the process is draining, and 200 otherwise. When Redis is down it answers 200 with `"status":"degraded"` and `"redis":"down"`. The API starts without Redis and carries on; go-redis reconnects by itself when Redis returns.

Redis will be a cache, not the source of truth, so losing it slows redirects down but loses nothing. Postgres holds the data and the outbox, so without it the API cannot do its job.

The API process does not connect to Kafka at all. With the single outbox (ADR-0011) it writes `outbox_events` rows in the same Postgres transaction as the domain change, and only the relay worker publishes them. A Kafka outage therefore cannot affect the API; events wait in the outbox. The Kafka client is built with the relay.

## Consequences

- Liveness (`GET /health`) is always 200 while the process runs; it checks nothing, so an orchestrator does not restart the API because a dependency is down.
- The readiness body carries `up`/`down` per dependency and never error text, because the endpoint is public. Failures are logged with their error.
- A mis-set `REDIS_ADDRESS` shows as `degraded`, not as a crash. The startup log line (`redis is up` / `redis is down, continuing without it`) and the readiness body are the signals.
- When a feature starts depending on Redis for correctness (not just speed), that check must be registered as required.
```

- [ ] **Step 3: Add the docs section**

In `docs/LocalInfrastructure.md`, insert immediately before the line `## The stacks`:

````markdown
## Running the app against the stack

The API defaults match the stack, so on your host no `.env` edits are needed.

```bash
make infra.core.up MODE=single      # or HA: also run `make infra.hosts` once, and use the six-address REDIS_ADDRESS in .env.example
go run ./app/cmd/migrator           # optional: infra.core.up already migrated through the compose job; a second run says "no change"
make run                            # the API on :8085
curl -s localhost:8085/health/ready
```

`go run ./app/cmd/migrator` is the same migration code the tests use (`postgres.Migrate`). Run it from the repository root, or set `MIGRATIONS_PATH`. `make run.with.migrations` runs it and then the API. The API never migrates at startup (ADR-0014).

| Endpoint | Meaning |
|---|---|
| `GET /health` | liveness: 200 while the process runs |
| `GET /health/ready` | readiness: 200 `ok` (everything up) or `degraded` (Redis down); 503 `unavailable` (Postgres down) or `draining` (shutting down) |

Readiness never includes error text; look in the API's log for the reason (ADR-0015). Postgres is required, Redis is optional, and the API does not connect to Kafka.

On SIGTERM or Ctrl-C the API turns readiness to 503, finishes in-flight requests for up to `SHUTDOWN_TIMEOUT` seconds (default 15), closes Redis and Postgres and exits 0. A second Ctrl-C during the drain ends it at once.

The variables are listed, with their defaults, in `.env.example`. A value that does not parse stops startup with a message naming the variable, and any `ENVIRONMENT` other than `development` or `test` refuses the development secrets (`AUTH_SECRET`, `DATABASE_PASSWORD`, `REDIS_PASSWORD`).

Debugging:

- `redis is down, continuing without it` at startup: check `REDIS_ADDRESS` (HA needs all six seed nodes and the `/etc/hosts` line from `make infra.hosts`) and `REDIS_USERNAME`/`REDIS_PASSWORD`.
- Postgres connection errors: the write port is `5432` in both modes; `5433` is the HA read port and rejects writes.
- `make infra.psql`, `make infra.redis.cli` and `make infra.patroni.list` show the other side of each connection.

````

- [ ] **Step 4: Update the README**

In `README.md`, replace the line `> This will boot up the application with the provided environment variables.` with:

```markdown
> This will boot up the application with the provided environment variables. Check that it is ready with `curl -s localhost:8085/health/ready`; Postgres is required, Redis is optional. Apply the database migrations from the app's own migrator with `go run ./app/cmd/migrator` (or `make run.with.migrations`). See [Running the app against the stack](./docs/LocalInfrastructure.md#running-the-app-against-the-stack).
```

- [ ] **Step 5: Verify and commit**

Run: `grep -c "Running the app against the stack" docs/LocalInfrastructure.md README.md; ls docs/adr | grep -c "^001[45]-"; go test ./... 2>&1 | grep -v "no test files" | grep -v "^ok" ; echo "suite exit: done"`
Expected: `docs/LocalInfrastructure.md:1`, `README.md:1`; `2`; no non-`ok` lines before `suite exit: done`.

```bash
git add docs/adr/0014-migrations-run-from-a-migrator-command.md docs/adr/0015-postgres-is-the-only-required-dependency.md docs/LocalInfrastructure.md README.md
git commit -m "$(cat <<'EOF'
docs: document running the app against the stack, the migrator and the readiness policy

EOF
)"
```

---

### Task 8: Live verification against the real stack (single and HA)

**Files:**

- Modify (only if a drill finds something): `deploy/redis/start.sh`, the Go files named by the finding
- Modify: `docs/superpowers/specs/2026-10-04-app-connectivity-design.md` (append `## 13. Implementation notes`)

**Interfaces:** consumes everything built so far; produces evidence and, if a drill fails, a fix with a test that failed first.

**Task test command:** `go test ./... && go test -tags integration -count=1 ./app/cmd/... ./app/pkg/infra/cache/... ./app/pkg/infra/database/postgres/`

Preconditions: Docker is running; `docker ps --filter name=fupi` shows nothing (if the user's own containers are up, leave them alone and tell the user); `which python3 curl` both succeed. Use a scratch directory for binaries and logs: `SCRATCH=$(mktemp -d)`; never write them into the repo.

- [ ] **Step 1: Build the binaries and start the single-node stack**

```bash
SCRATCH=$(mktemp -d); echo "$SCRATCH"
go build -o "$SCRATCH/fupi" ./app/cmd && go build -o "$SCRATCH/migrator" ./app/cmd/migrator
make infra.core.up MODE=single
```

Expected: both builds succeed; the stack reports ready (Postgres, Redis, Kafka healthy; the compose `migrate` job exited 0).

- [ ] **Step 2: The migrator on a fresh database, then idempotently**

```bash
docker compose --profile '*' exec -T postgres-single psql -U postgres -c 'CREATE DATABASE migrator_check OWNER "fupi-user"'
DATABASE_NAME=migrator_check "$SCRATCH/migrator" > "$SCRATCH/m1.log" 2>&1; echo "first run exit: $?"; tail -5 "$SCRATCH/m1.log"
DATABASE_NAME=migrator_check "$SCRATCH/migrator" > "$SCRATCH/m2.log" 2>&1; echo "second run exit: $?"; tail -5 "$SCRATCH/m2.log"
docker compose --profile '*' exec -T postgres-single psql -U postgres -d migrator_check -c "SELECT version, dirty FROM schema_migrations" -c "SELECT to_regclass('outbox_events')"
docker compose --profile '*' exec -T postgres-single psql -U postgres -c 'DROP DATABASE migrator_check'
```

Expected: the first run logs `migrate: up success`, the second `migrate: no change`, both exit 0; `schema_migrations` has the highest migration version and `dirty = f`; `outbox_events` exists. The throwaway database is dropped (this touches only `migrator_check`, never `fupidb`).

- [ ] **Step 3: Start the API and read readiness**

```bash
nohup "$SCRATCH/fupi" > "$SCRATCH/fupi.log" 2>&1 &
echo $! > "$SCRATCH/fupi.pid"
sleep 3; curl -s -i localhost:8085/health | head -1; curl -s localhost:8085/health/ready; echo
grep -E "redis is (up|down)|connected to DB" "$SCRATCH/fupi.log"
```

Expected: `HTTP/1.1 200 OK`; `{"status":"ok","checks":{"postgres":"up","redis":"up"}}`; the log shows `connected to DB` and `redis is up`. If Redis reports `down` on single mode, run `make infra.redis.cli` and check whether `fupi-svc` can run `CLUSTER SLOTS`; that is spec step 7 and a finding.

- [ ] **Step 4: Redis down is degraded, and recovery needs no restart**

```bash
docker compose --profile '*' stop redis-single
sleep 1; curl -s -w ' [%{http_code}]\n' localhost:8085/health/ready
docker compose --profile '*' start redis-single
sleep 8; curl -s -w ' [%{http_code}]\n' localhost:8085/health/ready
```

Expected: `{"status":"degraded","checks":{"postgres":"up","redis":"down"}} [200]`, then (without restarting the API) `{"status":"ok",...} [200]`. If the service name differs, `docker compose --profile '*' ps` lists it.

- [ ] **Step 5: Postgres down is not ready**

```bash
docker compose --profile '*' stop postgres-single
sleep 1; curl -s -w ' [%{http_code}]\n' localhost:8085/health/ready; curl -s -o /dev/null -w 'live [%{http_code}]\n' localhost:8085/health
docker compose --profile '*' start postgres-single
sleep 10; curl -s -w ' [%{http_code}]\n' localhost:8085/health/ready
```

Expected: `unavailable ... [503]` with `"postgres":"down"`, liveness still `live [200]`, then `ok [200]` after Postgres is back.

- [ ] **Step 6: End to end: register a user and see the outbox row**

```bash
curl -s -w ' [%{http_code}]\n' -X POST localhost:8085/api/v1/fupi/auth/register -H 'Content-Type: application/json' \
  -d '{"username":"live-check","first_name":"Live","email":"live-check@example.com","password":"Sup3r-secret-pw!"}'
docker compose --profile '*' exec -T postgres-single sh -c 'PGPASSWORD="$PG_APP_PASSWORD" psql -h postgres -U "$PG_APP_USER" "$PG_DATABASE" -c "SELECT event_type, destination, sent_time FROM outbox_events ORDER BY created_at DESC LIMIT 3"'
```

Expected: `[201]` (or the documented success status from ADR-0007's endpoint) and at least one `outbox_events` row for the registration with `sent_time` empty (no relay yet). Record the event type seen.

- [ ] **Step 7: SIGTERM during an in-flight request, then a second signal**

```bash
python3 - "$(cat "$SCRATCH/fupi.pid")" <<'PY'
import os, signal, socket, sys, time
pid = int(sys.argv[1])
body = b'{"email":"a@example.com","password":"x"}'
s = socket.create_connection(("127.0.0.1", 8085))
s.sendall(b"POST /api/v1/fupi/auth/login HTTP/1.1\r\nHost: x\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n" % len(body) + body[:5])
time.sleep(0.5)
os.kill(pid, signal.SIGTERM)          # the request is half-sent and therefore in flight
time.sleep(1.0)
s.sendall(body[5:])
print(s.recv(4096).split(b"\r\n")[0].decode())
PY
sleep 2; kill -0 "$(cat "$SCRATCH/fupi.pid")" 2>/dev/null && echo "still running" || echo "exited"
grep -E "draining|shutting down" "$SCRATCH/fupi.log"
```

Expected: a status line such as `HTTP/1.1 401 Unauthorized` (the in-flight request was answered during the drain), then `exited`, and the log shows `readiness: draining` before `shutting down server`.

Exit code of an idle shutdown:

```bash
"$SCRATCH/fupi" > "$SCRATCH/fupi-idle.log" 2>&1 &
P=$!; sleep 3; kill -TERM $P; wait $P; echo "exit $?"
```

Expected: `exit 0`.

Second-signal drill (Review Focus 5): hold a request open, send SIGTERM, then SIGINT while the drain is still waiting:

```bash
"$SCRATCH/fupi" > "$SCRATCH/fupi-second.log" 2>&1 &
P=$!; sleep 3
python3 - "$P" <<'PY'
import os, signal, socket, sys, time
pid = int(sys.argv[1])
s = socket.create_connection(("127.0.0.1", 8085))
s.sendall(b"POST /api/v1/fupi/auth/login HTTP/1.1\r\nHost: x\r\nContent-Type: application/json\r\nContent-Length: 100\r\n\r\n{")
time.sleep(0.5)
os.kill(pid, signal.SIGTERM)          # the drain starts and waits for the incomplete request
time.sleep(1.0)
os.kill(pid, signal.SIGINT)           # the second signal
time.sleep(1.0)
PY
kill -0 $P 2>/dev/null && echo "STILL RUNNING: the second signal was swallowed" || echo "exited"
wait $P; echo "exit $?"
```

Expected: `exited` within about two seconds of the SIGINT and a non-zero exit status (the default signal behaviour), not a wait for `SHUTDOWN_TIMEOUT` (15 seconds). If the process is still running, the `main()` goroutine that calls `stop()` is not working: debug it with `superpowers:systematic-debugging`, add what test is possible, fix it.

- [ ] **Step 8: Production guard**

```bash
ENVIRONMENT=production "$SCRATCH/fupi" > "$SCRATCH/prod-api.log" 2>&1; echo "api exit: $?"; tail -3 "$SCRATCH/prod-api.log"
ENVIRONMENT=production "$SCRATCH/migrator" > "$SCRATCH/prod-mig.log" 2>&1; echo "migrator exit: $?"; tail -3 "$SCRATCH/prod-mig.log"
```

Expected: both print an error naming `AUTH_SECRET`/`DATABASE_PASSWORD`/`REDIS_PASSWORD` (the migrator only `DATABASE_PASSWORD`), never the values, and exit 1.

- [ ] **Step 9: Switch to HA and repeat the connection drills**

```bash
make infra.core.down
make infra.hosts
```

Add the printed line to `/etc/hosts` only if it is not already there (ask the user if it needs `sudo`; never edit `/etc/hosts` without them). Then:

```bash
make infra.core.up
REDIS_ADDRESS=localhost:7001,localhost:7002,localhost:7003,localhost:7004,localhost:7005,localhost:7006 \
  nohup "$SCRATCH/fupi" > "$SCRATCH/fupi-ha.log" 2>&1 &
echo $! > "$SCRATCH/fupi.pid"
sleep 5; curl -s localhost:8085/health/ready; echo
```

Expected: `ok` with both up (the cluster client reached the six-node cluster as `fupi-svc`). Then:

- Stop one Redis master (find one with `make infra.redis.cli`, then `cluster nodes`; the node is `redis-N` for the matching port `700N`): `docker compose --profile '*' stop redis-N`; send 20 readiness requests over 20 seconds (`for i in $(seq 20); do curl -s localhost:8085/health/ready; echo; sleep 1; done`). Expected: `ok` or `degraded` throughout and never a `503`; once a replica is promoted it should settle on `ok`. If readiness reports Redis `down` while the cluster still serves, treat it as a code bug (systematic debugging, a failing test where one is possible), not a documentation note. Restart the node afterwards.
- Stop the Postgres primary (`make infra.patroni.list` shows which node leads; `docker compose --profile '*' stop <leader>`): readiness goes `503`, and after Patroni and HAProxy recover (about 30 to 40 seconds) it returns to `ok` without restarting the API. Restart the stopped node afterwards.
- Run the migrator against HA (`"$SCRATCH/migrator"`): `migrate: no change`, exit 0.

If the `fupi-svc` ACL blocks a cluster command the client needs (the log shows `NOPERM`), fix `deploy/redis/start.sh`, re-run `bash scripts/infra_test.sh` and `make infra.config`, and record the change in the spec's implementation notes (this edits a slice 1 file, which spec §10.7 allows).

- [ ] **Step 10: Tear down and record the evidence**

```bash
kill "$(cat "$SCRATCH/fupi.pid")" 2>/dev/null; make infra.core.down; docker ps --filter name=fupi --format '{{.Names}}'
```

Expected: nothing left running. Append `## 13. Implementation notes` to `docs/superpowers/specs/2026-10-04-app-connectivity-design.md`: one line per drill step above with what was observed (including "none" where nothing changed and any fix made), then:

```bash
go test ./... 2>&1 | grep -v "no test files" | grep -v "^ok"; echo "suite check done"
git add -A docs/superpowers/specs deploy
git commit -m "$(cat <<'EOF'
docs: record the live verification of app connectivity in both modes

EOF
)"
```

Expected: no non-`ok` lines; a clean `git status`.
