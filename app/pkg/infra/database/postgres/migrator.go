package postgres

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"

	// migrate tools
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

const (
	_defaultAttempts = 5
	_defaultTimeout  = time.Second
)

// migrationsTable is the table golang-migrate records its state in. `make migrate` and the compose migrate job use
// the same name, so every way of running the migrations agrees on what has been applied.
const migrationsTable = "schema_migrations"

// migrationURL prepares a database URL for golang-migrate: it selects the migrations table, defaults sslmode to
// disable only when the URL does not say otherwise, and drops the pool_* parameters that pgxpool understands but the
// migration driver would send to the server as unknown settings.
func migrationURL(databaseURL string) (string, error) {
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		// The parse error quotes the whole URL, password included, and Migrate logs what it returns.
		return "", errors.New("the database URL is not a valid URL")
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

func Migrate(databaseURL string, migrationPath string, inDocker bool) error {
	ctx := context.Background()

	databaseURL, urlErr := migrationURL(databaseURL)
	if urlErr != nil {
		slog.ErrorContext(ctx, "migrate: invalid DATABASE_URL", "error", urlErr)
		return urlErr
	}

	var (
		attempts = _defaultAttempts
		err      error
		m        *migrate.Migrate
	)

	for attempts > 0 {
		m, err = migrate.New(migrationPath, databaseURL)
		if err == nil {
			break
		}

		log.Printf("Migrate: postgres is trying to connect, attempts left: %d\n", attempts)
		time.Sleep(_defaultTimeout)
		attempts--
	}

	if err != nil {
		slog.ErrorContext(ctx, "migrate: postgres connection error", "error", err)
		return err
	}

	err = m.Up()
	defer func() { _, _ = m.Close() }()
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		slog.ErrorContext(ctx, "migrate: up error", "error", err)
		return err
	}

	if errors.Is(err, migrate.ErrNoChange) {
		slog.InfoContext(ctx, "migrate: no change")
		return nil
	}

	slog.InfoContext(ctx, "migrate: up success")
	return nil
}
