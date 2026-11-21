package probes

import (
	"encoding/json"
	"net/http"

	"github.com/sanctumlabs/fupi/app/pkg/infra/monitoring/health"
)

// NewHTTPHandler serves the same two probes as NewRouter over net/http, for processes that have no Fiber app (the outbox
// worker). Liveness answers 200 while alive returns true and 503 when it does not, which is how a wedged loop gets a
// container restarted; readiness answers the registry's report exactly as the API does.
func NewHTTPHandler(registry *health.Registry, alive func() bool) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+LivePath, func(w http.ResponseWriter, _ *http.Request) {
		if alive() {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
			return
		}
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "stalled"})
	})
	mux.HandleFunc("GET "+ReadyPath, func(w http.ResponseWriter, r *http.Request) {
		report := registry.Run(r.Context())
		status := http.StatusOK
		if !report.Ready() {
			status = http.StatusServiceUnavailable
		}
		writeJSON(w, status, report)
	})
	return mux
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
