package probes

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/sanctumlabs/fupi/app/pkg/infra/monitoring/health"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func appWith(registry *health.Registry) *fiber.App {
	app := fiber.New()
	for _, route := range NewRouter(registry).Routes() {
		app.Add(route.Method(), route.Path(), route.Handler())
	}
	return app
}

func get(t *testing.T, app *fiber.App, path string) (int, string) {
	t.Helper()
	resp, err := app.Test(httptest.NewRequest("GET", path, nil))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(body)
}

func registryWith(postgres, redis error) *health.Registry {
	registry := health.NewRegistry(time.Second)
	registry.Add(health.Check{Name: "postgres", Required: true, Fn: func(context.Context) error { return postgres }})
	registry.Add(health.Check{Name: "redis", Required: false, Fn: func(context.Context) error { return redis }})
	return registry
}

func TestLiveness_IsAlwaysOkWhileTheProcessRuns(t *testing.T) {
	registry := registryWith(errors.New("down"), errors.New("down"))
	registry.SetDraining()

	status, body := get(t, appWith(registry), LivePath)

	assert.Equal(t, 200, status)
	assert.JSONEq(t, `{"status":"ok"}`, body)
}

func TestReadiness_ReportsEachDependency(t *testing.T) {
	cases := map[string]struct {
		registry   *health.Registry
		wantStatus int
		wantBody   string
	}{
		"all up":        {registryWith(nil, nil), 200, `{"status":"ok","checks":{"postgres":"up","redis":"up"}}`},
		"redis down":    {registryWith(nil, errors.New("x")), 200, `{"status":"degraded","checks":{"postgres":"up","redis":"down"}}`},
		"postgres down": {registryWith(errors.New("x"), nil), 503, `{"status":"unavailable","checks":{"postgres":"down","redis":"up"}}`},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			status, body := get(t, appWith(tc.registry), ReadyPath)

			assert.Equal(t, tc.wantStatus, status)
			assert.JSONEq(t, tc.wantBody, body)
		})
	}
}

func TestReadiness_ReturnsServiceUnavailableWhileDraining(t *testing.T) {
	registry := registryWith(nil, nil)
	registry.SetDraining()

	status, body := get(t, appWith(registry), ReadyPath)

	assert.Equal(t, 503, status)
	assert.JSONEq(t, `{"status":"draining","checks":{}}`, body)
}

// The endpoint is public, so a failing dependency's error text (addresses, user names) must stay in the log.
func TestReadiness_NeverLeaksErrorText(t *testing.T) {
	leak := errors.New("dial tcp 10.0.0.5:5432: password authentication failed for user fupi-user")

	_, body := get(t, appWith(registryWith(leak, leak)), ReadyPath)

	assert.NotContains(t, body, "10.0.0.5")
	assert.NotContains(t, body, "password")
	assert.NotContains(t, body, "fupi-user")
	var decoded map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &decoded))
}
