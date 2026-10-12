package config

import (
	"log/slog"
	"os"
	"strings"
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

	// An unset ENVIRONMENT warns and the example sets it, so the warnings differ on purpose.
	defaults.Warnings, fromExample.Warnings = nil, nil
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
		"SHORT_CODE_KEY": "k",
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
	assert.Equal(t, "k", app.ShortCode.Key)
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
			for _, variable := range []string{"AUTH_SECRET", "DATABASE_PASSWORD", "REDIS_PASSWORD", "SHORT_CODE_KEY"} {
				assert.ErrorContains(t, err, variable)
			}
			for _, secret := range []string{"fupi-secret", "fupi-pass", "fupi-svc", devShortCodeKey} {
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
		"SHORT_CODE_KEY": strings.Repeat("k", minShortCodeKeyLength),
	}))

	assert.NoError(t, err)
}

// A short key is refused in production: the permutation is only as unguessable as its key.
func TestLoadShortCode_RefusesAShortKeyInProduction(t *testing.T) {
	_, err := LoadShortCode(lookupOf(map[string]string{
		"ENVIRONMENT": "production", "SHORT_CODE_KEY": strings.Repeat("k", minShortCodeKeyLength-1),
	}))
	require.Error(t, err)
	assert.ErrorContains(t, err, "SHORT_CODE_KEY must be at least")

	_, err = LoadShortCode(lookupOf(map[string]string{"ENVIRONMENT": "test", "SHORT_CODE_KEY": "short"}))
	assert.NoError(t, err, "development and test accept any key")
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

// A provider URL pasted into REDIS_ADDRESS carries a password; the error must point at the entry, not repeat it.
func TestLoad_ARedisAddressErrorNeverRepeatsTheEntry(t *testing.T) {
	_, err := Load(lookupOf(map[string]string{"REDIS_ADDRESS": "redis://default:S3CRETPW@cache.internal:6379"}))

	require.Error(t, err)
	assert.ErrorContains(t, err, "REDIS_ADDRESS")
	assert.NotContains(t, err.Error(), "S3CRETPW")
}

// A host that cannot form a valid connection string must be rejected without echoing the password in the string.
func TestLoadDatabase_RejectsAHostThatBreaksTheConnectionStringWithoutLeakingThePassword(t *testing.T) {
	for _, host := range []string{"[::1]", "db host", "db/host"} {
		t.Run(host, func(t *testing.T) {
			_, err := LoadDatabase(lookupOf(map[string]string{"DATABASE_HOST": host, "DATABASE_PASSWORD": "TOPSECRET"}))

			require.Error(t, err)
			assert.ErrorContains(t, err, "DATABASE_HOST")
			assert.NotContains(t, err.Error(), "TOPSECRET")
		})
	}
}

func TestLoadDatabase_AcceptsUnixSocketURLsAndRejectsOtherSchemes(t *testing.T) {
	_, err := LoadDatabase(lookupOf(map[string]string{"DATABASE_URL": "postgres:///fupidb?host=/var/run/postgresql"}))
	assert.NoError(t, err, "a unix-socket URL has no host and is valid")

	_, err = LoadDatabase(lookupOf(map[string]string{"DATABASE_URL": "postgresql://u:p@db:5432/fupidb"}))
	assert.NoError(t, err)

	_, err = LoadDatabase(lookupOf(map[string]string{"DATABASE_URL": "mongodb://u:p@db:27017/fupidb"}))
	require.Error(t, err)
	assert.ErrorContains(t, err, "DATABASE_URL")
}

func TestLoad_WarnsWhenEnvironmentIsUnsetAndDevelopmentSecretsAreInUse(t *testing.T) {
	app, err := Load(lookupOf(nil))
	require.NoError(t, err)
	require.Len(t, app.Warnings, 1)
	assert.Contains(t, app.Warnings[0], "ENVIRONMENT is not set")

	for _, environment := range []string{"development", "test"} {
		app, err = Load(lookupOf(map[string]string{"ENVIRONMENT": environment}))
		require.NoError(t, err)
		assert.Empty(t, app.Warnings, "an explicit %s environment is deliberate", environment)
	}

	app, err = Load(lookupOf(map[string]string{
		"AUTH_SECRET": "a-real-secret", "DATABASE_PASSWORD": "a-real-password", "REDIS_PASSWORD": "a-real-redis-password",
		"SHORT_CODE_KEY": strings.Repeat("k", minShortCodeKeyLength),
	}))
	require.NoError(t, err)
	assert.Empty(t, app.Warnings, "nothing to warn about once the secrets are real")
}

func TestLoad_WarnsAboutAnUnencryptedDatabaseConnectionOutsideDevelopment(t *testing.T) {
	secrets := map[string]string{
		"ENVIRONMENT": "production", "AUTH_SECRET": "a-real-secret", "DATABASE_PASSWORD": "a-real-password", "REDIS_PASSWORD": "a-real-redis-password",
		"SHORT_CODE_KEY": strings.Repeat("k", minShortCodeKeyLength),
	}

	app, err := Load(lookupOf(secrets))
	require.NoError(t, err)
	require.Len(t, app.Warnings, 1)
	assert.Contains(t, app.Warnings[0], "DATABASE_SSL_MODE")

	secrets["DATABASE_SSL_MODE"] = "require"
	app, err = Load(lookupOf(secrets))
	require.NoError(t, err)
	assert.Empty(t, app.Warnings)
}

func TestLoadLogging_DefaultsToJSONAtInfo(t *testing.T) {
	settings, err := LoadLogging(lookupOf(nil))
	require.NoError(t, err)

	assert.Equal(t, LoggingSettings{Level: slog.LevelInfo, Format: "json"}, settings)
}

func TestLoadLogging_ReadsLevelAndFormat(t *testing.T) {
	cases := map[string]struct {
		env  map[string]string
		want LoggingSettings
	}{
		"debug text":        {map[string]string{"LOG_LEVEL": "debug", "LOG_FORMAT": "text"}, LoggingSettings{slog.LevelDebug, "text"}},
		"warn":              {map[string]string{"LOG_LEVEL": "warn"}, LoggingSettings{slog.LevelWarn, "json"}},
		"warning alias":     {map[string]string{"LOG_LEVEL": "warning"}, LoggingSettings{slog.LevelWarn, "json"}},
		"error":             {map[string]string{"LOG_LEVEL": "error"}, LoggingSettings{slog.LevelError, "json"}},
		"upper case":        {map[string]string{"LOG_LEVEL": "DEBUG", "LOG_FORMAT": "TEXT"}, LoggingSettings{slog.LevelDebug, "text"}},
		"empty means unset": {map[string]string{"LOG_LEVEL": "", "LOG_FORMAT": ""}, LoggingSettings{slog.LevelInfo, "json"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			settings, err := LoadLogging(lookupOf(tc.env))
			require.NoError(t, err)
			assert.Equal(t, tc.want, settings)
		})
	}
}

func TestLoadLogging_RejectsUnknownValuesAndFallsBackToTheDefaults(t *testing.T) {
	settings, err := LoadLogging(lookupOf(map[string]string{"LOG_LEVEL": "verbose", "LOG_FORMAT": "xml"}))

	require.Error(t, err)
	assert.ErrorContains(t, err, "LOG_LEVEL")
	assert.ErrorContains(t, err, "LOG_FORMAT")
	assert.NotContains(t, err.Error(), "verbose", "error text names variables, never values")
	assert.Equal(t, LoggingSettings{Level: slog.LevelInfo, Format: "json"}, settings, "usable settings so the error itself can be logged")
}

func TestLoad_IncludesTheLoggingSettings(t *testing.T) {
	app, err := Load(lookupOf(map[string]string{"LOG_FORMAT": "text"}))
	require.NoError(t, err)
	assert.Equal(t, "text", app.Logging.Format)

	_, err = Load(lookupOf(map[string]string{"LOG_FORMAT": "xml"}))
	assert.ErrorContains(t, err, "LOG_FORMAT", "an unknown format is a startup error")
}

// A key set twice in .env.example is ambiguous to read and silently resolved by the last line (LOG_LEVEL was set to
// debug and then to info).
func TestEnvExample_SetsEachKeyOnce(t *testing.T) {
	raw, err := os.ReadFile("../../.env.example")
	require.NoError(t, err)

	seen := map[string]int{}
	for _, line := range strings.Split(string(raw), "\n") {
		key, _, found := strings.Cut(line, "=")
		if !found || strings.HasPrefix(strings.TrimSpace(key), "#") {
			continue
		}
		seen[strings.TrimSpace(key)]++
	}
	for key, count := range seen {
		assert.Equal(t, 1, count, "%s is set %d times in .env.example", key, count)
	}
}

func TestLoadRedirectCache_DefaultsAndValidation(t *testing.T) {
	cfg, err := LoadRedirectCache(lookupOf(nil))
	require.NoError(t, err)
	require.Equal(t, 300*time.Second, cfg.TTL)
	require.Equal(t, 30*time.Second, cfg.AbsentTTL)

	_, err = LoadRedirectCache(lookupOf(map[string]string{"REDIRECT_CACHE_TTL": "0"}))
	require.ErrorContains(t, err, "REDIRECT_CACHE_TTL")
}
