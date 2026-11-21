############################################################################################################################################################################################################
## Docker scripts
############################################################################################################################################################################################################
# make arguments that are defaulted
DOCKER_FILE ?= Dockerfile
DOCKER_IMAGE_TAG ?= curtz-service

.PHONY: create.dockerenvfile
create.dockerenvfile: ## Create a docker environment file
	if [ ! -f .env.docker ]; then cp .env.example .env.docker; fi

scan.docker.image:
	@echo "Scanning Docker Image: $(IMAGE)"
	./bin/trivy $(IMAGE)

# See local hadolint install instructions: https://github.com/hadolint/hadolint
.PHONY: lint.docker
lint.docker: ## lints the Dockerfile
	@echo "Running lint checks on Dockerfile"
	docker run --rm -i -v hadolint.yaml:/.config/hadolint.yaml hadolint/hadolint < $(DOCKER_FILE)
	@echo "Done linting Dockerfile"

# Reference: https://trivy.dev/latest/getting-started/
.PHONY: scan.docker
scan.docker: ## scans a docker image for vulnerabilities, but first it will build the image
	@if ! docker image inspect $(DOCKER_IMAGE_TAG) >/dev/null 2>&1; then \
		echo ">>> Building Docker image '$(DOCKER_IMAGE_TAG)' as it does not exist locally <<<<"; \
		docker build -f $(DOCKER_FILE) . -t $(DOCKER_IMAGE_TAG); \
		echo ">>> Done building Docker image $(DOCKER_IMAGE_TAG), scanning for vulnerabilities <<<<"; \
		docker run -v /var/run/docker.sock:/var/run/docker.sock -v ~/Library/Caches:/root/.cache/ aquasec/trivy image $(DOCKER_IMAGE_TAG); \
	else \
		echo ">>> Scanning Docker $(DOCKER_IMAGE_TAG) image for vulnerabilities <<<<"; \
		docker run -v /var/run/docker.sock:/var/run/docker.sock -v ~/Library/Caches:/root/.cache/ aquasec/trivy image $(DOCKER_IMAGE_TAG); \
		echo "\n >>> Done scanning docker image $(DOCKER_IMAGE_TAG) for vulnerabilities"; \
	fi

.PHONY: build.docker
build.docker: ## Build Docker image
	@echo "Building Docker image"
	docker build -f $(DOCKER_FILE) . -t $(DOCKER_IMAGE_TAG)
	@echo "Done building Docker image"

.PHONY: push.docker
push.docker: ## Pushes Docker image if it exists, otherwise it builds it, usage: 'make push.docker DOCKER_IMAGE_TAG=notification-svc', the DOCKER_IMAGE_TAG is optional
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
slim.dockerxray: ## Runs an xray command using slim on the docker image, usage: 'make slim.dockerxray DOCKER_IMAGE_TAG=notification-svc', the DOCKER_IMAGE_TAG is optional
	@if ! docker image inspect $(DOCKER_IMAGE_TAG) >/dev/null 2>&1; then \
		echo ">>> Building Docker image '$(DOCKER_IMAGE_TAG)' as it does not exist locally <<<<"; \
		docker build -f $(DOCKER_FILE) . -t $(DOCKER_IMAGE_TAG); \

		echo ">>> Done building Docker image $(DOCKER_IMAGE_TAG), running Xray <<<<"; \

		docker run -it --rm -v /var/run/docker.sock:/var/run/docker.sock dslim/slim xray $(DOCKER_IMAGE_TAG); \
	else \
		echo ">>> Running Xray Docker image '$(DOCKER_IMAGE_TAG)' <<<<"; \
		docker run -it --rm -v /var/run/docker.sock:/var/run/docker.sock dslim/slim xray $(DOCKER_IMAGE_TAG); \
	fi
