# Root Makefile — orchestrates backend, frontend, infra, and local dev.
# Run `make help` for the list of targets.

SHELL := /bin/bash
.DEFAULT_GOAL := help

SERVICES := api-gateway auth tenant cluster provisioning deployment build secrets domain observability notification audit

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z0-9_./-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'

## ---------- Local environment ----------
.PHONY: dev-up dev-down dev-logs
dev-up: ## Start local dependencies (postgres, redis, nats, minio, observability)
	docker compose -f docker-compose.yml up -d

dev-down: ## Stop and remove local dependencies
	docker compose -f docker-compose.yml down -v

dev-logs: ## Tail local dependency logs
	docker compose -f docker-compose.yml logs -f

## ---------- Backend ----------
.PHONY: backend-build backend-test backend-lint backend-tidy
backend-build: ## Build all Go services
	$(MAKE) -C backend build

backend-test: ## Run Go tests
	$(MAKE) -C backend test

backend-lint: ## Lint Go code
	$(MAKE) -C backend lint

backend-tidy: ## Sync Go module dependencies
	$(MAKE) -C backend tidy

## ---------- Frontend ----------
.PHONY: frontend-install frontend-dev frontend-build frontend-lint
frontend-install: ## Install frontend dependencies
	$(MAKE) -C frontend install

frontend-dev: ## Run the web console in dev mode
	$(MAKE) -C frontend dev

frontend-build: ## Build the web console
	$(MAKE) -C frontend build

frontend-lint: ## Lint the frontend
	$(MAKE) -C frontend lint

## ---------- Quality gates ----------
.PHONY: lint test build
lint: backend-lint frontend-lint ## Lint everything
test: backend-test ## Test everything
build: backend-build frontend-build ## Build everything

## ---------- Container images ----------
.PHONY: images
images: ## Build container images for all backend services
	@for svc in $(SERVICES); do \
		echo "==> building $$svc"; \
		docker build -f backend/Dockerfile --build-arg SERVICE=$$svc -t platform/$$svc:dev backend; \
	done
