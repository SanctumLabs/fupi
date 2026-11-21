package postgres

import "time"

const (
	EnvDatabaseHost            = "DATABASE_HOST"
	EnvDatabasePort            = "DATABASE_PORT"
	EnvDatabaseUrl             = "DATABASE_URL"
	EnvDatabase                = "DATABASE_NAME"
	EnvDatabaseSchema          = "DATABASE_SCHEMA"
	EnvDatabaseUsername        = "DATABASE_USERNAME"
	EnvDatabasePassword        = "DATABASE_PASSWORD"
	EnvDatabaseSslMode         = "DATABASE_SSL_MODE"
	EnvDatabaseMaxConns        = "DATABASE_MAX_CONNS"
	EnvDatabaseMinConns        = "DATABASE_MIN_CONNS"
	EnvDatabaseMaxConnLifetime = "DATABASE_MAX_CONN_LIFETIME"
	EnvDatabaseMaxConnIdleTime = "DATABASE_MAX_CONN_IDLE_TIME"
	EnvDatabaseConnTimeout     = "DATABASE_CONN_TIMEOUT"
	EnvDatabaseQueryTimeout    = "DATABASE_QUERY_TIMEOUT"
)

const (
	defaultConnAttempts               = 3
	defaultConnTimeout  time.Duration = time.Second

	OperationTypeDefault  = "default"
	OperationTypeCritical = "critical"
	OperationTypeRead     = "read"
	OperationTypeBulk     = "bulk"
)
