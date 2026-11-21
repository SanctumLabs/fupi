---
status: accepted
---

# Integration tests require the `integration` build tag

Tests that start a PostgreSQL or Redis container via testcontainers live in `*_integration_test.go` files tagged `//go:build integration`. `go test ./...` (and `make test` / `make test.coverage`) run without Docker; `make test.integration` runs the containerised suites, matching the split already in `.github/workflows/tests.yml`. Each package has exactly one Ginkgo `RunSpecs` bootstrap (`*_suite_test.go`) so unit and integration `Describe`s can coexist in one binary.
