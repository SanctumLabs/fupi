package health

import (
	"context"
	"time"
)

// MonitoringHealthClient is an interface containing a method set on handling an health checks on the system
type MonitoringHealthClient interface {
	Check(ctx context.Context) error

	Configure(...Option) MonitoringHealthClient

	WithDbConnAttempts(int)

	WithDbConnTimeout(time.Duration)
}
