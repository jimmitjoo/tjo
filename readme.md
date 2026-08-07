# Tjo

[![CI](https://github.com/jimmitjoo/tjo/actions/workflows/ci.yml/badge.svg)](https://github.com/jimmitjoo/tjo/actions/workflows/ci.yml)
[![OpenSSF Scorecard](https://api.securityscorecards.dev/projects/github.com/jimmitjoo/tjo/badge)](https://scorecard.dev/viewer/?uri=github.com/jimmitjoo/tjo)

![alt tjo](https://raw.githubusercontent.com/jimmitjoo/tjo-bare/main/public/images/tjo-logo.webp)

Tjo is a modern, full-featured web application framework for Go that provides everything you need to build scalable web applications quickly and securely.

## Installation

Download a prebuilt CLI for macOS, Linux or Windows from the
[latest release](https://github.com/jimmitjoo/tjo/releases/latest), or build
from source:

```bash
go install github.com/jimmitjoo/tjo/cmd/tjo@latest
```

macOS binaries are unsigned, so Gatekeeper will ask before the first run.

## Requirements

- Go 1.25+ (to build applications; not needed to run the CLI)

## Features

- Chi Router - Fast and lightweight HTTP router
- Multi-Database Support - PostgreSQL, MySQL, MariaDB, SQLite (PostgreSQL needs `WithDialect`, see [query builder docs](docs/query-builder.md#database-dialects))
- Internationalisation - CLDR plurals, locale negotiation, right-to-left, and the framework's own strings translatable ([docs](docs/i18n.md))
- Admin Panel - Model-driven CRUD over your own structs, server-rendered, no build step ([docs](docs/admin.md))
- Ops Dashboard - Self-hosted errors, slow queries, job queue, cron and health ([docs](docs/admin.md#the-ops-dashboard))
- Authentication - Passwords, 2FA with recovery codes, remember-me, passkeys, social login, organizations and roles, as a package rather than generated code ([social login docs](docs/social-login.md))
- Security First - CSRF protection, rate limiting, input validation, XSS prevention, 2FA
- Email System - Multiple provider support with templates
- Caching - Redis and Badger cache implementations
- Background Jobs - Database-backed queue with cron scheduler and durable, checkpointed workflows
- WebSocket Support - Real-time communication with hub pattern
- Server-Sent Events - Streaming, and broadcast of rendered fragments to subscribed clients
- File Storage - S3 and MinIO filesystem integrations
- SMS Integration - Multiple SMS provider support
- Template Engine - Jet template engine for dynamic views
- Logging & Metrics - Structured logging with health monitoring
- OpenTelemetry - Distributed tracing and observability
- LLM Integration - Chat, tools, structured output and embeddings over the first-party SDKs ([docs](docs/modules.md#llm-module))
- Vector Search - pgvector and sqlite-vec in the query builder
- Session Management - Secure session handling with multiple stores
- CLI Tools - Project scaffolding and code generation
- AI-Native Development - MCP server for AI assistants

### Building the CLI from a checkout

```bash
git clone https://github.com/jimmitjoo/tjo
cd tjo
make build
```

This creates the `tjo` executable in `dist/tjo`. Add it to your PATH for global access.

### Go Install

```bash
go install github.com/jimmitjoo/tjo/cmd/tjo@latest
```

## Quick Start

### Create a New Project

```bash
tjo new myapp
cd myapp
tjo run
```

### Starter Templates

Tjo includes starter templates for common use cases:

```bash
tjo new myapp                      # Default template
tjo new myapp -t blog              # Blog starter
tjo new myapp -t api               # API-only starter
tjo new myapp -t saas              # SaaS starter with billing
```

| Template | Description |
|----------|-------------|
| `default` | Basic web application with authentication |
| `blog` | Blog with posts, categories, and comments |
| `api` | REST API with versioning and JWT auth |
| `saas` | SaaS with Stripe billing and subscriptions |

### Running Your Application

```bash
tjo run              # Start the application
tjo run --watch      # Hot-reload during development (requires air)
tjo run -w           # Short form
```

### Project Structure

```
myapp/
├── .env                 # Environment configuration
├── Makefile             # Build and development commands
├── handlers/            # HTTP handlers
├── migrations/          # Database migrations
├── views/               # Template files
├── email/               # Email templates
├── data/                # Models and database logic
├── public/              # Static assets
├── middleware/          # Custom middleware
└── logs/                # Application logs
```

## CLI Commands

```bash
tjo new <name>           # Create new project
tjo new <name> -t <tpl>  # Create with starter template
tjo run                  # Run application
tjo run -w               # Run with hot-reload
tjo migrate              # Run migrations up
tjo migrate down         # Rollback last migration
tjo migrate reset        # Reset all migrations
tjo make model <name>    # Create model
tjo make handler <name>  # Create handler
tjo make migration <name># Create migration
tjo make mail <name>     # Create email template
tjo make auth            # Setup authentication
tjo make session         # Create session tables
tjo mcp                  # Start MCP server
```

## AI-Native Development (MCP)

Tjo includes an MCP server for AI assistants like Claude Code and Cursor.

### Setup

Add to your MCP config:

```json
{
  "mcpServers": {
    "tjo": {
      "command": "tjo",
      "args": ["mcp"]
    }
  }
}
```

### Available Tools

| Tool | Description |
|------|-------------|
| `tjo_create_project` | Create a new Tjo project |
| `tjo_create_model` | Create a database model |
| `tjo_create_handler` | Create an HTTP handler |
| `tjo_create_migration` | Create a database migration |
| `tjo_create_middleware` | Create middleware |
| `tjo_create_mail` | Create email template |
| `tjo_run_migrations` | Run pending migrations |
| `tjo_rollback` | Rollback migrations |
| `tjo_setup_auth` | Setup auth with 2FA |
| `tjo_create_session_table` | Create session table |
| `tjo_setup_docker` | Generate Docker config |
| `tjo_module_info` | Get module setup instructions |

### Usage

Just ask your AI assistant:

- "Create a User model with name and email"
- "Add a migration to create a posts table"
- "Create a handler for managing products"
- "Setup authentication for my app"

## Opt-in Modules

Import only what you need:

```go
import (
    "github.com/jimmitjoo/tjo"
    "github.com/jimmitjoo/tjo/sms"
    "github.com/jimmitjoo/tjo/email"
    "github.com/jimmitjoo/tjo/websocket"
    "github.com/jimmitjoo/tjo/otel"
)

func main() {
    app := tjo.Tjo{}
    app.New(rootPath,
        sms.NewModule(),
        email.NewModule(),
        websocket.NewModule(),
        otel.NewModule(
            otel.WithServiceName("my-app"),
            otel.WithOTLPExporter("localhost:4317", true),
        ),
    )
}
```

### Module Configuration

```go
// SMS with Twilio
sms.NewModule(sms.WithTwilio(accountSid, apiKey, apiSecret, fromNumber))

// Email with SMTP
email.NewModule(
    email.WithSMTP("smtp.example.com", 587, "user", "pass", "tls"),
    email.WithFrom("noreply@example.com", "My App"),
)

// WebSocket with auth
websocket.NewModule(
    websocket.WithAllowedOrigins([]string{"https://example.com"}),
    websocket.WithAuthenticateConnection(myAuthFunc),
)
```

## Configuration

Configuration via `.env` file:

```env
# Application
APP_NAME=MyApp
DEBUG=true
PORT=4000

# Database
DATABASE_TYPE=postgres
DATABASE_HOST=localhost
DATABASE_PORT=5432
DATABASE_NAME=myapp
DATABASE_USER=postgres
DATABASE_PASS=password

# Cache
CACHE=redis
REDIS_HOST=localhost:6379

# Session
SESSION_TYPE=redis
SESSION_LIFETIME=24

# OpenTelemetry (optional)
OTEL_ENABLED=false
OTEL_SERVICE_NAME=my-app
OTEL_ENDPOINT=localhost:4317
```

## Security

- Cross-origin request protection (`net/http.CrossOriginProtection`) and CSRF tokens
- Rate limiting and throttling, with trusted-proxy-aware client IP resolution
- Input validation and sanitization
- SQL injection prevention
- XSS protection
- Secure password hashing (bcrypt)
- Two-Factor Authentication (TOTP)

To report a vulnerability, see [SECURITY.md](SECURITY.md). Reports go through
GitHub private vulnerability reporting, not public issues.

Four advisories have been published and fixed, all explained in the
[changelog](CHANGELOG.md) with what was measured rather than only what changed.
Generated code is in scope: a flaw in what `tjo make auth` produces is a flaw in
the framework.

`govulncheck` gates every build across all five modules, weekly as well as on
every change. Run it yourself:

```bash
make vuln
```

## Authentication

The `auth` package works with `net/http` and your own storage. It declares
interfaces and provides verbs; it never owns a table, so an application that
needs to join on users has one users table rather than two.

```go
// Login. The lookup and the comparison happen unconditionally, in that order,
// so an unknown address costs the same as a known one.
account, err := auth.Authenticate(ctx, store, email, password)

// Password reset. Single-use, database-persisted, bound to a user, and
// consumed atomically.
token, _ := auth.NewResetToken(userID, auth.PurposePasswordReset, time.Hour)
resetStore.Save(ctx, token)          // mail token.PlainText; only the hash is stored

userID, hash, err := auth.ResetPassword(ctx, resetStore, policy, submitted, newPassword)

// Social login. Resolve decides which account a verified identity signs into,
// and never merges on an email -- see docs/social-login.md.
identity, err := google.Finish(ctx, ceremonyState, r)
resolution, err := auth.Resolve(ctx, identities, identity, auth.ResolveOptions{
    CurrentAccountID: signedIn, Accounts: accounts,
})

// Organizations, which is where multi-tenancy lives.
err = auth.Authorize(ctx, orgs, perms, orgID, accountID, auth.PermManageMembers)
qb, err := auth.ScopeTo(ctx, database.NewQueryBuilder(db).Table("invoices"), "organization_id")
```

`SQLResetStore` ships for PostgreSQL, MySQL and SQLite because token consumption
has to be atomic, and a SELECT-then-UPDATE implementation loses that race with
somebody else's account as the prize.

## Agent evaluation

Can a coding agent build a working Tjo application? [`evals/`](evals/) measures
it with the compiler as the grader — a task passes if the project builds, vets
and passes its own tests.

```bash
make build
go run ./evals -cli $(pwd)/dist/tjo              # deterministic baseline
go run ./evals -cli $(pwd)/dist/tjo -agent '...' # with a model in the loop
```

The deterministic baseline is 5/5. No generative number has been published yet;
see [evals/README.md](evals/README.md) for why one without a named model, prompt
and date is worse than none.

## Testing

```bash
make test              # Run all tests
make vuln              # Known vulnerabilities reachable from our code
./run-tests -p ./pkg   # Test specific package
./run-tests -c         # With coverage
./run-tests -s         # Skip Docker tests
make cover             # Coverage report
```

Some tests need a service and skip without one:

```bash
# PostgreSQL integration tests (database/postgres_integration_test.go)
docker run -d --name tjo-pg -e POSTGRES_PASSWORD=secret -e POSTGRES_USER=tjo \
  -e POSTGRES_DB=tjotest -p 5432:5432 postgres:16-alpine
TJO_TEST_POSTGRES_DSN='postgres://tjo:secret@localhost:5432/tjotest?sslmode=disable' go test ./database/...
```

## Documentation

- [docs/modules.md](docs/modules.md) - Module guide
- [docs/opentelemetry.md](docs/opentelemetry.md) - OpenTelemetry guide
- [docs/query-builder.md](docs/query-builder.md) - Query builder guide
- [docs/social-login.md](docs/social-login.md) - Social login and the linking policy
- [docs/configuration.md](docs/configuration.md) - Configuration reference
- [TESTING.md](TESTING.md) - Testing guide
- [CLAUDE.md](CLAUDE.md) - AI assistant guide

## Contributing

Pull requests welcome at [github.com/jimmitjoo/tjo](https://github.com/jimmitjoo/tjo/).

## License

MIT License

## For coding agents

- [AGENTS.md](AGENTS.md) — how to work in this repository, and the traps that have shipped defects here
- [llms.txt](llms.txt) — a short orientation, leading with what models get wrong about this framework
- [llms-full.txt](llms-full.txt) — the same with signatures and worked examples
- [skills/tjo](skills/tjo) — an Agent Skills bundle, installable as a Claude Code plugin:

  ```
  /plugin marketplace add jimmitjoo/tjo
  /plugin install tjo@tjo
  ```

- `tjo mcp` — a Model Context Protocol server over stdio: the generators, plus
  introspection over your application's routes, schema and configuration, plus
  the documentation of the version you have installed.

Whether any of this changes what an agent produces is a measurable question and
[evals/README.md](evals/README.md) says how it is measured, including that the
number has not been recorded yet.
