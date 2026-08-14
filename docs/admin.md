# The admin panel and the ops dashboard

Two screens that most applications end up building by hand, and one of the
reasons people choose a framework rather than a router.

## Three lines

```go
import (
    "github.com/jimmitjoo/tjo/admin"
)

panel := admin.New(admin.Config{
    DB:         app.Data.DB.Pool,
    Driver:     app.Config.Database.Type,
    Authorizer: rules,          // see below; there is no permissive default
})
panel.Register(admin.Resource{Model: data.User{}, Table: "users"})

mux.Handle("/admin/", http.StripPrefix("/admin", panel.Handler("/admin")))
```

That is a list with search, per-column filters, sorting and pagination, and a
form with an input per column. Field types, labels and which columns are
searchable are read off the struct.

## It will not serve without an authorizer

A panel configured with no `Authorizer` answers 404 to every request. This is
not a bug to work around: the failure mode of a permissive default is a CRUD
interface to the entire database, published by someone who was going to
configure it later.

```go
// Development.
Authorizer: admin.AllowAll,

// Production, using the auth package's roles.
Authorizer: admin.RoleAuthorizer(store, auth.DefaultPermissions(),
    func(ctx admin.Context) (orgID, accountID string, err error) {
        session := app.HTTP.Session
        org := session.GetString(ctx, "organizationID")
        account := session.GetString(ctx, "userID")
        if org == "" || account == "" {
            return "", "", admin.ErrUnauthenticated
        }
        return org, account, nil
    },
    admin.DefaultPermissions()),
```

Anything more specific is a function:

```go
Authorizer: admin.AuthorizerFunc(func(ctx admin.Context, q admin.Query) error {
    // q.Action is list/view/create/update/delete.
    // q.Record is the row, for a per-record rule.
    // q.Field is set when the question is about one column.
    if q.Record != nil && q.Record["organization_id"] != currentOrg(ctx) {
        return admin.ErrForbidden
    }
    return nil
}),
```

An anonymous visitor gets 404 (`admin.ErrUnauthenticated`); a known account
without the permission gets 403 (`admin.ErrForbidden`). Hiding the panel from
someone already looking at it buys nothing.

## Configuring a resource

Reflection covers most of it. The `admin` tag covers the rest:

```go
type Article struct {
    ID       int    `db:"id,omitempty"`
    Title    string `db:"title"    admin:"required"`
    Body     string `db:"body"     admin:"widget=longtext"`
    AuthorID int    `db:"author_id" admin:"belongsTo=authors.id:name,label=Author"`
    Status   string `db:"status"   admin:"choices=draft|review|published"`
    Cover    string `db:"cover"    admin:"file"`
    Secret   string `db:"secret"`  // hidden: see below
}
```

| Tag | Effect |
|---|---|
| `label=...` | the heading a person sees |
| `hidden` / `show` | keep it off every screen, or override the sensitive-column default |
| `readonly` | rendered, not accepted back |
| `required` | rejects an empty value |
| `search` / `nosearch` | include or exclude from the search box |
| `widget=longtext` | force a textarea; any `FieldKind` works |
| `choices=a\|b\|c` | a fixed dropdown |
| `belongsTo=table.key:label` | a dropdown of another table's rows |
| `file` | an upload, stored through `Config.Uploads` |

### Columns that hold credentials are hidden

`password`, `password_hash`, `token`, `token_hash`, `secret`, `totp_secret`,
`remember_token`, `api_key`, `private_key` and `session_data` are hidden unless
the tag says `show`. An admin panel renders whatever is in the table, so the
alternative default publishes every password hash to whoever can reach it.

### The rest of `Resource`

```go
admin.Resource{
    Model:       data.Article{},
    Table:       "articles",
    Plural:      "Articles",
    Singular:    "article",
    ListColumns: []string{"id", "title", "status"},
    DefaultSort: "created_at",
    DefaultDesc: true,
    PerPage:     50,
    ReadOnly:    false,

    HasMany: []admin.HasMany{{
        Title: "Comments", Table: "comments", ForeignKey: "article_id",
        Columns: []string{"id", "author", "created_at"},
    }},

    BulkActions: []admin.BulkAction{{
        Name: "publish", Label: "Publish", Confirm: true,
        Run: func(ctx admin.Context, ids []string) error { return publish(ctx, ids) },
    }},
}
```

Every selected record is authorized individually before a bulk action runs.

## Uploads

```go
Uploads: admin.FileStore{
    FS:                app.Data.Files.Get("minio"),
    Folder:            "covers",
    AllowedExtensions: []string{".png", ".jpg", ".webp"},
},
```

`AllowedExtensions` is empty by default and that is a decision to make
deliberately: a panel that accepts `.html` is a way to host an attacker's page
on your origin, and `.svg` is the same thing with a friendlier extension.

## The audit trail

On by default when a `DB` is configured. One row per write, with the columns
that changed — never whole records, which would put copies of the hidden
columns in a second table.

```go
panel.Audit().Migrate(ctx)     // once
panel.Audit().Retain = 90 * 24 * time.Hour
panel.Audit().Prune(ctx)       // from the scheduler
```

Give it an actor or the trail records the action and not who took it:

```go
Actor: func(ctx admin.Context) string {
    return app.HTTP.Session.GetString(ctx, "userEmail")
},
```

## Custom pages

Half the value of an admin panel is that it becomes where internal tooling
lives.

```go
panel.AddPage(admin.Page{
    Path:  "reports",
    Title: "Reports",
    Body: func(ctx admin.Context) (admin.Content, error) {
        return admin.Content(renderReport()), nil
    },
    Post: func(ctx admin.Context) (string, error) {
        return "", regenerate(ctx, ctx.Request.Form.Get("month"))
    },
})
```

`Body` returns HTML the panel wraps in its own chrome. `Content` is inserted
without escaping, so a page rendering user input must escape it.

## The ops dashboard

```go
import "github.com/jimmitjoo/tjo/ops"

recorder := ops.NewRecorder(0)
app.Logging.OTel.TracerProvider().RegisterSpanProcessor(recorder)

panel.AddPage(ops.Pages(ops.Config{
    Recorder:  recorder,
    Queues:    []*jobs.SQLQueue{queue},
    Workflows: workflows,
    Health:    database.NewHealthChecker(app.Data.DB.Pool, 2*time.Second),
    Cron: ops.CronFunc(func() []ops.CronRun {
        runs := app.Background.CronStatus()
        out := make([]ops.CronRun, 0, len(runs))
        for _, r := range runs {
            out = append(out, ops.CronRun(r))
        }
        return out
    }),
}))...
```

Panels: grouped errors, slowest requests, slowest queries, queue depth and age,
failed jobs with a retry and a discard, workflows that stopped and which step
they stopped on, cron last-run, and database health.

Reading the page is `ActionList`; pressing its buttons is `ActionUpdate`, so an
operator who may look at the queue but not retry jobs is expressible without
configuring anything here.

### The profiler

```go
ops.Config{
    // ...
    Profiler: true,
}
```

Adds a second page and links the dashboard to it. An authorized operator then
runs

```bash
go tool pprof http://host/_admin/p/pprof/heap
```

and everybody else gets a 404 — not a 403, which would confirm to somebody
guessing that the path is a profiler.

**It needs `admin.ActionProfile`, which is not in `admin.DefaultPermissions`.**
A heap dump is whatever was in memory: session identifiers, tokens, request
bodies, decrypted secrets. Somebody who may read the ops dashboard should not
thereby be able to download the process. `RoleAuthorizer` refuses actions its
map does not mention, so granting it is something somebody wrote down:

```go
permissions := admin.DefaultPermissions()
permissions[admin.ActionProfile] = auth.PermManageOrg
```

Turning `Profiler` on and granting the action to nobody gives a page that
refuses everyone, which is the safe way round.

**Nothing is registered on `http.DefaultServeMux`.** `import _ "net/http/pprof"`
publishes `/debug/pprof/` there as a side effect of the import, so anything else
in the binary that serves the default mux starts serving heap dumps to whoever
can reach it. This package writes its handlers over `runtime/pprof` and
`runtime/trace` instead, and there is a test asserting the default mux is
untouched — because the failure mode of getting it wrong is silent and total.

`?seconds=` on `profile` and `trace` is capped at `ops.MaxProfileSeconds` (120).
An unbounded one holds a profiling session open for as long as the caller likes,
and no second profile can start while it runs.

There is no continuous profiling, no stored history and no flame graphs.
`go tool pprof` reads the endpoint; that is the interface.

### What the recorder is

Everything else on the page is read from rows or from process state. Errors,
slow requests and slow queries are not: the framework *emits* those as spans and
exports them, and nothing keeps them. `ops.Recorder` is a bounded ring buffer
registered on the span pipeline that already exists — no new service, no second
instrumentation, and a fixed memory cost. The page says how big the window is
rather than implying it has seen everything.

An empty panel says why it is empty. "No slow queries" and "the database is not
instrumented" look identical and mean opposite things; if no database spans have
been recorded at all, the panel names `otel.WrapDB`.
