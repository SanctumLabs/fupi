package prometheusmetrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// increment increments a Prometheus Counter metric with a given counter and labels
func Increment(counter prometheus.Counter) {
	counter.Inc()
}

// StartTimer starts a Prometheus Timer metric
func StartTimer(summary prometheus.Observer) *prometheus.Timer {
	timer := prometheus.NewTimer(summary)
	return timer
}

// ObserveDuration records duration for a given Prometheus timer
func ObserveDuration(timer *prometheus.Timer) {
	timer.ObserveDuration()
}

func RecordRequest(method, endpoint string) {
	RequestsTotalCounterVec.WithLabelValues(method, endpoint).Inc()
}

func RecordGrpcRequest(method string) {
	RequestsTotalCounterVec.WithLabelValues(method, "grpc").Inc()
}

func ObserveResponseDuration(method, endpoint string, durationSeconds float64) {
	ResponseDurationHistogramVec.WithLabelValues(method, endpoint).Observe(durationSeconds)
}

// TrackMetrics sets up metrics tracking and returns a cleanup function
func TrackMetrics(method, endpoint string) func() {
	startTime := time.Now()
	RecordRequest(method, endpoint)

	return func() {
		ObserveResponseDuration(method, endpoint, time.Since(startTime).Seconds())
	}
}
