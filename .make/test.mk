
.PHONY: test
test: ## Runs all tests
	@echo "${YELLOW} Running tests ${NC}"
	go test ./...
	@echo "${YELLOW} Done Running tests ${NC}"

.PHONY: test.coverage
test.coverage: ## Runs all tests with coverage
	@echo "${GREEN} Running tests with coverage ${NC}"
	go test -tags testing -v -cover -covermode=atomic -coverprofile=coverage.out ./...
	@echo "${GREEN} Done running tests with coverage ${NC}"

.PHONY: mocks.generate
mocks.generate: ## generates mocks usage: make mocks.generate MOCK_SOURCE=internal/infra/database/postgresql/queries.go MOCK_DESTINATION=internal/infra/database/postgresql/mocks/queries_mock.go MOCK_PACKAGE=postgresqlmocks
	@echo "${GREEN} >>>> Generating mocks for $(MOCK_SOURCE) in $(MOCK_DESTINATION) ${NC}"
	@set -e; \
	MOCKGEN_BIN=$$(command -v mockgen 2>/dev/null || true); \
	if [ -z "$$MOCKGEN_BIN" ]; then \
		echo "${YELLOW} mockgen was not found on PATH. Would you like to install it? ${NC}"; \
		if $(MAKE) confirm; then \
			echo "${YELLOW} Installing mockgen... ${NC}"; \
			go install go.uber.org/mock/mockgen@latest; \
			GOBIN="$$(go env GOBIN 2>/dev/null || true)"; \
			if [ -z "$$GOBIN" ]; then \
				GOBIN="$$(go env GOPATH)/bin"; \
			fi; \
			MOCKGEN_BIN="$$GOBIN/mockgen"; \
			PATH="$$GOBIN:$$PATH"; \
		else \
			echo "${RED} mockgen installation cancelled. ${NC}"; \
			exit 1; \
		fi; \
	fi; \
	"$$MOCKGEN_BIN" -destination=$(MOCK_DESTINATION) -package=$(MOCK_PACKAGE) -source=$(MOCK_SOURCE); \
	echo "${GREEN} >>>> Done generating mocks for $(MOCK_SOURCE) in $(MOCK_DESTINATION) ${NC}"
