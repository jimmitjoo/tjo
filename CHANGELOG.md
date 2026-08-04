# Changelog

All notable changes to this project are documented here.

This project follows [Semantic Versioning](https://semver.org/). While the
major version is 0, breaking changes may land in a minor release.

## [0.7.0] - 2026-08-04

A correctness and security release. Every fix below has a regression test that
was verified to fail without it.

Three security issues are addressed. If you are running 0.6.1 or earlier,
upgrading is strongly recommended — see **Security** below.

Continuous integration now runs `go build`, `go vet` and `go test -race` across
all five modules in the workspace. Previously nothing ran the tests for `email`,
`otel`, `sms` or `websocket` at all: they are separate modules, so
`go test ./...` from the repository root never reached them. Several of the bugs
below lived in exactly that blind spot.

### Security

- **Renderer had no output escaping** (GHSA-2w6x-c7q3-qcgr, critical).
  `render.GoPage` used `text/template` instead of `html/template`, so every
  interpolated value in a `.page.tmpl` was written to the response verbatim.
  Any application configured with `RENDERER=go` had no XSS protection at all.
  The Jet renderer was unaffected.

- **Password reset performed no authorisation check** (GHSA-44g2-5v2v-xh66,
  critical). The scaffolded `PostResetPassword` verified nothing: it decrypted
  an email address out of a form field and reset that user's password. The
  ciphertext was produced by an unauthenticated AES-CFB helper and was therefore
  malleable, allowing an attacker to bit-flip their own reset token into another
  address. The flow now re-verifies the signed link and reads the address out of
  it. Note that the generated code did not compile before this release, which
  limited real-world exposure.

- **Rate limiter was trivially bypassed** (GHSA-hm83-wmj9-52fm, high).
  `IPThrottler.getRealIP` trusted `X-Forwarded-For` without checking that the
  peer was a proxy, so rotating a forged header minted a fresh bucket per
  request. The same path let an attacker drive suspicious-behaviour detection to
  blacklist arbitrary third-party addresses. Proxy headers are now consulted
  only when the peer is a configured trusted proxy.

Additional hardening:

- API tokens are stored and looked up as SHA-256 hashes rather than plaintext,
  and the plaintext is no longer persisted or serialisable.
- Activation links now expire.
- `Encryption` uses AES-GCM, so tampering fails instead of yielding altered
  plaintext.
- IPv6 clients no longer bypass `IPBlacklistMiddleware`.
- `CORSMiddleware` sets `Vary: Origin`.

### Breaking changes

**1. Session initialisation returns an error**

`InitSession` and `InitSecureSession` previously fell through to an in-memory
store for any session type they did not recognise, silently.

```go
// before
manager := sess.InitSession()

// after
manager, err := sess.InitSession()
if err != nil {
    return err
}
```

`SESSION_TYPE=database` now resolves to a real store via the new `DBType`
field. `SESSION_TYPE=badger` is rejected — no badger session store exists.

**2. `Module.Initialize` takes `any`**

The interface asked for `Initialize(*Tjo)` while every shipped module
implemented `Initialize(interface{})`, so no module satisfied it and
`app.New(email.NewModule())` did not compile. Modules are separate Go modules
and can never name `*Tjo`, so the parameter is now `any`.

```go
func (m *Module) Initialize(app any) error
```

If you wrote a module against the old signature, widen the parameter. To reach
the framework, declare the narrow interface you need and assert against it.

**3. `Encryption` changed format**

AES-CFB → AES-GCM. Values encrypted by 0.6.1 or earlier **cannot be decrypted**
by 0.7.0. If you have persisted ciphertexts, decrypt them with the old version
before upgrading. The only in-repo consumer was the password reset flow, which
no longer uses this helper.

**4. `tokens` table lost its plaintext column**

Bearer tokens are now looked up by `token_hash`. Existing applications need a
migration:

```sql
-- backfill hashes for tokens you want to keep, then:
ALTER TABLE tokens DROP COLUMN token;
```

There is no way to derive the hash for tokens you only have in plaintext form
in the database — but that is the point. Simplest path is to expire existing
tokens and have clients request new ones.

**5. Enqueued jobs no longer update in place**

The queue now stores a copy, because handing the same `*Job` to both the caller
and a worker meant `job.Status` raced with `Job.MarkRunning`.

```go
job := jobs.NewJob("email", "default", payload)
manager.Enqueue(job)
_ = job.Status // no longer reflects progress; it stays as you submitted it
```

Read status back through the manager instead.

**6. Malformed environment values now fail startup**

`envInt`, `envBool` and `envFloat` used to discard the parse error and return
the default, so `PORT=80O0` silently bound 4000. Invalid values are now
reported by `Validate`. Defaults still apply to *unset* variables.

**7. Email module reads `MAILER_*`**

It read `MAIL_API`, `MAIL_API_KEY` and `MAIL_API_URL`, which nothing else used.
It now reads `MAILER_API`, `MAILER_KEY` and `MAILER_URL` — the names the
scaffolded `.env` ships and the framework's own config reads.

**8. A registered module owns its subsystem**

`New()` built an SMS provider and a mailer unconditionally, so registering the
email module gave you two mail pipelines. If you register `email.NewModule()`,
`app.Background.Mail` is no longer populated by the core; use the module's.

**9. `config.CORSConfig` removed**

`Config.CORS` was dead: nothing read it, so setting `CORS_ALLOWED_ORIGINS` and
expecting `config` to act on it configured nothing. CORS is owned by the
`security` package, which reads the same variable through
`security.LoadFromEnv()` and applies it in `CORSMiddleware`. The duplicate
struct is gone; the environment variable is unchanged.

**10. PostgreSQL requires `WithDialect`**

Not a signature change, but required to make PostgreSQL work at all:

```go
qb := database.NewQueryBuilder(db).WithDialect(database.DialectDollar)
model := database.NewModel("users").WithDialect(database.DialectFor(cfg.Database.Type))
```

MySQL and SQLite are unaffected; `DialectQuestion` remains the default.

### Fixed

- Query builder emitted `?` placeholders unconditionally, so every query failed
  on PostgreSQL despite it being an advertised feature. (#1)
- WebSocket hub deadlocked itself whenever a client that had joined a room
  disconnected — `unregisterClient` held the hub lock and called a method that
  took it again. The hub wedged permanently. (#7)
- WebSocket hub shutdown closed channels whose producers are application code,
  so `BroadcastToAll` and `Client.Send` panicked during graceful shutdown. (#8)
- `New()` set up structured logging before the database connected, so health
  checks were never registered: `/health` reported healthy unconditionally and a
  liveness probe could never fail. Log lines also carried an empty service
  name. (#9)
- `SESSION_TYPE=database` silently produced in-memory sessions that vanished on
  restart and broke behind a second replica. `COOKIE_LIFETIME` was parsed and
  then discarded, so every app got 30-minute sessions. (#10)
- The cron scheduler was never started, so badger never reclaimed value-log
  space and every user cron silently no-opped while returning an entry ID. (#11)
- Closing the mail `Jobs` channel — which is how `Module.Shutdown` stops the
  listener — spun it at full tilt instead of stopping it, at over three million
  iterations per second. (#12)
- Worker pool reused IDs, so scaling down and up orphaned running workers that
  no longer had a handle to stop them. `StopAll` did not wait, so workers
  outlived shutdown and the database closed underneath them. A `Stop`/`Start`
  cycle produced a manager that reported running but scheduled nothing. (#13)
- Scaffolded `Auth` and `AuthToken` middleware never called `next.ServeHTTP`, so
  every route behind them returned an empty response. `CheckRemember` panicked
  on any malformed cookie — an unauthenticated denial of service on every
  route. (#14)
- Both renderers panicked on unexpected data instead of returning an error, and
  `GoPage` never populated `.CSRFToken`. (#15)
- Dead `CORS_ALLOWED_ORIGINS` config field that nothing read; CORS is owned by
  the `security` package. (#16)
- `go vet` failures: a mutex copied by value in the jobs package. (#3)
- Data races in the jobs package and in `logging`, where `writeEntry` took a
  read lock while writing to a shared writer, interleaving log lines. (#4, #24)
- Job dispatch latency: the queue slept a flat 100 ms between scans instead of
  waking on push. Event listeners were dispatched as one goroutine each, so a
  single listener ran concurrently with itself. (#4)
- `EmptyByMatch` aliased badger's iterator key buffer and could delete the wrong
  keys. (#21)
- `IPThrottler` leaked a goroutine and a ticker per instance, spawned an
  uncancellable goroutine per blacklisting, and grew its IP map without
  bound. (#20)
- Input validation computed sanitised values and discarded them. (#22)

### Added

- CI covering all five modules (#2).
- `database.Dialect`, `DialectFor`, and `WithDialect` on both `QueryBuilder` and
  `Model`.
- `security.CleanedInput` for retrieving sanitised request values.
- `IPThrottler.Stop`.
- Tests that parse the scaffolding templates and check that wrapping middleware
  calls `next` — templates are embedded data and are not otherwise compiled by
  anything.
- `config` package tests, which did not exist.

### Removed

- `config.CORSConfig` — dead duplication of settings the `security` package
  owns. See breaking change 9.
- A 16 MB compiled binary that had been committed to the repository (#5).

[0.7.0]: https://github.com/jimmitjoo/tjo/compare/v0.6.1...v0.7.0
