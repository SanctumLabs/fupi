package interceptors

import (
	"context"
	envutils "github.com/sanctumlabs/curtz/app/pkg/infra/env"
	"github.com/sanctumlabs/curtz/app/pkg/infra/monitoring/metrics"
	prometheusmetrics "github.com/sanctumlabs/curtz/app/pkg/infra/monitoring/metrics/prometheus"

	"google.golang.org/grpc"
)

// grpcServerMetricsInterceptor adds metrics to gRPC requests
func GrpcServerMetricsInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		// track metrics per handler if metrics are enabled, this has been defaulted to false by default
		if envutils.NewEnvConfig().EnvBoolOr(metrics.EnvMetricsEnabled, false) {
			methodName := extractMethodName(info.FullMethod)
			defer prometheusmetrics.TrackMetrics(methodName, info.FullMethod)()
		}

		return handler(ctx, req)
	}
}
