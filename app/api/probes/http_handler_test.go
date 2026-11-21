package probes

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sanctumlabs/fupi/app/pkg/infra/monitoring/health"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func httpGet(t *testing.T, handler http.Handler, method, path string) (int, string) {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(method, path, nil))
	body, err := io.ReadAll(recorder.Result().Body)
	require.NoError(t, err)
	return recorder.Code, string(body)
}

func workerRegistry(postgres, kafka error) *health.Registry {
	registry := health.NewRegistry(time.Second)
	registry.Add(health.Check{Name: "postgres", Required: true, Fn: func(context.Context) error { return postgres }})
	registry.Add(health.Check{Name: "kafka", Required: true, Fn: func(context.Context) error { return kafka }})
	return registry
}

func TestHTTPHandler_LivenessFollowsTheLoopNotTheDependencies(t *testing.T) {
	registry := workerRegistry(errors.New("down"), errors.New("down"))
	registry.SetDraining()

	status, body := httpGet(t, NewHTTPHandler(registry, func() bool { return true }), "GET", LivePath)
	assert.Equal(t, 200, status, "a down dependency is no reason to restart the process")
	assert.JSONEq(t, `{"status":"ok"}`, body)

	status, body = httpGet(t, NewHTTPHandler(registry, func() bool { return false }), "GET", LivePath)
	assert.Equal(t, 503, status, "a stalled loop is")
	assert.JSONEq(t, `{"status":"stalled"}`, body)
}

func TestHTTPHandler_ReadinessReportsEachDependencyLikeTheAPI(t *testing.T) {
	cases := map[string]struct {
		registry   *health.Registry
		wantStatus int
		wantBody   string
	}{
		"all up":     {workerRegistry(nil, nil), 200, `{"status":"ok","checks":{"postgres":"up","kafka":"up"}}`},
		"kafka down": {workerRegistry(nil, errors.New("x")), 503, `{"status":"unavailable","checks":{"postgres":"up","kafka":"down"}}`},
		"both down":  {workerRegistry(errors.New("x"), errors.New("x")), 503, `{"status":"unavailable","checks":{"postgres":"down","kafka":"down"}}`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			status, body := httpGet(t, NewHTTPHandler(tc.registry, func() bool { return true }), "GET", ReadyPath)

			assert.Equal(t, tc.wantStatus, status)
			assert.JSONEq(t, tc.wantBody, body)
		})
	}
}

func TestHTTPHandler_ReadinessIsUnavailableWhileDraining(t *testing.T) {
	registry := workerRegistry(nil, nil)
	registry.SetDraining()

	status, body := httpGet(t, NewHTTPHandler(registry, func() bool { return true }), "GET", ReadyPath)

	assert.Equal(t, 503, status)
	assert.JSONEq(t, `{"status":"draining","checks":{}}`, body)
}

func TestHTTPHandler_ServesOnlyTheTwoProbesAndOnlyGet(t *testing.T) {
	handler := NewHTTPHandler(workerRegistry(nil, nil), func() bool { return true })

	status, _ := httpGet(t, handler, "GET", "/metrics")
	assert.Equal(t, 404, status)
	status, _ = httpGet(t, handler, "POST", LivePath)
	assert.Equal(t, 405, status)
}
