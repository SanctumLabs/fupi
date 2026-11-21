package database

import "time"

type PostgresqlDbOption func(PostgresDatabaseClient)

// ConnAttempts sets the number of attempts to establish a connection
func ConnAttempts(attempts int) PostgresqlDbOption {
	return func(client PostgresDatabaseClient) {
		client.WithConnAttempts(attempts)
	}
}

// ConnTimeout sets the connection timeout
func ConnTimeout(timeout time.Duration) PostgresqlDbOption {
	return func(client PostgresDatabaseClient) {
		client.WithConnTimeout(timeout)
	}
}
