package prometheusmetrics

import "github.com/prometheus/client_golang/prometheus"

var (

	// must be registered
	nodeUsageGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "bid-service_gauge",
		Help: "Monitoring node usage",
	}, []string{"node", "namespace"})
)
