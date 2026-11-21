package middleware

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/idempotency"
)

// HelmetMiddleware is a middleware that allows access to the swagger docs
func IdempotencyMiddleware() fiber.Handler {
	// Ref: https://docs.gofiber.io/api/middleware/idempotency
	return idempotency.New()
}
