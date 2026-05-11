.PHONY: help build build-controller build-webhook test fmt vet generate \
       build-runtime push-runtime test-runtime \
       kind-setup kind-base kind-deploy kind-redeploy kind-configure kind-restart \
       setup-repo logs-webhook logs-controller

SHELL := /bin/bash
KIND_CLUSTER_NAME ?= aac
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

kind-base: ## Create kind cluster + nginx only (no agents-as-code)
	./hack/dev/kind/install.sh -b

kind-deploy: ## Deploy agents-as-code to existing kind cluster (ko apply)
	./hack/dev/kind/install.sh -y

kind-redeploy: ## Rebuild and redeploy agents-as-code (ko apply + restart pods)
	./hack/dev/kind/install.sh -y
	./hack/dev/kind/install.sh -R

kind-configure: ## Reconfigure agents-as-code (ingress + secrets) without rebuild
	./hack/dev/kind/install.sh -c

kind-restart: ## Restart agents-as-code pods
	./hack/dev/kind/install.sh -R

# ── Agent Runtime ────────────────────────────────────────────────────

build-runtime: ## Build the agent runtime container image
	docker build -t localhost:$(REG_PORT)/aac-agent-runtime:latest runtime/

push-runtime: build-runtime ## Build and push agent runtime to local registry
	docker push localhost:$(REG_PORT)/aac-agent-runtime:latest

test-runtime: ## Run agent runtime unit tests
	cd runtime && python -m pytest test_agent_run.py -v

kind-delete: ## Delete the kind cluster
	kind delete cluster --name $(KIND_CLUSTER_NAME)

# ── Repository ───────────────────────────────────────────────────────

setup-repo: ## Create Repository CR and secrets from .env
	./hack/dev/kind/setup-repo.sh

# ── Logs ─────────────────────────────────────────────────────────────

logs-webhook: ## Tail webhook pod logs
	kubectl logs -n agents-as-code-system -l app.kubernetes.io/name=webhook -f

logs-controller: ## Tail controller pod logs
	kubectl logs -n agents-as-code-system -l app.kubernetes.io/name=controller -f
