package config

import (
	"errors"
	"time"

	"github.com/sanctumlabs/fupi/app/pkg/infra/cache/redis"
)

// KafkaSettings configures the Kafka producer.
type KafkaSettings struct {
	// Brokers are the seed brokers, as host:port.
	Brokers  []string
	ClientID string
	// PublishTimeout is how long one batch may take to be acknowledged.
	PublishTimeout time.Duration
}

// OutboxSettings configures the outbox relay.
type OutboxSettings struct {
	// PollInterval is the wait between cycles when nothing was claimed.
	PollInterval time.Duration
	// BatchSize is how many events per destination one cycle claims.
	BatchSize int
	// MaxAttempts is how many permanent rejections park an event.
	MaxAttempts int
	// StandbyInterval is how often a relay that is not the leader tries to become it.
	StandbyInterval time.Duration
	// Retention is how long sent events are kept; zero turns the purge off.
	Retention time.Duration
}

// SecuritySettings configures the url scanner. Scanning is optional: without a key the worker does not consume url.created.
type SecuritySettings struct {
	// SafeBrowsingAPIKey is the Google Safe Browsing API key; empty turns scanning off.
	SafeBrowsingAPIKey string
}

// ScanningEnabled reports whether a scanner is configured.
func (s SecuritySettings) ScanningEnabled() bool { return s.SafeBrowsingAPIKey != "" }

// ExpirySettings configures the URL expiry job.
type ExpirySettings struct {
	// Interval is the wait between runs.
	Interval time.Duration
	// BatchSize is how many URLs one fetch loads.
	BatchSize int
}

// HealthSettings is where the worker's health listener binds.
type HealthSettings struct {
	Host string
	Port int
}

// Worker is everything the worker process reads from its environment. It needs no AUTH_SECRET, and no Redis unless scanning
// is on: a flagged url is evicted from the redirect cache, so Redis and the cache settings are read only then.
type Worker struct {
	Environment     string
	Health          HealthSettings
	Database        DatabaseSettings
	Kafka           KafkaSettings
	Outbox          OutboxSettings
	Security        SecuritySettings
	Redis           redis.RedisClientConfig
	RedirectCache   RedirectCacheSettings
	Expiry          ExpirySettings
	Logging         LoggingSettings
	ShutdownTimeout time.Duration
	// Warnings are problems that do not stop startup but that the operator should see in the log.
	Warnings []string
}

// LoadWorker reads and validates the whole worker configuration. Every problem is reported, not just the first.
func LoadWorker(lookup Lookup) (Worker, error) {
	r := newReader(lookup)
	worker := Worker{
		Environment:     r.str("ENVIRONMENT", environmentDevelopment),
		ShutdownTimeout: r.units("SHUTDOWN_TIMEOUT", 15, time.Second),
	}
	if worker.ShutdownTimeout <= 0 {
		r.fail("SHUTDOWN_TIMEOUT must be greater than zero")
	}

	errs := []error{r.err()}
	var err error
	worker.Health, err = LoadWorkerHealth(lookup)
	errs = append(errs, err)
	worker.Database, err = LoadDatabase(lookup)
	errs = append(errs, err)
	worker.Kafka, err = LoadKafka(lookup)
	errs = append(errs, err)
	worker.Outbox, err = LoadOutbox(lookup)
	errs = append(errs, err)
	worker.Security, err = LoadSecurity(lookup)
	errs = append(errs, err)
	if worker.Security.ScanningEnabled() {
		worker.Redis, err = LoadRedis(lookup)
		errs = append(errs, err)
		worker.RedirectCache, err = LoadRedirectCache(lookup)
		errs = append(errs, err)
	}
	worker.Expiry, err = LoadExpiry(lookup)
	errs = append(errs, err)
	worker.Logging, err = LoadLogging(lookup)
	errs = append(errs, err)

	if _, set := r.raw("ENVIRONMENT"); !set && worker.Database.Postgres.Url == "" && worker.Database.Postgres.Password == devDatabasePassword {
		worker.Warnings = append(worker.Warnings, "ENVIRONMENT is not set, so the development database password is accepted; set ENVIRONMENT (for example production) in a real deployment so it is refused")
	}
	return worker, errors.Join(errs...)
}

// LoadWorkerHealth reads where the health listener binds. The container health check uses only this loader, so it needs
// no database or Kafka settings.
func LoadWorkerHealth(lookup Lookup) (HealthSettings, error) {
	r := newReader(lookup)
	settings := HealthSettings{
		Host: r.str("SERVER_HOST", "0.0.0.0"),
		Port: r.integer("WORKER_HTTP_PORT", 8086),
	}
	if settings.Port < 1 || settings.Port > 65535 {
		r.fail("WORKER_HTTP_PORT must be a port between 1 and 65535")
	}
	return settings, r.err()
}

// LoadKafka reads the Kafka settings.
func LoadKafka(lookup Lookup) (KafkaSettings, error) {
	r := newReader(lookup)
	settings := KafkaSettings{
		Brokers:        splitList(r.str("KAFKA_BROKERS", "localhost:19092")),
		ClientID:       r.str("KAFKA_CLIENT_ID", "fupi-worker"),
		PublishTimeout: r.units("KAFKA_PUBLISH_TIMEOUT", 10, time.Second),
	}
	if len(settings.Brokers) == 0 {
		r.fail("KAFKA_BROKERS must list at least one host:port")
	}
	for i, entry := range settings.Brokers {
		if !validAddress(entry) {
			r.fail("KAFKA_BROKERS entry %d must be host:port with a port between 1 and 65535", i+1)
		}
	}
	if settings.PublishTimeout < time.Second {
		r.fail("KAFKA_PUBLISH_TIMEOUT must be at least 1 (seconds)")
	}
	return settings, r.err()
}

// LoadOutbox reads the outbox relay settings.
func LoadOutbox(lookup Lookup) (OutboxSettings, error) {
	r := newReader(lookup)
	settings := OutboxSettings{
		PollInterval:    r.units("OUTBOX_POLL_INTERVAL_MS", 100, time.Millisecond),
		BatchSize:       r.integer("OUTBOX_BATCH_SIZE", 100),
		MaxAttempts:     r.integer("OUTBOX_MAX_ATTEMPTS", 3),
		StandbyInterval: r.units("OUTBOX_STANDBY_INTERVAL", 5, time.Second),
		Retention:       r.units("OUTBOX_RETENTION_DAYS", 7, 24*time.Hour),
	}
	if settings.PollInterval < 10*time.Millisecond {
		r.fail("OUTBOX_POLL_INTERVAL_MS must be at least 10")
	}
	if settings.BatchSize < 1 || settings.BatchSize > 1000 {
		r.fail("OUTBOX_BATCH_SIZE must be between 1 and 1000")
	}
	if settings.MaxAttempts < 1 {
		r.fail("OUTBOX_MAX_ATTEMPTS must be at least 1")
	}
	if settings.StandbyInterval < time.Second {
		r.fail("OUTBOX_STANDBY_INTERVAL must be at least 1 (seconds)")
	}
	if settings.Retention < 0 {
		r.fail("OUTBOX_RETENTION_DAYS must not be negative (0 turns the purge off)")
	}
	return settings, r.err()
}

// LoadSecurity reads the url scanner settings.
func LoadSecurity(lookup Lookup) (SecuritySettings, error) {
	r := newReader(lookup)
	return SecuritySettings{SafeBrowsingAPIKey: r.str("SAFE_BROWSING_API_KEY", "")}, r.err()
}

// LoadExpiry reads the URL expiry job settings.
func LoadExpiry(lookup Lookup) (ExpirySettings, error) {
	r := newReader(lookup)
	settings := ExpirySettings{
		Interval:  r.units("EXPIRY_INTERVAL", 60, time.Second),
		BatchSize: r.integer("EXPIRY_BATCH_SIZE", 100),
	}
	if settings.Interval < time.Second {
		r.fail("EXPIRY_INTERVAL must be at least 1 (seconds)")
	}
	if settings.BatchSize < 1 || settings.BatchSize > 1000 {
		r.fail("EXPIRY_BATCH_SIZE must be between 1 and 1000")
	}
	return settings, r.err()
}
