package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// The types a described API is built out of.
type Invoice struct {
	ID        int        `json:"id"`
	Number    string     `json:"number"`
	Total     float64    `json:"total"`
	Paid      bool       `json:"paid"`
	DueAt     time.Time  `json:"due_at"`
	SettledAt *time.Time `json:"settled_at,omitempty"`
	Note      string     `json:"note,omitempty"`
	internal  string
}

type NewInvoice struct {
	Number string  `json:"number"`
	Total  float64 `json:"total"`
}

// A router of the shape a scaffolded project has: a group, middleware, path
// parameters, and one route nobody described.
func describedRouter() chi.Router {
	r := chi.NewRouter()

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(func(next http.Handler) http.Handler { return next })

		r.Method("GET", "/invoices", Describe(Op{
			Summary: "List invoices",
			Tags:    []string{"invoices"},
			Params: []Parameter{
				{Name: "page", Description: "Page number", Type: 0},
				{Name: "paid", Type: false},
			},
			Response: Envelope[[]Invoice]{},
			Errors:   []int{http.StatusInternalServerError},
		}, listInvoices))

		r.Group(func(r chi.Router) {
			r.Use(func(next http.Handler) http.Handler { return next })

			r.Method("POST", "/invoices", Describe(Op{
				Summary:  "Create an invoice",
				Tags:     []string{"invoices"},
				Request:  NewInvoice{},
				Response: Envelope[Invoice]{},
				Status:   http.StatusCreated,
				Errors:   []int{http.StatusBadRequest},
			}, createInvoice))

			r.Method("GET", "/invoices/{id}", Describe(Op{
				Summary:  "Fetch an invoice",
				Response: Envelope[Invoice]{},
				Errors:   []int{http.StatusNotFound},
			}, getInvoice))

			r.Method("DELETE", "/invoices/{id}", Describe(Op{
				Summary: "Delete an invoice",
			}, deleteInvoice))
		})
	})

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {})

	return r
}

func listInvoices(w http.ResponseWriter, r *http.Request) {
	JSON(w, http.StatusOK, []Invoice{sampleInvoice()})
}

func createInvoice(w http.ResponseWriter, r *http.Request) {
	JSON(w, http.StatusCreated, sampleInvoice())
}

func getInvoice(w http.ResponseWriter, r *http.Request) {
	if chi.URLParam(r, "id") == "0" {
		Error(w, http.StatusNotFound, "NOT_FOUND", "no such invoice", nil)
		return
	}
	JSON(w, http.StatusOK, sampleInvoice())
}

func deleteInvoice(w http.ResponseWriter, r *http.Request) { NoContent(w) }

func sampleInvoice() Invoice {
	return Invoice{ID: 1, Number: "INV-1", Total: 99.5, DueAt: time.Unix(0, 0).UTC()}
}

func buildDocument(t *testing.T) *Document {
	t.Helper()

	document, err := OpenAPI(describedRouter(), Info{
		Title:   "Invoices",
		Version: "1.0.0",
		Servers: []string{"https://api.example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return document
}

// The document describes every described route, and nothing else.
func TestTheDocumentCoversTheDescribedRoutes(t *testing.T) {
	document := buildDocument(t)

	want := map[string][]string{
		"/api/v1/invoices":      {"get", "post"},
		"/api/v1/invoices/{id}": {"delete", "get"},
	}

	if len(document.Paths) != len(want) {
		t.Fatalf("paths are %v", keysOf(document.Paths))
	}
	for path, methods := range want {
		for _, method := range methods {
			if document.Paths[path][method] == nil {
				t.Errorf("%s %s is missing", strings.ToUpper(method), path)
			}
		}
	}

	// The undescribed route is absent rather than invented.
	if _, described := document.Paths["/healthz"]; described {
		t.Error("an undescribed route was given an operation")
	}
}

// Undescribed is how a project asserts its API is documented, so it has to
// report exactly the routes that carry no description.
func TestUndescribedReportsTheGaps(t *testing.T) {
	missing := Undescribed(describedRouter())

	if len(missing) != 1 || missing[0] != (Route{Method: "GET", Pattern: "/healthz"}) {
		t.Fatalf("undescribed routes are %+v", missing)
	}
}

// This is the definition-of-done check: a document a client generator can read.
// The OpenAPI 3.1 meta-schema is JSON Schema 2020-12, and validating against it
// would mean a JSON Schema library in every consumer's module graph -- so the
// spec's requirements on the subset this package emits are asserted directly,
// which also catches the things the meta-schema does not: a $ref with no
// target, a path variable with no parameter, a duplicate operation id.
func TestTheDocumentSatisfiesTheSpecification(t *testing.T) {
	document := buildDocument(t)

	body, err := document.JSON()
	if err != nil {
		t.Fatal(err)
	}

	// Round-trips through a strict decode, so a field that is not in the
	// document object is a failure rather than something a reader ignores.
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()

	var reread Document
	if err := decoder.Decode(&reread); err != nil {
		t.Fatalf("the document does not decode as one: %v", err)
	}

	if document.OpenAPI != "3.1.0" {
		t.Errorf("openapi is %q", document.OpenAPI)
	}
	if document.Info.Title == "" || document.Info.Version == "" {
		t.Error("info needs a title and a version")
	}

	// A 3.0-ism the 3.1 meta-schema has no keyword for, and the single
	// easiest mistake to make when emitting a nullable type.
	if strings.Contains(string(body), `"nullable"`) {
		t.Error("the document uses 3.0's nullable rather than a 3.1 type array")
	}

	seenIDs := map[string]string{}

	for path, methods := range document.Paths {
		if !strings.HasPrefix(path, "/") {
			t.Errorf("path %q does not start with /", path)
		}

		for method, operation := range methods {
			where := fmt.Sprintf("%s %s", strings.ToUpper(method), path)

			if len(operation.Responses) == 0 {
				t.Errorf("%s: an operation must have responses", where)
			}
			for status := range operation.Responses {
				if code, err := strconv.Atoi(status); err != nil || code < 100 || code > 599 {
					t.Errorf("%s: %q is not a status code", where, status)
				}
			}

			if operation.OperationID == "" {
				t.Errorf("%s: no operationId", where)
			}
			if previous, clash := seenIDs[operation.OperationID]; clash {
				t.Errorf("%s: operationId %q is also %s, and a generator makes two methods of that name",
					where, operation.OperationID, previous)
			}
			seenIDs[operation.OperationID] = where

			// Every template variable in the path needs a parameter, and every
			// path parameter must be required. The spec insists on both, and a
			// generator produces an uncallable method without them.
			for _, name := range pathParams(path) {
				var found bool
				for _, param := range operation.Parameters {
					if param.In == "path" && param.Name == name {
						found = true
						if !param.Required {
							t.Errorf("%s: path parameter %q is not required", where, name)
						}
					}
				}
				if !found {
					t.Errorf("%s: the path has {%s} and the operation does not declare it", where, name)
				}
			}

			for _, param := range operation.Parameters {
				switch param.In {
				case "path", "query", "header", "cookie":
				default:
					t.Errorf("%s: parameter %q is in %q", where, param.Name, param.In)
				}
			}
		}
	}

	// Every $ref resolves. The meta-schema does not check this, and a dangling
	// reference is the failure a generator actually hits.
	for name, schema := range document.Components.Schemas {
		walkRefs(t, schema, "components.schemas."+name, document.Components.Schemas)
	}
	for path, methods := range document.Paths {
		for method, operation := range methods {
			where := method + " " + path
			for _, param := range operation.Parameters {
				walkRefs(t, param.Schema, where+" parameter "+param.Name, document.Components.Schemas)
			}
			if operation.RequestBody != nil {
				for media, content := range operation.RequestBody.Content {
					walkRefs(t, content.Schema, where+" request "+media, document.Components.Schemas)
				}
			}
			for status, response := range operation.Responses {
				for media, content := range response.Content {
					walkRefs(t, content.Schema, where+" "+status+" "+media, document.Components.Schemas)
				}
			}
		}
	}
}

func walkRefs(t *testing.T, schema *Schema, where string, components map[string]*Schema) {
	t.Helper()

	if schema == nil {
		return
	}

	if schema.Ref != "" {
		name := strings.TrimPrefix(schema.Ref, "#/components/schemas/")
		if name == schema.Ref {
			t.Errorf("%s: %q is not a local component reference", where, schema.Ref)
			return
		}
		if _, ok := components[name]; !ok {
			t.Errorf("%s: %s does not resolve", where, schema.Ref)
		}
		return
	}

	walkRefs(t, schema.Items, where+".items", components)
	walkRefs(t, schema.AdditionalProperties, where+".additionalProperties", components)
	for property, sub := range schema.Properties {
		walkRefs(t, sub, where+"."+property, components)
	}
}

// The document is a reviewable artefact, so two runs must produce the same
// bytes -- otherwise checking it into a repository is a permanent diff.
func TestTheDocumentIsStable(t *testing.T) {
	first, err := buildDocument(t).JSON()
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 5; i++ {
		next, err := buildDocument(t).JSON()
		if err != nil {
			t.Fatal(err)
		}
		if string(next) != string(first) {
			t.Fatal("two runs produced different documents")
		}
	}
}

// The drift check, against handlers that really answer.
func TestCheckResponseAcceptsWhatTheHandlersActuallyWrite(t *testing.T) {
	router := describedRouter()

	cases := []struct {
		method, path string
	}{
		{"GET", "/api/v1/invoices"},
		{"POST", "/api/v1/invoices"},
		{"GET", "/api/v1/invoices/1"},
		{"DELETE", "/api/v1/invoices/1"},
		{"GET", "/api/v1/invoices/0"}, // the declared 404
	}

	for _, c := range cases {
		t.Run(c.method+" "+c.path, func(t *testing.T) {
			op := operationFor(t, router, c.method, c.path)

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(c.method, c.path, strings.NewReader(`{}`)))

			if err := CheckResponse(op, rec.Code, rec.Body.Bytes()); err != nil {
				t.Fatalf("%v\nbody: %s", err, rec.Body.String())
			}
		})
	}
}

// And it has to fail when they part company, or it is decoration.
func TestCheckResponseCatchesDrift(t *testing.T) {
	cases := []struct {
		name   string
		op     Op
		status int
		body   string
	}{
		{
			// The mistake the Envelope type exists to prevent: declaring the
			// payload where the handler writes the envelope around it.
			name:   "the payload declared where the envelope is written",
			op:     Op{Response: Invoice{}},
			status: 200,
			body:   `{"success":true,"data":{"id":1},"timestamp":0}`,
		},
		{
			name:   "a field the schema does not declare",
			op:     Op{Response: NewInvoice{}},
			status: 200,
			body:   `{"number":"INV-1","total":1,"discount":5}`,
		},
		{
			name:   "a required field the handler stopped sending",
			op:     Op{Response: NewInvoice{}},
			status: 200,
			body:   `{"number":"INV-1"}`,
		},
		{
			name:   "the wrong type",
			op:     Op{Response: NewInvoice{}},
			status: 200,
			body:   `{"number":"INV-1","total":"free"}`,
		},
		{
			name:   "a status nobody declared",
			op:     Op{Response: NewInvoice{}, Errors: []int{404}},
			status: 500,
			body:   `{}`,
		},
		{
			name:   "a body where none was declared",
			op:     Op{},
			status: 204,
			body:   `{"surprise":true}`,
		},
		{
			name:   "a list where an object was declared",
			op:     Op{Response: NewInvoice{}},
			status: 200,
			body:   `[{"number":"INV-1","total":1}]`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := CheckResponse(c.op, c.status, []byte(c.body)); err == nil {
				t.Fatal("the drift was accepted")
			}
		})
	}
}

// The document is not served by anything. Publishing a map of the attack
// surface is a decision, so the handler is built by the application or not at
// all.
func TestTheDocumentIsOnlyServedWhenAsked(t *testing.T) {
	router := describedRouter()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("GET", "/openapi.json", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("the document is served at /openapi.json without being mounted: %d", rec.Code)
	}

	handler, err := OpenAPIHandler(router, Info{Title: "Invoices", Version: "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}

	rec = httptest.NewRecorder()
	handler(rec, httptest.NewRequest("GET", "/openapi.json", nil))

	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("%d %q", rec.Code, rec.Header().Get("Content-Type"))
	}

	var document Document
	if err := json.Unmarshal(rec.Body.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	if document.OpenAPI != "3.1.0" {
		t.Errorf("served %q", document.OpenAPI)
	}
}

// A document with no title or version is not a document.
func TestOpenAPIRefusesToBuildWithoutInfo(t *testing.T) {
	for _, info := range []Info{{}, {Title: "x"}, {Version: "1"}} {
		if _, err := OpenAPI(chi.NewRouter(), info); err == nil {
			t.Errorf("%+v built a document", info)
		}
	}
}

// operationFor finds the description a route was registered with.
func operationFor(t *testing.T, routes chi.Routes, method, path string) Op {
	t.Helper()

	var found *Op
	chi.Walk(routes, func(m, pattern string, handler http.Handler, _ ...func(http.Handler) http.Handler) error {
		if m != method || !matchesPattern(normalisePattern(pattern), path) {
			return nil
		}
		if op, ok := DescriptionOf(handler); ok {
			found = &op
		}
		return nil
	})

	if found == nil {
		t.Fatalf("no description for %s %s", method, path)
	}
	return *found
}

// matchesPattern compares a concrete path with a route pattern, treating any
// {placeholder} as a wildcard segment.
func matchesPattern(pattern, path string) bool {
	patternParts := strings.Split(pattern, "/")
	pathParts := strings.Split(path, "/")

	if len(patternParts) != len(pathParts) {
		return false
	}
	for i := range patternParts {
		if strings.HasPrefix(patternParts[i], "{") {
			continue
		}
		if patternParts[i] != pathParts[i] {
			return false
		}
	}
	return true
}

func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
