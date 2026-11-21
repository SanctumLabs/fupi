//go:build legacy

package router

import "github.com/gin-gonic/gin"

// Router specifies an interface to specify a group of routes to add to the server
type Router interface {
	Routes() []Route
}

type Route interface {
	// Handler returns the raw function to create the HTTP handler
	Handler() func(ctx *gin.Context)

	// Method returns the HTTP method that the route responds to
	Method() string

	// Path returns the subpath where the route responds to
	Path() string
}

// NewRoute initializes a new local route for the router.
func NewRoute(method, path string, handler func(ctx *gin.Context), opts ...RouteWrapper) Route {
	var r Route = localRoute{method, path, handler}
	for _, o := range opts {
		r = o(r)
	}
	return r
}
