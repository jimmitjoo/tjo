# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

### Testing
```bash
# Run all tests with colorful output
make test

# Run tests without colors  
make test-simple

# Run tests for specific package
./run-tests -p ./cache/...
go test ./cache/...

# Run a single test
go test -run TestFunctionName ./package

# Generate coverage report (opens in browser)
make cover

# Show coverage in terminal
make coverage

# Run tests with coverage
./run-tests -c
go test -cover ./...

# Skip Docker-dependent tests
./run-tests -s
go test -short ./...
```

### Building
```bash
# Build the CLI tool to dist/tjo
make build

# Build and copy to ../myapp/tjo
make build_cli

# Clean build artifacts
make clean
```

### Linting
No specific linting commands found. Consider using `go fmt ./...` and `go vet ./...` for basic checks.

## Architecture

### Core Framework (`tjo.go`)
The main Tjo struct orchestrates all framework components:
- **Web Server**: Chi router with middleware support
- **Database**: Abstracted database interface supporting multiple drivers
- **Session Management**: SCS session manager with Redis/Badger support  
- **Caching**: Redis or Badger cache implementations
- **Template Engine**: Jet template engine for views
- **Job System**: Background job processing with cron scheduler
- **Email/SMS**: Integrated mail and SMS providers
- **File Systems**: S3 and MinIO filesystem support
- **Security**: CSRF protection, rate limiting, input validation
- **Logging**: Structured logging with metrics and health monitoring

### Package Structure
- `api/` - REST API utilities (versioning, response helpers, middleware)
- `cache/` - Cache implementations (Redis, Badger)
- `cmd/tjo/` - CLI tool for project scaffolding and migrations
- `database/` - Database utilities (query builder, health checks, seeders)
- `email/` - Email sending with multiple providers
- `filesystems/` - File storage abstractions (S3, MinIO)
- `jobs/` - Background job processing system
- `logging/` - Structured logging and metrics
- `otel/` - OpenTelemetry distributed tracing and observability
- `render/` - Template rendering utilities
- `security/` - Security middleware (CSRF, rate limiting, validation)
- `session/` - Session management
- `sms/` - SMS provider integrations
- `urlsigner/` - URL signing for secure links
- `websocket/` - WebSocket support with hub pattern

### Key Patterns
1. **Dependency Injection**: Core services injected through Tjo struct
2. **Interface-based Design**: Cache, database, filesystems use interfaces for flexibility
3. **Middleware Chain**: Chi middleware for cross-cutting concerns
4. **Table-driven Tests**: Extensive use of table tests for comprehensive coverage
5. **Configuration via Environment**: `.env` file for configuration

### Database Migrations
The framework includes a migration system (`migrations.go`) that:
- Tracks migration history in database
- Supports up/down migrations
- Can be run via CLI: `tjo migrate up/down`

### Testing Philosophy
- Comprehensive test coverage with colorful test runner
- Docker-dependent tests can be skipped with `-short` flag
- Security-focused tests for input validation, XSS, CSRF protection
- Integration tests for database operations

### CLI Tool (`cmd/tjo/`)
The `tjo` command provides:
- `new` - Create new Tjo project
- `migrate` - Run database migrations
- `make auth/mail/model/handler` - Generate boilerplate code
- `session` - Create session tables in database

### OpenTelemetry (`otel/`)
Distributed tracing and observability with OpenTelemetry:

**Environment Variables:**
```bash
OTEL_ENABLED=true              # Enable OpenTelemetry
OTEL_SERVICE_NAME=my-app       # Service name in traces
OTEL_SERVICE_VERSION=1.0.0     # Service version
OTEL_ENVIRONMENT=production    # Deployment environment
OTEL_EXPORTER=otlp             # Exporter: otlp, zipkin, none
OTEL_ENDPOINT=localhost:4317   # Collector endpoint
OTEL_INSECURE=true             # Disable TLS (dev only)
OTEL_SAMPLER=always            # Sampling: always, never, ratio, parent
OTEL_SAMPLE_RATIO=0.1          # Ratio when using ratio sampler
```

**Features:**
- Automatic HTTP request tracing via middleware
- Database operation tracing with `otel.WrapDB()`
- Log correlation with `otel.LoggerWithTrace(ctx, logger)`
- Custom spans with `otel.Start(ctx, "operation")`
- Support for OTLP, Zipkin exporters

**Local Development with Jaeger:**
```bash
docker run -d --name jaeger \
  -p 16686:16686 \
  -p 4317:4317 \
  jaegertracing/all-in-one:latest
# View traces at http://localhost:16686
```