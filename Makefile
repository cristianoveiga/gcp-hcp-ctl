.PHONY: build test lint clean help release generate

# Path to the GCP HyperFleet merged openapi.yaml (output of tsp compile in hyperfleet-gcp-api-spec).
# TODO(GCP-833): update default to openshift-online/hyperfleet-gcp-api-spec once the repo exists.
# Override with: make generate HYPERFLEET_SCHEMA=/path/to/openapi.yaml
HYPERFLEET_SCHEMA ?= $(GOPATH)/src/github.com/cristianoveiga/hyperfleet-api-spec-template/schemas/template/openapi.yaml

HYPERFLEET_SCHEMA ?= ../hyperfleet-gcp-api-spec/schemas/gcp/openapi.yaml

MODULE  := github.com/openshift-online/gcp-hcp-ctl
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE    := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS := -X $(MODULE)/pkg/cli.version=$(VERSION) \
	-X $(MODULE)/pkg/cli.commit=$(COMMIT) \
	-X $(MODULE)/pkg/cli.date=$(DATE)

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*##' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "  %-14s %s\n", $$1, $$2}'

build: ## Build the gcphcpctl binary
	@mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o bin/gcphcpctl ./cmd/gcphcpctl
	@echo "Built bin/gcphcpctl"

test: ## Run unit tests
	go test -race ./...

lint: ## Run go vet
	go vet ./...

clean: ## Remove build artifacts
	rm -rf bin/

generate: ## Regenerate HyperFleet API types and client from schema (usage: make generate)
	oapi-codegen --package=hyperfleet --generate=types  -o pkg/hyperfleet/types.gen.go  $(HYPERFLEET_SCHEMA)
	oapi-codegen --package=hyperfleet --generate=client -o pkg/hyperfleet/client.gen.go $(HYPERFLEET_SCHEMA)

release: ## Tag and push a release (usage: make release v=0.2.0)
	@if [ -z "$(v)" ]; then echo "Usage: make release v=0.2.0"; exit 1; fi
	@echo "Tagging v$(v)..."
	git tag -a "v$(v)" -m "Release v$(v)"
	git push origin "v$(v)"
	@echo "Pushed v$(v)"
