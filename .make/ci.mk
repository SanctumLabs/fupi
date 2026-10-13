############################################################################################################################################################################################################
## CI scripts
############################################################################################################################################################################################################

# Reference: https://github.com/rhysd/actionlint/blob/v1.7.12/docs/usage.md

.PHONY: ci.actionlint.all
ci.actionlint.all: ## Runs actionlint for linting GitHub Actions workflows. e.g. make ci.actionlint.all
	@echo "${GREEN} Linting Github action workflows ${NC}"
	actionlint

.PHONY: ci.actionlint
ci.actionlint: ## Runs actionlint for linting GitHub Actions workflows for a specific workflow file. e.g. make ci.actionlint WORKFLOW_FILE=.github/workflows/ci.yml
	@echo "${GREEN} Linting Github action workflows ${NC}"
	actionlint $(WORKFLOW_FILE)
