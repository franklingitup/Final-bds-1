# Backend Makefile — builds every service under ./services/<name>/cmd/server.

SHELL := /bin/bash
.DEFAULT_GOAL := build

SERVICES := api-gateway auth tenant cluster provisioning deployment build secrets domain observability notification audit
BIN_DIR := ../bin

.PHONY: build
build: ## Build all service binaries into ../bin
	@mkdir -p $(BIN_DIR)
	@for svc in $(SERVICES); do \
		echo "==> building $$svc"; \
		go build -o $(BIN_DIR)/$$svc ./services/$$svc/cmd/server || exit 1; \
	done

.PHONY: run-%
run-%: ## Run a single service, e.g. `make run-auth`
	go run ./services/$*/cmd/server

.PHONY: test
test: ## Run all tests
	go test ./...

.PHONY: lint
lint: ## Run golangci-lint
	golangci-lint run ./...

.PHONY: tidy
tidy: ## Sync dependencies
	go mod tidy

.PHONY: vet
vet: ## Run go vet
	go vet ./...
