package server

import (
	"context"
	"fmt"
	"github.com/sanctumlabs/curtz/app/pkg/infra/server/interceptors"
	"github.com/sanctumlabs/curtz/app/pkg/infra/server/middleware"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

type GrpcServer struct {
	grpcSrv *grpc.Server
	cfg     GrpcServerConfig
}

func NewGrpcServer(cfg GrpcServerConfig) *GrpcServer {
	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			interceptors.GrpcServerTracingInterceptor(),
			interceptors.GrpcServerMetricsInterceptor(),
			interceptors.GrpcServerRecoveryUnaryInterceptor(),
		),
	)
	reflection.Register(grpcServer)

	return &GrpcServer{
		grpcSrv: grpcServer,
		cfg:     cfg,
	}
}

// Server retrieves the underlying gRPC Server
func (srv *GrpcServer) Server() *grpc.Server {
	return srv.grpcSrv
}

func (srv *GrpcServer) Start(ctx context.Context, cleanup func()) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel() // Ensure cancel is called in all paths

	address := fmt.Sprintf("%s:%d", srv.cfg.Host, srv.cfg.Port)
	network := "tcp"

	l, err := net.Listen(network, address)
	if err != nil {
		slog.ErrorContext(ctx, "failed to listen to GRPC address", "error", err, "network", network, "address", address)
		return
	}

	slog.InfoContext(ctx, "🌏 server started...", "address", address)
	defer func() {
		if err1 := l.Close(); err != nil {
			slog.ErrorContext(ctx, "failed to close", "err", err1, "network", network, "address", address)
			<-ctx.Done()
		}
	}()

	// setup global recover
	defer middleware.GlobalRecover()

	err = srv.grpcSrv.Serve(l)
	if err != nil {
		slog.ErrorContext(ctx, "failed start gRPC server", "err", err, "network", network, "address", address)
		cancel()
		<-ctx.Done()
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	select {
	case v := <-quit:
		cleanup()
		slog.InfoContext(ctx, "signal.Notify", "signal", v)
	case done := <-ctx.Done():
		cleanup()
		slog.InfoContext(ctx, "ctx.Done", "app done", done)
	}
}

func (srv *GrpcServer) GracefulStop() {
	srv.grpcSrv.GracefulStop()
}
