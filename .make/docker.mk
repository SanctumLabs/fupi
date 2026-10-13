############################################################################################################################################################################################################
## Docker scripts
############################################################################################################################################################################################################
# make arguments that are defaulted
DOCKER_FILE ?= Dockerfile
DOCKER_IMAGE_TAG ?= fupi-service

.PHONY: create.dockerenvfile
create.dockerenvfile: ## Create a docker environment file
	if [ ! -f .env.docker ]; then cp .env.example .env.docker; fi

# Pinned tool images. hadolint is pinned by the digest of the image already on the machine.
HADOLINT_IMAGE ?= hadolint/hadolint@sha256:32dac94127fd60b7b7e3fbfc65e1383b9b5e25c9bfd7b8536de7a539fe68a12d
TRIVY_IMAGE ?= aquasec/trivy:0.75.0

# `docker save <repository>` exports every tag of the repository, and Trivy rejects a tar with more than one image, so the
# scan always names exactly one reference (a bare name means :latest). Only the last path component can carry a tag: a
# registry port (localhost:5000/team/app) is not one.
DOCKER_IMAGE_REF = $(if $(findstring :,$(lastword $(subst /, ,$(DOCKER_IMAGE_TAG)))),$(DOCKER_IMAGE_TAG),$(DOCKER_IMAGE_TAG):latest)

# Build metadata for the image labels and the version variables in the binary
DOCKER_VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo unknown)
DOCKER_GIT_COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
DOCKER_BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

# See hadolint: https://github.com/hadolint/hadolint. The config is mounted by absolute path; a relative path would be a
# named volume and the rules in hadolint.yaml would never be read.
.PHONY: lint.docker
lint.docker: ## lints the Dockerfile with the rules in hadolint.yaml
	@echo "Running lint checks on Dockerfile"
	docker run --rm -i -v "$(ROOT_DIR)/hadolint.yaml:/.config/hadolint.yaml:ro" $(HADOLINT_IMAGE) < $(DOCKER_FILE)
	@echo "Done linting Dockerfile"

# Reference: https://trivy.dev/latest/getting-started/
# The image is saved to a tar and scanned from there, so the scanner never gets the Docker socket.
.PHONY: scan.docker
scan.docker: ## scans the image for fixable HIGH and CRITICAL vulnerabilities, building it first if it is missing
	@if ! docker image inspect $(DOCKER_IMAGE_REF) >/dev/null 2>&1; then $(MAKE) build.docker; fi
	@dir=$$(mktemp -d) && docker save -o "$$dir/image.tar" $(DOCKER_IMAGE_REF) && \
		docker run --rm -v "$$dir":/scan:ro -v fupi-trivy-cache:/root/.cache $(TRIVY_IMAGE) image --quiet --no-progress --input /scan/image.tar --severity HIGH,CRITICAL --ignore-unfixed --exit-code 1; \
		status=$$?; rm -rf "$$dir"; exit $$status

ACTIONLINT_IMAGE ?= rhysd/actionlint:1.7.12

.PHONY: lint.workflows
lint.workflows: ## lints the GitHub workflows (actionlint, pinning rules) and checks the GitLab and Bitbucket pipelines against them
	@$(ROOT_DIR)/scripts/workflows_check.sh
	@$(ROOT_DIR)/scripts/mirror_ci_check.sh
	docker run --rm -v "$(ROOT_DIR):/repo:ro" -w /repo $(ACTIONLINT_IMAGE) -color

.PHONY: build.docker
build.docker: ## Build the Docker image with its version metadata, usage make build.docker DOCKER_IMAGE_TAG=fupi-service
	@echo "Building Docker image"
	docker build -f $(DOCKER_FILE) -t $(DOCKER_IMAGE_TAG) \
		--build-arg VERSION=$(DOCKER_VERSION) \
		--build-arg GIT_COMMIT=$(DOCKER_GIT_COMMIT) \
		--build-arg BUILD_TIME=$(DOCKER_BUILD_TIME) .
	@echo "Done building Docker image"

.PHONY: push.docker
push.docker: ## Pushes Docker image if it exists, otherwise it builds it, usage 'make push.docker DOCKER_IMAGE_TAG=notification-svc', the DOCKER_IMAGE_TAG is optional
	@if ! docker image inspect $(DOCKER_IMAGE_TAG) >/dev/null 2>&1; then \
		echo "Docker Image $(DOCKER_IMAGE_TAG) does not exist, building..."; \
		docker build -f $(DOCKER_FILE) . -t $(DOCKER_IMAGE_TAG); \
		echo "Done building docker Image $(DOCKER_IMAGE_TAG), pushing image"; \
		echo "Pushing Docker image $(DOCKER_IMAGE_TAG)"; \
		docker push $(DOCKER_IMAGE_TAG); \
		echo "Done pushing docker image $(DOCKER_IMAGE_TAG)"; \
	else \
		echo "Pushing Docker image $(DOCKER_IMAGE_TAG)"; \
		docker push $(DOCKER_IMAGE_TAG); \
		echo "Done pushing docker image $(DOCKER_IMAGE_TAG)"; \
	fi

.PHONY: run.docker
run.docker: create.dockerEnvFile # Run the Docker container
	@if ! docker image inspect $(DOCKER_IMAGE_TAG) >/dev/null 2>&1; then \
		echo "Building Docker Image $(DOCKER_IMAGE_TAG)"; \
		docker build -f $(DOCKER_FILE) . -t $(DOCKER_IMAGE_TAG); \
		echo "Done building docker Image $(DOCKER_IMAGE_TAG), running container"; \
		docker run --env-file .env.docker -t $(DOCKER_IMAGE_TAG); \
	else \
		echo "Running docker image $(DOCKER_IMAGE_TAG)"; \
		docker run --env-file .env.docker -t $(DOCKER_IMAGE_TAG); \
	fi

# Ref: https://github.com/slimtoolkit/slim
.PHONY: slim.dockerxray
slim.dockerxray: ## Runs an xray command using slim on the docker image, usage 'make slim.dockerxray DOCKER_IMAGE_TAG=notification-svc', the DOCKER_IMAGE_TAG is optional
	@if ! docker image inspect $(DOCKER_IMAGE_TAG) >/dev/null 2>&1; then \
		echo ">>> Building Docker image '$(DOCKER_IMAGE_TAG)' as it does not exist locally <<<<"; \
		docker build -f $(DOCKER_FILE) . -t $(DOCKER_IMAGE_TAG); \

		echo ">>> Done building Docker image $(DOCKER_IMAGE_TAG), running Xray <<<<"; \

		docker run -it --rm -v /var/run/docker.sock:/var/run/docker.sock dslim/slim xray $(DOCKER_IMAGE_TAG); \
	else \
		echo ">>> Running Xray Docker image '$(DOCKER_IMAGE_TAG)' <<<<"; \
		docker run -it --rm -v /var/run/docker.sock:/var/run/docker.sock dslim/slim xray $(DOCKER_IMAGE_TAG); \
	fi


############################################################################################################################################################################################################
## Local infrastructure (docker compose, see docs/LocalInfrastructure.md)
############################################################################################################################################################################################################
# MODE picks the topology for stacks that have one: ha (default) or single, e.g. make infra.kafka.up MODE=single
MODE ?= ha
INFRA := $(ROOT_DIR)/scripts/infra.sh
COMPOSE := docker compose --project-directory $(ROOT_DIR)
INFRA_PROFILES := kafka-ha kafka-single redis-ha redis-single postgres-ha postgres-single elk-ha elk-single observability legacy core-ha core-single full-ha full-single app-ha app-single worker-ha worker-single

# Everything except the legacy stack: `infra.clean` must not delete data that predates the infrastructure stacks
INFRA_CLEAN_PROFILES := $(foreach p,$(filter-out legacy,$(INFRA_PROFILES)),--profile $(p))

# Service that answers for "node 1" of a stack in the selected mode, used by the debugging helpers
KAFKA_SERVICE := $(if $(filter single,$(MODE)),kafka-single,kafka-1)
REDIS_SERVICE := $(if $(filter single,$(MODE)),redis-single,redis-1)
POSTGRES_SERVICE := $(if $(filter single,$(MODE)),postgres-single,patroni-1)
ES_SERVICE := $(if $(filter single,$(MODE)),es-single,es-1)

.PHONY: infra.kafka.up infra.kafka.down
infra.kafka.up: create.envfile ## Start Kafka and Kafka UI (MODE=ha or single)
	@$(INFRA) up kafka $(MODE)
infra.kafka.down: create.envfile ## Stop Kafka (data is kept)
	@$(INFRA) down kafka

.PHONY: infra.redis.up infra.redis.down
infra.redis.up: create.envfile ## Start the Redis cluster (MODE=ha or single)
	@$(INFRA) up redis $(MODE)
infra.redis.down: create.envfile ## Stop Redis (data is kept)
	@$(INFRA) down redis

.PHONY: infra.postgres.up infra.postgres.down
infra.postgres.up: create.envfile ## Start Postgres and run migrations (MODE=ha or single)
	@$(INFRA) up postgres $(MODE)
infra.postgres.down: create.envfile ## Stop Postgres (data is kept)
	@$(INFRA) down postgres

.PHONY: infra.elk.up infra.elk.down
infra.elk.up: create.envfile ## Start Elasticsearch Logstash Kibana and Filebeat (MODE=ha or single)
	@$(INFRA) up elk $(MODE)
infra.elk.down: create.envfile ## Stop ELK (data is kept)
	@$(INFRA) down elk

.PHONY: infra.observability.up infra.observability.down
infra.observability.up: create.envfile ## Start Prometheus Grafana Tempo Alertmanager and the OTel Collector
	@$(INFRA) up observability
infra.observability.down: create.envfile ## Stop the observability stack (data is kept)
	@$(INFRA) down observability

.PHONY: infra.legacy.up infra.legacy.down
infra.legacy.up: create.envfile ## Start the legacy MongoDB and standalone Redis
	@$(INFRA) up legacy
infra.legacy.down: create.envfile ## Stop the legacy services (data is kept)
	@$(INFRA) down legacy

.PHONY: infra.core.up infra.core.down
infra.core.up: create.envfile ## Start everything the app needs - Postgres Redis Kafka (MODE=ha or single)
	@$(INFRA) up core $(MODE)
infra.core.down: create.envfile ## Stop Postgres Redis and Kafka (data is kept)
	@$(INFRA) down core

.PHONY: infra.full.up infra.full.down
infra.full.up: create.envfile ## Start every stack except legacy (MODE=ha or single)
	@$(INFRA) up full $(MODE)
infra.full.down: create.envfile ## Stop every stack except legacy (data is kept)
	@$(INFRA) down full

.PHONY: infra.app.up infra.app.down
infra.app.up: create.envfile ## Build and start the API container, with Postgres and Redis brought up first (MODE=ha or single)
	@$(INFRA) up app $(MODE)
infra.app.down: create.envfile ## Stop the API container (Postgres and Redis keep running)
	@$(INFRA) down app

.PHONY: infra.worker.up infra.worker.down
infra.worker.up: create.envfile ## Build and start the outbox relay worker, with Postgres and Kafka brought up first (MODE=ha or single)
	@$(INFRA) up worker $(MODE)
infra.worker.down: create.envfile ## Stop the worker (Postgres and Kafka keep running)
	@$(INFRA) down worker

.PHONY: infra.config
infra.config: create.envfile ## Check that every compose profile renders and the env defaults agree
	@$(ROOT_DIR)/scripts/infra_env_check.sh
	@for p in $(INFRA_PROFILES); do \
		$(COMPOSE) --profile $$p config --quiet || exit 1; \
		echo "ok: $$p"; \
	done
	@$(COMPOSE) --profile '*' config --quiet && echo "ok: all profiles"

.PHONY: infra.ps
infra.ps: create.envfile ## Show every infrastructure container and its health
	@$(COMPOSE) --profile '*' ps -a

.PHONY: infra.logs
infra.logs: create.envfile ## Follow logs, usage - make infra.logs SERVICE=kafka-1 (omit SERVICE for everything)
	@$(COMPOSE) --profile '*' logs -f --tail=100 $(SERVICE)

.PHONY: infra.stats
infra.stats: create.envfile ## Show memory and CPU of the running infrastructure containers
	@docker stats --no-stream --format "table {{.Name}}\t{{.MemUsage}}\t{{.CPUPerc}}" \
		$$(docker ps --filter label=com.docker.compose.project=fupi -q)

.PHONY: infra.clean
infra.clean: create.envfile confirm ## Remove every infrastructure container AND volume except the legacy MongoDB and Redis
	@$(COMPOSE) $(INFRA_CLEAN_PROFILES) down -v

.PHONY: infra.clean.legacy
infra.clean.legacy: create.envfile confirm ## Remove the legacy MongoDB and Redis containers AND their volumes (legacy data is lost)
	@$(COMPOSE) --profile legacy down -v

.PHONY: infra.hosts
infra.hosts: ## Print the hosts-file line needed to run the app on the host against Redis HA
	@echo "Redis HA announces the hostnames redis-1 to redis-6. An app running on your host must resolve them."
	@echo "Add this line to /etc/hosts (needs sudo, not required when the app runs in a container):"
	@echo "  127.0.0.1 redis-1 redis-2 redis-3 redis-4 redis-5 redis-6"
	@if grep -q 'redis-1' /etc/hosts; then echo "(an entry for redis-1 already exists)"; fi


.PHONY: infra.kafka.topics
infra.kafka.topics: create.envfile ## Describe the Kafka topics (MODE=ha or single)
	@$(COMPOSE) --profile '*' exec -T $(KAFKA_SERVICE) /opt/kafka/bin/kafka-topics.sh --bootstrap-server localhost:9092 --describe


.PHONY: infra.redis.cli
infra.redis.cli: create.envfile ## Open redis-cli as the application user in cluster mode (MODE=ha or single)
	@$(COMPOSE) --profile '*' exec $(REDIS_SERVICE) sh -c 'redis-cli -p "$$NODE_PORT" --user "$$REDIS_USERNAME" --pass "$$REDIS_PASSWORD" --no-auth-warning -c'


.PHONY: infra.psql
infra.psql: create.envfile ## Open psql as the application user on the primary (MODE=ha or single)
	@$(COMPOSE) --profile '*' exec $(POSTGRES_SERVICE) sh -c 'PGPASSWORD="$$PG_APP_PASSWORD" psql -h postgres -U "$$PG_APP_USER" "$$PG_DATABASE"'

# pg_stat_statements is preloaded in both modes and the extension is created in the `postgres` database (deploy/postgres/roles.sh),
# whose view covers every database. The superuser reads it: the application role lacks pg_read_all_stats.
PG_ADMIN = $(COMPOSE) --profile '*' exec -T -e PGPASSWORD=$${PG_SUPERUSER_PASSWORD:-fupi-postgres-admin} $(POSTGRES_SERVICE) psql -h 127.0.0.1 -U postgres

.PHONY: infra.pgstat
infra.pgstat: create.envfile ## Top statements of the app database by total time (MODE=ha or single), usage make infra.pgstat TOP=10
	@$(PG_ADMIN) -d postgres -c "SELECT calls, round(total_exec_time::numeric) AS total_ms, round(mean_exec_time::numeric,2) AS mean_ms, shared_blks_hit AS hit, shared_blks_read AS read, (wal_bytes/NULLIF(calls,0)) AS wal_per_call, left(regexp_replace(query,'\\s+',' ','g'),90) AS query FROM pg_stat_statements WHERE dbid = (SELECT oid FROM pg_database WHERE datname = '$${PG_DATABASE:-fupidb}') ORDER BY total_exec_time DESC LIMIT $(or $(TOP),10)"

.PHONY: infra.pgstat.reset
infra.pgstat.reset: create.envfile ## Reset pg_stat_statements, do it just before a load run (MODE=ha or single)
	@$(PG_ADMIN) -d postgres -Atc "SELECT pg_stat_statements_reset()" >/dev/null && echo "pg_stat_statements reset"

.PHONY: infra.psql.admin
infra.psql.admin: create.envfile ## Open psql as the Postgres superuser on the app database, for EXPLAIN ANALYZE and locks (MODE=ha or single)
	@$(COMPOSE) --profile '*' exec -e PGPASSWORD=$${PG_SUPERUSER_PASSWORD:-fupi-postgres-admin} $(POSTGRES_SERVICE) sh -c 'psql -h 127.0.0.1 -U postgres "$${PG_DATABASE:-fupidb}"'

.PHONY: infra.migrate
infra.migrate: create.envfile ## Re-run the database migrations against the running Postgres
	@$(COMPOSE) --profile '*' run --rm migrate


.PHONY: infra.patroni.list
infra.patroni.list: create.envfile ## Show the Patroni cluster members and roles (Postgres MODE=ha only)
	@$(COMPOSE) --profile '*' exec -T patroni-1 /opt/patroni/bin/patronictl -c /etc/patroni/patroni.yml list


.PHONY: infra.es.health
infra.es.health: create.envfile ## Show the Elasticsearch cluster health (MODE=ha or single)
	@$(COMPOSE) --profile '*' exec -T $(ES_SERVICE) sh -c 'curl -s -u elastic:"$$ELASTIC_PASSWORD" "localhost:9200/_cluster/health?pretty"'


.PHONY: infra.wait
infra.wait: create.envfile ## Wait until a stack is healthy, usage - make infra.wait STACK=kafka MODE=single
	@$(INFRA) wait $(STACK) $(MODE)
