# Gemquick

![alt gemquick](https://raw.githubusercontent.com/jimmitjoo/gemquick-bare/main/public/images/gemquick-logo.png)

Gemquick is a modern, full-featured web application framework for Go that provides everything you need to build scalable web applications quickly and securely.

## Features

- 🚀 **Chi Router** - Fast and lightweight HTTP router
- 🗄️ **Multi-Database Support** - PostgreSQL, MySQL, MariaDB, SQLite
- 🔐 **Security First** - CSRF protection, rate limiting, input validation, XSS prevention, 2FA
- 📧 **Email System** - Multiple provider support with templates
- 💾 **Caching** - Redis and Badger cache implementations
- 🔄 **Background Jobs** - Job queue with cron scheduler
- 🌐 **WebSocket Support** - Real-time communication with hub pattern
- 📁 **File Storage** - S3 and MinIO filesystem integrations
- 📱 **SMS Integration** - Multiple SMS provider support
- 🎨 **Template Engine** - Jet template engine for dynamic views
- 📊 **Logging & Metrics** - Structured logging with health monitoring
- 🔭 **OpenTelemetry** - Distributed tracing and observability
- 🔑 **Session Management** - Secure session handling with multiple stores
- 🛠️ **CLI Tools** - Project scaffolding and code generation
- 🤖 **AI-Native Development** - MCP server for AI assistants

## AI-Native Development (MCP)

Gemquick is the first AI-native Go framework. Use natural language with AI assistants to build your application.

### Setup

Add to your Claude Code / Cursor MCP config:

```json
{
  "mcpServers": {
    "gemquick": {
      "command": "gq",
      "args": ["mcp"]
    }
  }
}
```

### Available Tools

| Tool | Description |
|------|-------------|
| `gemquick_create_project` | Create a new GemQuick project |
| `gemquick_create_model` | Create a new database model |
| `gemquick_create_handler` | Create a new HTTP handler |
| `gemquick_create_migration` | Create a new database migration |
| `gemquick_create_middleware` | Create a new middleware |
| `gemquick_create_mail` | Create email template |
| `gemquick_run_migrations` | Run pending migrations |
| `gemquick_rollback` | Rollback migrations |
| `gemquick_setup_auth` | Setup auth with 2FA support |
| `gemquick_create_session_table` | Create session table |
| `gemquick_setup_docker` | Generate Docker config |
| `gemquick_module_info` | Get module setup instructions |

### Usage

Just ask your AI assistant:

- "Create a User model with name and email"
- "Add a migration to create a posts table"
- "Create a handler for managing products"
- "Run the database migrations"
- "Setup authentication for my app"
- "How do I add WebSocket support?"
- "Generate Docker configuration"

## Installation

Clone the repository and build the CLI tool:

```bash
git clone https://github.com/jimmitjoo/gemquick
cd gemquick
make build
```

This will create the `gq` executable in `dist/gq`. You can move this file to your PATH for global access.

## Quick Start

### Create a New Project

```bash
gq new my_project
cd my_project
make start
```

### Project Structure

```
my_project/
├── .env                 # Environment configuration
├── Makefile            # Build and development commands
├── handlers/           # HTTP handlers
├── migrations/         # Database migrations
├── views/              # Template files
├── email/              # Email templates
├── data/               # Models and database logic
├── public/             # Static assets
├── middleware/         # Custom middleware
└── logs/               # Application logs
```

## Development

### Available Make Commands

```bash
make key           # Generate new encryption key
make auth          # Create authentication system with user model
make mail          # Create new email template
make model         # Create new model in data directory
make migration     # Create new database migration
make handler       # Create new HTTP handler
make session       # Create session tables in database
make api-controller    # Create new API controller
make controller    # Create new resource controller
make middleware    # Create new middleware
make docker        # Generate Docker configuration
make deploy        # Generate deployment configuration
```

### Testing

Gemquick includes a beautiful test runner with colored output:

```bash
# Run all tests with colors
make test

# Run tests for specific package
./run-tests -p ./cache/...

# Generate coverage report
make cover

# Run tests without Docker dependencies
./run-tests -s
```

### Database Migrations

```bash
# Run migrations up
gq migrate up

# Roll back migrations
gq migrate down

# Create new migration
make migration name=create_users_table
```

## Opt-in Modules

Gemquick uses a modular architecture. Import only what you need:

```go
import (
    "github.com/jimmitjoo/gemquick"
    "github.com/jimmitjoo/gemquick/sms"
    "github.com/jimmitjoo/gemquick/email"
    "github.com/jimmitjoo/gemquick/websocket"
    "github.com/jimmitjoo/gemquick/otel"
)

func main() {
    app := gemquick.Gemquick{}

    // Initialize with only the modules you need
    app.New(rootPath,
        sms.NewModule(),                    // SMS support
        email.NewModule(),                  // Email support
        websocket.NewModule(),              // WebSocket support
        otel.NewModule(                     // OpenTelemetry tracing
            otel.WithServiceName("my-app"),
            otel.WithOTLPExporter("localhost:4317", true),
        ),
    )
}
```

### Using Modules

```go
// Send SMS
if sms := app.GetModule("sms"); sms != nil {
    sms.(*sms.Module).Send("+1234567890", "Hello!", false)
}

// Send Email
if email := app.GetModule("email"); email != nil {
    email.(*email.Module).Send(email.Message{
        To:      "user@example.com",
        Subject: "Welcome!",
    })
}

// WebSocket broadcast
if ws := app.GetModule("websocket"); ws != nil {
    ws.(*websocket.Module).Broadcast([]byte("Hello everyone!"))
}

// Mount WebSocket handler
if ws := app.GetModule("websocket"); ws != nil {
    app.HTTP.Router.Get("/ws", ws.(*websocket.Module).Handler())
}
```

### Module Configuration

Modules read from environment variables by default, or use functional options:

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

## Core Components

### Web Server Configuration

```go
app := gemquick.Gemquick{}
app.New(rootPath)
app.AppName = "MyApp"
app.Debug = true
```

### Database Connection

Supports multiple databases through environment configuration:

- PostgreSQL
- MySQL/MariaDB  
- SQLite

### Caching

Choose between Redis or Badger cache:

```go
// Redis cache
app.Cache = app.CreateRedisCache()

// Badger cache
app.Cache = app.CreateBadgerCache()
```

### Background Jobs

```go
// Register a job processor
app.JobManager.RegisterProcessor("send-email", emailProcessor)

// Queue a job
app.JobManager.QueueJob("send-email", data)
```

### WebSocket Support

```go
// Initialize WebSocket hub
hub := websocket.NewHub()
go hub.Run()
```

### Security Features

- CSRF protection middleware
- Rate limiting and throttling
- Input validation and sanitization
- SQL injection prevention
- XSS protection
- Session fixation protection
- Secure password hashing
- Two-Factor Authentication (TOTP)

### OpenTelemetry (Distributed Tracing)

Enable distributed tracing for production observability:

```env
OTEL_ENABLED=true
OTEL_SERVICE_NAME=my-app
OTEL_ENDPOINT=localhost:4317
OTEL_INSECURE=true
```

**Features:**
- Automatic HTTP request tracing
- Database query tracing
- Log correlation with trace IDs
- Support for OTLP, Zipkin exporters

**Custom Spans:**
```go
import "github.com/jimmitjoo/gemquick/otel"

func (h *Handler) ProcessOrder(w http.ResponseWriter, r *http.Request) {
    ctx, span := otel.Start(r.Context(), "process_order")
    defer span.End()

    otel.SetAttributes(ctx, otel.String("order.id", orderID))

    if err != nil {
        otel.RecordError(ctx, err)
    }
}
```

**Database Tracing:**
```go
tracedDB := otel.WrapDB(db, "postgres", "mydb")
rows, _ := tracedDB.Query(ctx, "SELECT * FROM users")
```

**Local Development with Jaeger:**
```bash
docker run -d --name jaeger \
  -p 16686:16686 -p 4317:4317 \
  jaegertracing/all-in-one:latest
# View traces at http://localhost:16686
```

## Configuration

Configuration is managed through `.env` file:

```env
# Application
APP_NAME=MyApp
DEBUG=true
PORT=4000

# Database
DATABASE_TYPE=postgres
DATABASE_DSN=your_connection_string

# Cache
CACHE=redis
REDIS_HOST=localhost:6379

# Session
SESSION_TYPE=redis
SESSION_LIFETIME=24

# Mail
MAIL_PROVIDER=smtp
SMTP_HOST=localhost
SMTP_PORT=1025

# OpenTelemetry (optional)
OTEL_ENABLED=false
OTEL_SERVICE_NAME=my-app
OTEL_ENDPOINT=localhost:4317
```

## API Development

Gemquick includes API utilities for building RESTful services:

- Version management
- Standardized JSON responses
- API middleware
- Error handling

```go
// API versioning
api.Version("v1", func(r chi.Router) {
    r.Get("/users", handlers.GetUsers)
})

// JSON responses
api.JSON(w, http.StatusOK, data)
```

## Testing Philosophy

- Comprehensive test coverage (aim for >80% on critical paths)
- Table-driven tests for better coverage
- Security-focused testing
- Docker-optional test execution
- Colorful test output for better readability

## Contributing

Bug reports and pull requests are welcome on GitHub at the [Gemquick repository](https://github.com/jimmitjoo/gemquick/). This project is intended to be a safe, welcoming space for collaboration. Contributors are expected to adhere to the [Contributor Covenant](https://www.contributor-covenant.org/).

## License

The Gemquick framework is available as open source under the terms of the [MIT License](https://opensource.org/licenses/MIT).

## Documentation

For detailed documentation and examples, see:

- [docs/modules.md](docs/modules.md) - SMS, Email, WebSocket, OTel modules guide
- [docs/extending.md](docs/extending.md) - Creating custom implementations
- [docs/opentelemetry.md](docs/opentelemetry.md) - OpenTelemetry integration guide
- [docs/query-builder.md](docs/query-builder.md) - Database query builder guide
- [docs/configuration.md](docs/configuration.md) - Full configuration reference
- [TESTING.md](TESTING.md) - Complete testing guide
- [CLAUDE.md](CLAUDE.md) - AI assistant integration guide

## Support

For issues, questions, or suggestions, please open an issue on the [GitHub repository](https://github.com/jimmitjoo/gemquick/issues).