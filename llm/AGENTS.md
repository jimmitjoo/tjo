# AGENTS.md — llm

This is a **separate Go module**, not a package of the root module. Commands run
from the repository root do not reach it.

```bash
cd llm
go build ./... && go vet ./... && go test -short -race ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...
go mod tidy      # from here, never from the root
```

It is tagged in lockstep with the root module (`llm/v0.12.0`), so a change here
is part of the next framework release, not independent of it.

## Thin is the specification

The scope is chat with streaming, tool calling, structured output, and
embeddings. Anything else — evals, prompt management, chunking strategy, agent
orchestration, cost *control* — is out, and the reasons are in the package
comment rather than in anyone's memory. A change that adds one of those is
changing the specification, not implementing it.

The escape hatch is deliberate: a caller who needs something a provider offers
and this does not should hold the SDK client directly. That is cheaper than
growing this package to cover it.

## No test may call a live API

Every test here runs against `httptest`. A test that needs a key is a test that
does not run in CI, costs money when it does, and asserts nothing a recorded
response does not. Two tests elsewhere in this repository once called real APIs
and asserted only that something failed — which was equally true with no
network.

## Providers must stay symmetric

The point of the package is that one `Request` works on both providers. When
they disagree, the difference is absorbed here rather than surfaced:

- The system prompt is a message on OpenAI and a top-level field on Anthropic.
- `max_tokens` is optional on OpenAI and required on Anthropic.
- A tool result is its own role on OpenAI and a user message on Anthropic.
- Structured output is `response_format` on OpenAI and a forced tool call on
  Anthropic, with the result lifted back into `Response.Text`.

A test that passes on one provider and is not run against the other is how those
drift. `TestTheSameRequestWorksOnBothProviders` and its siblings exist to make
that visible.

See the root [AGENTS.md](../AGENTS.md) for conventions that apply everywhere.
