# AGENTS.md — sms

This is a **separate Go module**, not a package of the root module. Commands run
from the repository root do not reach it.

```bash
cd sms
go build ./... && go vet ./... && go test -short -race ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...
go mod tidy      # from here, never from the root
```

It is tagged in lockstep with the root module (`sms/v0.9.0`), so a change here
is part of the next framework release, not independent of it.

See the root [AGENTS.md](../AGENTS.md) for conventions that apply everywhere.
