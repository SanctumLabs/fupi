package middleware

import (
	"github.com/gofiber/contrib/swagger"
	"github.com/gofiber/fiber/v2"
)

// SwaggerMiddleware is a middleware that allows access to the swagger docs
func SwaggerMiddleware(cfg swagger.Config) fiber.Handler {
	return swagger.New(cfg)
}
