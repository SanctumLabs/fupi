package metrics

import (
	envutils "github.com/sanctumlabs/curtz/app/pkg/infra/env"
)

func IsEnabled() bool {
	return envutils.NewEnvConfig().EnvBoolOr(EnvMetricsEnabled, false)
}
