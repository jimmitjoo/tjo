.PHONY: test test-simple cover coverage build_cli build clean release release-push vuln lint-workflows

# Get version from git tag (exact match), fallback to "dev"
VERSION := $(shell git describe --tags --exact-match 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X github.com/jimmitjoo/tjo/core.Version=$(VERSION) -X github.com/jimmitjoo/tjo.version=$(VERSION)"

## test: runs all tests with colors
test:
	@go run scripts/test-runner.go

## test-simple: runs all tests without colors
test-simple:
	@go test -v ./...

## vuln: reports known vulnerabilities with a call path from our own code
#
# Same module list as the CI matrix, and for the same reason: this is a
# workspace, so `./...` from the root never reaches the four submodules.
# Mirrors the `vuln` job in .github/workflows/ci.yml -- keep them in step.
vuln:
	@for m in . email llm otel sms websocket; do \
		echo "==> $$m"; \
		(cd $$m && go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...) || exit 1; \
	done

## lint-workflows: checks the GitHub Actions workflows
#
# In Docker, because that is the only way to match what CI does. actionlint runs
# shellcheck over every `run:` block when shellcheck is present -- and it simply
# skips those checks when it is not. Running `go run .../actionlint` on a machine
# without shellcheck therefore reports success on scripts CI will reject, which
# is exactly what happened once: two real quoting findings were invisible
# locally and red on main.
lint-workflows:
	@docker run --rm -v "$(PWD):/repo:ro" -w /repo rhysd/actionlint:1.7.7 -color \
		.github/workflows/ci.yml .github/workflows/release.yml

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
SUBMODULES := email llm otel sms websocket

## release: creates a new release (usage: make release v=0.7.0)
##
## If this release fixes a vulnerability, publish the advisory AND request a CVE
## for it (gh api -X POST repos/jimmitjoo/tjo/security-advisories/GHSA-xxxx/cve).
##
## The CVE request is the load-bearing half. A repository advisory does not enter
## GitHub's global database on its own, so it never reaches OSV and never reaches
## vuln.go.dev -- which is what govulncheck reads. Publishing without it produces
## an advisory that the exact tool we tell users to run cannot see. All four of
## our advisories shipped that way before anyone checked; see #67.
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
	@echo "Release v$(v) created locally. Nothing is published until you run:"
	@echo ""
	@echo "  make release-push v=$(v)"

## release-push: pushes the tags and starts the build
##
## This exists because the previous instructions were a list of commands to run
## by hand, and one of them silently did nothing. The release workflow declared
## a tag trigger that never fired once: GitHub does not create workflow runs
## when more than three tags are pushed at once, and a release pushes five. The
## first v0.7.0 and v0.8.0 builds were both skipped that way.
##
## The trigger is gone and this dispatches the workflow explicitly, so there is
## one code path instead of an automation that looks like it works. See #61.
release-push:
	@if [ -z "$(v)" ]; then echo "Usage: make release-push v=0.9.0"; exit 1; fi
	@# Framework tags first. The skeleton has to require the released version,
	@# and it cannot resolve that requirement until the tag exists -- checking
	@# the skeleton before pushing, as this target did originally, is a
	@# dependency cycle that can never be satisfied on a first release.
	@echo "Pushing..."
	@git push origin main
	@# Fully-qualified refs, because a branch named after the release makes
	@# "v$(v)" ambiguous and git refuses the push outright: "src refspec v0.11.0
	@# matches more than one". Naming a release branch after its version is a
	@# reasonable thing to do, and it broke this target once.
	@git push origin refs/tags/v$(v) $(foreach m,$(SUBMODULES),refs/tags/$(m)/v$(v))
	@echo ""
	@# Now the skeleton can be checked and tagged. `tjo new` clones the skeleton
	@# tag matching the CLI's own version, so a tag pointing at a tree that
	@# requires the previous release produces projects that are wrong in a way
	@# nothing in this repository would catch. v0.8.0 shipped exactly that,
	@# because tagging was separated from checking.
	@echo "Checking the skeleton requires v$(v)..."
	@skel=$$(gh api repos/jimmitjoo/tjo-bare/contents/go.mod --jq '.content' | base64 -d | grep -oE 'jimmitjoo/tjo v[0-9.]+' | head -1); \
		case "$$skel" in \
			*"v$(v)") echo "  tjo-bare requires $$skel";; \
			*) echo "  tjo-bare requires $$skel, not v$(v)"; \
			   echo "  update and push tjo-bare, then re-run this target"; exit 1;; \
		esac
	@gh api repos/jimmitjoo/tjo-bare/git/refs -f ref=refs/tags/v$(v) \
		-f sha=$$(gh api repos/jimmitjoo/tjo-bare/git/ref/heads/main --jq .object.sha) >/dev/null 2>&1 \
		&& echo "  tagged tjo-bare v$(v)" \
		|| gh api -X PATCH repos/jimmitjoo/tjo-bare/git/refs/tags/v$(v) \
			-f sha=$$(gh api repos/jimmitjoo/tjo-bare/git/ref/heads/main --jq .object.sha) -F force=true >/dev/null \
		&& echo "  moved tjo-bare v$(v)"
	@echo ""
	@echo "Starting the release build..."
	@gh workflow run release.yml -f version=v$(v)
	@echo "  dispatched; watch with: gh run watch"
	@echo ""
	@echo "Once it finishes:"
	@echo "  go mod tidy && git commit -am 'Tidy go.sum now the v$(v) tags are published'"
	@echo "  make release-check"

## release-check: verifies every module the way a user sees it
##
## CI runs the tests inside the workspace, so it resolves the submodules to the
## local directories. That is not what a consumer gets. This builds and tests
## each one with GOWORK=off, against the published versions of everything else.
release-check:
	@echo "Checking for known vulnerabilities..."
	@$(MAKE) --no-print-directory vuln >/dev/null 2>&1 && echo "  vuln      OK" || { echo "  vuln      FAILED -- run 'make vuln'"; exit 1; }
	@echo ""
	@echo "Building and testing without the workspace (as a consumer resolves it)..."
	@GOWORK=off go build ./... && echo "  root      build OK"
	@GOWORK=off go test -short ./... >/dev/null 2>&1 && echo "  root      test OK" || echo "  root      TEST FAILED"
	@for m in $(SUBMODULES); do \
		(cd $$m && GOWORK=off go build ./... >/dev/null 2>&1 && echo "  $$m	build OK") || echo "  $$m	BUILD FAILED"; \
		(cd $$m && GOWORK=off go test -short ./... >/dev/null 2>&1 && echo "  $$m	test OK") || echo "  $$m	TEST FAILED"; \
	done

clean:
	@rm -rf ./dist/*