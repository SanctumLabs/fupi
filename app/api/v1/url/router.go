//go:build legacy

package url

import (
	"fmt"

	"github.com/sanctumlabs/curtz/app/internal/core/contracts"
	"github.com/sanctumlabs/curtz/app/server/router"
)

// urlRouter is a router for the url API.
type urlRouter struct {
	urlSvc      contracts.UrlService
	urlReadSvc  contracts.UrlReadService
	urlWriteSvc contracts.UrlWriteService
	routes      []router.Route
	baseUri     string
}

// NewUrlRouter initializes a new router
func NewUrlRouter(baseUri string, urlSvc contracts.UrlService, urlReadSvc contracts.UrlReadService, urlWriteSvc contracts.UrlWriteService) router.Router {
	r := &urlRouter{
		urlSvc:      urlSvc,
		urlReadSvc:  urlReadSvc,
		urlWriteSvc: urlWriteSvc,
		baseUri:     baseUri,
	}
	r.initRoutes()
	return r
}

// Routes returns the available routes to the controller
func (route *urlRouter) Routes() []router.Route {
	return route.routes
}

func (route *urlRouter) initRoutes() {
	route.routes = []router.Route{
		router.NewPostRoute(fmt.Sprintf("%s/urls", route.baseUri), route.createShortUrl),
		router.NewGetRoute(fmt.Sprintf("%s/urls", route.baseUri), route.getAllUrls),
		router.NewGetRoute(fmt.Sprintf("%s/urls/:id", route.baseUri), route.getUrlById),
		router.NewDeleteRoute(fmt.Sprintf("%s/urls/:id", route.baseUri), route.deleteUrl),
		router.NewPatchRoute(fmt.Sprintf("%s/urls/:id", route.baseUri), route.updateUrl),
	}
}
