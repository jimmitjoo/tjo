.PHONY: test test-simple cover coverage build_cli build clean release

# Get version from git tag (exact match), fallback to "dev"
VERSION := $(shell git describe --tags --exact-match 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X github.com/jimmitjoo/gemquick/core.Version=$(VERSION) -X github.com/jimmitjoo/gemquick.version=$(VERSION)"

## test: runs all tests with colors
test:
	@go run scripts/test-runner.go

## test-simple: runs all tests without colors
test-simple:
	@go test -v ./...

## cover: open coverage in browser
cover:
	@go test -coverprofile=coverage.out ./... && go tool cover -html=coverage.out

## coverage: displays test coverage
coverage:
	@go test -cover ./...

## build_cli: builds the command line tool gemquick and copies it to myapp
build_cli:
	@go build $(LDFLAGS) -o ../myapp/gq ./cmd/cli

## build: builds the command line tool to dist directory
build:
	@go build $(LDFLAGS) -o ./dist/gq ./cmd/cli
	@echo "Built gq version $(VERSION)"

## release: creates a new release (usage: make release v=0.5.0)
release:
	@if [ -z "$(v)" ]; then echo "Usage: make release v=0.5.0"; exit 1; fi
	@echo "Creating release v$(v)..."
	@git tag -a v$(v) -m "Release v$(v)"
	@echo "Tag v$(v) created. Push with: git push origin v$(v)"

clean:
	@rm -rf ./dist/*