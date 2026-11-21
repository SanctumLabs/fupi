package test

import (
	"context"
	"fmt"
	"log"
	"log/slog"

	"testing"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/sanctumlabs/curtz/app/pkg/infra/database"
	"github.com/sanctumlabs/curtz/app/pkg/infra/database/postgres"
	"github.com/testcontainers/testcontainers-go"
	postgresTestContainer "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// TestDatabaseConfig is the test database configuration
type TestDatabaseConfig struct {
	// Version is the database version to use
	Version string

	// Name is the database name
	Name string

	// Username is the username to use for the database
	Username string

	// Password is the database password
	Password string

	// Schema is the database schema
	Schema string

	// StartupTimeout is how long the wait strategy polls for the
	// "ready to accept connections" log line after the container is running.
	// Note: this does NOT cover Docker image pull time; for that, set a
	// sufficiently long -timeout on `go test`, or export
	// TEST_CONTAINER_READY_TIMEOUT=<duration>.
	StartupTimeout time.Duration
}

// DefaultTestDatabaseConfig returns sensible defaults, respecting the
// TEST_CONTAINER_READY_TIMEOUT environment variable when set.
func DefaultTestDatabaseConfig() TestDatabaseConfig {
	return TestDatabaseConfig{
		Version:        TEST_POSTGRES_DATABASE_VERSION,
		Name:           TEST_DATABASE_NAME,
		Username:       TEST_DATABASE_USERNAME,
		Password:       TEST_DATABASE_PASSWORD,
		Schema:         TEST_DATABASE_SCHEMA,
		StartupTimeout: containerReadyTimeout(),
	}
}

// DefaultPostgresDatabaseConfig returns a default PostgresDatabaseConfig for testing
func DefaultPostgresDatabaseConfig(host, port, connectionString string) postgres.PostgresDatabaseConfig {
	return postgres.PostgresDatabaseConfig{
		Host:        host,
		Username:    TEST_DATABASE_USERNAME,
		Password:    TEST_DATABASE_PASSWORD,
		Name:        TEST_DATABASE_NAME,
		Port:        port,
		Url:         connectionString,
		Schema:      TEST_DATABASE_SCHEMA,
		MaxConns:    TEST_DATABASE_MAX_CONNS,
		MinConns:    TEST_DATABASE_MIN_CONNS,
		ConnTimeout: TEST_DATABASE_CONN_TIMEOUT,
	}
}

func TestPostgresDatabaseContainer(ctx context.Context, config TestDatabaseConfig) (*postgresTestContainer.PostgresContainer, error) {
	if config.Version == "" {
		config.Version = TEST_POSTGRES_DATABASE_VERSION
	}

	if config.Name == "" {
		config.Name = TEST_DATABASE_NAME
	}

	if config.Username == "" {
		config.Username = TEST_DATABASE_USERNAME
	}

	if config.Password == "" {
		config.Password = TEST_DATABASE_PASSWORD
	}

	if config.StartupTimeout == 0 {
		config.StartupTimeout = containerReadyTimeout()
	}

	if config.Schema == "" {
		config.Schema = TEST_DATABASE_SCHEMA
	}

	dbContainer, err := postgresTestContainer.Run(
		ctx,
		config.Version,
		postgresTestContainer.WithDatabase(config.Name),
		postgresTestContainer.WithUsername(config.Username),
		postgresTestContainer.WithPassword(config.Password),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(config.StartupTimeout)),
	)
	if err != nil {
		log.Fatalf("failed to start database container: %v", err)
		return nil, err
	}

	return dbContainer, nil
}

// TestPostgresDatabaseClientHelper is a test helper that creates a test database client and runs migrations. It will fail the test immediately if any step fails.
func TestPostgresDatabaseClientHelper(t *testing.T, ctx context.Context) database.PostgresDatabaseClient {
	t.Helper()

	testDatabase, err := TestPostgresDatabaseContainer(ctx, DefaultTestDatabaseConfig())
	if err != nil {
		t.Fatalf("issue creating database container %s", err.Error())
	}

	// Always terminate the container when the test finishes, even on failure.
	// Use a fresh background context so a cancelled test context does not
	// prevent clean-up.
	t.Cleanup(func() {
		if err := testDatabase.Terminate(context.Background()); err != nil {
			t.Logf("warning: failed to terminate database container: %v", err)
		}
	})

	connectionString, err := testDatabase.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Error("failed to acquire database connection string")
	}

	host, err := testDatabase.Host(ctx)
	if err != nil {
		t.Fatalf("failed to acquire database host: %v", err)
	}

	port, err := nat.NewPort("tcp", "5432")
	if err != nil {
		t.Fatalf("failed to construct postgres port: %v", err)
	}

	mappedPort, err := testDatabase.MappedPort(ctx, port.Port())
	if err != nil {
		t.Fatalf("failed to acquire mapped database port: %v", err)
	}

	if err := RunDatabaseMigration(ctx, connectionString); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	testDatabaseClient, err := postgres.NewPostgresClient(DefaultPostgresDatabaseConfig(host, mappedPort.Port(), connectionString))
	if err != nil {
		t.Fatalf("failed to connect to database at %s: %v", connectionString, err)
	}

	return testDatabaseClient
}

// TestPostgresDatabaseClient creates a test database client with default configuration
func TestPostgresDatabaseClient(ctx context.Context) (database.PostgresDatabaseClient, error) {
	testDatabase, err := TestPostgresDatabaseContainer(ctx, DefaultTestDatabaseConfig())
	if err != nil {
		return nil, fmt.Errorf("issue creating database container %s", err.Error())
	}

	connectionString, err := testDatabase.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return nil, fmt.Errorf("failed to acquire database connection string: %w", err)
	}

	if err := RunDatabaseMigration(ctx, connectionString); err != nil {
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	host, err := testDatabase.Host(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire database host: %w", err)
	}

	port, err := nat.NewPort("tcp", "5432")
	if err != nil {
		return nil, fmt.Errorf("failed to construct postgres port: %w", err)
	}

	mappedPort, err := testDatabase.MappedPort(ctx, port.Port())
	if err != nil {
		return nil, fmt.Errorf("failed to acquire mapped database port: %w", err)
	}

	testDatabaseClient, err := TestDatabaseClient(ctx, DefaultPostgresDatabaseConfig(host, mappedPort.Port(), connectionString))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database at %s: %w", connectionString, err)
	}

	return testDatabaseClient, nil
}

// RunDatabaseMigration runs database migrations for a given connection string and migration path
func RunDatabaseMigration(ctx context.Context, connectionString string) error {
	migrationSourcePath := fmt.Sprintf("file://%s", migrationsDir())
	slog.InfoContext(ctx, "running migrations",
		slog.String("connectionString", connectionString),
		slog.String("path", migrationSourcePath),
	)

	if err := postgres.Migrate(connectionString, migrationSourcePath, false); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}
	return nil
}

// TestDatabaseClient creates a test database client with default configuration
func TestDatabaseClient(ctx context.Context, config postgres.PostgresDatabaseConfig) (database.PostgresDatabaseClient, error) {
	testDatabaseClient, err := postgres.NewPostgresClient(config)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database with config %v: %w", config, err)
	}

	return testDatabaseClient, nil
}
