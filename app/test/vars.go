package test

import "time"

const (
	TEST_POSTGRES_DATABASE_VERSION = "postgres:16.2-alpine"
	TEST_DATABASE_NAME             = "curtz_database_test"
	TEST_DATABASE_USERNAME         = "curtz_user"
	TEST_DATABASE_PASSWORD         = "curtz_password"
	TEST_DATABASE_SCHEMA           = "public"
	TEST_DATABASE_STARTUP_TIMEOUT  = 5 * time.Second
	TEST_DATABASE_MAX_CONNS        = 10
	TEST_DATABASE_MIN_CONNS        = 5
	TEST_DATABASE_CONN_TIMEOUT     = 5 * time.Minute

	// defaultContainerReadyTimeout is intentionally generous: pulling the image
	// on a cold host can take several minutes before the container even starts.
	defaultContainerReadyTimeout = 5 * time.Minute

	// envContainerReadyTimeout overrides the ready-wait duration at runtime.
	// Accepts any Go duration string, e.g. TEST_CONTAINER_READY_TIMEOUT=3m.
	envContainerReadyTimeout = "TEST_CONTAINER_READY_TIMEOUT"

	// migrationRelPath is the migrations directory relative to the repo root.
	migrationRelPath = "app/internal/adapters/postgres/migrations"
)
