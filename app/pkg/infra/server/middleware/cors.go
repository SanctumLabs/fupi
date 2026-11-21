package middleware

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
)

// CORSMiddleware is a middleware that allows cross-origin requests
func CORSMiddleware() fiber.Handler {
	return cors.New(cors.Config{
		AllowMethods: "GET,POST,HEAD,PUT,PATCH,DELETE",
		AllowHeaders: "Content-Type, Accept, Content-Length, Accept-Language, X-CSRF-Token, Authorization, Token",
	})
}
