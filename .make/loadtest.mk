############################################################################################################################################################################################################
## Load tests (k6). See loadtest/README.md
############################################################################################################################################################################################################
K6_IMAGE ?= grafana/k6:1.4.0
# The k6 container joins the compose network and reaches the API by its alias, so the host port forward does not cap throughput.
K6_NETWORK ?= fupi
LOADTEST_BASE_URL ?= http://fupi-app:8085
# The Go seeder runs on the host, so it uses the published port.
SEED_BASE_URL ?= http://127.0.0.1:8085
USERS ?= 5
URLS_PER_USER ?= 20

# --user keeps the seed file owned by the caller on Linux hosts.
K6_RUN = docker run --rm -i --network $(K6_NETWORK) --user "$$(id -u):$$(id -g)" \
	-v "$(ROOT_DIR)/loadtest:/loadtest" \
	-e BASE_URL=$(LOADTEST_BASE_URL) -e USERS=$(USERS) -e URLS_PER_USER=$(URLS_PER_USER) $(K6_ARGS)

.PHONY: loadtest.seed
loadtest.seed: ## Seeds load-test users and urls with the Go seeder, usage make loadtest.seed USERS=5 URLS_PER_USER=20 SEED_BASE_URL=http://127.0.0.1:8085
	BASE_URL=$(SEED_BASE_URL) USERS=$(USERS) URLS_PER_USER=$(URLS_PER_USER) go run ./loadtest/seed

.PHONY: loadtest.seed.k6
loadtest.seed.k6: ## Seeds the same data from k6 (only Docker needed), usage make loadtest.seed.k6
	$(K6_RUN) $(K6_IMAGE) run /loadtest/seed/seed.js

.PHONY: loadtest.smoke
loadtest.smoke: ## Runs the smoke scenario against the seeded data (needs make loadtest.seed first)
	$(K6_RUN) $(K6_IMAGE) run /loadtest/scenarios/smoke.js

.PHONY: loadtest.run
loadtest.run: ## Runs a scenario, usage make loadtest.run SCENARIO=redirect K6_ARGS="-e RATE=50 -e DURATION=5m"
	$(K6_RUN) $(K6_IMAGE) run /loadtest/scenarios/$(SCENARIO).js
