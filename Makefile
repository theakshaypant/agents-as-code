.PHONY: help build build-controller build-webhook test fmt vet generate \
       kind-setup kind-base kind-deploy kind-redeploy kind-configure kind-restart \
       setup-repo logs-webhook logs-controller

SHELL := /bin/bash
KIND_CLUSTER_NAME ?= yeet
REG_PORT ?= 5000
KO ?= $(shell which ko)

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

# ── Build ────────────────────────────────────────────────────────────

build: build-controller build-webhook ## Build all binaries

build-controller: ## Build the controller binary
	go build -o bin/controller ./cmd/controller

build-webhook: ## Build the webhook binary
	go build -o bin/webhook ./cmd/webhook

# ── Code quality ─────────────────────────────────────────────────────

test: ## Run tests
	go test ./...

fmt: ## Run gofmt
	gofmt -w -s .

vet: ## Run go vet
	go vet ./...

# ── Code generation ──────────────────────────────────────────────────

generate: ## Regenerate deepcopy and CRD schemas
	./hack/update-schemas.sh

# ── Kind cluster ─────────────────────────────────────────────────────

kind-setup: ## Full kind setup: cluster + nginx + deploy + secrets
	./hack/dev/kind/install.sh

kind-base: ## Create kind cluster + nginx only (no yeet)
	./hack/dev/kind/install.sh -b

kind-deploy: ## Deploy yeet to existing kind cluster (ko apply)
	./hack/dev/kind/install.sh -y

kind-redeploy: ## Rebuild and redeploy yeet (ko apply + restart pods)
	./hack/dev/kind/install.sh -y
	./hack/dev/kind/install.sh -R

kind-configure: ## Reconfigure yeet (ingress + secrets) without rebuild
	./hack/dev/kind/install.sh -c

kind-restart: ## Restart yeet pods
	./hack/dev/kind/install.sh -R

kind-delete: ## Delete the kind cluster
	kind delete cluster --name $(KIND_CLUSTER_NAME)

# ── Repository ───────────────────────────────────────────────────────

setup-repo: ## Create Repository CR and secrets from .env
	./hack/dev/kind/setup-repo.sh

# ── Logs ─────────────────────────────────────────────────────────────

logs-webhook: ## Tail webhook pod logs
	kubectl logs -n yeet-system -l app.kubernetes.io/name=webhook -f

logs-controller: ## Tail controller pod logs
	kubectl logs -n yeet-system -l app.kubernetes.io/name=controller -f
