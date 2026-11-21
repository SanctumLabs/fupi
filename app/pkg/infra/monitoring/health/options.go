package health

import "time"

type Option func(MonitoringHealthClient)

// ConnAttempts sets the number of attempts to establish a connection
func ConnDbAttempts(attempts int) Option {
	return func(client MonitoringHealthClient) {
		client.WithDbConnAttempts(attempts)
	}
}

// ConnTimeout sets the connection timeout
func ConnDbTimeout(timeout time.Duration) Option {
	return func(client MonitoringHealthClient) {
		client.WithDbConnTimeout(timeout)
	}
}
