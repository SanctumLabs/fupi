package grpc

import (
	"context"
	"github.com/sanctumlabs/curtz/app/pkg/infra/tracing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// grpcUnaryClientInterceptor adds tracing to outgoing gRPC requests
func grpcUnaryClientInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		// Create metadata with tracing information
		md := metadata.Pairs(
			string(tracing.TraceIDKey), tracing.GetTraceID(ctx),
			string(tracing.RequestIDKey), tracing.GetRequestID(ctx),
			string(tracing.CorrelationIDKey), tracing.GetCorrelationID(ctx),
		)

		// Merge with existing metadata
		outCtx := metadata.NewOutgoingContext(ctx, md)

		// Make the call with enhanced context
		return invoker(outCtx, method, req, reply, cc, opts...)
	}
}

// NewGRPCClient creates a new gRPC client connection with tracing enabled
func NewGRPCClient(target string) (*grpc.ClientConn, error) {
	return grpc.NewClient(
		target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(grpcUnaryClientInterceptor()),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(10*1024*1024), // 10MB
			grpc.MaxCallSendMsgSize(10*1024*1024), // 10MB
		),
	)
}
