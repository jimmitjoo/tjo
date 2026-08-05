---
name: tjo
description: Build and maintain applications with the Tjo web framework for Go. Use when working in a repository that imports github.com/jimmitjoo/tjo, when running the tjo CLI (tjo new, tjo make, tjo migrate, tjo deploy), or when the user mentions Tjo, tjo make auth, the admin panel, the ops dashboard, or durable job steps.
---

# Tjo

A full-featured Go web framework. This skill covers generating code with the
CLI, the packages that must be used instead of hand-written equivalents, and
the two ways this repository has repeatedly shipped defects.

## Generate, do not write

`tjo make` produces handlers, controllers, models, auth and migrations. The
output is deterministic and compiles. Hand-written equivalents drift from the
templates.

```bash
tjo new myapp -t default -d postgres   # default | blog | api | saas
tjo make handler Thing
tjo make controller Widget
tjo make model User
tjo make auth
tjo migrate up
tjo run
```

Changing generated code means changing the template in `cmd/tjo/templates/`,
not the file it produced.

## Testing is per module, not `./...`

This is a Go workspace. `email`, `otel`, `sms` and `websocket` are separate
modules, and from the repository root `go build ./...`, `go vet ./...`,
`go test ./...`, `go mod tidy` and `govulncheck ./...` all cover the root
module **only** and silently skip the other four.

```bash
make test   # all five modules
make vuln   # govulncheck, all five modules
```

That blind spot shipped six defects and 71 reachable vulnerabilities behind a
green build. It is the single most common way to be wrong about this codebase.

## Never write authentication by hand

The `auth` package owns every security decision. Two of this project's four
published advisories were in generated authentication code, precisely because
generated code cannot be unit-tested.

| Instead of | Use |
|---|---|
| `bcrypt.CompareHashAndPassword` | `auth.Authenticate` / `auth.AuthenticateAndUpgrade` |
| comparing a reset token | `auth.ResetPassword` |
| `totp.Validate` | `auth.VerifyTOTP` (returns the step; store it, replay protection) |
| a remember-me cookie table | `auth.Remember` / `auth.Recall` / `auth.Forget` |
| hashing an API token | `auth.NewToken` / `auth.HashToken` |
| a WebAuthn ceremony | `auth.NewPasskeys` |

Wherever authentication completes -- password, 2FA, passkey, remember-me --
renew the session and rotate the CSRF token:

```go
session.RenewToken(ctx)
tjo.RotateCSRFToken(session, r)
```

Doing one without the other, or neither, is session fixation. It has happened
here twice.

## The admin panel and the ops dashboard

```go
panel := admin.New(admin.Config{DB: db, Driver: "postgres", Authorizer: rules})
panel.Register(admin.Resource{Model: User{}, Table: "users"})
panel.AddPage(ops.Page(ops.Config{Recorder: recorder, Queues: queues}))
mux.Handle("/admin/", http.StripPrefix("/admin", panel.Handler("/admin")))
```

There is no permissive authorizer default. A panel without one answers 404 to
everything; `admin.AllowAll` exists for development and is named to be obvious
in a diff.

## Jobs, and the part that bites

`jobs.SQLQueue` claims a row rather than removing it, so something has to write
the outcome back. The framework's `Worker` does. A hand-rolled pop-and-run loop
will re-run every job forever.

Durable steps checkpoint their results, so a retry resumes rather than
restarts. `Sleep` and `WaitForEvent` return `*jobs.Parked`, which the handler
must return unchanged -- swallowing it reports success and loses the workflow.

## Two recurring failure modes

1. **A control that removes itself when a dependency is missing.** The router
   installed CSRF only `if session != nil`, and session was assigned later, so
   it never ran. Fail closed and return an error.
2. **A fix verified at the function it changed rather than through the path a
   request takes.** The rate-limiter fix was correct and was defeated by a
   middleware mounted two lines earlier. Test through the router, and for
   generated code, test over HTTP against a scaffolded app with a real
   database -- `go build` compiles a form that posts a field its handler does
   not read.

## Reference

- `reference/packages.md` -- what is in each package and when to reach for it
- `reference/cli.md` -- every command and flag
- [AGENTS.md](https://github.com/jimmitjoo/tjo/blob/main/AGENTS.md) in the repository is the source of truth and takes precedence over this file
