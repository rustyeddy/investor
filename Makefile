# Common development tasks for Investor. Run `make help` to list them.

BIN  := bin/investor
CONFIG ?= examples/firm.json
# Extra arguments for `make run`, e.g. make run ARGS='--runtime model --objective "Evaluate EUR/USD"'.
ARGS ?= --workflow research

.DEFAULT_GOAL := help
.PHONY: help build run validate plan serve test vet fmt fmt-check check clean

help: ## List targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-10s %s\n", $$1, $$2}'

build: ## Build bin/investor
	go build -o $(BIN) ./cmd/investor

run: build ## Run a workflow (dry-run unless ARGS selects --runtime model)
	./$(BIN) run --config $(CONFIG) $(ARGS)

validate: build ## Validate the configuration and skills
	./$(BIN) validate --config $(CONFIG)

plan: build ## Print the workflow plan
	./$(BIN) plan --config $(CONFIG) $(ARGS)

serve: build ## Serve the HTTP API in the foreground
	./$(BIN) serve --config $(CONFIG)

test: ## Run tests with the race detector
	go test -race ./...

vet: ## Run go vet
	go vet ./...

fmt: ## Format Go code
	gofmt -w .

fmt-check: ## Fail if any Go file is not gofmt-formatted
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

check: fmt-check vet test ## Run the checks required before a PR

clean: ## Remove build output
	rm -rf bin
