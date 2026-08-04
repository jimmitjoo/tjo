# Changelog

All notable changes to this project are documented here.

This project follows [Semantic Versioning](https://semver.org/). While the
major version is 0, breaking changes may land in a minor release.

## [0.10.0] - 2026-08-05

Authentication, as a package you can import without importing the framework.

The reason to build this is architectural rather than featural. Val Town's
account of leaving Clerk gives three reasons and all of them are structural: a
5 req/s account-wide read limit, a vendor that wants to own the users table so
apps that join on users end up with two of them, and session refresh that calls
the vendor's servers — so **the vendor's outage takes the whole site down, not
just login**. Auth that runs in your process against your database has none of
those failure modes, and that is something a Go framework can say without
qualification.

### Added — `auth`

Storage stays yours throughout. The package provides verbs and declares
interfaces; it never owns a table. That is what makes it adoptable without
adopting Tjo.

- **Passwords** — bcrypt with cost upgrade on login, a rune-counted length
  policy following NIST SP 800-63B rather than composition rules, and a
  timing-equalised failure path.
- **Login** — `Authenticate` does the lookup and the comparison
  unconditionally, in that order.
- **Password reset** — single-use, database-persisted tokens bound to a user,
  separated by purpose, with `SQLResetStore` for PostgreSQL, MySQL and SQLite.
- **Organizations** — memberships, roles, permissions, invitations, and tenant
  scoping.
- **Passkey records** — stored in Valsorda's opaque interoperable format.
- **API tokens** — landed in v0.9.0, unchanged.

### The passkey format is a bet with a date on it

Filippo Valsorda opened [golang/go#80663, `proposal: crypto/passkey`](https://github.com/golang/go/issues/80663)
on 2026-07-31, targeting **Go 1.28**, alongside a proposal for an opaque record
format shaped like a PHC password hash:

```
$webauthn$v=1$transports=hybrid+internal$<base64 authdata>
```

Records are stored that way here. The application keeps a string and never
parses it, so the library behind it is replaceable without migrating
credentials — including replaceable by `crypto/passkey` if the proposal lands.

Ceremonies are not implemented yet. The format is the part with a deadline; the
flows can follow without the stored rows moving, which is the whole point.

Passkeys are an option, never the only route. The 2026 discourse is genuinely
negative — one thread ran to 781 comments in July — and account recovery is the
unsolved part. A framework that made passkeys the only way in would inherit that
problem on its users' behalf.

### Fixed

- **The scaffolded forgot-password endpoint was a user-enumeration oracle.**
  `PostForgot` looked the user up and then used `user.Email` regardless, and
  `ByEmail` returns `ErrUserNotFound` — so an unknown address produced a 500 and
  a known one a 303. Anyone could ask the form whether a given person had an
  account. Both now return the same response; verified against a running
  application.
- The scaffolded reset flow is rewired onto `auth.ResetPassword`. Generated
  handlers no longer decide whether a token is valid, how a password is
  compared, or when tokens are invalidated.

### On tenancy

Multi-tenancy is implemented as organizations inside auth rather than as a
tenancy layer, because the three questions it actually asks — which organization
is this request acting as, is this person a member, what may they do — are
session questions. `stancl/tenancy` and `django-tenants` are complicated because
they solve database-per-tenant routing; most applications need a WHERE clause.

`ScopeTo` returns an error rather than an unscoped builder when no organization
is active, because forgetting a tenant filter does not produce an error — it
produces a query that returns every tenant's rows, looks correct in development
where there is one tenant, and leaks in production.

Deliberately absent: database-per-tenant, schema-per-tenant, domain routing.

### A note on the tests, because it changed one

The first version of the reset store's concurrency test launched 16 goroutines
with no coordination, and **passed against a deliberately broken
SELECT-then-UPDATE implementation on both engines** — goroutine startup is
staggered enough that the window is never contended.

Rewritten with a start barrier and thirty rounds, it fails on round 1 against
the naive version with two successful redemptions: two people resetting the same
account's password. It only discriminates on PostgreSQL, and the comment says
so — on SQLite the pool is one write connection and the writer is serialised, so
the naive version is genuinely safe there.

That is the fifth guard this project has found that could not observe the bug it
was written for.

### Not in this release

Passkey ceremonies, OAuth social login, magic links, email OTP. Login, 2FA and
remember-me are still generated rather than delegated (#72).

## [0.9.0] - 2026-08-04

Where v0.8.0 was about being correct, this one is about being shippable: a
deploy story, signed releases, and the batteries that make a database the only
service you have to run.

Four dependencies were removed rather than upgraded.

### Breaking changes

**1. SQLite is now pure Go, and the driver name changed**

`mattn/go-sqlite3` is replaced by `modernc.org/sqlite`. Configuration accepts
both `sqlite` and `sqlite3` as before, and `OpenDB` normalises them — but code
calling `sql.Open("sqlite3", …)` directly must now use `"sqlite"`.

Migrations moved with it, so the golang-migrate URL scheme is `sqlite://`
rather than `sqlite3://`.

This costs measured performance. On darwin/arm64 with one write connection
under WAL:

| | mattn | modernc | |
|---|---|---|---|
| insert | 6699 ns/op | 8273 ns/op | 1.2× slower |
| select | 32388 ns/op | 100475 ns/op | 3.1× slower |

It is bought deliberately. cgo makes `GOOS=linux CGO_ENABLED=0 go build`
impossible, and that cross-compile is what `tjo deploy` does and what collapses
the release matrix from five native runners to one. For a request doing a
handful of point lookups the difference is tens of microseconds against template
rendering and network; past that point the answer is PostgreSQL.

**2. `nosurf` is gone; CSRF tokens live in the session**

`NoSurf` still exists as an alias, so mounting it keeps working. But there is no
longer a `csrf_token` cookie — the token is in the session, which is what
removes the need for masking. `{{.CSRFToken}}` and the `csrf_token` form field
are unchanged, so templates need no edits.

If you renew the session yourself, rotate the token too:

```go
tjo.RotateCSRFToken(app.HTTP.Session, r)
```

Scaffolded auth does this at all three renewal points. Without it the token
minted for the anonymous session survives into the authenticated one.

**3. `WriteTimeout` is no longer set**

It is an absolute deadline from the start of the request rather than an idle
timeout, so it cut every stream at ten minutes regardless of activity. Streams
bound individual writes with `Stream.SetWriteDeadline`; `ReadTimeout` and
`IdleTimeout` still cover the slow-client cases it was reaching for.

**4. Duplicate JSON object names are rejected**

`ReadJson` and `api.JSONRequest` now return an error for
`{"role":"user","role":"admin"}` rather than silently taking the last one. See
below.

### Added

- **`tjo deploy`** — build a static binary, copy it over SSH, restart a systemd
  unit. No Docker, no registry, no orchestrator, nothing on the host but the app
  and optionally Caddy. Keeps five releases, health-checks after restarting and
  rolls back if that fails.
- **Socket activation and readiness notification.** The app adopts a listening
  socket from systemd rather than opening its own, so restarts drop no
  connections, and reports readiness so `systemctl restart` blocks until the new
  binary is actually serving.
- **`sse`** — Server-Sent Events. The transport every major LLM API streams
  over, that htmx 4 moves into core, and that Datastar uses natively. No client
  library is picked; the wire format is what all three consume.
- **HTTP/2, including cleartext**, which SSE requires to be usable at all.
- **`jobs.SQLQueue`** — a job queue in the database you already have, with
  `PushTx` to enqueue inside the caller's transaction. Postgres and MySQL use
  `FOR UPDATE SKIP LOCKED`; SQLite claims with a single serialised `UPDATE`.
- **`filesystems.ContextFS`** — cancellation, deadlines and trace propagation
  for S3 and MinIO. `DeleteContext` returns an error and honours its arguments,
  which `Delete` never did.
- **Signed releases** — SLSA build provenance, a keyless cosign signature and a
  CycloneDX 1.6 SBOM per artifact. Verify with
  `gh attestation verify <file> --repo jimmitjoo/tjo`.
- **Reproducible builds.** Two builds of the same tag produce identical bytes.
- **`auth`** — API token generation and verification as real tested code rather
  than only as template output.
- **`AGENTS.md`**, the cross-tool convention now stewarded by the Linux
  Foundation. `CLAUDE.md` points at it.
- **MCP introspection** — `tjo_routes_list`, `tjo_schema_describe` and
  `tjo_config_describe` answer questions about *your* application. Routes are
  parsed statically, so they work while the project does not compile.
- **`evals/`** — measures whether a coding agent produces a working Tjo app,
  with the compiler as the grader.
- **A CRA position** in `SECURITY.md`, and an OpenSSF Scorecard badge.

### Removed

`mattn/go-sqlite3`, `justinas/nosurf`, `asaskevich/govalidator` and
`twilio/twilio-go`. The last two were replaced by four small functions and by
the HTTP call that was already in the file.

### Fixed

- **The release workflow's tag trigger had never fired.** Every release in this
  repository's history was built by manual dispatch, because GitHub does not
  create workflow runs when more than three tags are pushed at once and a
  release pushes five. The first v0.7.0 and v0.8.0 builds were both silently
  skipped. The trigger is deleted and `make release-push` dispatches explicitly;
  a trigger that cannot fire is worse than none, because it reads as coverage.
- **Twilio shipped through its SDK while its tested HTTP path ran only in
  tests.** The same split hid `vonage-go-sdk`'s dependency on `golang-jwt/jwt`
  v3 from every test in that package until `govulncheck` found it. API errors
  now surface Twilio's code and message instead of a raw body dump.
- **Two tests reached live third-party APIs** and asserted only that something
  failed — equally true with no network. They were skipped under `-short`, which
  is what CI runs, so the branch users execute had no coverage at all.
- The `#21` badger aliasing guard, which stopped guarding anything under v4 and
  was rewritten in v0.8.0, is unaffected here — but the equivalent problem
  appeared again in `filesystems`: `Delete` ignoring its arguments was carried
  forward deliberately and is corrected in `DeleteContext`.

### On duplicate JSON keys

The issue behind this was written on the premise that Go 1.27 would make
`encoding/json/v2` the default and that its strictness would arrive with it.
Measuring first showed that is wrong: v1's semantics are deliberately preserved
behind the v1 API.

```
encoding/json     {"role":"user","role":"admin"}  ->  err=<nil>  role="admin"
encoding/json/v2  {"role":"user","role":"admin"}  ->  duplicate object member name
```

So the permissive behaviour is not arriving and not going away. It is a
smuggling primitive whenever anything else in the request path — a proxy, a WAF,
an audit log — parses the same body and resolves the conflict differently, so
the framework now rejects it. CI gains a `jsonv2` leg as a canary rather than a
migration check.

### Not in this release

**Extracting authentication as a standalone module (#52).** The token primitives
landed; users, sessions, 2FA and password reset did not. That is ~1,100 lines
still in templates plus store interfaces that do not exist yet, and the issue's
own definition of done requires a security review before tagging — on the
grounds that an auth library with a session-fixation bug is worse than no auth
library. It gets its own cycle.

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
