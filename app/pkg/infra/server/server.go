package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"time"

	"github.com/sanctumlabs/fupi/app/api/openapi"
	"github.com/sanctumlabs/fupi/app/pkg/infra/logger"
	"github.com/sanctumlabs/fupi/app/pkg/infra/server/middleware"
	"github.com/sanctumlabs/fupi/app/pkg/infra/server/router"

	"github.com/bytedance/sonic"
	"github.com/gofiber/contrib/swagger"
	"github.com/gofiber/fiber/v2"
)

type Server struct {
	app *fiber.App
	cfg ServerConfig
	log logger.Logger
}

// NewServer creates a new server
func NewServer(cfg ServerConfig) *Server {
	appLogger := logger.New(nil)

	app := fiber.New(fiber.Config{
		ServerHeader: cfg.Header,
		AppName:      cfg.AppName,
		// Using a custom encoder and decoder to marshal and unmarshal JSON
		// ref: https://github.com/bytedance/sonic
		JSONEncoder: sonic.Marshal,
		JSONDecoder: sonic.Unmarshal,
	})

	// middleware. Tracing comes first so every middleware behind it, and the access log in particular, sees the request's span.
	app.Use(middleware.OTelMiddleware(middleware.OTelConfig{SkipPaths: cfg.ProbePaths}))
	app.Use(middleware.RequestIdMiddleware())
	app.Use(middleware.AccessLogMiddleware(cfg.ProbePaths))
	app.Use(middleware.CORSMiddleware())

	// The spec is generated from the handler annotations and embedded (ADR-0023). The docs sit before the auth middleware,
	// which is added later, so they are public. The swagger middleware matches /docs exactly, hence the rewrite of /docs/.
	app.Use(func(ctx *fiber.Ctx) error {
		if ctx.Path() == "/docs/" {
			ctx.Path("/docs")
		}
		return ctx.Next()
	})
	app.Use(middleware.SwaggerMiddleware(swagger.Config{
		BasePath:    "/",
		Path:        "docs",
		FilePath:    "docs/swagger.json",
		FileContent: openapi.Spec,
		Title:       "Fupi API Docs",
	}))

	app.Use(middleware.HelmetMiddleware())
	app.Use(middleware.IdempotencyMiddleware())

	app.Use(middleware.RecoverMiddleware())

	return &Server{
		app: app,
		cfg: cfg,
		log: appLogger,
	}
}

// Serve listens on the configured port and blocks until ctx is cancelled or the listener fails. See ServeListener.
func (srv *Server) Serve(ctx context.Context, timeout time.Duration, onDrain func()) error {
	ln, err := net.Listen("tcp", net.JoinHostPort(srv.cfg.Host, strconv.Itoa(srv.cfg.Port)))
	if err != nil {
		return fmt.Errorf("listen on port %d: %w", srv.cfg.Port, err)
	}

	slog.Info("listening", "port", srv.cfg.Port)
	return srv.ServeListener(ctx, ln, timeout, onDrain)
}

// ServeListener serves on ln until ctx is cancelled or the listener fails. When ctx is cancelled it calls onDrain
// (use it to turn readiness to 503), stops accepting connections, and waits up to timeout for in-flight requests to
// finish. It returns nil after a clean drain and an error if the listener failed or the deadline passed first. When it
// returns, ln is closed and nothing is accepting connections.
func (srv *Server) ServeListener(ctx context.Context, ln net.Listener, timeout time.Duration, onDrain func()) error {
	if ctx.Err() != nil {
		// Cancelled before serving began (a SIGTERM during startup): there is nothing to drain.
		if onDrain != nil {
			onDrain()
		}
		_ = ln.Close()
		return nil
	}

	listenErr := make(chan error, 1)
	go func() { listenErr <- srv.app.Listener(ln) }()

	select {
	case err := <-listenErr:
		return err
	case <-ctx.Done():
	}

	if onDrain != nil {
		onDrain()
	}
	slog.Info("shutting down server", "timeout", timeout.String())
	shutdownErr := srv.app.ShutdownWithTimeout(timeout)

	// Shutdown can run before fasthttp has registered the listener, in which case it closes nothing and serving would
	// start anyway. Closing the listener ourselves ends that, and the wait makes sure Serve is gone before we return.
	_ = ln.Close()
	if shutdownErr != nil {
		return fmt.Errorf("shut down within %s: %w", timeout, shutdownErr)
	}
	select {
	case <-listenErr:
	case <-time.After(timeout):
		return fmt.Errorf("server did not stop within %s", timeout)
	}
	return nil
}

// Shutdown shutdowns the server
func (srv *Server) Shutdown() error {
	srv.log.Infow("shutting down server", "port", srv.cfg.Port)
	return srv.app.Shutdown()
}

// Use registers a middleware that runs for every request. Call it before RegisterHandlers.
func (srv *Server) Use(handlers ...fiber.Handler) {
	for _, handler := range handlers {
		srv.app.Use(handler)
	}
}

// App exposes the underlying Fiber app so tests can drive it in-process without binding a port.
func (srv *Server) App() *fiber.App {
	return srv.app
}

// RegisterHandlers registers all the handlers for the user v1 endpoint
func (srv *Server) RegisterHandlers(router []router.Router) {
	for _, r := range router {
		routes := r.Routes()
		for _, route := range routes {
			srv.app.Add(route.Method(), route.Path(), route.Handler())
		}
	}
}
