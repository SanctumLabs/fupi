package postgres

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"net/url"
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

func Migrate(databaseURL string, migrationPath string, inDocker bool) error {
	ctx := context.Background()

	// Parse the database URL and properly append sslmode parameter
	parsedURL, urlErr := url.Parse(databaseURL)
	if urlErr != nil {
		slog.ErrorContext(ctx, "migrate: invalid DATABASE_URL", "error", urlErr)
		return urlErr
	}
	// Get existing query parameters or create new ones
	query := parsedURL.Query()

	// Add or override sslmode parameter
	query.Set("sslmode", "disable")
	query.Set("x-migrations-table", "bid_schema_migrations")

	// Update the URL with the modified query parameters
	parsedURL.RawQuery = query.Encode()

	// Use the updated URL
	databaseURL = parsedURL.String()

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
	defer m.Close()
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
