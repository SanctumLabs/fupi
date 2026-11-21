package metrics

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	prometheusmetrics "github.com/sanctumlabs/curtz/app/pkg/infra/monitoring/metrics/prometheus"
)

func init() {
	if IsEnabled() {
		slog.Info("Registering Prometheus metrics")
		prometheus.MustRegister(
			prometheusmetrics.RequestsTotalCounterVec,
			prometheusmetrics.ResponseDurationHistogramVec,
			// prometheusmetrics.BidsCreatedCounter,
			prometheusmetrics.IndividualBidCreationDuration,
			prometheusmetrics.IndividualOutboxEventDuration,
			prometheusmetrics.OutboxEventBatchDuration,
			prometheusmetrics.OutboxEventBatchCount,
			prometheusmetrics.OutboxEventProcessedCounter,
			prometheusmetrics.SendLeadRequestDuration,
			prometheusmetrics.SendNotificationRequestDuration,
			prometheusmetrics.SendBidPaymentRequestDuration,
			prometheusmetrics.RetrieveBidPaymentRequestDuration,
			prometheusmetrics.GetUserByIdRequestDuration,
			prometheusmetrics.GetListingInfoByIdRequestDuration,
		)
	}
}

// StartPrometheusMetricsServer starts a Prometheus metrics server
func StartPrometheusMetricsServer(config MetricsServerConfig) (*http.Server, error) {
	if config.Enabled {
		slog.Info("Starting Prometheus metrics server", "address", config.Addr)
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		server := &http.Server{
			Addr:         config.Addr,
			Handler:      mux,
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 10 * time.Second,
			IdleTimeout:  60 * time.Second,
		}
		go func() {
			if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Warn("Failed to start metrics server", "error", err)
			}
		}()

		return server, nil
	}
	return nil, nil
}
