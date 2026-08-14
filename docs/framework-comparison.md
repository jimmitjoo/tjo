# Go Web Framework Comparison: Tjo vs The Competition

*An honest comparison in the spirit of Linus Torvalds*

---

## How this was checked

Star counts and last-push dates come from the GitHub API on **2026-08-14**, via
`make comparison-check`. Feature rows for Tjo are asserted against this
repository's source on every build -- see "Rows this repository asserts about
itself" below. Feature rows for **GoFr, GoFrame and Encore** were filled in on 2026-08-05 by
reading their repository trees: package layouts, middleware directories and
runtime packages. Rows for Echo and Fiber were corrected the same way.

Rows for Gin, Buffalo, Beego, Revel and Iris are carried over from earlier
versions of this file and have **not** been re-verified. Those are the rows most
likely to be stale, and this paragraph exists so that is visible rather than
implied.

A comparison table with no date on it rots invisibly. This one had claimed
"SSE: No" for Tjo since before the `sse` package existed, and "CSRF: Plugin" for
Echo and Fiber, both of which ship it in core. Both were found by hand, once,
because somebody happened to look. So the rows that can be checked by a machine
now are.

### Rows this repository asserts about itself

`docs/comparison_test.go` reads this file and checks each of these against the
source. A row that says No about something that exists fails the build, and so
does a row that says Yes about something that does not. The list below is
checked against the test too: a row here with no check, or a check not listed
here, fails.

- `gRPC`
- `OpenAPI generation`
- `OpenAPI drift check`
- `CSRF Protection`
- `Rate Limiting`
- `XSS Prevention`
- `Input Validation`
- `Authentication`
- `2FA (TOTP)`
- `Passkeys / WebAuthn`
- `Social login`
- `Roles & multi-tenancy`
- `Anti-Bot (CAPTCHA)`
- `Migrations`
- `Session Management`
- `Job Queue`
- `Transactional enqueue`
- `Durable steps / workflows`
- `Cron Scheduler`
- `WebSocket`
- `SSE`
- `LLM chat / tools / embeddings`
- `Vector search in query builder`
- `MCP server`
- `Template Engine`
- `Hot Reload`
- `WebDAV`
- `OpenTelemetry`
- `PPROF`
- `Auto-HTTPS`
- `i18n`

### Rows that are read rather than asserted

Everything else. Three kinds, and they are different:

- **Other frameworks' rows** are checked by reading their repository trees, on
  the dates above. No test can assert them offline, and scraping their
  documentation sites would be checking marketing rather than source.
- **`Asset Pipeline`** is true because the skeleton repository ships a Tailwind
  build, and a test here cannot see another repository. A check that reached
  across repositories would fail for reasons unrelated to the change under
  test.
- **Editorial rows** — "is Buffalo's ORM good", "is Encore comparable at all" —
  are judgement, and a generated comparison table would be one nobody stands
  behind.

### Keeping the landscape table current

`make comparison-check` reads the star counts and last-push dates from the
GitHub API, reports which have drifted from what is written here, and names any
framework that has not been pushed in a year. It needs the network, so it is not
in CI: a job that depends on the GitHub API is a job that goes red when GitHub
does. It is in the release checklist instead.

## The landscape

| Framework | Stars | Last push | Read |
|---|--:|---|---|
| [Gin](https://github.com/gin-gonic/gin) | 89,114 | 2026-08-04 | Dominant. Minimalist router plus middleware |
| [PocketBase](https://github.com/pocketbase/pocketbase) | 60,670 | 2026-08-14 | A backend in one binary, not a framework. Included because its admin UI is what people compare an admin panel to |
| [Fiber](https://github.com/gofiber/fiber) | 40,066 | 2026-08-14 | Express-shaped, on fasthttp |
| [Echo](https://github.com/labstack/echo) | 32,652 | 2026-08-04 | Minimalist, with a large built-in middleware set |
| [Beego](https://github.com/beego/beego) | 32,412 | 2026-07-28 | Full-stack, still active |
| [Iris](https://github.com/kataras/iris) | 25,563 | 2026-07-27 | Feature-rich |
| [Chi](https://github.com/go-chi/chi) | 22,670 | 2026-08-10 | A router, not a framework. Tjo uses it |
| [GoFr](https://github.com/gofr-dev/gofr) | 21,020 | 2026-08-13 | Opinionated microservice framework, observability first |
| [GoFrame](https://github.com/gogf/gf) | 13,242 | 2026-08-14 | Batteries-included, large in China |
| [Revel](https://github.com/revel/revel) | 13,221 | **2023-10-28** | **Unmaintained.** Two years and ten months without a push |
| [Encore](https://github.com/encoredev/encore) | 12,254 | 2026-08-14 | Infrastructure-from-code; a different category |
| [Buffalo](https://github.com/gobuffalo/buffalo) | 8,409 | 2026-03-21 | Rails-shaped. Slowing: nearly 5 months since a push |
| [Huma](https://github.com/danielgtaylor/huma) | 4,319 | 2026-08-08 | OpenAPI-first, layers over other routers |
| [Fuego](https://github.com/go-fuego/fuego) | 1,760 | 2026-08-10 | OpenAPI-first, newer |

**Revel is in the tables below and should be read as a historical column.** It
has not been pushed since October 2023. It is also, along with Iris, one of the
two frameworks that beat Tjo on i18n — which says more about how long Tjo has
gone without i18n than it does about Revel.

**GoFr, GoFrame and Encore were added to the tables below on 2026-08-05**, each
checked against its source tree rather than its marketing:

- **GoFrame** is the closest comparator to Tjo in philosophy and the one that
  makes the most rows uncomfortable. `os/gcron`, `os/gsession`, `os/gcache`,
  `database/gdb` with eleven drivers, `net/goai` for OpenAPI generation,
  `contrib/rpc/grpcx`, and **`i18n/gi18n`** — it has had the
  internationalisation Tjo does not, for years.
- **GoFr** is API-first and observability-first: `pkg/gofr/http/middleware` has
  rate limiting, OAuth, API-key and basic auth; `pkg/gofr/rbac` has roles;
  `pkg/gofr/migration` has migrations; `datasource/` supports fifteen-plus
  stores; and `pkg/gofr/ai` has **both an LLM layer and an MCP server**. It is
  the only other framework here doing the AI work this project has been doing.
- **Encore** is a different category — infrastructure-from-code. Its Go runtime
  has `cron`, `pubsub`, `storage/{cache,objects,sqldb}`, `metrics`, `rlog` and
  `shutdown`, and you get them by declaring them rather than by wiring them.
  Comparing it row by row flatters the others: it does less inside the process
  and more outside it.

The three of them cost Tjo several rows it used to hold alone. That is the
point of re-checking.

## Overview

| Aspect | Tjo | Gin | Echo | Fiber | Buffalo | Beego | Revel | Iris | GoFr | GoFrame | Encore |
|--------|----------|-----|------|-------|---------|-------|-------|------|-----|---------|--------|
| **GitHub Stars** | New | 89.0k | 32.6k | 40.0k | 8.4k | 32.4k | 13.2k | 25.6k |
| **Type** | Full-stack | Minimalist | Minimalist | Minimalist | Full-stack | Full-stack | Full-stack | Feature-rich |
| **Philosophy** | Laravel for Go | Express for Go | Balanced | Express for Go | Rails for Go | Django for Go | Play for Go | All-in-one |
| **Go Version** | 1.25+ | 1.20+ | 1.18+ | 1.25+ | Latest 2 | 1.20+ | 1.18+ | 1.20+ |
| **HTTP Engine** | net/http (Chi) | net/http | net/http | fasthttp | net/http | net/http | net/http | net/http |
| **Last push** | Active | 2026-08-04 | 2026-08-04 | 2026-08-05 | 2026-03-21 | 2026-07-28 | **2023-10-28** | 2026-07-27 |

---

## Feature Comparison

### Routing & HTTP

| Feature | Tjo | Gin | Echo | Fiber | Buffalo | Beego | Revel | Iris | GoFr | GoFrame | Encore |
|---------|----------|-----|------|-------|---------|-------|-------|------|-----|---------|--------|
| HTTP Router | Chi | httprouter | Radix tree | fasthttp | Gorilla Mux | Built-in | Built-in | Built-in | Built-in | ghttp | Built-in |
| Route Groups | Yes | Yes | Yes | Yes | Yes | Yes | Yes | Yes | Yes | Yes | Services |
| Path Parameters | Yes | Yes | Yes | Yes | Yes | Yes | Yes | Typed | Yes | Yes | Typed |
| Named Routes | Yes | No | Yes | Yes | Yes | Yes | Yes | Yes | No | Yes | n/a |
| RESTful Resources | Auto | Manual | Manual | Manual | Auto | Auto | Auto | Auto | Manual | Auto (gf gen) | From code |
| HTTP/2 | Yes | Yes | Yes | Limited | Yes | Yes | Yes | Push | Yes | Yes | Yes |
| gRPC | No ([#84](https://github.com/jimmitjoo/tjo/issues/84)) | No | No | No | No | No | No | Yes | Built-in | grpcx | No |
| OpenAPI generation | Built-in, from Go declarations | No | No | No | No | No | No | No | No | `net/goai`, from handler types | From code |
| OpenAPI drift check | Built-in (`api.CheckResponse`) | No | No | No | No | No | No | No | No | No | Compiler |

### Middleware & Security

| Feature | Tjo | Gin | Echo | Fiber | Buffalo | Beego | Revel | Iris | GoFr | GoFrame | Encore |
|---------|----------|-----|------|-------|---------|-------|-------|------|-----|---------|--------|
| Middleware System | Yes | Yes | Yes | Yes | Yes | Yes | Interceptors | Yes | Yes | Yes | Yes |
| CSRF Protection | Built-in | Plugin | Built-in | Built-in | Built-in | Built-in | Yes | Yes | No | Plugin | n/a |
| Rate Limiting | Built-in | Plugin | Built-in | Built-in | Plugin | Plugin | No | Built-in | Built-in | Plugin | No |
| XSS Prevention | Bluemonday | No | No | No | No | No | No | No | No | gvalid | No |
| Input Validation | Built-in | go-playground | Yes | No | Yes | Yes | Built-in | Yes | Built-in | gvalid | From types |
| Authentication | `auth` package | No | No | No | No | No | No | No | Basic/APIKey/OAuth | No | Auth handler |
| 2FA (TOTP) | Built-in, replay-checked | No | No | No | No | No | No | No | No | No | No |
| Passkeys / WebAuthn | Built-in | No | No | No | No | No | No | No | No | No | No |
| Social login | Built-in, OIDC + PKCE | No | No | No | No | No | No | No | OAuth middleware | No | No |
| Roles & multi-tenancy | Built-in | No | No | No | No | No | No | No | RBAC | No | No |
| JWT | Plugin | Plugin | Plugin | Plugin | Plugin | Plugin | No | Built-in | Plugin | Plugin | Plugin |
| Anti-Bot (CAPTCHA) | No | No | No | No | No | No | No | Built-in | No | No | No |
| Recovery | Yes | Yes | Yes | Yes | Yes | Yes | Yes | Yes | Yes | Yes | Yes |

### Database & Persistence

| Feature | Tjo | Gin | Echo | Fiber | Buffalo | Beego | Revel | Iris | GoFr | GoFrame | Encore |
|---------|----------|-----|------|-------|---------|-------|-------|------|-----|---------|--------|
| ORM/Query Builder | Fluent QB | No | No | No | Pop | Beego ORM | No | No | Yes | gdb | sqldb |
| Migrations | golang-migrate | No | No | No | Soda | Built-in | No | No | Built-in | No | Built-in |
| Multi-DB Support | PG/MySQL/SQLite | No | No | No | Via Pop | Via ORM | No | No | 15+ datasources | 11 drivers | PostgreSQL |
| Seeding | Yes | No | No | No | Yes | Yes | No | No | No | No | No |
| Connection Pool | Yes | No | No | No | Yes | Yes | No | No | Yes | Yes | Yes |

### Caching & Sessions

| Feature | Tjo | Gin | Echo | Fiber | Buffalo | Beego | Revel | Iris | GoFr | GoFrame | Encore |
|---------|----------|-----|------|-------|---------|-------|-------|------|-----|---------|--------|
| Session Management | SCS | No | No | Built-in | Yes | Yes | Cookie | Yes | No | gsession | No |
| Session Stores | Redis/DB/Badger/Cookie | - | - | Redis/Memory | DB | Memory/File | Cookie | Multiple | - | Memory/Redis/File | - |
| Cache System | Redis/Badger | No | No | No | No | Yes | No | Yes | Redis | gcache/gredis | Built-in |

### Background Processing

| Feature | Tjo | Gin | Echo | Fiber | Buffalo | Beego | Revel | Iris | GoFr | GoFrame | Encore |
|---------|----------|-----|------|-------|---------|-------|-------|------|-----|---------|--------|
| Job Queue | Worker pool + SQL-backed | No | No | No | No | Task | No | No | Pub/Sub | No | Pub/Sub |
| Transactional enqueue | Yes | No | No | No | No | No | No | No | No | No | No |
| Durable steps / workflows | Yes | No | No | No | No | No | No | No | No | No | No |
| Cron Scheduler | robfig/cron, with last-run status | No | No | No | No | Toolbox | No | No | Built-in | gcron | Built-in |
| Task Runner | Makefile | No | No | No | Grift | Bee | revel cmd | No | No | gf cli | encore cli |

### Communication

| Feature | Tjo | Gin | Echo | Fiber | Buffalo | Beego | Revel | Iris | GoFr | GoFrame | Encore |
|---------|----------|-----|------|-------|---------|-------|-------|------|-----|---------|--------|
| Email (SMTP) | go-simple-mail | No | No | No | Plugin | No | No | No | No | No | No |
| Email (API) | SendGrid/Mailgun+ | No | No | No | No | No | No | No | No | No | No |
| SMS | Twilio/Vonage | No | No | No | No | No | No | No | No | No | No |
| WebSocket | Hub-pattern | No | Yes | contrib | Plugin | Yes | No | Yes | Built-in | Built-in | No |
| SSE | Built-in, with topic broadcast | No | No | No | No | No | No | Yes | No | No | No |

### AI & Vectors

| Feature | Tjo | Gin | Echo | Fiber | Buffalo | Beego | Revel | Iris | GoFr | GoFrame | Encore |
|---------|----------|-----|------|-------|---------|-------|-------|------|-----|---------|--------|
| LLM chat / tools / embeddings | `llm` module | No | No | No | No | No | No | No | Built-in | No | No |
| Vector search in query builder | pgvector, sqlite-vec | No | No | No | No | No | No | No | No | No | No |
| MCP server | 16 tools | No | No | No | No | No | No | No | Built-in | No | No |
| Agent Skills bundle | Yes | No | No | No | No | No | No | No | No | No | No |

### Views & Templates

| Feature | Tjo | Gin | Echo | Fiber | Buffalo | Beego | Revel | Iris | GoFr | GoFrame | Encore |
|---------|----------|-----|------|-------|---------|-------|-------|------|-----|---------|--------|
| Template Engine | Jet | html/template | html/template | Multi-engine | Plush | Built-in | Built-in | Django/Pug/etc | html/template | gview | None |
| Asset Pipeline | Tailwind, in the scaffold | No | No | No | Webpack | No | No | No | No | No | No |
| Hot Reload | `tjo run --watch` (air) | No | No | No | Yes | Bee | Yes | No | No | gf run | encore run |

### File Storage

| Feature | Tjo | Gin | Echo | Fiber | Buffalo | Beego | Revel | Iris | GoFr | GoFrame | Encore |
|---------|----------|-----|------|-------|---------|-------|-------|------|-----|---------|--------|
| S3 Integration | Yes | No | No | No | No | No | No | No | No | No | Object storage |
| MinIO Integration | Yes | No | No | No | No | No | No | No | No | No | No |
| File Upload | Yes | Yes | Yes | Yes | Yes | Yes | Yes | Yes | Yes | Yes | Yes |
| WebDAV | No | No | No | No | No | No | No | Yes | No | No | No |

### Observability

| Feature | Tjo | Gin | Echo | Fiber | Buffalo | Beego | Revel | Iris | GoFr | GoFrame | Encore |
|---------|----------|-----|------|-------|---------|-------|-------|------|-----|---------|--------|
| Structured Logging | Yes | No | Yes | No | Yes | Yes | No | Accesslog | Yes | glog | rlog |
| OpenTelemetry | Built-in | Plugin | Plugin | Plugin | No | No | No | No | Built-in | gtrace | Built-in |
| Health Checks | Yes | No | No | No | No | No | No | No | Built-in | No | Yes |
| Metrics/Monitor | Yes | No | No | Plugin | No | Yes | No | Yes | Built-in | gmetric | Built-in |
| PPROF | Behind the admin authorizer, own permission | No | No | No | No | No | No | Built-in | No | Built-in | No |

### Developer Experience

| Feature | Tjo | Gin | Echo | Fiber | Buffalo | Beego | Revel | Iris | GoFr | GoFrame | Encore |
|---------|----------|-----|------|-------|---------|-------|-------|------|-----|---------|--------|
| CLI Tool | tjo | No | No | No | buffalo | bee | revel | No | gofr | gf | encore |
| Code Generation | Extensive | No | No | No | Scaffolding | Scaffolding | Yes | No | No | Extensive | Clients |
| Project Scaffolding | Yes | No | No | No | Yes | Yes | Yes | No | Yes | Yes | Yes |
| MCP/AI Integration | 16 tools | No | No | No | No | No | No | No | Built-in | No | No |
| MVC Pattern | Optional | No | No | No | Yes | Yes | Yes | DI | No | Optional | No |
| Admin panel | Model-driven CRUD | No | No | No | No | Yes | No | No | No | No | No |
| Ops dashboard | Built-in | No | No | No | No | Yes | No | No | No | No | Local dashboard |
| i18n | CLDR plurals, RTL | No | No | No | No | No | Yes | Yes | No | **gi18n** | No |

### Production

| Feature | Tjo | Gin | Echo | Fiber | Buffalo | Beego | Revel | Iris | GoFr | GoFrame | Encore |
|---------|----------|-----|------|-------|---------|-------|-------|------|-----|---------|--------|
| Graceful Shutdown | Yes | Manual | Manual | Yes | Yes | Yes | Yes | Yes | Yes | Yes | Yes |
| Docker Support | Generator | No | No | No | No | No | No | No | Yes | Yes | Built-in |
| Config Validation | Startup | No | No | No | No | Yes | No | No | Yes | gcfg | From code |
| Auto-HTTPS | autocert, host policy required | No | Let's Encrypt | No | No | No | No | Yes | No | Yes | Yes |
| ngrok Integration | No | No | No | No | No | No | No | Yes | No | No | No |

### Compression

| Feature | Tjo | Gin | Echo | Fiber | Buffalo | Beego | Revel | Iris | GoFr | GoFrame | Encore |
|---------|----------|-----|------|-------|---------|-------|-------|------|-----|---------|--------|
| gzip | Yes | Yes | Yes | Yes | Yes | Yes | Yes | Yes |
| brotli | No | No | No | No | No | No | No | Yes |
| snappy/s2 | No | No | No | No | No | No | No | Yes |

---

## Pros & Cons (Honest Assessment)

### Tjo

**Pros:**
- **Most complete package** - Everything from database to email/SMS built-in.
  GoFrame is the closest competitor on breadth and does not have email, SMS,
  passkeys, an admin panel or durable job steps
- **AI-native** - MCP integration, an Agent Skills bundle, and an LLM layer.
  GoFr is the only other framework here doing this work, and it does not have
  the vector search
- **Security first** - CSRF, rate limiting, validation, XSS built-in
- **OpenTelemetry built-in** - Observability without extra work
- **Modern stack** - Go 1.25, latest best practices
- **Admin panel** - Model-driven CRUD over your own structs, no build step. The
  thing that usually makes people pick Django, in Go
- **Ops dashboard** - Errors, slow queries, queue and cron on a page you host,
  rather than a monthly bill
- **Real-time without a sync engine** - SSE with topic broadcast, and the
  decision not to build a CRDT layer written down rather than reconsidered
  every quarter
- **An OpenAPI document with a drift check** - the description is a Go value
  next to the route, not a comment block a build step parses, and a test fails
  when a handler stops writing what it declared ([docs](openapi.md))
- **Social login with the linking policy written down** - state, PKCE and a
  nonce on every ceremony, and a documented refusal to merge accounts on a
  matching email address, which is the usual way this feature becomes an
  account takeover ([docs](social-login.md))

**Cons:**
- **New** - Small community, fewer Stack Overflow answers
- **Opinionated** - Must do things the Tjo way
- **Documentation maturity** - Still growing
- **Few battle-tested production examples**

### Gin

**Pros:**
- **Industry standard** - 81k stars, largest community
- **Fast** - 40x faster than Martini
- **Stable** - Battle-tested in production everywhere
- **Middleware ecosystem** - Plugin for everything

**Cons:**
- **Minimalist** - You must build everything yourself
- **No ORM** - Choose and integrate yourself
- **No CLI** - Manual setup
- **No auth/session** - Third-party libraries

### Echo

**Pros:**
- **Balanced** - Good middle ground between performance and features
- **HTTP/2** - Good for modern applications
- **Good routing** - Radix tree, fast lookup
- **Enterprise-friendly** - Structured and type-safe
- **Auto-HTTPS** - Let's Encrypt built-in

**Cons:**
- **Minimalist** - Same issues as Gin
- **Smaller community than Gin**
- **No database integration**

### Fiber

**Pros:**
- **Fastest** - fasthttp under the hood
- **Express-like API** - Familiar for Node developers
- **Low memory** - Zero allocation goal

**Cons:**
- **fasthttp limitations** - Some libraries don't work
- **Unsafe code** - Potential Go compatibility issues
- **Values reused** - Requires caution
- **v3 still RC** - Potentially unstable

### Buffalo

**Pros:**
- **True full-stack** - Frontend + Backend
- **Best scaffolding** - Fastest to get started
- **Pop ORM** - Good database handling
- **Hot reload** - Good DX

**Cons:**
- **Requires GOPATH-mode** - Older Go patterns
- **Less actively developed** - Last release 2022?
- **Webpack integration** - Complexity
- **No published benchmarks**

### Beego

**Pros:**
- **Complete MVC** - Django/Rails style
- **Built-in ORM** - Well documented
- **Bee CLI** - Good tools
- **Modular** - Choose what you need

**Cons:**
- **Older design** - Feels dated
- **Heavy** - More overhead than sometimes needed
- **Chinese documentation** - Sometimes hard to find info

### Revel

**Pros:**
- **Play Framework inspired** - Familiar for Scala/Java developers
- **Hot reload** - Good DX
- **Built-in validation** - Comprehensive
- **i18n** - Internationalization built-in

**Cons:**
- **Inactive** - Last release April 2022
- **No ORM** - Must integrate yourself
- **Older architecture** - Feels dated
- **Smaller community now** - 13k stars but stagnating

### Iris

**Pros:**
- **Feature-rich** - Most features of all minimalist frameworks
- **Fast HTTP/2** - With push support
- **Comprehensive middleware** - JWT, CAPTCHA, rate limit built-in
- **Multi-template** - Django, Pug, Handlebars, etc.
- **gRPC + WebSocket + SSE** - All protocols

**Cons:**
- **Controversial history** - Previous license issues/drama
- **No ORM** - Must integrate yourself
- **Overwhelming** - A lot to learn
- **Less Go-idiomatic** - More "enterprise Java" feel

---

## Summary: Which Framework Wins?

| Use Case | Recommendation |
|----------|----------------|
| **Microservices/API (speed critical)** | Gin or Fiber |
| **Quick prototype with database** | Buffalo or Tjo |
| **Enterprise with observability** | Tjo or Echo |
| **Already know Express/Node** | Fiber |
| **Full-stack with email/SMS/jobs** | Tjo (alone in this class) |
| **Built-in security** | Tjo or Iris |
| **Largest community/support** | Gin |
| **AI-assisted development** | Tjo (alone) |
| **Most features without ORM** | Iris |
| **Legacy/Play Framework background** | Revel |

---

## Tjo's Unique Selling Points

Features **only Tjo has** compared to all others:

1. **MCP Integration** - AI can build your app (no one else has this)
2. **Email + SMS built-in** - SendGrid, Mailgun, Twilio, Vonage
3. **OpenTelemetry native** - Not a plugin
4. **XSS Prevention** - Bluemonday built-in
5. **S3/MinIO integration** - File storage built-in
6. **Health checks** - Production-ready
7. **Badger cache** - Embedded caching without Redis
8. **Docker generator** - `tjo make docker`
9. **Query Builder + Multi-DB** - Without heavy ORM

---

## Feature Score (Total)

| Framework | Built-in Features | Community | Active | Total |
|-----------|-------------------|-----------|--------|-------|
| Tjo | 5/5 | 1/5 | 5/5 | 11/15 |
| Gin | 2/5 | 5/5 | 5/5 | 12/15 |
| Echo | 3/5 | 4/5 | 5/5 | 12/15 |
| Fiber | 2/5 | 4/5 | 5/5 | 11/15 |
| Buffalo | 4/5 | 2/5 | 2/5 | 8/15 |
| Beego | 4/5 | 3/5 | 4/5 | 11/15 |
| Revel | 3/5 | 2/5 | 1/5 | 6/15 |
| Iris | 4/5 | 3/5 | 4/5 | 11/15 |

---

## Sources

- [Gin GitHub](https://github.com/gin-gonic/gin)
- [Echo Official](https://echo.labstack.com/)
- [Fiber GitHub](https://github.com/gofiber/fiber)
- [Buffalo GitHub](https://github.com/gobuffalo/buffalo)
- [Beego GitHub](https://github.com/beego/beego)
- [Revel GitHub](https://github.com/revel/revel)
- [Iris GitHub](https://github.com/kataras/iris)
- [Go Web Framework Benchmark](https://github.com/smallnest/go-web-framework-benchmark)
- [Top Go Frameworks 2025 - LogRocket](https://blog.logrocket.com/top-go-frameworks-2025/)
- [Framework Comparison 2025 - BuanaCoding](https://www.buanacoding.com/2025/09/fiber-vs-gin-vs-echo-golang-framework-comparison-2025.html)
