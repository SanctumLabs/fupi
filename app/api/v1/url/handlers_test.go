//go:build legacy

package url

import (
	"fmt"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/sanctumlabs/curtz/app/server/router"
	"github.com/sanctumlabs/curtz/app/test/mocks"
)

var baseURI = "/api/v1/curtz"

func createUrlRouter(t *testing.T) (*urlRouter, mocks.MockUrlService, mocks.MockUrlReadService, mocks.MockUrlWriteService) {
	mockCtrl := gomock.NewController(t)
	mockUrlSvc := mocks.NewMockUrlService(mockCtrl)
	mockUrlReadSvc := mocks.NewMockUrlReadService(mockCtrl)
	mockUrlWriteSvc := mocks.NewMockUrlWriteService(mockCtrl)

	urlRouter := &urlRouter{
		urlSvc:      mockUrlSvc,
		urlReadSvc:  mockUrlReadSvc,
		urlWriteSvc: mockUrlWriteSvc,
		baseUri:     baseURI,
		routes:      []router.Route{},
	}

	routes := []router.Route{
		router.NewPostRoute(fmt.Sprintf("%s/urls", baseURI), urlRouter.createShortUrl),
		router.NewGetRoute(fmt.Sprintf("%s/urls", baseURI), urlRouter.getAllUrls),
		router.NewGetRoute(fmt.Sprintf("%s/urls/:id", baseURI), urlRouter.getUrlById),
		router.NewDeleteRoute(fmt.Sprintf("%s/urls/:id", baseURI), urlRouter.deleteUrl),
		router.NewPatchRoute(fmt.Sprintf("%s/urls/:id", baseURI), urlRouter.updateUrl),
	}

	urlRouter.routes = append(urlRouter.routes, routes...)

	return urlRouter, *mockUrlSvc, *mockUrlReadSvc, *mockUrlWriteSvc
}
