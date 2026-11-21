
# will setup all the tools required by the project
setup: setup.linting setup.trivy install install.git-hooks

.PHONY: setup.linting
setup.linting: ## Will setup linting tools for the project, currently it sets up golangci-lint
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b ./bin v1.46.2
	chmod +x ./bin/golangci-lint

.PHONY: setup.trivy
setup.trivy: ## Will setup trivy for scanning docker images for vulnerabilities
	curl -sfL https://raw.githubusercontent.com/aquasecurity/trivy/main/contrib/install.sh | sh -s -- -b ./bin v0.16.0

.PHONY: install.git-hooks
install.git-hooks: ## Installs Git hooks for the project
	@echo "Installing Git hooks..."
	cp ./githooks/prepare-commit-msg.sh .git/hooks/prepare-commit-msg
	cp ./githooks/pre-commit.sh .git/hooks/pre-commit
	cp ./githooks/pre-push.sh .git/hooks/pre-push

.PHONY: healthGrpcProbe
healthGrpcProbe: ## Checks the health of the running grpc server the GRPC port
	@echo "${GREEN} Checking health of the running application ${NC}"
	grpc-health-probe -addr=localhost:5002
	@echo "${GREEN} Done checking health of the running application ${NC}"


.PHONY: wire
wire: # Generate dependency injection instances with wire
	@echo "${GREEN} Generating dependency injection instances ${NC}"
	cd internal/app && wire
	@echo "${GREEN} Done generating dependency injection instances ${NC}"


################################################## Protobuf Commands ########################################################

.PHONY: buf.generate
buf.generate: ## Generates code from protobuf files 
	@echo "${GREEN} Generating gRPC API ${NC}"
	buf generate
	@echo "${GREEN} Done generating gRPC API ${NC}" 

BUF_CLIENTS_TEMPLATE ?= buf.gen.infra.yaml
.PHONY: buf.generate.clients
buf.generate.clients: ## Generates gRPC client code from protobuf files for external gRPC services, defaults to buf.gen.infra.yaml template. Usage: make bufGenerateClients BUF_CLIENTS_TEMPLATE=path/to/buf.gen.infra.yaml
	@echo "${GREEN} Generating gRPC Client APIs For external services ${NC}"
	buf generate --template $(BUF_CLIENTS_TEMPLATE)
	@echo "${GREEN} Done generating gRPC client APIs for external services ${NC}" 

.PHONY: buf.dep.update
buf.dep.update: ## Update Buf dependencies
	@echo "${GREEN} Updating Buf dependencies$ ${NC}" 
	buf dep update
	@echo ${GREEN} "Done updating Buf dependencies ${NC}" 

.PHONY: generate.open.api.docs
generate.open.api.docs: ## Generate OpenApi Docs
	@echo "${GREEN} Generating Open API Docs ${NC}"
	swag init --generalInfo main.go --dir cmd,api/rest/routes/service/v1/curtz,api/rest/routes/monitoring/health --output api/openapi-spec
	@echo "${GREEN} Done Generating Open API Docs ${NC}"