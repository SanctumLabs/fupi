package middleware

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/requestid"
)

// RequestIdMiddleware middleware that adds a request id to every incoming request
func RequestIdMiddleware() fiber.Handler {
	// Ref: https://docs.gofiber.io/api/middleware/requestid
	return requestid.New()
}
