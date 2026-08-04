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

- **Session and CSRF middleware were never installed** (GHSA-9m5v-pvgv-cv8j,
  critical). `New()` built the HTTP router before assigning the session
  manager, and `routes()` installs `SessionLoad` and `NoSurf` only when a
  session manager is already present. The condition was therefore never true:
  no application built with this framework had CSRF protection or session
  loading on its router. Measured on a stock application, the router set zero
  cookies before the fix and issues a `csrf_token` after it. The CSRF fixes
  further down this list were corrections to code that was never reached.

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

- **Scaffolded login did not rotate the session ID.** `tjo make auth`
  generated a login handler that wrote `userID` into the existing session
  without renewing the token — on both the plain path and after 2FA
  verification. Logout renewed it; login did not. An attacker able to fix a
  session ID on the victim's browser beforehand therefore still held a valid,
  now-authenticated session. Both paths renew before establishing the session,
  and a test asserts it for each.

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
- `getClientIPWithTrustedProxies` read the **leftmost** `X-Forwarded-For`
  entry, which is the value the client sent and a proxy forwards untouched.
  One client produced 50 distinct identities across 50 requests by varying the
  header. It had no callers, but GHSA-hm83-wmj9-52fm and a code comment both
  named it as the correct implementation to copy; both are corrected. It now
  walks the chain from the right and rejects hops that are not IP addresses.
- `SecurityMonitor.IsIPBlocked` deleted from two maps while holding only a
  read lock. The expiry path takes the write lock and re-checks.
- The input blocklist gained a pattern for a quote followed by a comment
  marker. `admin'--` is the most common SQL probe there is and every existing
  SQL pattern missed it.
- `ValidateSession` no longer skips its age check when `auth_time` is absent.
  See breaking change 10.

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

**10. `AuthSessionHandler` uses the framework's session keys**

It wrote the user under `user_id` while everything else in the framework —
`render.defaultData`, the scaffolded auth middleware, the TOTP handlers, the
remember-me middleware — reads `userID`. The two mechanisms could not see each
other's sessions, so an application using `LoginUser` appeared logged out to
every template and middleware in the same application.

It now writes `userID`. The keys are exported as `session.KeyUserID`,
`KeyAuthTime`, `KeyCreatedAt` and `KeyFingerprint` so they cannot drift apart
again. Any session established by the previous release is not recognised by
this one; users log in again.

**11. `ValidateSession` rejects sessions it cannot age**

`AuthSessionHandler.ValidateSession` skipped its `MaxLifetime` check entirely
when `auth_time` was missing, so a session carrying `user_id` but no
`auth_time` — which is what an application gets if it writes `user_id` itself
instead of calling `LoginUser` — was never aged out regardless of
configuration. An age check that cannot determine the age now fails closed.

If you set session keys yourself, either go through `LoginUser` or set
`auth_time` alongside `user_id`. Note also that this handler stores the user
under `user_id` while the scaffolded auth handlers use `userID`; the two have
never interoperated, which is now documented on the type.

**12. `routes()` refuses to build an unprotected router**

It used to skip `SessionLoad` and `NoSurf` when the session manager was
missing and return a router anyway. That silent path is what made
GHSA-9m5v-pvgv-cv8j possible. It now returns an error, and `New()` propagates
it, so a misconfiguration fails startup instead of quietly producing an
application with no CSRF protection.

Only relevant if you call the unexported `routes()` yourself, which you cannot
from outside the package — listed because it changes what a broken
configuration does: it stops rather than starts.

**13. PostgreSQL requires `WithDialect`**

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

### Testing

Coverage went from 59% to 62% overall, aimed at code where being wrong is
expensive rather than at the number: `security` 59% → 78%, `session` 40% → 86%.
Four defects surfaced from writing those tests — the forwarded-header handling,
the monitor's read-lock delete, the missing SQL comment pattern and the session
age fail-open — all listed above.

`S3.Get` also ignored its `destination` argument entirely, dropping every
download into the process's working directory while the minio implementation of
the same interface honoured it. The existing test passed a destination and
never checked where files landed.

### Removed

- `config.CORSConfig` — dead duplication of settings the `security` package
  owns. See breaking change 9.
- A 16 MB compiled binary that had been committed to the repository (#5).

[0.7.0]: https://github.com/jimmitjoo/tjo/compare/v0.6.1...v0.7.0
