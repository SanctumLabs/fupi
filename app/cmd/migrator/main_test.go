package main

import (
	"errors"
	"testing"

	"github.com/sanctumlabs/fupi/app/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func lookupOf(env map[string]string) config.Lookup {
	return func(key string) (string, bool) {
		value, ok := env[key]
		return value, ok
	}
}

type recorder struct {
	calls []call
	err   error
}

type call struct {
	url, path string
	inDocker  bool
}

func (r *recorder) migrate(databaseURL, migrationPath string, inDocker bool) error {
	r.calls = append(r.calls, call{databaseURL, migrationPath, inDocker})
	return r.err
}

func TestRun_MigratesTheConfiguredDatabase(t *testing.T) {
	dir := t.TempDir()
	rec := &recorder{}

	err := run(lookupOf(map[string]string{
		"DATABASE_HOST": "db.internal", "DATABASE_PORT": "6432", "MIGRATIONS_PATH": dir,
	}), rec.migrate)

	require.NoError(t, err)
	require.Len(t, rec.calls, 1)
	assert.Contains(t, rec.calls[0].url, "postgres://fupi-user:fupi-pass@db.internal:6432/fupidb?")
	assert.Equal(t, "file://"+dir, rec.calls[0].path)
	assert.False(t, rec.calls[0].inDocker)
}

func TestRun_RefusesTheDevelopmentPasswordInProduction(t *testing.T) {
	rec := &recorder{}

	err := run(lookupOf(map[string]string{"ENVIRONMENT": "production", "MIGRATIONS_PATH": t.TempDir()}), rec.migrate)

	require.Error(t, err)
	assert.ErrorContains(t, err, "DATABASE_PASSWORD")
	assert.Empty(t, rec.calls, "nothing may be migrated with a development password in production")
}

// The migrator needs only the database section; it must not demand an AUTH_SECRET.
func TestRun_DoesNotNeedAnAuthSecret(t *testing.T) {
	rec := &recorder{}

	err := run(lookupOf(map[string]string{
		"ENVIRONMENT": "production", "DATABASE_PASSWORD": "a-real-password", "MIGRATIONS_PATH": t.TempDir(),
	}), rec.migrate)

	require.NoError(t, err)
	assert.Len(t, rec.calls, 1)
}

func TestRun_FailsWhenTheMigrationsDirectoryIsMissing(t *testing.T) {
	rec := &recorder{}

	err := run(lookupOf(map[string]string{"MIGRATIONS_PATH": "/does/not/exist"}), rec.migrate)

	require.Error(t, err)
	assert.ErrorContains(t, err, "/does/not/exist")
	assert.Empty(t, rec.calls)
}

func TestRun_ReturnsTheMigrationError(t *testing.T) {
	rec := &recorder{err: errors.New("dirty database version 2")}

	err := run(lookupOf(map[string]string{"MIGRATIONS_PATH": t.TempDir()}), rec.migrate)

	assert.ErrorContains(t, err, "dirty database version 2")
}
