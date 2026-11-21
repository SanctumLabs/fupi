// Command migrator applies the SQL migrations in app/internal/adapters/postgres/migrations to the configured
// database and exits. It is the production caller of postgres.Migrate. The API never migrates at startup, so several
// replicas cannot race to migrate at boot, and the migrator can run with its own database role.
//
// Run it from the repository root (or set MIGRATIONS_PATH):
//
//	go run ./app/cmd/migrator
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
