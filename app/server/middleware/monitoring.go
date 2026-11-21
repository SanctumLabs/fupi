//go:build legacy

package middleware

import (
	sentrygin "github.com/getsentry/sentry-go/gin"
	"github.com/gin-gonic/gin"
	"github.com/sanctumlabs/curtz/app/config"
)

func NewMonitoringMiddleware(config config.MonitoringConfig) Middleware {
	return func(context *gin.Context) {

		if config.Sentry.Enabled {
			sentrygin.New(sentrygin.Options{
				Repanic: true,
			})
		}

		context.Next()
	}
}
