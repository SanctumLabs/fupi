package config

import (
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadWorker_DefaultsMatchTheLocalStack(t *testing.T) {
	worker, err := LoadWorker(lookupOf(nil))
	require.NoError(t, err)

	assert.Equal(t, "development", worker.Environment)
	assert.Equal(t, HealthSettings{Host: "0.0.0.0", Port: 8086}, worker.Health)
	assert.Equal(t, 15*time.Second, worker.ShutdownTimeout)
	assert.Equal(t, KafkaSettings{Brokers: []string{"localhost:19092"}, ClientID: "fupi-worker", PublishTimeout: 10 * time.Second}, worker.Kafka)
	assert.Equal(t, OutboxSettings{
		PollInterval: 100 * time.Millisecond, BatchSize: 100, MaxAttempts: 3, StandbyInterval: 5 * time.Second, Retention: 7 * 24 * time.Hour,
	}, worker.Outbox)
	assert.Equal(t, ExpirySettings{Interval: time.Minute, BatchSize: 100}, worker.Expiry)
	assert.Equal(t, "localhost", worker.Database.Postgres.Host, "the database settings are the API's")
	assert.Equal(t, "5432", worker.Database.Postgres.Port)
	assert.Equal(t, "json", worker.Logging.Format)
}

func TestLoadWorker_Overrides(t *testing.T) {
	worker, err := LoadWorker(lookupOf(map[string]string{
		"WORKER_HTTP_PORT": "9000", "SHUTDOWN_TIMEOUT": "30",
		"KAFKA_BROKERS": "kafka-1:9092, kafka-2:9092,kafka-3:9092", "KAFKA_CLIENT_ID": "relay-1", "KAFKA_PUBLISH_TIMEOUT": "20",
		"OUTBOX_POLL_INTERVAL_MS": "250", "OUTBOX_BATCH_SIZE": "500", "OUTBOX_MAX_ATTEMPTS": "5",
		"OUTBOX_STANDBY_INTERVAL": "2", "OUTBOX_RETENTION_DAYS": "30",
		"EXPIRY_INTERVAL": "10", "EXPIRY_BATCH_SIZE": "500",
	}))
	require.NoError(t, err)

	assert.Equal(t, 9000, worker.Health.Port)
	assert.Equal(t, 30*time.Second, worker.ShutdownTimeout)
	assert.Equal(t, []string{"kafka-1:9092", "kafka-2:9092", "kafka-3:9092"}, worker.Kafka.Brokers)
	assert.Equal(t, "relay-1", worker.Kafka.ClientID)
	assert.Equal(t, 20*time.Second, worker.Kafka.PublishTimeout)
	assert.Equal(t, OutboxSettings{
		PollInterval: 250 * time.Millisecond, BatchSize: 500, MaxAttempts: 5, StandbyInterval: 2 * time.Second, Retention: 30 * 24 * time.Hour,
	}, worker.Outbox)
	assert.Equal(t, ExpirySettings{Interval: 10 * time.Second, BatchSize: 500}, worker.Expiry)
}

func TestLoadWorker_ARetentionOfZeroTurnsThePurgeOffAndAnEmptyValueCountsAsUnset(t *testing.T) {
	worker, err := LoadWorker(lookupOf(map[string]string{"OUTBOX_RETENTION_DAYS": "0", "KAFKA_BROKERS": "", "OUTBOX_BATCH_SIZE": ""}))
	require.NoError(t, err)

	assert.Equal(t, time.Duration(0), worker.Outbox.Retention)
	assert.Equal(t, []string{"localhost:19092"}, worker.Kafka.Brokers)
	assert.Equal(t, 100, worker.Outbox.BatchSize)
}

func TestLoadWorker_RejectsValuesOutOfRangeAndNamesTheVariablesNotTheValues(t *testing.T) {
	cases := map[string]map[string]string{
		"WORKER_HTTP_PORT":        {"WORKER_HTTP_PORT": "70000"},
		"KAFKA_BROKERS":           {"KAFKA_BROKERS": "no-port"},
		"KAFKA_PUBLISH_TIMEOUT":   {"KAFKA_PUBLISH_TIMEOUT": "0"},
		"OUTBOX_POLL_INTERVAL_MS": {"OUTBOX_POLL_INTERVAL_MS": "5"},
		"OUTBOX_BATCH_SIZE":       {"OUTBOX_BATCH_SIZE": "5000"},
		"OUTBOX_MAX_ATTEMPTS":     {"OUTBOX_MAX_ATTEMPTS": "0"},
		"OUTBOX_STANDBY_INTERVAL": {"OUTBOX_STANDBY_INTERVAL": "0"},
		"OUTBOX_RETENTION_DAYS":   {"OUTBOX_RETENTION_DAYS": "-1"},
		"EXPIRY_INTERVAL":         {"EXPIRY_INTERVAL": "0"},
		"EXPIRY_BATCH_SIZE":       {"EXPIRY_BATCH_SIZE": "5000"},
		"SHUTDOWN_TIMEOUT":        {"SHUTDOWN_TIMEOUT": "0"},
	}
	for variable, env := range cases {
		t.Run(variable, func(t *testing.T) {
			_, err := LoadWorker(lookupOf(env))
			require.Error(t, err)
			assert.ErrorContains(t, err, variable)
		})
	}

	_, err := LoadWorker(lookupOf(map[string]string{"KAFKA_BROKERS": "secret-host-name-without-a-port"}))
	assert.NotContains(t, err.Error(), "secret-host-name", "an entry is named by position, never repeated")
	assert.ErrorContains(t, err, "entry 1")

	_, err = LoadWorker(lookupOf(map[string]string{"OUTBOX_BATCH_SIZE": "many"}))
	assert.ErrorContains(t, err, "OUTBOX_BATCH_SIZE must be an integer")
}

func TestLoadWorker_ReportsEveryProblemAtOnce(t *testing.T) {
	_, err := LoadWorker(lookupOf(map[string]string{"OUTBOX_BATCH_SIZE": "0", "KAFKA_BROKERS": "bad", "LOG_FORMAT": "xml", "DATABASE_PORT": "x"}))

	require.Error(t, err)
	for _, variable := range []string{"OUTBOX_BATCH_SIZE", "KAFKA_BROKERS", "LOG_FORMAT", "DATABASE_PORT"} {
		assert.ErrorContains(t, err, variable)
	}
}

func TestLoadWorker_RefusesTheDevelopmentDatabasePasswordOutsideDevelopmentAndTest(t *testing.T) {
	_, err := LoadWorker(lookupOf(map[string]string{"ENVIRONMENT": "production"}))
	assert.ErrorContains(t, err, "DATABASE_PASSWORD")

	for _, environment := range []string{"development", "test"} {
		_, err := LoadWorker(lookupOf(map[string]string{"ENVIRONMENT": environment}))
		assert.NoError(t, err, environment)
	}
	_, err = LoadWorker(lookupOf(map[string]string{"ENVIRONMENT": "production", "DATABASE_PASSWORD": "a-real-password"}))
	assert.NoError(t, err)
}

func TestLoadWorker_NeedsNoAuthSecretAndNoRedis(t *testing.T) {
	_, err := LoadWorker(lookupOf(map[string]string{"ENVIRONMENT": "production", "DATABASE_PASSWORD": "a-real-password"}))

	assert.NoError(t, err, "the worker reads neither AUTH_SECRET nor REDIS_*")
}

func TestLoadWorker_WarnsWhenEnvironmentIsUnsetAndTheDevelopmentPasswordIsInUse(t *testing.T) {
	worker, err := LoadWorker(lookupOf(nil))
	require.NoError(t, err)
	require.Len(t, worker.Warnings, 1)
	assert.Contains(t, worker.Warnings[0], "ENVIRONMENT is not set")

	explicit, err := LoadWorker(lookupOf(map[string]string{"ENVIRONMENT": "development"}))
	require.NoError(t, err)
	assert.Empty(t, explicit.Warnings, "an explicit development environment is deliberate")
}

func TestLoadWorkerHealth_NeedsNothingElse(t *testing.T) {
	settings, err := LoadWorkerHealth(lookupOf(map[string]string{"SERVER_HOST": "127.0.0.1", "WORKER_HTTP_PORT": "9100", "KAFKA_BROKERS": "broken"}))

	require.NoError(t, err, "the health check must work without valid Kafka or database settings")
	assert.Equal(t, HealthSettings{Host: "127.0.0.1", Port: 9100}, settings)

	_, err = LoadWorkerHealth(lookupOf(map[string]string{"WORKER_HTTP_PORT": "abc"}))
	assert.ErrorContains(t, err, "WORKER_HTTP_PORT")
}

// .env.example is what a new developer copies to .env. If it drifts from the loader's defaults, the documented setup
// stops matching the stack.
func TestLoadWorker_EnvExampleDocumentsTheDefaults(t *testing.T) {
	example, err := godotenv.Read("../../.env.example")
	require.NoError(t, err)
	for _, key := range []string{
		"KAFKA_BROKERS", "KAFKA_CLIENT_ID", "KAFKA_PUBLISH_TIMEOUT", "OUTBOX_POLL_INTERVAL_MS", "OUTBOX_BATCH_SIZE",
		"OUTBOX_MAX_ATTEMPTS", "OUTBOX_STANDBY_INTERVAL", "OUTBOX_RETENTION_DAYS", "EXPIRY_INTERVAL", "EXPIRY_BATCH_SIZE", "WORKER_HTTP_PORT",
	} {
		assert.Contains(t, example, key, "%s is documented in .env.example", key)
	}

	defaults, err := LoadWorker(lookupOf(nil))
	require.NoError(t, err)
	fromExample, err := LoadWorker(lookupOf(example))
	require.NoError(t, err)

	// An unset ENVIRONMENT warns and the example sets it, so the warnings differ on purpose.
	defaults.Warnings, fromExample.Warnings = nil, nil
	assert.Equal(t, defaults, fromExample)
}
