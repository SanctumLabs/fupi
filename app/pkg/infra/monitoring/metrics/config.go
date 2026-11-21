package metrics

const (
	EnvMetricsAddr    = "METRICS_ADDRESS"
	EnvMetricsEnabled = "METRICS_ENABLED"
)

type (
	MetricsServerConfig struct {
		Addr    string `env:"METRICS_ADDRESS" env-default:"5555" env-description:"Metrics address used by metrics collectors to collect metrics" env-required:"false"`
		Enabled bool   `env:"METRICS_ENABLED" env-default:"false" env-description:"Whether scraping of metrics is enabled" env-required:"false"`
	}
)
