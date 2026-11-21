package server

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/sanctumlabs/curtz/app/pkg/infra/server/router"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The server must start even when no OpenAPI spec has been generated; a missing docs file is not
// a reason to take the service down.
func TestNewServer_StartsWithoutSwaggerSpecs(t *testing.T) {
	require.NotPanics(t, func() {
		srv := NewServer(ServerConfig{Port: 0, AppName: "curtz-test"})
		require.NotNil(t, srv)
		require.NotNil(t, srv.App())
	})
}

type stubRouter struct{ routes []router.Route }

func (s stubRouter) Routes() []router.Route { return s.routes }

func TestServer_RegisterHandlers(t *testing.T) {
	srv := NewServer(ServerConfig{Port: 0, AppName: "curtz-test"})
	srv.RegisterHandlers([]router.Router{
		stubRouter{routes: []router.Route{
			router.NewGetRoute("/ping", func(ctx *fiber.Ctx) error {
				return ctx.SendString("pong")
			}),
		}},
	})

	resp, err := srv.App().Test(httptest.NewRequest("GET", "/ping", nil))
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusOK, resp.StatusCode)
}

func TestServer_UseRegistersMiddleware(t *testing.T) {
	srv := NewServer(ServerConfig{Port: 0, AppName: "curtz-test"})

	called := false
	srv.Use(func(ctx *fiber.Ctx) error {
		called = true
		return ctx.Next()
	})
	srv.RegisterHandlers([]router.Router{
		stubRouter{routes: []router.Route{
			router.NewGetRoute("/ping", func(ctx *fiber.Ctx) error { return ctx.SendStatus(fiber.StatusNoContent) }),
		}},
	})

	_, err := srv.App().Test(httptest.NewRequest("GET", "/ping", nil))
	require.NoError(t, err)
	assert.True(t, called, "registered middleware should run for a request")
}
