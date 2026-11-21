//go:build legacy

package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/sanctumlabs/curtz/app/config"
	"github.com/sanctumlabs/curtz/app/tools/logger"
	"time"
)

// NewLoggingMiddleware sets up logging middleware in application
func NewLoggingMiddleware(config config.LoggingConfig) Middleware {
	log := logger.NewLogger("log-requests")
	log.EnableJSONOutput(config.EnableJSONOutput)

	return func(context *gin.Context) {
		start := time.Now()

		// before request
		context.Next()

		// get latency
		latency := time.Since(start)

		// Log request
		log.Infof("Incoming Request. Status: %d. Method: %s. Path: %s. Latency: %s", context.Writer.Status(), context.Request.Method, context.Request.URL.EscapedPath(), latency)
		// log.WithFields(logger.Fields{
		// 	"status":   context.Writer.Status(),
		// 	"method":   context.Request.Method,
		// 	"path":     context.Request.URL.EscapedPath(),
		// 	"duration": latency,
		// }).Info("Incoming Request")
	}
}
