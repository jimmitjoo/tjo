.PHONY: test test-simple cover coverage build_cli build clean release

# Get version from git tag (exact match), fallback to "dev"
VERSION := $(shell git describe --tags --exact-match 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X github.com/jimmitjoo/tjo/core.Version=$(VERSION) -X github.com/jimmitjoo/tjo.version=$(VERSION)"

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

## build_cli: builds the command line tool tjo and copies it to myapp
build_cli:
	@go build $(LDFLAGS) -o ../myapp/tjo ./cmd/cli

## build: builds the command line tool to dist directory
build:
	@go build $(LDFLAGS) -o ./dist/tjo ./cmd/cli
	@echo "Built tjo version $(VERSION)"

## release: creates a new release (usage: make release v=0.5.0)
release:
	@if [ -z "$(v)" ]; then echo "Usage: make release v=0.5.0"; exit 1; fi
	@echo "Creating release v$(v)..."
	@echo "Updating version references in tjo..."
	@# Update otel/go.mod
	@sed -i 's|github.com/jimmitjoo/tjo v[0-9.]*|github.com/jimmitjoo/tjo v$(v)|g' otel/go.mod
	@# Update template go.mod
	@sed -i 's|github.com/jimmitjoo/tjo v[0-9.]*|github.com/jimmitjoo/tjo v$(v)|g' cmd/cli/templates/go.mod.txt
	@# Run go mod tidy in otel to update go.sum
	@cd otel && go mod tidy 2>/dev/null || true
	@# Commit changes if any
	@git add -A
	@git diff --cached --quiet || git commit -m "Update version references to v$(v)"
	@git tag -a v$(v) -m "Release v$(v)"
	@echo ""
	@echo "Updating tjo-bare..."
	@if [ -d "../tjo-bare" ]; then \
		sed -i 's|github.com/jimmitjoo/tjo v[0-9.]*|github.com/jimmitjoo/tjo v$(v)|g' ../tjo-bare/go.mod; \
		cd ../tjo-bare && go mod tidy 2>/dev/null || true; \
		git add -A; \
		git diff --cached --quiet || git commit -m "Update tjo to v$(v)"; \
		echo "tjo-bare updated"; \
	else \
		echo "Warning: ../tjo-bare not found, skipping"; \
	fi
	@echo ""
	@echo "Release v$(v) created!"
	@echo "Push tjo:      git push && git push origin v$(v)"
	@echo "Push tjo-bare: cd ../tjo-bare && git push"

clean:
	@rm -rf ./dist/*