package middleware

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
)

func LoggerMiddleware() fiber.Handler {
	// Ref: https://docs.gofiber.io/api/middleware/logger
	return logger.New()
}
