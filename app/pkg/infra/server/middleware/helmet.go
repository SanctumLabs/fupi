package middleware

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/helmet"
)

// HelmetMiddleware is a middleware that allows access to the swagger docs
func HelmetMiddleware() fiber.Handler {
	// Ref: https://docs.gofiber.io/api/middleware/helmet
	return helmet.New()
}
