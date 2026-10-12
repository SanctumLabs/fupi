// Package probes exposes the liveness and readiness endpoints that orchestrators and load balancers poll.
package probes

import (
	"github.com/gofiber/fiber/v2"
	"github.com/sanctumlabs/fupi/app/pkg/infra/monitoring/health"
	"github.com/sanctumlabs/fupi/app/pkg/infra/server/router"
)

const (
	// LivePath answers 200 while the process runs.
	LivePath = "/health"
	// ReadyPath answers 200 when the process can serve traffic and 503 when it cannot or is draining.
	ReadyPath = "/health/ready"
)

type probesRouter struct {
	registry *health.Registry
	routes   []router.Route
}

// NewRouter creates the liveness and readiness routes. Both are unauthenticated, so list them as public paths in the
// auth middleware.
func NewRouter(registry *health.Registry) router.Router {
	r := &probesRouter{registry: registry}
	r.routes = []router.Route{
		router.NewGetRoute(LivePath, r.live),
		router.NewGetRoute(ReadyPath, r.ready),
	}
	return r
}

func (r *probesRouter) Routes() []router.Route { return r.routes }

// live answers 200 while the process runs.
//
// Live godoc
// @Summary     Liveness probe
// @Tags        probes
// @Produce     json
// @Success     200 {object} map[string]string
// @Router      /health [get]
func (r *probesRouter) live(ctx *fiber.Ctx) error {
	return ctx.JSON(fiber.Map{"status": "ok"})
}

// ready answers 200 when the process can serve traffic and 503 when it cannot or is draining.
//
// Ready godoc
// @Summary     Readiness probe
// @Tags        probes
// @Produce     json
// @Success     200 {object} health.Report
// @Failure     503 {object} health.Report
// @Router      /health/ready [get]
func (r *probesRouter) ready(ctx *fiber.Ctx) error {
	report := r.registry.Run(ctx.UserContext())
	status := fiber.StatusOK
	if !report.Ready() {
		status = fiber.StatusServiceUnavailable
	}
	return ctx.Status(status).JSON(report)
}
