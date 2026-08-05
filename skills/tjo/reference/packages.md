# Packages

Root module, importable individually.

| Package | What it is for |
|---|---|
| `tjo` | framework wiring: `New`, routing, middleware, `RotateCSRFToken` |
| `auth` | passwords, login, single-use tokens, TOTP, recovery codes, remember-me, passkeys, organizations and roles. Verbs plus store interfaces; owns no table |
| `admin` | model-driven CRUD panel over your structs. Reflection, server-rendered, no build step |
| `ops` | self-hosted dashboard: errors, slow requests, slow queries, queues, cron, health |
| `jobs` | database-backed queue, workers, and durable checkpointed workflows |
| `database` | query builder, migrations, seeding, health checks |
| `session` | session stores: Redis, Badger, SQL |
| `cache` | Redis and Badger caches |
| `sse` | server-sent events, and topic broadcast to subscribed streams |
| `security` | throttling, security headers, validation, CSRF configuration |
| `render` | Jet and html/template rendering |
| `filesystems` | local, S3 and MinIO |
| `urlsigner` | signed URLs |
| `api` | REST helpers, versioning, rate limiting |
| `core` | helpers usable without the framework |

Separate modules, each with its own `go.mod` and its own dependency weight:

| Module | Why it is separate |
|---|---|
| `email` | mailer SDKs |
| `otel` | the OpenTelemetry SDK |
| `sms` | provider SDKs |
| `websocket` | its own transport dependencies |

A root-level `go test ./...` does not reach any of the four.

## Choosing between them

- Storing a password, checking one, or issuing anything a browser presents
  later: `auth`. Never hand-rolled.
- A screen for internal staff over existing tables: `admin`.
- "Is it broken and why": `ops`.
- Work that must survive a crash or a deploy: `jobs`, with `Step` if the work
  has stages that must not repeat.
- Pushing a change to a connected browser: `sse`.
