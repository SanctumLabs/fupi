package prometheusmetrics

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func RecordMetrics(ctx context.Context, counter prometheus.Counter) {
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				counter.Inc()
			}
		}
	}()
}
