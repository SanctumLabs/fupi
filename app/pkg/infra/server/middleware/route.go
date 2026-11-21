package middleware

import (
	"slices"
	"strings"
	"sync"

	"github.com/gofiber/fiber/v2"
)

// settle runs the app's error handler for an error a handler returned, so the response, and with it the status code, is
// final when the caller reads it. It reports the error as handled. Fiber's own logger middleware does the same; without
// it a middleware that wraps the handlers would see the status 200 of a request that is about to become a 500.
func settle(c *fiber.Ctx, err error) {
	if err == nil {
		return
	}
	if handlerErr := c.App().ErrorHandler(c, err); handlerErr != nil {
		_ = c.SendStatus(fiber.StatusInternalServerError)
	}
}

// routeTable finds the route pattern of a request from the routes registered on the app. It reads them on first use,
// which is after RegisterHandlers has run.
type routeTable struct {
	once     sync.Once
	byMethod map[string][]string
}

// template returns the route pattern that serves the request (/users/:id), which is the only form of the path that may
// become a metric label or part of a span name. The second result is false when no registered route matches, and the
// raw path must not be used then. requestPath is the path as it was before the handlers ran.
//
// Fiber's c.Route() is the route that served the request, except when a middleware answered it (401, a CORS preflight,
// the docs, an idempotency replay) or nothing matched: it then reports the middleware's own route, which is "/" for every
// app.Use(handler) and cannot be told from a registered "/" route. Those requests are matched against the registered routes in
// registration order, as Fiber's router would have.
func (t *routeTable) template(c *fiber.Ctx, requestPath string) (string, bool) {
	t.once.Do(func() { t.byMethod = registeredRoutes(c.App()) })

	paths := t.byMethod[c.Method()]
	if route := c.Route().Path; (route != "/" || requestPath == "/") && slices.Contains(paths, route) {
		return route, true
	}
	cfg := c.App().Config()
	for _, path := range paths {
		if fiber.RoutePatternMatch(requestPath, path, cfg) {
			return path, true
		}
	}
	return "", false
}

// registeredRoutes lists the route patterns of the app's handlers, without its middleware, per HTTP method.
func registeredRoutes(app *fiber.App) map[string][]string {
	byMethod := map[string][]string{}
	for _, route := range app.GetRoutes(true) {
		byMethod[route.Method] = append(byMethod[route.Method], route.Path)
	}
	return byMethod
}

// pathSet builds a lookup of paths for inSet.
func pathSet(paths []string) map[string]struct{} {
	set := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		set[strings.ToLower(path)] = struct{}{}
	}
	return set
}

// inSet reports whether path is in set the way Fiber's router would match it: ignoring case and trailing slashes.
func inSet(set map[string]struct{}, path string) bool {
	path = strings.ToLower(path)
	if _, ok := set[path]; ok {
		return true
	}
	if trimmed := strings.TrimRight(path, "/"); trimmed != path && trimmed != "" {
		_, ok := set[trimmed]
		return ok
	}
	return false
}
