#COLORS
# Define colors
GREEN  := $(shell tput -Txterm setaf 2)
WHITE  := $(shell tput -Txterm setaf 7)
YELLOW := $(shell tput -Txterm setaf 3)
RED := $(shell tput -Txterm setaf 1)
RESET  := $(shell tput -Txterm sgr0)
# RED=\033[0;31m
# GREEN=\033[0;32m
# YELLOW=\033[0;33m
BLUE=\033[0;34m
# No Color
NC=\033[0m

.PHONY: confirm
confirm:
	@( read -p "$(RED)Are you sure? [y/N]$(RESET): " sure && case "$$sure" in [yY]) true;; *) false;; esac )

print.dirname: ## Prints the directory name
	@echo "Directory name: $(DIR_NAME)"

show.dir: ## Prints the current directory
	@echo "Current directory: $(CURRENT_DIR)"

print.parent: ## Prints the parent directory
	@echo "Parent directory: $(PARENT_DIR)"

echoos:
	@echo $(OSFLAG)

.DEFAULT_GOAL := help

.PHONY: help
help: ## This help dialog describing all commands
	@IFS=$$'\n' ; \
	help_lines=(`fgrep -h "##" $(MAKEFILE_LIST) | fgrep -v fgrep | sed -e 's/\\$$//' | sed -e 's/##/:/'`); \
	printf "%-30s %s\n" "target" "help" ; \
	printf "%-30s %s\n" "------" "----" ; \
	for help_line in $${help_lines[@]}; do \
		IFS=$$':' ; \
		help_split=($$help_line) ; \
		help_command=`echo $${help_split[0]} | sed -e 's/^ *//' -e 's/ *$$//'` ; \
		help_info=`echo $${help_split[2]} | sed -e 's/^ *//' -e 's/ *$$//'` ; \
		printf '\033[36m'; \
		printf "%-30s %s" $$help_command ; \
		printf '\033[0m'; \
		printf "%s\n" $$help_info; \
	done

.PHONY: create.envfile
create.envfile: ## Create an environment file
	if [ ! -f .env ]; then cp .env.example .env; fi

