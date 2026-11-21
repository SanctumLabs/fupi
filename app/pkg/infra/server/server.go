package server

import (
	"fmt"
	"os"

	"github.com/sanctumlabs/curtz/app/pkg/infra/logger"
	"github.com/sanctumlabs/curtz/app/pkg/infra/server/middleware"
	"github.com/sanctumlabs/curtz/app/pkg/infra/server/router"

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

	// middleware
	app.Use(middleware.RequestIdMiddleware())
	app.Use(middleware.LoggerMiddleware())
	app.Use(middleware.CORSMiddleware())

	// Swagger specs are optional: the server must still start when they have not been generated
	// (a fresh checkout, or a test binary running from a different working directory).
	for _, cfg := range []swagger.Config{
		{
			BasePath: "/",
			Path:     "docs/v1",
			FilePath: "./api/openapi-spec/curtz_v1.swagger.json",
			Title:    "Curtz V1 API Docs",
		},
		{
			BasePath: "/",
			Path:     "docs/monitoring",
			FilePath: "./api/openapi-spec/monitoring.swagger.json",
			Title:    "Curtz Monitoring API Docs",
		},
	} {
		if _, err := os.Stat(cfg.FilePath); err != nil {
			appLogger.Warnw("skipping swagger docs: spec file not found", "path", cfg.FilePath)
			continue
		}
		app.Use(middleware.SwaggerMiddleware(cfg))
	}

	app.Use(middleware.HelmetMiddleware())
	app.Use(middleware.IdempotencyMiddleware())

	app.Get("/metrics", middleware.MonitoringMiddleware())

	app.Use(middleware.RecoverMiddleware())

	return &Server{
		app: app,
		cfg: cfg,
		log: appLogger,
	}
}

func (srv *Server) Listen() error {
	srv.log.Infow("Listening on port", "port", srv.cfg.Port)
	return srv.app.Listen(fmt.Sprintf(":%d", srv.cfg.Port))
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
