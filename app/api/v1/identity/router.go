// Package identityapi exposes the Identity bounded context over HTTP.
package identityapi

import (
	"fmt"

	identityapp "github.com/sanctumlabs/curtz/app/internal/application/identity"
	"github.com/sanctumlabs/curtz/app/pkg/infra/server/router"
)

type identityRouter struct {
	svc     *identityapp.Service
	routes  []router.Route
	baseUri string
}

// NewRouter creates the Identity HTTP routes under the given base URI.
func NewRouter(baseUri string, svc *identityapp.Service) router.Router {
	rtr := &identityRouter{svc: svc, baseUri: baseUri}
	rtr.initRoutes()
	return rtr
}

func (rtr *identityRouter) Routes() []router.Route {
	return rtr.routes
}

func (rtr *identityRouter) initRoutes() {
	rtr.routes = []router.Route{
		router.NewPostRoute(fmt.Sprintf("%s/auth/register", rtr.baseUri), rtr.register),
		router.NewPostRoute(fmt.Sprintf("%s/auth/login", rtr.baseUri), rtr.login),
		router.NewPostRoute(fmt.Sprintf("%s/auth/oauth/token", rtr.baseUri), rtr.oauthToken),
		router.NewGetRoute(fmt.Sprintf("%s/auth/verify", rtr.baseUri), rtr.verify),
	}
}
