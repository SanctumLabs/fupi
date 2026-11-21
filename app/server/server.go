//go:build legacy

package server

import (
	"github.com/gin-gonic/gin"
	"github.com/sanctumlabs/curtz/app/config"
	_ "github.com/sanctumlabs/curtz/app/docs"
	"github.com/sanctumlabs/curtz/app/server/middleware"
	"github.com/sanctumlabs/curtz/app/server/router"
	"github.com/sanctumlabs/curtz/app/tools/logger"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

type server struct {
	config      *config.Config
	middlewares []middleware.Middleware
	logger      logger.Logger
	routers     []router.Router
}

func NewServer(cfg *config.Config) *server {
	log := logger.NewLogger("server")

	log.EnableJSONOutput(cfg.Logging.EnableJSONOutput)
	log.SetOutputLevel(logger.LogLevel(cfg.Logging.Level))

	return &server{
		config:      cfg,
		middlewares: []middleware.Middleware{},
		logger:      log,
		routers:     []router.Router{},
	}
}

func setMode(env string) string {
	switch env {
	case "development":
		return gin.DebugMode
	case "production":
		return gin.ReleaseMode
	case "test":
		return gin.TestMode
	}
	return gin.DebugMode
}

// InitRouter initializes the list of routers for the server
func (srv *server) InitRouter(routers ...router.Router) {
	srv.routers = append(srv.routers, routers...)
}

// UseMiddleware appends a new middleware to the request chain.
// This needs to be called before the API routes are configured.
func (srv *server) UseMiddleware(m middleware.Middleware) {
	srv.middlewares = append(srv.middlewares, m)
}

// CreateServer creates the server and returns it
func (srv *server) CreateServer() *gin.Engine {
	mode := setMode(srv.config.Env)

	gin.SetMode(mode)
	engine := gin.New()

	srv.registerMiddlewares(engine)
	srv.registerRouters(engine)

	return engine
}

func (srv *server) registerMiddlewares(engine *gin.Engine) {
	srv.logger.Debug("Registering middlewares...")
	for _, mid := range srv.middlewares {
		m := gin.HandlerFunc(mid)
		engine.Use(m)
	}
}

func (srv *server) registerRouters(engine *gin.Engine) {
	srv.logger.Debug("Registering routers...")

	for _, apiRouter := range srv.routers {
		for _, route := range apiRouter.Routes() {
			srv.logger.Debugf("Registering %s, %s", route.Method(), route.Path())
			engine.Handle(route.Method(), route.Path(), route.Handler())
		}
	}

	if srv.config.DocsEnabled {
		engine.GET("/docs/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	}
}
