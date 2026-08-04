# AGENTS.md

## Read this first: `go test ./...` does not test this repository

This is a Go **workspace** (`go.work`). `email`, `otel`, `sms` and `websocket`
are separate modules. From the repository root, `go build ./...`,
`go vet ./...`, `go test ./...` and `govulncheck ./...` all cover the root
module **only** and silently skip the other four.

That blind spot shipped six defects (#25–#28) and 71 reachable vulnerabilities
behind a green build. Run everything per module, or use the make targets, which
already do:

```bash
make test        # all five modules
make vuln        # govulncheck, all five modules
```

Per module, by hand:

```bash
for m in . email otel sms websocket; do (cd $m && go build ./... && go vet ./... && go test -short -race ./...); done
```

`go mod tidy` has the same trap. Run it in each module directory; a tidy from
the root does not reach the submodules.

## Generate code, do not write it

`tjo make` produces handlers, models, controllers, auth and migrations. Use it.
Generated output is deterministic and compiles; hand-written equivalents drift
from the templates and are how `tjo make auth` came to emit 181 references to a
struct that no longer existed.

```bash
tjo make handler Thing
tjo make controller Widget
tjo make model User
tjo make auth
tjo migrate up
```

The templates live in `cmd/tjo/templates/`. Changing generated code means
changing the template, not the output.

## Testing conventions

- **Every fix ships a regression test that was run against the unfixed code and
  observed to fail.** Not "a test was added" — the test is verified to fail
  first. If you cannot make it fail, you have not understood the bug.
- `-short` skips tests that need Docker or a live service. CI runs `-short`, so
  anything only covered without it is effectively uncovered.
- `-race` is not optional. Several fixed bugs were data races.
- Tests that need a service skip when its environment variable is unset:
  `TJO_TEST_POSTGRES_DSN`, `TJO_TEST_S3_ENDPOINT`.
- No test may call a live third-party API. Two did, asserted only that
  something failed — equally true with no network — and were replaced with
  `httptest`.

## Security-sensitive areas

Changes here need a regression test and careful review:

- `security/` — throttling, headers, validation, CSRF configuration
- `session/`, `middleware.go`, `routes.go` — session and CSRF installation
- `cmd/tjo/templates/` — **generated code is in scope for advisories.** A flaw
  in what `tjo make auth` emits is a framework vulnerability; see `SECURITY.md`.
- `internal/jsonstrict` — request body parsing

Two recurring failure modes in this codebase, both of which have shipped:

1. **A control that removes itself when a dependency is missing.** The router
   installed CSRF only `if session != nil`, and session was assigned later, so
   it never ran. Fail closed and return an error instead.
2. **A fix verified at the function it changed rather than through the path a
   request takes.** The rate-limiter fix was correct and was defeated by a
   middleware mounted two lines earlier. Test through the router.

## Layout

| Path | What |
|---|---|
| `tjo.go`, `routes.go`, `middleware.go` | framework wiring |
| `api/` | REST helpers, versioning, rate limiting |
| `cache/`, `session/` | Redis and Badger backends |
| `cmd/tjo/` | the CLI, its templates, and the MCP server |
| `core/` | minimal helpers usable without the framework |
| `database/` | query builder, migrations, seeding |
| `security/` | throttling, headers, validation |
| `email/`, `otel/`, `sms/`, `websocket/` | **separate modules** |

## Conventions

- Comments explain **why**, not what. Several in this codebase record a bug that
  a change prevents — do not delete those when refactoring past them.
- Commit messages describe the defect and the evidence, not the diff.
- Prefer the standard library. v0.9.0 removed four dependencies by replacing
  them with stdlib calls or forty lines.
- Go 1.25 is the language floor (`go.mod`), 1.26 the toolchain. Do not raise the
  floor without a reason that names the feature.
