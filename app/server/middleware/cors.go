//go:build legacy

package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/sanctumlabs/curtz/app/tools/logger"
)

func NewCORSMiddleware(defaultHeaders string) Middleware {
	log := logger.NewLogger("log-cors")
	return func(context *gin.Context) {
		// If "api-cors-header" is not given, but "api-enable-cors" is true, we set cors to "*"
		// otherwise, all head values will be passed to HTTP handler
		corsHeaders := defaultHeaders
		if corsHeaders == "" {
			corsHeaders = "*"
		}

		log.Debugf("CORS Header is enabled & set to: %s", corsHeaders)
		context.Header("Access-Control-Allow-Origin", corsHeaders)
		context.Header("Access-Control-Allow-Headers", "Origin, X-Requested-With, Content-Type, Accept, X-Registry-Auth, Authorization")
		context.Header("Access-Control-Allow-Methods", "HEAD, GET, POST, DELETE, PUT, OPTIONS")
	}
}
