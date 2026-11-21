//go:build legacy

package health

import (
	router2 "github.com/sanctumlabs/curtz/app/server/router"
)

// healthRouter is a router to talk to when we are checking health of the api
type healthRouter struct {
	routes []router2.Route
}

// NewHealthRouter initializes a new router
func NewHealthRouter() router2.Router {
	r := &healthRouter{}
	r.initRoutes()
	return r
}

// Routes returns the available routes to the controller
func (h *healthRouter) Routes() []router2.Route {
	return h.routes
}

func (h *healthRouter) initRoutes() {
	h.routes = []router2.Route{
		router2.NewGetRoute("/health", h.health),
	}
}
