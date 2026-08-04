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
	@go build $(LDFLAGS) -o ../myapp/tjo ./cmd/tjo

## build: builds the command line tool to dist directory
build:
	@go build $(LDFLAGS) -o ./dist/tjo ./cmd/tjo
	@echo "Built tjo version $(VERSION)"

# Submodules that are tagged in lockstep with the root module.
SUBMODULES := email otel sms websocket

## release: creates a new release (usage: make release v=0.7.0)
##
## This repository is a Go workspace: email, otel, sms and websocket are
## separate modules with their own tags. Tagging only the root leaves the root
## go.mod pointing at the previous versions of those modules, so half the
## release never reaches anyone -- go.work hides that locally.
release:
	@if [ -z "$(v)" ]; then echo "Usage: make release v=0.7.0"; exit 1; fi
	@git diff --quiet || { echo "Working tree is dirty; commit first."; exit 1; }
	@echo "Creating release v$(v)..."
	@echo ""
	@echo "Updating version references..."
	@# go mod edit rather than sed: portable, and it understands the format.
	@for m in $(SUBMODULES); do \
		go mod edit -require=github.com/jimmitjoo/tjo/$$m@v$(v) go.mod 2>/dev/null || true; \
	done
	@# The scaffolding template pins the framework version for new projects.
	@sed -i.bak 's|github.com/jimmitjoo/tjo v[0-9.]*|github.com/jimmitjoo/tjo v$(v)|g' \
		cmd/tjo/templates/go.mod.txt && rm -f cmd/tjo/templates/go.mod.txt.bak
	@git add -A
	@git diff --cached --quiet || git commit -m "Update version references to v$(v)"
	@echo ""
	@echo "Tagging..."
	@git tag -a v$(v) -m "Release v$(v)"
	@for m in $(SUBMODULES); do \
		git tag -a $$m/v$(v) -m "Release $$m/v$(v)"; \
		echo "  $$m/v$(v)"; \
	done
	@echo ""
	@echo "Release v$(v) created locally. Nothing is published until you push."
	@echo ""
	@echo "  git push origin main"
	@echo "  git push origin v$(v) $(foreach m,$(SUBMODULES),$(m)/v$(v))"
	@echo ""
	@echo "Then verify a consumer sees it, which go.work would otherwise mask:"
	@echo "  GOWORK=off go build ./..."

## release-check: verifies the module resolves the way a user sees it
release-check:
	@echo "Building without the workspace (as an external consumer would)..."
	@GOWORK=off go build ./... && echo "  root module OK"
	@for m in $(SUBMODULES); do \
		(cd $$m && GOWORK=off go build ./... >/dev/null 2>&1 && echo "  $$m OK") || echo "  $$m FAILED"; \
	done

clean:
	@rm -rf ./dist/*