############################################################################################################################################################################################################
## Load tests (k6). See loadtest/README.md
############################################################################################################################################################################################################
K6_IMAGE ?= grafana/k6:1.4.0
# The k6 container joins the compose network and reaches the API by its alias, so the host port forward does not cap throughput.
K6_NETWORK ?= fupi
LOADTEST_BASE_URL ?= http://fupi-app:8085
# The Go seeder runs on the host, so it uses the published port.
SEED_BASE_URL ?= http://127.0.0.1:8085
SCENARIO ?= redirect
START ?= 100
STEP ?= 100
MAX ?= 5000
STEP_DURATION ?= 45s
REPEATS ?= 3
# Cap on the generator's CPUs (empty = no cap). GOMAXPROCS follows it so the Go runtime does not run into CFS throttling.
K6_CPUS ?=
USERS ?= 5
URLS_PER_USER ?= 20
# Limits the API runs with during load tests, so the rate limiter does not throttle the generator. Windows are seconds.
LOADTEST_LIMITS ?= RATE_LIMIT_AUTH_MAX=100000 RATE_LIMIT_CREATE_MAX=100000 RATE_LIMIT_REDIRECT_MAX=1000000 RATE_LIMIT_DEFAULT_MAX=100000
# The ratelimit scenario's view of the redirect group: keep these equal to RATE_LIMIT_REDIRECT_MAX and _WINDOW.
GROUP_MAX ?= 100000
GROUP_WINDOW_S ?= 60

# --user keeps the seed file owned by the caller on Linux hosts.
K6_RUN = docker run --rm -i --network $(K6_NETWORK) --user "$$(id -u):$$(id -g)" \
	$(if $(K6_CPUS),--cpus $(K6_CPUS) -e GOMAXPROCS=$(K6_CPUS)) \
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
loadtest.run: ## Runs a scenario, usage make loadtest.run SCENARIO=redirect K6_ARGS="-e RATE=50 -e DURATION=5m" (K6_FLAGS are k6 flags, K6_ARGS docker flags)
	$(K6_RUN) $(K6_IMAGE) run $(K6_FLAGS) /loadtest/scenarios/$(SCENARIO).js

.PHONY: loadtest.stress
loadtest.stress: ## Steps the arrival rate up until a threshold fails, usage make loadtest.stress SCENARIO=redirect START=100 STEP=100 MAX=5000 STEP_DURATION=45s REPEATS=3 K6_CPUS=2
	@SCENARIO=$(SCENARIO) START=$(START) STEP=$(STEP) MAX=$(MAX) STEP_DURATION=$(STEP_DURATION) REPEATS=$(REPEATS) $(ROOT_DIR)/loadtest/stress.sh

.PHONY: loadtest.app.up
loadtest.app.up: ## Starts the API with load-test rate limits, usage make loadtest.app.up MODE=single LOADTEST_LIMITS="RATE_LIMIT_REDIRECT_MAX=200 RATE_LIMIT_REDIRECT_WINDOW=3600"
	env $(LOADTEST_LIMITS) $(MAKE) infra.app.up MODE=$(MODE)

.PHONY: loadtest.ratelimit
loadtest.ratelimit: ## Checks the redirect rate limit, usage make loadtest.ratelimit GROUP_MAX=200 GROUP_WINDOW_S=3600 (the API must run with the same limit)
	$(K6_RUN) -e GROUP_MAX=$(GROUP_MAX) -e GROUP_WINDOW_S=$(GROUP_WINDOW_S) $(K6_IMAGE) run /loadtest/scenarios/ratelimit.js
