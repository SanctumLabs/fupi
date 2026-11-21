package router

import (
	"net/http"

	"github.com/gofiber/fiber/v2"
)

// RouteWrapper wraps a route with extra functionality.
// It is passed in when creating a new route.
type RouteWrapper func(r Route) Route

// localRoute defines an individual API route to connect
// it implements Route.
type localRoute struct {
	method  string
	path    string
	handler func(ctx *fiber.Ctx) error
}

// Handler returns the APIFunc to let the server wrap it in middlewares.
func (l localRoute) Handler() func(ctx *fiber.Ctx) error {
	return l.handler
}

// Method returns the http method that the route responds to.
func (l localRoute) Method() string {
	return l.method
}

// Path returns the subpath where the route responds to.
func (l localRoute) Path() string {
	return l.path
}

// NewGetRoute initializes a new route with the http method GET.
func NewGetRoute(path string, handler func(ctx *fiber.Ctx) error, opts ...RouteWrapper) Route {
	return NewRoute(http.MethodGet, path, handler, opts...)
}

// NewPostRoute initializes a new route with the http method POST.
func NewPostRoute(path string, handler func(ctx *fiber.Ctx) error, opts ...RouteWrapper) Route {
	return NewRoute(http.MethodPost, path, handler, opts...)
}

// NewPutRoute initializes a new route with the http method PUT.
func NewPutRoute(path string, handler func(ctx *fiber.Ctx) error, opts ...RouteWrapper) Route {
	return NewRoute(http.MethodPut, path, handler, opts...)
}

// NewDeleteRoute initializes a new route with the http method DELETE.
func NewDeleteRoute(path string, handler func(ctx *fiber.Ctx) error, opts ...RouteWrapper) Route {
	return NewRoute(http.MethodDelete, path, handler, opts...)
}

// NewOptionsRoute initializes a new route with the http method OPTIONS.
func NewOptionsRoute(path string, handler func(ctx *fiber.Ctx) error, opts ...RouteWrapper) Route {
	return NewRoute(http.MethodOptions, path, handler, opts...)
}

// NewHeadRoute initializes a new route with the http method HEAD.
func NewHeadRoute(path string, handler func(ctx *fiber.Ctx) error, opts ...RouteWrapper) Route {
	return NewRoute(http.MethodHead, path, handler, opts...)
}

// NewPatchRoute initializes a new route with the http method PATCH.
func NewPatchRoute(path string, handler func(ctx *fiber.Ctx) error, opts ...RouteWrapper) Route {
	return NewRoute(http.MethodPatch, path, handler, opts...)
}
