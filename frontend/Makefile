SHELL := /bin/bash
.DEFAULT_GOAL := build

.PHONY: install
install: ## Install dependencies
	npm install

.PHONY: dev
dev: ## Run the dev server
	npm run dev

.PHONY: build
build: ## Production build
	npm run build

.PHONY: lint
lint: ## Lint and typecheck
	npm run lint
	npm run typecheck
