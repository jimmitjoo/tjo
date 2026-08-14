# OpenAPI

An API described in Go, and an OpenAPI 3.1 document generated from the routes
that were actually registered.

```go
r.Method("POST", "/invoices", api.Describe(api.Op{
    Summary:  "Create an invoice",
    Tags:     []string{"invoices"},
    Request:  NewInvoice{},
    Response: api.Envelope[Invoice]{},
    Status:   http.StatusCreated,
    Errors:   []int{http.StatusBadRequest},
}, h.CreateInvoice))
```

```go
document, err := api.OpenAPI(router, api.Info{
    Title:   "Invoices",
    Version: "1.0.0",
    Servers: []string{"https://api.example.com"},
})

body, err := document.JSON()
```

`tjo new -t api` and `tjo make api-controller` produce this shape already,
along with the tests that keep it honest.

## Why a Go declaration and not annotation comments

swaggo is the popular Go answer: `@Summary`, `@Router`, `@Success` in a comment
block above each handler, and a `swag init` build step that parses them.

It is a second language embedded in comments, and nothing type-checks it. A
renamed struct leaves the comment naming the old one. A changed route leaves
`@Router` pointing at a URL that no longer exists. A typo in `@Success` produces
a document that is confidently wrong. None of it shows up until somebody reads
the generated spec, which is usually a client developer.

A declaration in Go is checked by the compiler, renamed by a refactoring tool,
found by "go to definition", and cannot reference a type that does not exist.
There is no build step and nothing to regenerate.

This framework had swaggo comments in its own `make api-controller` template
for several releases. Nothing read them.

## Why the description sits on the handler

The obvious shape is a middleware:

```go
r.With(api.Describe(op)).Post("/invoices", h.CreateInvoice)   // does not work
```

A middleware runs per request, so it never learns the method and pattern it was
registered under. Recovering them afterwards means matching the middleware value
against what `chi.Walk` reports — and every closure returned by one function
shares a single code pointer, so they are indistinguishable from each other.

Wrapping the handler in a named type solves it exactly. `chi.Walk` reports the
method, the pattern and the handler; a type assertion recovers the description.
Nothing is registered twice and nothing is global.

The cost: a described handler is an `http.Handler`, so it is registered with
chi's `Method` rather than `Get`/`Post`, which take an `http.HandlerFunc`.

## Declare what is on the wire

`api.JSON` wraps every payload in the standard response:

```json
{"success": true, "data": {...}, "timestamp": 1700000000}
```

So the declaration is usually `api.Envelope[T]`, not `T`:

```go
Response: api.Envelope[Invoice]{}       // api.JSON(w, 200, invoice)
Response: api.Envelope[[]Invoice]{}     // a list
Response: Invoice{}                     // api.RawJSON(w, 200, invoice)
```

The generator does not wrap for you. A declaration that quietly differs from the
bytes poisons the document and the drift check together, and the drift check is
the only thing standing between a description and fiction.

## The drift check

A declaration is checked by the compiler for being well-formed and by nothing at
all for being true. `CheckResponse` closes that gap:

```go
rec := httptest.NewRecorder()
h.CreateInvoice(rec, request)

if rec.Code != http.StatusCreated {
    t.Fatalf("answered %d", rec.Code)
}
if err := api.CheckResponse(createInvoiceOp, rec.Code, rec.Body.Bytes()); err != nil {
    t.Fatal(err)
}
```

It catches a missing required field, an undeclared field, a wrong type, a wrong
status, and a body where none was declared.

**Assert the success status separately, and first.** `CheckResponse` accepts any
status the operation declares, including its error ones — so a handler that
answered 400 to everything would pass a `CheckResponse`-only test. That is not
hypothetical: it is the bug the generated tests shipped with for one commit,
because `api.Param` is `chi.URLParam` and a test that set the parameter with
`SetPathValue` left it empty, so every id failed to parse and every case landed
on the declared 400.

It takes bytes and returns an error rather than taking a `*testing.T`, so `api`
does not link `testing` into every application that imports it.

## Coverage

```go
for _, route := range api.Undescribed(router) {
    t.Errorf("%s %s has no api.Describe", route.Method, route.Pattern)
}
```

Undescribed routes are omitted from the document rather than guessed at: an
operation nobody wrote is worse than one that is missing. `Undescribed` is how a
project insists that does not happen.

## Serving it

Nothing mounts the document.

An API description is a map of the attack surface — every path, every parameter,
every field name — so publishing one is a decision, the same rule the ops
dashboard follows.

```go
handler, err := api.OpenAPIHandler(router, apiInfo)
if err != nil {
    log.Fatal(err)
}
mux.Method("GET", "/openapi.json", handler)   // behind your admin authorisation
```

It is built once, from the routes that were registered, so it cannot describe an
endpoint that does not exist or miss one that does.

No Swagger UI is bundled. Shipping a JavaScript UI would undo the "no build
step, no CDN" property the admin panel was built to keep — point any viewer at
the JSON instead.

No client generator either. That is what the document is for, and other
people's generators are better at it.

## Schemas

Generated by reflection over the JSON encoding, not the Go struct:

| Go | Schema |
|---|---|
| `json:"name"` | property `name` |
| `json:"-"` | omitted |
| `json:"x,omitempty"` | not in `required` |
| embedded struct | fields promoted |
| `*T` | `"type": ["...", "null"]`, not in `required` |
| `time.Time` | `{"type":"string","format":"date-time"}` |
| `[]byte` | `{"type":"string","contentEncoding":"base64"}` |
| `map[string]T` | `additionalProperties` |
| `any` | `{}` — the empty schema |
| named struct | `$ref` into `components/schemas` |

Named types become components, which is what lets a self-referential type — a
comment with replies — terminate in a `$ref` rather than in the stack.

`nullable: true` is never emitted. It was an OpenAPI 3.0 keyword; 3.1 is JSON
Schema 2020-12, where a nullable type is a type array.

The document is byte-stable between runs, so checking it into a repository does
not produce a permanent diff.

## What "valid" is checked against

The OpenAPI 3.1 meta-schema is itself JSON Schema 2020-12, so validating against
it would mean a JSON Schema library in every consumer's module graph.

The specification's requirements on the subset emitted here are asserted
directly instead — which also catches what the meta-schema does not: a `$ref`
with no target, a path template variable with no parameter, a duplicate
`operationId` that would produce two same-named methods in a generated client.

That runs once, in this framework's own suite, against the same generator a
project calls. A generated project asserts what is specific to it: that every
route is described, that the document builds, and that the handlers write what
they declared.

## For agents

The MCP server's `tjo_routes_list` parses `routes.go` statically to answer "what
endpoints are there". It exists because there was no machine-readable
description of the API. Where one is now generated, it is the better answer, and
the route lister is the fallback for the parts that are not described.
