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

// The URL parse error quotes the whole URL, password included, and Migrate logs the error it gets back.
func TestMigrationURL_ARejectedURLNeverLeaksItsPassword(t *testing.T) {
	_, err := migrationURL("postgres://u:TOPSECRET@[[::1]]:5432/d")

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "TOPSECRET")
}
