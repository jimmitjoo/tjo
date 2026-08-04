# AGENTS.md — cmd/tjo

The CLI, the project templates, and the MCP server.

## Templates are the product

`templates/` is embedded at build time. Editing generated output in a scaffolded
project changes nothing; edit the template.

The generated project must **compile**, which is not something `go build ./...`
in this repository checks. CI has a `scaffold` job that generates all four
starter templates, builds them, then runs `tjo make auth`, `make controller` and
`make handler` on top and builds again — that last combination is what breaks,
because a helper defined in one template and called from another compiles alone
and not together.

To reproduce it locally:

```bash
go build -o /tmp/tjo ./cmd/tjo
cd $(mktemp -d) && /tmp/tjo new demo -t default -d sqlite
cd demo && go build ./... && go vet ./...
```

Note the template's `go.mod` pins a released framework version, so a local
checkout needs a `replace` before this proves anything about your changes — see
the `scaffold` job for how CI injects it.

## Version references

`templates/go.mod.txt` pins the framework version and is bumped by
`make release`. `TestGeneratedGoModMatchesFramework` fails if its `go` directive
drifts from the root module's. The Dockerfile templates pin a Go image and have
no such guard; check them when the floor moves.

## MCP server

`mcp.go` exposes the generators over the Model Context Protocol. Tool names are
namespaced `tjo_*`. Ordering of `tools/list` must be deterministic — clients
cache the list and diff it to detect silent tool redefinition.

See the root [AGENTS.md](../../AGENTS.md).
