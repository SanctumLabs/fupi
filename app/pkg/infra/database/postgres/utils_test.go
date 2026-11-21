package postgres

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConnectionString_UrlWinsWhenSet(t *testing.T) {
	cfg := PostgresDatabaseConfig{Url: "postgres://u:p@h:1/d?sslmode=require", Host: "ignored", Port: "5432"}

	assert.Equal(t, "postgres://u:p@h:1/d?sslmode=require", ConnectionString(cfg))
}

func TestConnectionString_EscapesCredentialsSoTheyRoundTrip(t *testing.T) {
	password := "p@ss/w:rd?#%x y"
	cfg := PostgresDatabaseConfig{
		Host: "db.internal", Port: "5432", Name: "fupidb",
		Username: "fupi user", Password: password,
		SslMode: "disable", MaxConns: 30, MinConns: 5,
	}

	parsed, err := pgxpool.ParseConfig(ConnectionString(cfg))

	require.NoError(t, err)
	assert.Equal(t, password, parsed.ConnConfig.Password)
	assert.Equal(t, "fupi user", parsed.ConnConfig.User)
	assert.Equal(t, "db.internal", parsed.ConnConfig.Host)
	assert.Equal(t, uint16(5432), parsed.ConnConfig.Port)
	assert.Equal(t, "fupidb", parsed.ConnConfig.Database)
	assert.Equal(t, int32(30), parsed.MaxConns)
	assert.Equal(t, int32(5), parsed.MinConns)
}

func TestConnectionString_HonoursSslMode(t *testing.T) {
	base := PostgresDatabaseConfig{Host: "h", Port: "5432", Name: "d", Username: "u", Password: "p", MaxConns: 2, MinConns: 1}

	disabled := base
	disabled.SslMode = "disable"
	parsed, err := pgxpool.ParseConfig(ConnectionString(disabled))
	require.NoError(t, err)
	assert.Nil(t, parsed.ConnConfig.TLSConfig, "sslmode=disable must not negotiate TLS")

	required := base
	required.SslMode = "require"
	parsed, err = pgxpool.ParseConfig(ConnectionString(required))
	require.NoError(t, err)
	assert.NotNil(t, parsed.ConnConfig.TLSConfig, "sslmode=require must negotiate TLS")
}
