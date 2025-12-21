# Configuration Reference

Tjo uses environment variables for configuration. All settings are loaded at startup and validated before the application starts.

## Quick Start

Create a `.env` file in your project root:

```env
APP_NAME=myapp
DEBUG=true
PORT=4000
KEY=your-32-character-encryption-key
```

---

## Application Settings

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `APP_NAME` | Application name (used in logs, emails) | - | No |
| `APP_URL` | Full application URL | - | No |
| `DEBUG` | Enable debug mode | `false` | No |
| `KEY` | 32-character encryption key | - | Yes (for sessions) |
| `RENDERER` | Template engine: `jet`, `go` | `jet` | No |
| `CACHE` | Cache driver: `redis`, `badger`, or empty | - | No |

### Generating an Encryption Key

```bash
tjo make key
```

---

## Server Settings

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `PORT` | HTTP server port | `4000` | No |
| `SERVER_NAME` | Server hostname | - | No |
| `SECURE` | Enable HTTPS | `true` | No |

### Example

```env
PORT=8080
SERVER_NAME=api.example.com
SECURE=true
```

---

## Database Settings

Tjo supports PostgreSQL, MySQL/MariaDB, and SQLite.

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `DATABASE_TYPE` | Database type: `postgres`, `mysql`, `mariadb`, `sqlite` | - | If using DB |
| `DATABASE_HOST` | Database server hostname | - | If using DB |
| `DATABASE_PORT` | Database server port | `5432` | No |
| `DATABASE_USER` | Database username | - | If using DB |
| `DATABASE_PASS` | Database password | - | If using DB |
| `DATABASE_NAME` | Database name | - | If using DB |
| `DATABASE_SSL_MODE` | SSL mode for PostgreSQL | `disable` | No |
| `DATABASE_TABLE_PREFIX` | Table name prefix | - | No |

### PostgreSQL Example

```env
DATABASE_TYPE=postgres
DATABASE_HOST=localhost
DATABASE_PORT=5432
DATABASE_USER=myapp
DATABASE_PASS=secret
DATABASE_NAME=myapp_db
DATABASE_SSL_MODE=disable
```

### MySQL/MariaDB Example

```env
DATABASE_TYPE=mysql
DATABASE_HOST=localhost
DATABASE_PORT=3306
DATABASE_USER=myapp
DATABASE_PASS=secret
DATABASE_NAME=myapp_db
```

### SQLite Example

```env
DATABASE_TYPE=sqlite
DATABASE_NAME=app.db
```

SQLite databases are stored in the `data/` directory by default.

---

## Redis Settings

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `REDIS_HOST` | Redis server hostname | `localhost` | If using Redis |
| `REDIS_PORT` | Redis server port | `6379` | No |
| `REDIS_PASSWORD` | Redis password | - | No |
| `REDIS_PREFIX` | Key prefix for namespacing | - | No |

### Example

```env
REDIS_HOST=localhost
REDIS_PORT=6379
REDIS_PASSWORD=secret
REDIS_PREFIX=myapp
```

---

## Session Settings

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `SESSION_TYPE` | Session storage: `cookie`, `redis`, `database`, `badger` | `cookie` | No |

### Session Types

- **cookie**: Encrypted client-side sessions (default)
- **redis**: Server-side sessions in Redis
- **database**: Server-side sessions in database
- **badger**: Server-side sessions in Badger (embedded)

---

## Cookie Settings

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `COOKIE_NAME` | Session cookie name | `tjo_session` | No |
| `COOKIE_LIFETIME` | Cookie lifetime in minutes | `1440` (24 hours) | No |
| `COOKIE_PERSIST` | Persist cookie across browser sessions | `true` | No |
| `COOKIE_SECURE` | Require HTTPS for cookies | `true` | No |
| `COOKIE_DOMAIN` | Cookie domain scope | - | No |

### Example

```env
COOKIE_NAME=myapp_session
COOKIE_LIFETIME=10080   # 7 days
COOKIE_PERSIST=true
COOKIE_SECURE=true
COOKIE_DOMAIN=.example.com
```

---

## Email Settings (SMTP)

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `SMTP_HOST` | SMTP server hostname | - | If sending email |
| `SMTP_PORT` | SMTP server port | `587` | No |
| `SMTP_USERNAME` | SMTP username | - | If required by server |
| `SMTP_PASSWORD` | SMTP password | - | If required by server |
| `SMTP_ENCRYPTION` | Encryption: `tls`, `ssl`, `none` | `tls` | No |
| `MAIL_FROM_ADDRESS` | Default from email | - | If sending email |
| `MAIL_FROM_NAME` | Default from name | - | No |
| `MAIL_DOMAIN` | Email domain | - | No |

### SMTP Example

```env
SMTP_HOST=smtp.mailgun.org
SMTP_PORT=587
SMTP_USERNAME=postmaster@mg.example.com
SMTP_PASSWORD=secret
SMTP_ENCRYPTION=tls
MAIL_FROM_ADDRESS=noreply@example.com
MAIL_FROM_NAME=MyApp
```

### API-Based Email

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `MAILER_API` | API provider name | - | If using API |
| `MAILER_KEY` | API key | - | If using API |
| `MAILER_URL` | API endpoint URL | - | If using API |

---

## File Storage Settings

### Amazon S3

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `S3_KEY` | AWS access key ID | - | If using S3 |
| `S3_SECRET` | AWS secret access key | - | If using S3 |
| `S3_REGION` | AWS region | - | If using S3 |
| `S3_BUCKET` | S3 bucket name | - | If using S3 |
| `S3_ENDPOINT` | Custom endpoint (for S3-compatible) | - | No |

### S3 Example

```env
S3_KEY=AKIAIOSFODNN7EXAMPLE
S3_SECRET=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
S3_REGION=us-east-1
S3_BUCKET=myapp-uploads
```

### MinIO

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `MINIO_ENDPOINT` | MinIO server endpoint | - | If using MinIO |
| `MINIO_ACCESS_KEY` | MinIO access key | - | If using MinIO |
| `MINIO_SECRET` | MinIO secret key | - | If using MinIO |
| `MINIO_REGION` | MinIO region | - | No |
| `MINIO_BUCKET` | MinIO bucket name | - | If using MinIO |
| `MINIO_USE_SSL` | Use SSL for MinIO | `false` | No |

### MinIO Example

```env
MINIO_ENDPOINT=127.0.0.1:9000
MINIO_ACCESS_KEY=minioadmin
MINIO_SECRET=minioadmin
MINIO_USE_SSL=false
MINIO_REGION=us-east-1
MINIO_BUCKET=uploads
```

---

## Logging Settings

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `LOG_LEVEL` | Log level: `trace`, `debug`, `info`, `warn`, `error`, `fatal` | `info` | No |
| `LOG_FORMAT` | Log format: `json`, `text` | `json` | No |

### Example

```env
# Development
LOG_LEVEL=debug
LOG_FORMAT=text

# Production
LOG_LEVEL=info
LOG_FORMAT=json
```

---

## Background Jobs Settings

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `JOB_WORKERS` | Number of worker goroutines | `5` | No |
| `JOB_ENABLE_PERSISTENCE` | Persist jobs to database | `false` | No |

### Example

```env
JOB_WORKERS=10
JOB_ENABLE_PERSISTENCE=true
```

---

## CORS Settings

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `CORS_ALLOWED_ORIGINS` | Comma-separated list of allowed origins | - (block all) | No |

### Example

```env
# Allow specific origins
CORS_ALLOWED_ORIGINS=https://app.example.com,https://admin.example.com

# Development: allow localhost
CORS_ALLOWED_ORIGINS=http://localhost:3000,http://localhost:5173
```

**Security Note:** Leaving `CORS_ALLOWED_ORIGINS` empty blocks all cross-origin requests. This is the safest default.

---

## OpenTelemetry Settings

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `OTEL_ENABLED` | Enable OpenTelemetry tracing | `false` | No |
| `OTEL_SERVICE_NAME` | Service name in traces | - | If OTEL enabled |
| `OTEL_SERVICE_VERSION` | Service version | - | No |
| `OTEL_ENVIRONMENT` | Deployment environment | - | No |
| `OTEL_EXPORTER` | Exporter: `otlp`, `zipkin`, `none` | `otlp` | No |
| `OTEL_ENDPOINT` | Collector endpoint | - | If OTEL enabled |
| `OTEL_INSECURE` | Disable TLS for exporter | `false` | No |
| `OTEL_SAMPLER` | Sampling strategy: `always`, `never`, `ratio`, `parent` | `always` | No |
| `OTEL_SAMPLE_RATIO` | Sample ratio (0.0-1.0) | `1.0` | No |
| `OTEL_ENABLE_METRICS` | Enable OTel metrics | `false` | No |

### Development Example (Jaeger)

```env
OTEL_ENABLED=true
OTEL_SERVICE_NAME=myapp
OTEL_SERVICE_VERSION=1.0.0
OTEL_ENDPOINT=localhost:4317
OTEL_INSECURE=true
OTEL_SAMPLER=always
```

### Production Example

```env
OTEL_ENABLED=true
OTEL_SERVICE_NAME=myapp
OTEL_SERVICE_VERSION=1.2.3
OTEL_ENVIRONMENT=production
OTEL_ENDPOINT=otel-collector.internal:4317
OTEL_INSECURE=false
OTEL_SAMPLER=ratio
OTEL_SAMPLE_RATIO=0.1
```

See [OpenTelemetry Guide](opentelemetry.md) for detailed setup instructions.

---

## SMS Settings

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `SMS_PROVIDER` | SMS provider name | - | If sending SMS |

### Vonage (Nexmo)

| Variable | Description |
|----------|-------------|
| `VONAGE_API_KEY` | Vonage API key |
| `VONAGE_API_SECRET` | Vonage API secret |
| `VONAGE_FROM_NUMBER` | Sender phone number |

### Twilio

| Variable | Description |
|----------|-------------|
| `TWILIO_ACCOUNT_SID` | Twilio account SID |
| `TWILIO_API_KEY` | Twilio API key |
| `TWILIO_API_SECRET` | Twilio API secret |
| `TWILIO_FROM_NUMBER` | Sender phone number |

---

## Complete Example

Here's a complete `.env` file for a production deployment:

```env
# Application
APP_NAME=MyApp
APP_URL=https://myapp.com
DEBUG=false
KEY=a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6
RENDERER=jet

# Server
PORT=4000
SERVER_NAME=myapp.com
SECURE=true

# Database (PostgreSQL)
DATABASE_TYPE=postgres
DATABASE_HOST=db.internal
DATABASE_PORT=5432
DATABASE_USER=myapp
DATABASE_PASS=supersecret
DATABASE_NAME=myapp_production
DATABASE_SSL_MODE=require

# Redis (for cache and sessions)
REDIS_HOST=redis.internal
REDIS_PORT=6379
REDIS_PASSWORD=redispassword
REDIS_PREFIX=myapp
CACHE=redis
SESSION_TYPE=redis

# Cookies
COOKIE_NAME=myapp_session
COOKIE_LIFETIME=10080
COOKIE_PERSIST=true
COOKIE_SECURE=true
COOKIE_DOMAIN=.myapp.com

# Email
SMTP_HOST=smtp.sendgrid.net
SMTP_PORT=587
SMTP_USERNAME=apikey
SMTP_PASSWORD=SG.xxxxx
SMTP_ENCRYPTION=tls
MAIL_FROM_ADDRESS=noreply@myapp.com
MAIL_FROM_NAME=MyApp

# File Storage (S3)
S3_KEY=AKIAIOSFODNN7EXAMPLE
S3_SECRET=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
S3_REGION=us-east-1
S3_BUCKET=myapp-uploads

# Logging
LOG_LEVEL=info
LOG_FORMAT=json

# Background Jobs
JOB_WORKERS=10
JOB_ENABLE_PERSISTENCE=true

# CORS
CORS_ALLOWED_ORIGINS=https://app.myapp.com

# OpenTelemetry
OTEL_ENABLED=true
OTEL_SERVICE_NAME=myapp
OTEL_SERVICE_VERSION=1.0.0
OTEL_ENVIRONMENT=production
OTEL_ENDPOINT=otel-collector.internal:4317
OTEL_SAMPLER=ratio
OTEL_SAMPLE_RATIO=0.1
```

---

## Environment-Specific Files

You can use different `.env` files for different environments:

```
.env           # Default, loaded always
.env.local     # Local overrides (git-ignored)
.env.test      # Test environment
.env.production # Production (use with caution)
```

---

## Validation

Tjo validates configuration at startup. Invalid configuration will prevent the application from starting:

```
configuration errors: invalid DATABASE_TYPE: mongodb; OTEL_SERVICE_NAME is required when OTEL_ENABLED=true
```

### Validation Rules

- `PORT`: Must be 1-65535
- `DATABASE_TYPE`: Must be `postgres`, `postgresql`, `pgx`, `mysql`, `mariadb`, `sqlite`, or `sqlite3`
- `SESSION_TYPE`: Must be `cookie`, `redis`, `database`, or `badger`
- `LOG_LEVEL`: Must be `trace`, `debug`, `info`, `warn`, `error`, or `fatal`
- `LOG_FORMAT`: Must be `json` or `text`
- `JOB_WORKERS`: Must be at least 1
- `OTEL_EXPORTER`: Must be `otlp`, `zipkin`, or `none`
- `OTEL_SAMPLER`: Must be `always`, `never`, `ratio`, or `parent`
- `OTEL_SAMPLE_RATIO`: Must be between 0.0 and 1.0

---

## Programmatic Access

Access configuration in your application:

```go
// Load configuration
cfg, err := config.Load()
if err != nil {
    log.Fatal(err)
}

// Access values
fmt.Println(cfg.App.Name)
fmt.Println(cfg.Server.Port)
fmt.Println(cfg.Database.Type)

// Check if features are enabled
if cfg.Database.IsEnabled() {
    // Connect to database
}

if cfg.OTel.IsEnabled() {
    // Setup tracing
}

if cfg.Storage.IsS3Enabled() {
    // Setup S3 client
}
```

