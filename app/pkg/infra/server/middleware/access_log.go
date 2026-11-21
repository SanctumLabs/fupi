package middleware

import (
	"log/slog"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

// AccessLogMiddleware writes one line per request to the default slog logger: method, route, path, status, duration,
// response bytes (-1 for a streamed body) and request ID. The line is logged with the request context, so it carries the trace and span IDs when
// OTelMiddleware is in front of it. Requests to quietPaths (the health probes) are logged at debug level only, so a
// probe polled every few seconds does not fill the log. Put it behind RequestIdMiddleware.
func AccessLogMiddleware(quietPaths []string) fiber.Handler {
	quiet := pathSet(quietPaths)
	routes := &routeTable{}

	return func(c *fiber.Ctx) error {
		start := time.Now()
		path := strings.Clone(c.Path())

		settle(c, c.Next())

		level := slog.LevelInfo
		if inSet(quiet, path) {
			level = slog.LevelDebug
		}
		route, _ := routes.template(c, path)
		requestID, _ := c.Locals("requestid").(string)
		// A streamed body is not in memory; counting it would buffer all of it. Its length is unknown here.
		bytes := -1
		if !c.Response().IsBodyStream() {
			bytes = len(c.Response().Body())
		}

		slog.LogAttrs(c.UserContext(), level, "request",
			slog.String("method", c.Method()),
			slog.String("route", route),
			slog.String("path", path),
			slog.Int("status", c.Response().StatusCode()),
			slog.Float64("duration_ms", float64(time.Since(start).Microseconds())/1000),
			slog.Int("bytes", bytes),
			slog.String("request_id", requestID),
		)
		return nil
	}
}
