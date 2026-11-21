//go:build legacy

package client

import (
	"github.com/sanctumlabs/curtz/app/internal/core/contracts"
	"github.com/sanctumlabs/curtz/app/server/router"
)

// clientRouter is a router for the url API.
type clientRouter struct {
	urlSvc  contracts.UrlService
	userSvc contracts.UserService
	routes  []router.Route
}

// NewUrlRouter initializes a new router
func NewClientRouter(urlSvc contracts.UrlService, userSvc contracts.UserService) router.Router {
	r := &clientRouter{
		urlSvc:  urlSvc,
		userSvc: userSvc,
	}
	r.initRoutes()
	return r
}

// Routes returns the available routes to the controller
func (route *clientRouter) Routes() []router.Route {
	return route.routes
}

func (route *clientRouter) initRoutes() {
	route.routes = []router.Route{
		router.NewGetRoute("/:shortCode", route.handleRedirect),
		router.NewGetRoute("/auth/verify", route.handleVerification),
	}
}
