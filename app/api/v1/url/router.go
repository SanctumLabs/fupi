// Package urlapi exposes the URL bounded context over HTTP: the authenticated management of a user's short urls and
// the public redirect.
package urlapi

import (
	"fmt"
	"regexp"

	urlapp "github.com/sanctumlabs/fupi/app/internal/application/url"
	"github.com/sanctumlabs/fupi/app/pkg/infra/server/router"
)

// RedirectPathPattern matches the paths the public redirect serves: one segment shaped like a short code (6-10 base62
// characters) or a custom alias (3-12 alphanumerics and dashes). The auth middleware exempts exactly these paths.
var RedirectPathPattern = regexp.MustCompile(`^/[a-zA-Z0-9-]{3,12}$`)

type urlRouter struct {
	svc          *urlapp.Service
	routes       []router.Route
	baseUri      string
	shortBaseURL string
}

// NewRouter creates the URL HTTP routes: management under baseUri and the redirect at the root. shortBaseURL is the
// externally visible address short urls are built on.
//
// Register it after every other router: the redirect matches any single-segment path, and Fiber serves the first route
// that matches.
func NewRouter(baseUri, shortBaseURL string, svc *urlapp.Service) router.Router {
	rtr := &urlRouter{svc: svc, baseUri: baseUri, shortBaseURL: shortBaseURL}
	rtr.initRoutes()
	return rtr
}

func (rtr *urlRouter) Routes() []router.Route {
	return rtr.routes
}

func (rtr *urlRouter) initRoutes() {
	rtr.routes = []router.Route{
		router.NewPostRoute(fmt.Sprintf("%s/urls", rtr.baseUri), rtr.create),
		router.NewGetRoute(fmt.Sprintf("%s/urls", rtr.baseUri), rtr.list),
		router.NewGetRoute(fmt.Sprintf("%s/urls/:id", rtr.baseUri), rtr.get),
		router.NewPatchRoute(fmt.Sprintf("%s/urls/:id", rtr.baseUri), rtr.update),
		router.NewDeleteRoute(fmt.Sprintf("%s/urls/:id", rtr.baseUri), rtr.delete),
		router.NewGetRoute("/:code", rtr.redirect),
	}
}
