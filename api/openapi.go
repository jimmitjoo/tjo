package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
)

// Describing an API, and generating OpenAPI 3.1 from the description.
//
// # Why the description sits on the handler
//
// The obvious shape is a middleware -- `r.With(api.Describe(op)).Post(...)` --
// and it cannot work. A middleware runs per request, not at registration, so
// it never learns the method and pattern it was registered under. Recovering
// them afterwards means matching the middleware value against what chi.Walk
// reports, and every closure returned by the same function shares one code
// pointer, so they are indistinguishable.
//
// Wrapping the handler in a named type solves it exactly: chi.Walk reports the
// method, the pattern and the handler, and a type assertion recovers the
// description. Nothing is stored globally, nothing has to be registered twice,
// and a route that is never registered contributes nothing.
//
// # Why not a build step
//
// swaggo's annotation comments are the popular Go answer, and they are a second
// language embedded in comments that no compiler checks. A declaration in Go is
// checked by the compiler, renamed by a refactoring tool, and found by "go to
// definition". The cost is that it can still drift from what the handler
// actually writes -- which is what CheckResponse is for.

// Op describes one operation.
type Op struct {
	Summary     string
	Description string
	Tags        []string

	// OperationID is what client generators name the method. Left empty it is
	// derived from the method and pattern, which is stable as long as the route
	// is -- set it explicitly if a generated client's method names matter more
	// than the URL structure.
	OperationID string

	// Request is a zero value of the type the request body decodes into, or
	// nil for an operation with no body.
	Request any

	// Response is a zero value of what is written on success -- what is
	// actually on the wire, not the payload inside it.
	//
	// Handlers that answer through JSON get the standard envelope, so this is
	// usually Envelope[T] rather than T:
	//
	//	Response: api.Envelope[Invoice]{}       // api.JSON(w, 200, invoice)
	//	Response: api.Envelope[[]Invoice]{}     // a list
	//	Response: Invoice{}                     // api.RawJSON(w, 200, invoice)
	//
	// Declaring T where the handler writes Envelope[T] describes a body nobody
	// sends, so CheckResponse fails on it.
	Response any

	// Status is the success status code. Zero means 200, or 204 when there is
	// no response body.
	Status int

	// Params are the query and header parameters. Path parameters are read off
	// the route pattern, because the spec requires every path template
	// variable to be declared and declaring them by hand only creates the
	// chance to disagree with the URL.
	Params []Parameter

	// Errors are the other status codes this operation returns. They are
	// described with the error envelope this package writes.
	Errors []int

	Deprecated bool
}

// Envelope is what JSON writes: the standard response with a typed payload.
//
// It exists so an operation can declare the body that is really on the wire
// without restating five fields. The JSON is identical to Response's; only the
// data field is typed.
type Envelope[T any] struct {
	Success   bool       `json:"success"`
	Data      T          `json:"data,omitempty"`
	Error     *ErrorInfo `json:"error,omitempty"`
	Meta      *Meta      `json:"meta,omitempty"`
	Timestamp int64      `json:"timestamp"`
}

// ErrorResponse is what Error writes: the envelope with no data field.
//
// A type of its own rather than Envelope[struct{}], because an error body
// genuinely has no data field and a component called "EnvelopeOfstruct" is not
// a name anybody wants in a generated client.
type ErrorResponse struct {
	Success   bool       `json:"success"`
	Error     *ErrorInfo `json:"error,omitempty"`
	Meta      *Meta      `json:"meta,omitempty"`
	Timestamp int64      `json:"timestamp"`
}

// described is a handler carrying its own description.
//
// A named type rather than a closure, because chi.Walk hands back the handler
// and a type assertion is what recovers the Op from it.
type described struct {
	op      Op
	handler http.Handler
}

func (d *described) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	d.handler.ServeHTTP(w, r)
}

// Describe attaches a description to a handler.
//
// The result is an http.Handler rather than an http.HandlerFunc, so it is
// registered with chi's Method rather than its Get/Post helpers:
//
//	r.Method("POST", "/invoices", api.Describe(api.Op{
//	    Summary:  "Create an invoice",
//	    Request:  Invoice{},
//	    Response: api.Envelope[Invoice]{},
//	    Status:   http.StatusCreated,
//	}, h.CreateInvoice))
//
// An undescribed route still works and simply does not appear in the document.
// Inventing an operation for it would be worse than leaving it out.
func Describe(op Op, handler http.HandlerFunc) http.Handler {
	return &described{op: op, handler: handler}
}

// DescriptionOf returns a handler's description, if it has one.
func DescriptionOf(h http.Handler) (Op, bool) {
	if d, ok := unwrap(h).(*described); ok {
		return d.op, true
	}
	return Op{}, false
}

// unwrap peels chi's middleware chains off a handler.
//
// chi stores the endpoint directly on a plain router and wrapped in a
// ChainHandler on an inline one -- which is every router made by Group or
// With, so this is the normal case rather than the exotic one.
func unwrap(h http.Handler) http.Handler {
	for {
		chain, ok := h.(*chi.ChainHandler)
		if !ok {
			return h
		}
		h = chain.Endpoint
	}
}

// Info is the document's metadata.
type Info struct {
	Title       string
	Version     string
	Description string

	// Servers are the base URLs the paths are relative to.
	Servers []string
}

// Document is an OpenAPI 3.1 document.
type Document struct {
	OpenAPI string   `json:"openapi"`
	Info    DocInfo  `json:"info"`
	Servers []Server `json:"servers,omitempty"`

	// Paths maps a path template to its operations. A map, so encoding/json
	// sorts the keys and the document is byte-identical between runs -- which
	// is what makes it reviewable in a diff.
	Paths map[string]map[string]*Operation `json:"paths"`

	Components Components `json:"components,omitempty"`
}

// DocInfo is the info object.
type DocInfo struct {
	Title       string `json:"title"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
}

// Server is one server entry.
type Server struct {
	URL string `json:"url"`
}

// Components holds the reusable schemas.
type Components struct {
	Schemas map[string]*Schema `json:"schemas,omitempty"`
}

// Operation is one method on one path.
type Operation struct {
	OperationID string   `json:"operationId"`
	Summary     string   `json:"summary,omitempty"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Deprecated  bool     `json:"deprecated,omitempty"`

	Parameters  []Parameter           `json:"parameters,omitempty"`
	RequestBody *RequestBody          `json:"requestBody,omitempty"`
	Responses   map[string]*Response3 `json:"responses"`
}

// Parameter is a path, query or header parameter.
//
// One type for the declaration and for the document, because two would differ
// only in that one holds a Go value and the other its schema -- and the
// generator fills Schema in from Type. `Param` was taken by this package's chi
// URL-parameter accessor, which applications call far more often than they
// write a description.
type Parameter struct {
	Name string `json:"name"`

	// In is "query" or "header". Empty means "query"; path parameters are
	// derived from the route pattern rather than declared.
	In string `json:"in"`

	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`

	// Type is a zero value whose type gives the schema. Nil means string.
	// Not serialised: Schema is what the document carries.
	Type any `json:"-"`

	Schema  *Schema `json:"schema,omitempty"`
	Example any     `json:"example,omitempty"`
}

// RequestBody is the body an operation accepts.
type RequestBody struct {
	Required bool                 `json:"required,omitempty"`
	Content  map[string]MediaType `json:"content"`
}

// Response3 is one response. Named for the version rather than for what it is,
// because this package already has a Response and renaming that would break
// every application using it.
type Response3 struct {
	Description string               `json:"description"`
	Content     map[string]MediaType `json:"content,omitempty"`
}

// MediaType is one content type's schema.
type MediaType struct {
	Schema *Schema `json:"schema,omitempty"`
}

// Route is one registered method and pattern.
type Route struct {
	Method  string
	Pattern string
}

// OpenAPI builds a document from a router's described routes.
//
// Routes with no description are omitted. Use Undescribed to find them, and
// assert in a test that the ones that matter are covered -- that is what keeps
// a document honest as routes are added, and it is cheaper than a linter.
func OpenAPI(routes chi.Routes, info Info) (*Document, error) {
	if info.Title == "" || info.Version == "" {
		return nil, fmt.Errorf("api: an OpenAPI document needs a title and a version")
	}

	document := &Document{
		OpenAPI: "3.1.0",
		Info: DocInfo{
			Title:       info.Title,
			Version:     info.Version,
			Description: info.Description,
		},
		Paths: map[string]map[string]*Operation{},
	}

	for _, url := range info.Servers {
		document.Servers = append(document.Servers, Server{URL: url})
	}

	schemas := newSchemaSet()
	ids := map[string]bool{}

	err := chi.Walk(routes, func(method, pattern string, handler http.Handler, _ ...func(http.Handler) http.Handler) error {
		op, ok := DescriptionOf(handler)
		if !ok {
			return nil
		}

		pattern = normalisePattern(pattern)

		operation := buildOperation(op, method, pattern, schemas, ids)

		if document.Paths[pattern] == nil {
			document.Paths[pattern] = map[string]*Operation{}
		}
		document.Paths[pattern][strings.ToLower(method)] = operation

		return nil
	})
	if err != nil {
		return nil, err
	}

	if len(schemas.byName) > 0 {
		document.Components.Schemas = schemas.byName
	}

	return document, nil
}

// Undescribed returns the routes carrying no description, so a test can insist
// that a part of the API is fully documented.
func Undescribed(routes chi.Routes) []Route {
	var missing []Route

	chi.Walk(routes, func(method, pattern string, handler http.Handler, _ ...func(http.Handler) http.Handler) error {
		if _, ok := DescriptionOf(handler); !ok {
			missing = append(missing, Route{Method: method, Pattern: normalisePattern(pattern)})
		}
		return nil
	})

	sort.Slice(missing, func(i, j int) bool {
		if missing[i].Pattern != missing[j].Pattern {
			return missing[i].Pattern < missing[j].Pattern
		}
		return missing[i].Method < missing[j].Method
	})

	return missing
}

// normalisePattern turns chi's trailing "/*" into the path it stands for, and
// strips chi's regexp constraints -- "{id:[0-9]+}" is "{id}" to a client.
func normalisePattern(pattern string) string {
	pattern = strings.TrimSuffix(pattern, "/*")
	if pattern == "" {
		pattern = "/"
	}

	var out strings.Builder
	for {
		before, rest, found := strings.Cut(pattern, "{")
		out.WriteString(before)
		if !found {
			break
		}

		name, remainder, closed := strings.Cut(rest, "}")
		if !closed {
			out.WriteString("{" + rest)
			break
		}

		if constraint := strings.Index(name, ":"); constraint >= 0 {
			name = name[:constraint]
		}
		out.WriteString("{" + name + "}")
		pattern = remainder
	}

	return out.String()
}

// pathParams reads the template variables out of a pattern.
func pathParams(pattern string) []string {
	var names []string

	for {
		_, rest, found := strings.Cut(pattern, "{")
		if !found {
			return names
		}
		name, remainder, closed := strings.Cut(rest, "}")
		if !closed {
			return names
		}
		names = append(names, name)
		pattern = remainder
	}
}

func buildOperation(op Op, method, pattern string, schemas *schemaSet, ids map[string]bool) *Operation {
	operation := &Operation{
		OperationID: operationID(op, method, pattern, ids),
		Summary:     op.Summary,
		Description: op.Description,
		Tags:        op.Tags,
		Deprecated:  op.Deprecated,
		Responses:   map[string]*Response3{},
	}

	// Path parameters first, and always required: the spec says so, and a
	// route cannot match without them.
	for _, name := range pathParams(pattern) {
		operation.Parameters = append(operation.Parameters, Parameter{
			Name:     name,
			In:       "path",
			Required: true,
			Schema:   &Schema{Type: "string"},
		})
	}

	for _, param := range op.Params {
		in := param.In
		if in == "" {
			in = "query"
		}

		schema := &Schema{Type: "string"}
		if param.Type != nil {
			schema = schemas.schemaFor(param.Type)
		}

		param.In, param.Schema = in, schema
		operation.Parameters = append(operation.Parameters, param)
	}

	if op.Request != nil {
		operation.RequestBody = &RequestBody{
			Required: true,
			Content: map[string]MediaType{
				"application/json": {Schema: schemas.schemaFor(op.Request)},
			},
		}
	}

	status := op.Status
	if status == 0 {
		status = http.StatusOK
		if op.Response == nil {
			status = http.StatusNoContent
		}
	}

	success := &Response3{Description: http.StatusText(status)}
	if op.Response != nil {
		success.Content = map[string]MediaType{
			"application/json": {Schema: schemas.schemaFor(op.Response)},
		}
	}
	operation.Responses[fmt.Sprint(status)] = success

	// The error envelope is this package's, so every operation describes it the
	// same way and a client generator produces one error type rather than one
	// per endpoint.
	errorSchema := schemas.schemaFor(ErrorResponse{})
	for _, code := range op.Errors {
		operation.Responses[fmt.Sprint(code)] = &Response3{
			Description: http.StatusText(code),
			Content:     map[string]MediaType{"application/json": {Schema: errorSchema}},
		}
	}

	return operation
}

// operationID derives a stable, unique id.
func operationID(op Op, method, pattern string, ids map[string]bool) string {
	id := op.OperationID

	if id == "" {
		id = strings.ToLower(method)
		for _, segment := range strings.Split(pattern, "/") {
			if segment == "" {
				continue
			}
			if strings.HasPrefix(segment, "{") {
				id += "By" + title(sanitise(segment))
				continue
			}
			id += title(sanitise(segment))
		}
	}

	// Client generators turn the id into a method name, so a duplicate is a
	// compile error in somebody else's codebase.
	base := id
	for n := 2; ids[id]; n++ {
		id = fmt.Sprintf("%s%d", base, n)
	}
	ids[id] = true

	return id
}

func title(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// JSON renders the document.
func (d *Document) JSON() ([]byte, error) {
	return json.MarshalIndent(d, "", "  ")
}

// OpenAPIHandler serves the document.
//
// Not registered by anything: an API description is a map of the attack
// surface, and publishing one is a decision. Mount it where the ops dashboard
// is mounted, behind the same authorisation, or serve it only in development.
//
// The document is built once, at construction, because it cannot change after
// the routes are registered and rebuilding it per request would walk the whole
// tree to produce the same bytes.
func OpenAPIHandler(routes chi.Routes, info Info) (http.HandlerFunc, error) {
	document, err := OpenAPI(routes, info)
	if err != nil {
		return nil, err
	}

	body, err := document.JSON()
	if err != nil {
		return nil, err
	}

	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}, nil
}

// CheckResponse reports whether a response matches what an operation declared.
//
// The drift check. A declaration next to a handler is checked by the compiler
// for being well-formed and by nothing at all for being true, so this closes
// the gap: give it what the handler actually wrote and it fails when the two
// have parted company.
//
//	rec := httptest.NewRecorder()
//	h.Create(rec, request)
//
//	if err := api.CheckResponse(createOp, rec.Code, rec.Body.Bytes()); err != nil {
//	    t.Fatal(err)
//	}
//
// It takes bytes and returns an error rather than taking a *testing.T, so the
// package does not link `testing` into every application that imports it.
func CheckResponse(op Op, status int, body []byte) error {
	expected := op.Status
	if expected == 0 {
		expected = http.StatusOK
		if op.Response == nil {
			expected = http.StatusNoContent
		}
	}

	if status != expected {
		if !contains(op.Errors, status) {
			return fmt.Errorf("api: the handler answered %d, and the operation declares %d%s",
				status, expected, declaredErrors(op))
		}
		// A declared error response carries the error envelope rather than the
		// success type, and its shape is this package's rather than the
		// application's.
		return checkAgainst(ErrorResponse{}, body, "error response")
	}

	if op.Response == nil {
		if len(body) > 0 && strings.TrimSpace(string(body)) != "" {
			return fmt.Errorf("api: the operation declares no response body and the handler wrote %d bytes", len(body))
		}
		return nil
	}

	return checkAgainst(op.Response, body, "response")
}

func checkAgainst(declared any, body []byte, what string) error {
	schemas := newSchemaSet()
	schema := schemas.schemaFor(declared)

	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return fmt.Errorf("api: the %s is not JSON: %w", what, err)
	}

	return checkInstance(value, schema, schemas.byName, what)
}

func contains(codes []int, code int) bool {
	for _, each := range codes {
		if each == code {
			return true
		}
	}
	return false
}

func declaredErrors(op Op) string {
	if len(op.Errors) == 0 {
		return ""
	}
	var codes []string
	for _, code := range op.Errors {
		codes = append(codes, fmt.Sprint(code))
	}
	return " (and " + strings.Join(codes, ", ") + ")"
}
