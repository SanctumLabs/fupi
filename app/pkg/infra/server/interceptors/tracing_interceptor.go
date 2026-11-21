package interceptors

import (
	"context"
	"github.com/sanctumlabs/curtz/app/pkg/infra/tracing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// grpcServerTracingInterceptor extracts tracing from gRPC metadata
func GrpcServerTracingInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		// Extract tracing from metadata
		md, ok := metadata.FromIncomingContext(ctx)
		if ok {
			if traceIDs := md.Get(string(tracing.TraceIDKey)); len(traceIDs) > 0 {
				ctx = context.WithValue(ctx, tracing.TraceIDKey, traceIDs[0])
			}

			if correlationIDs := md.Get(string(tracing.CorrelationIDKey)); len(correlationIDs) > 0 {
				ctx = context.WithValue(ctx, tracing.CorrelationIDKey, correlationIDs[0])
			}

			if requestIds := md.Get(string(tracing.RequestIDKey)); len(requestIds) > 0 {
				ctx = context.WithValue(ctx, tracing.RequestIDKey, requestIds[0])
			}
		}

		// Ensure we have all necessary IDs
		ctx = tracing.NewContext(ctx)

		// Continue with the enhanced context
		return handler(ctx, req)
	}
}
