BIN_DIR = $(PWD)/bin

.PHONY: install
install: ## Installs Go dependencies
	go mod download

.PHONY: tidy
tidy: ## Removes unused dependencies and adds any missing dependencies
	go mod tidy -v

.PHONY: run
run: ## Runs the project without hot reloading, useful for production testing and debugging
	@echo "${GREEN} Running application ${NC}"
	go run app/cmd/main.go

.PHONY: run.dev
run.dev: ## Runs the project in development mode with hot reloading using air
	@echo "${GREEN} Running application in development mode ${NC}"
	air

.PHONY: run.dev.d

run.dev.d: ## Runs the project in development mode with hot reloading using air and debug mode enabled
	@echo "${GREEN} Running application in development mode with debug enabled ${NC}"
	air -d

.PHONY: runWithMigrations
run.with.migrations: ## Runs the project applying migrations
	@echo "${GREEN} Running application ${NC}"
	cd cmd && CGO_ENABLED=0 go run -tags migrate main.go

.PHONY: fmt
fmt: ## gofmt and goimports all go files
	find . -name '*.go' -not -wholename './vendor/*' | while read -r file; do gofmt -w -s "$$file"; goimports -w "$$file"; done

.PHONY: clean
clean: ## Cleans the project by removing the bin directory and coverage files
	@echo "${RED} Cleaning project ${NC}"
	if [ -f ${BIN_DIR} ] ; then rm ${BIN_DIR} ; fi

.PHONY: lint
lint:
	@echo "${GREEN} Running lint checks ${NC}"
	docker run -t --rm -v $(CURRENT_DIR):/app -v ~/.cache/golangci-lint/:/root/.cache -w /app golangci/golangci-lint:v2.2.1 golangci-lint run -v
	@echo "${GREEN} Done linting ${NC}"

.PHONY: build
build: ## Builds the application binary and outputs it to the bin directory
	@echo "${GREEN} Building application ${NC}"
	go build -o $(BIN_DIR)/curtz app/cmd/main.go
	@echo "${GREEN} Done building application ${NC}"

all: install lint
