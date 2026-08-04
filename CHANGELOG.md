# Changelog

All notable changes to this project are documented here.

This project follows [Semantic Versioning](https://semver.org/). While the
major version is 0, breaking changes may land in a minor release.

## [0.8.0] - 2026-08-04

A dependency and security release. It closes 71 known vulnerabilities that had a
call path from this repository's own code, and one that nothing had reported yet.

If you are running 0.7.0 or earlier, upgrade. Three of the findings had **no
patched version available upstream** — they required replacing the dependency,
not bumping it — and one was a live bypass of a fix 0.7.0 had already published.

### The defect behind the release

`govulncheck` did not run in CI. It still does not exist in most Go projects'
pipelines, so this is not unusual, but it is the whole story here: v0.7.0
shipped with 71 reachable vulnerabilities and a green build. The findings were
the symptom. The missing gate was the defect.

It runs now, across all five modules, on every change and weekly on a schedule so
advisories that land against unchanged code are still caught. `make
release-check` — already the last step before tagging — refuses to pass if
anything is reachable. Run it yourself with `make vuln`.

### Security

- **The rate limiter and the IP blacklist were bypassable by anyone**
  (no advisory issued yet; see below). Both routers mounted chi's
  `middleware.RealIP`, which overwrites `r.RemoteAddr` from `X-Forwarded-For`
  with no check that the peer is entitled to set it.

  This defeated GHSA-hm83-wmj9-52fm, which 0.7.0 published as fixed.
  `IPThrottler.getRealIP` consults proxy headers only for trusted peers — but
  the "peer" it inspected was already the forged header, so it concluded the
  address was untrusted and returned it as the client IP. `api.IPKeyFunc` calls
  `GetClientIP(r, nil)`, whose own comment reads *"If no trusted proxies
  defined, only use RemoteAddr for security"*; that stopped being true two lines
  above it.

  Measured at a limit of 2 requests per minute: **10 of 10 requests from a
  single peer were allowed** while varying only `X-Forwarded-For`. Users who
  upgraded to 0.7.0 believing this fixed were still exposed.

- **The CSRF origin check never executed** (GO-2025-3683 / CVE-2025-46721).
  `nosurf` v1.1.1 compared `r.URL.Scheme`, which is empty on a server-side
  request — a server-side `http.Request` carries the request-target, not an
  absolute URL — so the comparison never matched.

  0.7.0 fixed the framework so that `NoSurf` is actually installed on the router
  (GHSA-9m5v-pvgv-cv8j). This is the other half: the middleware that now runs was
  delegating its origin check to a library where the check was a no-op.

- **An unpatchable SQL injection in the PostgreSQL driver** (GO-2026-5004),
  reachable through `QueryBuilder.ChunkByID`. `jackc/pgx/v4`'s advisory records
  `Fixed in: N/A`; the fix exists only in v5. `jackc/pgproto3/v2` carries
  GO-2026-4518 on the same terms.

- **CVSS 8.7 in `golang-jwt/jwt` v3** (GO-2025-3553), with no fix in the v3 line,
  arriving through `vonage-go-sdk` — whose latest release still pins it.

- 15 findings in the `go-git` chain, every one reachable through
  `git.PlainClone` in `tjo new` — the first command a new user runs.

- Three 2026 advisories in chi's `middleware.RealIP`, four in the MCP SDK, and 30
  in the standard library.

### Added

- `http.CrossOriginProtection` in front of the token CSRF layer. The two cover
  different halves: tokens only protect a form somebody remembered to
  instrument, while the origin gate protects every state-changing request
  regardless. Conversely the origin gate deliberately allows requests carrying
  neither `Sec-Fetch-Site` nor `Origin`, since those are same-origin or not from
  a browser — which is what keeps curl and server-to-server clients working, and
  why it does not replace tokens. Trusted origins come from
  `CORS_ALLOWED_ORIGINS`.
- `SECURITY.md`, with a reporting channel, response times one person can
  sustain, a supported-version policy, and two deliberate positions: generated
  code is in scope, and reports without a reproducer are closed.
- `make vuln`, and PostgreSQL integration tests that run when
  `TJO_TEST_POSTGRES_DSN` is set.

### Changed

- **Go 1.25 is now the minimum.** `go.mod` declares `go 1.25.0` with
  `toolchain go1.26.5`. Go 1.24 leaves support the moment 1.27 ships, which is
  imminent, and both `pgx/v5` and `net/http.CrossOriginProtection` require 1.25.
  The scaffold template and the generated Dockerfiles moved with it — the latter
  pinned `golang:1.21-alpine`, which cannot build a `go 1.25` module at all.
- `jackc/pgx` v4 → v5, `dgraph-io/badger` v3 → v4, `aws/aws-sdk-go` v1 →
  `aws-sdk-go-v2`, `go-git` v5.11.0 → v5.19.1, chi → v5.3.1, MCP SDK v1.1.0 →
  v1.7.0, `nosurf` → v1.2.0, plus `x/crypto`, `x/net`, `x/text`, `grpc` and
  `otel`.
- Vonage SMS now posts directly to the API instead of using the SDK. That code
  was already in the file — it was reached only when a client was injected,
  which happened only in tests. The shipped path and the covered path were
  different code. It also surfaces Vonage's `error-text`, which the SDK branch
  discarded.

### Removed

`aws/aws-sdk-go` (v1, EOL 2025-07-31), `dgraph-io/badger/v3` (no release since
December 2022), `vonage/vonage-go-sdk`, `golang-jwt/jwt` v3, `jackc/pgconn`,
`jackc/pgproto3/v2`, `jackc/pgtype`, `jackc/pgio`, `jackc/chunkreader/v2`.

### Fixed tests that were not testing anything

Three guards passed against code they were written to reject:

- `TestBadgerCache_EmptyByMatch` kept passing under badger v4 with the
  `KeyCopy` fix from #21 removed. Three short keys is too few iterations to force
  badger to reuse its key buffer. The replacement interleaves 200 keys with
  varying-length suffixes and fails with entries surviving deletion, because the
  delete list pointed at whatever the buffer held last.
- `TestS3_ErrorHandling` constructed `awserr` values and asserted they had a
  code and a message — a property of the SDK, not of any code in this package.
  It would have passed against an empty implementation. Its replacement pins
  `UsePathStyle`, the one v1-to-v2 difference that fails silently: v1 inferred
  path-style addressing from a custom endpoint and v2 infers nothing, so without
  it every request to MinIO or another S3-compatible server goes to a
  virtual-host URL that resolves nowhere.
- `TestVonage_ProductionPath` and its Twilio counterpart make live API calls and
  assert only that something failed — which is also what happens with no
  network. Both skip under `-short`, which is what CI runs.

Every fix in this release has a regression test that was run against the unfixed
code and observed to fail.

### Verified

- All five modules report zero reachable vulnerabilities.
- pgx v5 against PostgreSQL 16: driver registration and `$1` placeholder
  rewriting.
- s3filesystem against MinIO: `Put`, `List`, `Get` and `Delete` end to end.
- All four starter templates scaffold, build and vet, then still do so after
  `make auth`, `make controller` and `make handler`.

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

- `tjo new` clones the skeleton tag matching the CLI's own version, so a
  released binary produces the same project every time it runs. It followed the
  default branch before, which is how the skeleton and the CLI's templates
  drifted apart. A build from a checkout has no version to match and follows
  the default branch, saying so. Releasing now requires a matching tag in
  jimmitjoo/tjo-bare; `make release` prints the command. (#30)
- Prebuilt CLI binaries for macOS, Linux and Windows on version tags (#32).
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
