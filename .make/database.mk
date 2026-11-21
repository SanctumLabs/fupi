############################################################################################################################################################################################################
## Database scripts
############################################################################################################################################################################################################

# Database defaults

DATABASE_MIGRATION_PATH ?= ./app/internal/adapters/postgres/migrations
DATABASE_USER ?= curtz-user
DATABASE_PASSWORD ?= curtz-pass
DATABASE_NAME ?= curtzdb
DATABASE_HOST ?= localhost
DATABASE_PORT ?= 5432
DATABASE_SCHEMA ?= public
DATABASE_SCHEMA_MIGRATION ?= schema_migrations
MIGRATE_DIRECTION ?= up

.PHONY: sqlc.generate
sqlc.generate: ## generate SQLC code
	@echo "${GREEN} Generating SQLC code ${NC}"
	sqlc generate -f sqlc.yaml
	@echo "${GREEN} Done generating SQLC code ${NC}"

.PHONY: migrate
migrate: create.dockerenvfile ## Runs the migrations. Defaults to up, usage: make migrate MIGRATE_DIRECTION=down
	@echo "${GREEN} >>>>> Running migrations from directory $(DATABASE_MIGRATION_PATH) ${NC}"
	docker run -v $(DATABASE_MIGRATION_PATH):/migrations --network host migrate/migrate -path=/migrations/ -database 'postgres://$(DATABASE_USER):$(DATABASE_PASSWORD)@$(DATABASE_HOST):$(DATABASE_PORT)/$(DATABASE_NAME)?sslmode=disable&x-migrations-table=$(DATABASE_SCHEMA_MIGRATION)' $(MIGRATE_DIRECTION)
	@echo "${GREEN} >>>> Done running migrations ${NC}"

.PHONY: migrate.create
migrate.create: create.dockerenvfile ## Creates a migration up and down sql script, usage: make migrate.create MIGRATE_NAME=20220101_create_parking
	@echo "${GREEN} >>>>> Creating migration script $(MIGRATION_NAME) in $(DATABASE_MIGRATION_PATH) ${NC}"
	docker run --rm -v $(shell pwd)/$(DATABASE_MIGRATION_PATH):/migrations --network host migrate/migrate create -ext sql -dir /migrations -seq $(MIGRATION_NAME)
	@echo "${GREEN} >>>> Done creating migration script $(MIGRATION_NAME) ${NC}"
