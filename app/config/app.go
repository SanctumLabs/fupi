package config

import (
	"errors"
	"log/slog"
	"net/netip"
	"net/url"
	"strings"
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
	// ProxyHeader is the request header (for example X-Forwarded-For) that names the client behind a reverse proxy.
	// It is only believed when the request comes from one of TrustedProxies.
	ProxyHeader string
	// TrustedProxies are the addresses of the reverse proxies allowed to set ProxyHeader.
	TrustedProxies []netip.Prefix
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

// ShortCodeSettings configures short code generation.
type ShortCodeSettings struct {
	// Key keys the permutation that turns the sequential short code counter into codes that cannot be enumerated. It
	// must never change once codes have been issued with it: see ADR-0019.
	Key string
}

// RedirectCacheSettings configures how long the redirect cache keeps what it learns. Both are bounds on staleness as
// well as on memory: an entry another writer changed behind the cache's back lives this long at most.
type RedirectCacheSettings struct {
	// TTL is how long a url's target is kept; it is cut short at the url's expiry.
	TTL time.Duration
	// AbsentTTL is how long a code that does not exist is remembered. It is short so a code created right after a
	// probe starts redirecting soon even if the creating instance could not evict it.
	AbsentTTL time.Duration
}

// LoggingSettings configures the process logger.
type LoggingSettings struct {
	Level slog.Level
	// Format is "json" (one object per line, what the ELK pipeline parses) or "text" (for a terminal).
	Format string
}

// App is everything the API process reads from its environment.
type App struct {
	Environment     string
	Server          ServerSettings
	Database        DatabaseSettings
	Redis           redis.RedisClientConfig
	Kafka           KafkaSettings
	Auth            AuthConfig
	ShortCode       ShortCodeSettings
	RedirectCache   RedirectCacheSettings
	RateLimit       RateLimitSettings
	Logging         LoggingSettings
	ShutdownTimeout time.Duration
	// Warnings are problems that do not stop startup but that the operator should see in the log.
	Warnings []string
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
	app.Kafka, err = LoadKafka(lookup)
	errs = append(errs, err)
	app.Auth, err = LoadAuth(lookup)
	errs = append(errs, err)
	app.ShortCode, err = LoadShortCode(lookup)
	errs = append(errs, err)
	app.RedirectCache, err = LoadRedirectCache(lookup)
	errs = append(errs, err)
	app.RateLimit, err = LoadRateLimit(lookup)
	errs = append(errs, err)
	app.Logging, err = LoadLogging(lookup)
	errs = append(errs, err)

	app.Warnings = warnings(r, app)

	return app, errors.Join(errs...)
}

// warnings lists what the operator should see although startup may proceed. An unset ENVIRONMENT means the
// development default applies and the secret guard is off, so a deployment that forgot it would silently run with the
// public development secrets; an explicit development or test environment is deliberate and says nothing.
func warnings(r *reader, app App) []string {
	var out []string

	_, environmentSet := r.raw("ENVIRONMENT")
	usesDevelopmentSecret := app.Auth.Secret == devAuthSecret ||
		app.ShortCode.Key == devShortCodeKey ||
		app.Redis.Password == devRedisPassword ||
		(app.Database.Postgres.Url == "" && app.Database.Postgres.Password == devDatabasePassword)
	if !environmentSet && usesDevelopmentSecret {
		out = append(out, "ENVIRONMENT is not set, so the development secrets are accepted; set ENVIRONMENT (for example production) in a real deployment so they are refused")
	}

	if r.enforceSecrets() && app.Database.Postgres.SslMode == "disable" {
		out = append(out, "DATABASE_SSL_MODE=disable: the connection to Postgres is not encrypted")
	}

	return out
}

// LoadServer reads the HTTP server settings.
func LoadServer(lookup Lookup) (ServerSettings, error) {
	r := newReader(lookup)
	settings := ServerSettings{
		Host:        r.str("SERVER_HOST", "0.0.0.0"),
		Port:        r.integer("HTTP_PORT", 8085),
		Header:      r.str("SERVER_HEADER", "Fupi"),
		Name:        r.str("SERVER_NAME", "Fupi"),
		Version:     r.str("SERVER_VERSION", "1.0.0"),
		BaseURL:     r.str("APP_BASE_URL", "http://localhost:8085"),
		ProxyHeader: r.str("SERVER_PROXY_HEADER", ""),
	}
	if proxies, err := parseProxies(splitList(r.str("SERVER_TRUSTED_PROXIES", ""))); err != nil {
		r.fail("SERVER_TRUSTED_PROXIES: %v", err)
	} else {
		settings.TrustedProxies = proxies
	}
	if settings.ProxyHeader != "" && len(settings.TrustedProxies) == 0 {
		r.fail("SERVER_PROXY_HEADER needs SERVER_TRUSTED_PROXIES: the header is only believed from a trusted proxy")
	}
	if settings.Port < 1 || settings.Port > 65535 {
		r.fail("HTTP_PORT must be a port between 1 and 65535")
	}
	return settings, r.err()
}

// LoadLogging reads LOG_LEVEL (debug, info, warn or error; default info) and LOG_FORMAT (json or text; default json). A
// value that is not one of those is an error, and the settings returned alongside it are the defaults, so the caller can
// still log the error.
func LoadLogging(lookup Lookup) (LoggingSettings, error) {
	r := newReader(lookup)
	settings := LoggingSettings{Level: slog.LevelInfo, Format: "json"}

	if value, ok := r.raw("LOG_LEVEL"); ok {
		switch strings.ToLower(value) {
		case "debug":
			settings.Level = slog.LevelDebug
		case "info":
			settings.Level = slog.LevelInfo
		case "warn", "warning":
			settings.Level = slog.LevelWarn
		case "error":
			settings.Level = slog.LevelError
		default:
			r.fail("LOG_LEVEL must be one of debug, info, warn or error")
		}
	}
	if value, ok := r.raw("LOG_FORMAT"); ok {
		switch format := strings.ToLower(value); format {
		case "json", "text":
			settings.Format = format
		default:
			r.fail("LOG_FORMAT must be json or text")
		}
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
		MaxConns:        r.int32("DATABASE_MAX_CONNS", 30),
		MinConns:        r.int32("DATABASE_MIN_CONNS", 5),
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
		// A unix-socket URL (postgres:///db?host=/var/run/postgresql) has no host, so only the scheme is required.
		// The parse error text can contain the URL, and with it the password, so it is not repeated.
		if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
			r.fail("DATABASE_URL is not a valid postgres:// URL")
		} else if parsed.User != nil {
			password, _ = parsed.User.Password()
		} else {
			password = ""
		}
	} else if _, err := url.Parse(postgres.ConnectionString(pg)); err != nil {
		// Same reason: the error would print the whole connection string, password included.
		r.fail("DATABASE_HOST, DATABASE_PORT, DATABASE_NAME, DATABASE_USERNAME and DATABASE_PASSWORD must form a valid connection string (is DATABASE_HOST a bare host name or IP, without brackets or slashes?)")
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
	for i, entry := range cfg.Address {
		if !validAddress(entry) {
			// Entries are named by position: a provider URL pasted here would carry its password into the log.
			r.fail("REDIS_ADDRESS entry %d must be host:port with a port between 1 and 65535", i+1)
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

// LoadRedirectCache reads the redirect cache settings, in seconds.
func LoadRedirectCache(lookup Lookup) (RedirectCacheSettings, error) {
	r := newReader(lookup)
	cfg := RedirectCacheSettings{
		TTL:       r.units("REDIRECT_CACHE_TTL", 300, time.Second),
		AbsentTTL: r.units("REDIRECT_CACHE_ABSENT_TTL", 30, time.Second),
	}
	if cfg.TTL <= 0 {
		r.fail("REDIRECT_CACHE_TTL must be greater than zero")
	}
	if cfg.AbsentTTL <= 0 {
		r.fail("REDIRECT_CACHE_ABSENT_TTL must be greater than zero")
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

// minShortCodeKeyLength is the shortest SHORT_CODE_KEY accepted outside development and test: 32 bytes is the
// HMAC-SHA256 block of entropy the permutation can use.
const minShortCodeKeyLength = 32

// LoadShortCode reads the short code settings.
func LoadShortCode(lookup Lookup) (ShortCodeSettings, error) {
	r := newReader(lookup)
	cfg := ShortCodeSettings{Key: r.str("SHORT_CODE_KEY", devShortCodeKey)}

	if r.enforceSecrets() {
		if cfg.Key == devShortCodeKey {
			r.fail("SHORT_CODE_KEY must be set to a non-default value when ENVIRONMENT is not development or test")
		} else if len(cfg.Key) < minShortCodeKeyLength {
			r.fail("SHORT_CODE_KEY must be at least %d characters", minShortCodeKeyLength)
		}
	}

	return cfg, r.err()
}

// LoadMigrations reads where the SQL migrations live.
func LoadMigrations(lookup Lookup) (MigrationSettings, error) {
	r := newReader(lookup)
	return MigrationSettings{Path: r.str("MIGRATIONS_PATH", defaultMigrationsPath)}, r.err()
}
