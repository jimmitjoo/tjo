# Tjo

![alt tjo](https://raw.githubusercontent.com/jimmitjoo/tjo-bare/main/public/images/tjo-logo.webp)

Tjo is a modern, full-featured web application framework for Go that provides everything you need to build scalable web applications quickly and securely.

## Requirements

- Go 1.24+

## Features

- Chi Router - Fast and lightweight HTTP router
- Multi-Database Support - PostgreSQL, MySQL, MariaDB, SQLite
- Security First - CSRF protection, rate limiting, input validation, XSS prevention, 2FA
- Email System - Multiple provider support with templates
- Caching - Redis and Badger cache implementations
- Background Jobs - Job queue with cron scheduler
- WebSocket Support - Real-time communication with hub pattern
- File Storage - S3 and MinIO filesystem integrations
- SMS Integration - Multiple SMS provider support
- Template Engine - Jet template engine for dynamic views
- Logging & Metrics - Structured logging with health monitoring
- OpenTelemetry - Distributed tracing and observability
- Session Management - Secure session handling with multiple stores
- CLI Tools - Project scaffolding and code generation
- AI-Native Development - MCP server for AI assistants

## Installation

### From Source

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

- CSRF protection middleware
- Rate limiting and throttling
- Input validation and sanitization
- SQL injection prevention
- XSS protection
- Secure password hashing (bcrypt)
- Two-Factor Authentication (TOTP)

## Testing

```bash
make test              # Run all tests
./run-tests -p ./pkg   # Test specific package
./run-tests -c         # With coverage
./run-tests -s         # Skip Docker tests
make cover             # Coverage report
```

## Documentation

- [docs/modules.md](docs/modules.md) - Module guide
- [docs/opentelemetry.md](docs/opentelemetry.md) - OpenTelemetry guide
- [docs/query-builder.md](docs/query-builder.md) - Query builder guide
- [docs/configuration.md](docs/configuration.md) - Configuration reference
- [TESTING.md](TESTING.md) - Testing guide
- [CLAUDE.md](CLAUDE.md) - AI assistant guide

## Contributing

Pull requests welcome at [github.com/jimmitjoo/tjo](https://github.com/jimmitjoo/tjo/).

## License

MIT License
