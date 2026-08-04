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
	@#
	@# Only the root's requires are bumped. otel depends back on the root (for
	@# the logging package), and that requirement is deliberately left alone:
	@# under minimal version selection it is a floor, not a pin, so an app that
	@# requires root v$(v) compiles otel against v$(v) regardless. Bumping it
	@# would mean writing a requirement on a tag that does not exist yet, which
	@# breaks go.sum until the tag is pushed. Published otel v0.6.1 still
	@# requires root v0.5.4 for exactly this reason, and it resolves correctly.
	@# Only bump what the root already requires. Blindly requiring every
	@# submodule adds one the root does not import -- websocket appears only
	@# inside a documentation string in the MCP help -- which would put a
	@# needless dependency in every consumer's module graph.
	@for m in $(SUBMODULES); do \
		if grep -q "github.com/jimmitjoo/tjo/$$m v" go.mod; then \
			go mod edit -require=github.com/jimmitjoo/tjo/$$m@v$(v) go.mod; \
			echo "  root requires $$m v$(v)"; \
		fi; \
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
	@echo "The skeleton needs a matching tag, or tjo new will refuse to clone:"
	@echo "  gh api repos/jimmitjoo/tjo-bare/git/refs -f ref=refs/tags/v$(v) \\"
	@echo "    -f sha=\$$(gh api repos/jimmitjoo/tjo-bare/git/ref/heads/main --jq .object.sha)"
	@echo ""
	@echo "Then, once the tags are live:"
	@echo "  make release-check"

## release-check: verifies every module the way a user sees it
##
## CI runs the tests inside the workspace, so it resolves the submodules to the
## local directories. That is not what a consumer gets. This builds and tests
## each one with GOWORK=off, against the published versions of everything else.
release-check:
	@echo "Building and testing without the workspace (as a consumer resolves it)..."
	@GOWORK=off go build ./... && echo "  root      build OK"
	@GOWORK=off go test -short ./... >/dev/null 2>&1 && echo "  root      test OK" || echo "  root      TEST FAILED"
	@for m in $(SUBMODULES); do \
		(cd $$m && GOWORK=off go build ./... >/dev/null 2>&1 && echo "  $$m	build OK") || echo "  $$m	BUILD FAILED"; \
		(cd $$m && GOWORK=off go test -short ./... >/dev/null 2>&1 && echo "  $$m	test OK") || echo "  $$m	TEST FAILED"; \
	done

clean:
	@rm -rf ./dist/*