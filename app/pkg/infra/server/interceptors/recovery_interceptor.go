package interceptors

import (
	"context"
	"log/slog"

	recovery "github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/recovery"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var customRecoveryFunc recovery.RecoveryHandlerFuncContext

func GrpcServerRecoveryUnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		customRecoveryFunc := func(ctx context.Context, p any) (err error) {
			slog.ErrorContext(ctx, "Panic occurred", "panic", p, "req", req, "info", info)
			return status.Errorf(codes.Unknown, "internal error: %v", p)
		}

		opts := []recovery.Option{
			recovery.WithRecoveryHandlerContext(customRecoveryFunc),
		}

		return recovery.UnaryServerInterceptor(opts...)(ctx, req, info, handler)
	}
}
