BINARY  := app
VERSION ?= dev
LDFLAGS := -s -w -X main.version=$(VERSION)

# Keep in sync with .github/workflows so local results match CI.
GOLANGCI_LINT_VERSION := v2.14.0
SEMGREP_VERSION       := 1.181.0
GORELEASER_VERSION    := v2.18.3

.PHONY: build snapshot run test lint fmt sast check clean

build: ## Build a static binary into dist/
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/$(BINARY) ./cmd/app

snapshot: ## Build all release targets with GoReleaser, without publishing
	docker run --rm -v "$(CURDIR)":/src -w /src -e GOTOOLCHAIN=auto --entrypoint sh \
		goreleaser/goreleaser:$(GORELEASER_VERSION) \
		-c 'git config --global --add safe.directory /src && goreleaser release --snapshot --clean'

run: ## Run the server locally on :8080
	go run ./cmd/app

test: ## Run tests with the race detector
	go test -race -shuffle=on ./...

lint: ## Run golangci-lint (pinned, via Docker)
	docker run --rm -v "$(CURDIR)":/app -w /app \
		golangci/golangci-lint:$(GOLANGCI_LINT_VERSION) golangci-lint run ./...

fmt: ## Format code with the configured formatters
	docker run --rm -v "$(CURDIR)":/app -w /app \
		golangci/golangci-lint:$(GOLANGCI_LINT_VERSION) golangci-lint fmt ./...

sast: ## Run semgrep with the Go ruleset (pinned, via Docker)
	docker run --rm -v "$(CURDIR)":/src -w /src \
		semgrep/semgrep:$(SEMGREP_VERSION) semgrep scan --config p/golang --error --metrics=off

check: lint sast test ## Everything CI runs

clean:
	rm -rf dist/
