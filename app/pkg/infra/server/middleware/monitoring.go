package middleware

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/monitor"
)

func MonitoringMiddleware() fiber.Handler {
	// Ref: https://docs.gofiber.io/api/middleware/monitor
	return monitor.New(monitor.Config{Title: "Bids Service Metrics Page"})
}
