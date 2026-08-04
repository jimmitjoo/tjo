package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestFindRoutesReadsRegisteredRoutes(t *testing.T) {
	dir := t.TempDir()

	routes := `package main

import "github.com/go-chi/chi/v5"

func (a *application) routes() *chi.Mux {
	a.App.HTTP.Router.Get("/", a.Handlers.Home)
	a.App.HTTP.Router.Post("/login", a.Handlers.PostLogin)
	a.App.HTTP.Router.Get("/users/{id}", a.Handlers.ShowUser)
	a.App.HTTP.Router.Delete("/users/{id}", a.Handlers.DeleteUser)
	a.App.HTTP.Router.Handle("/public/*", fileServer)
	return a.App.HTTP.Router
}
`
	if err := os.WriteFile(filepath.Join(dir, "routes.go"), []byte(routes), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := findRoutes(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Fatalf("found %d routes, want 5: %+v", len(got), got)
	}

	// Sorted by pattern then method, so an agent diffing two calls sees a real
	// change rather than map iteration order.
	want := []struct{ method, pattern, handler string }{
		{"GET", "/", "a.Handlers.Home"},
		{"POST", "/login", "a.Handlers.PostLogin"},
		{"HANDLE", "/public/*", "fileServer"},
		{"DELETE", "/users/{id}", "a.Handlers.DeleteUser"},
		{"GET", "/users/{id}", "a.Handlers.ShowUser"},
	}
	for i, w := range want {
		if got[i].Method != w.method || got[i].Pattern != w.pattern || got[i].Handler != w.handler {
			t.Errorf("route %d = %s %s %s, want %s %s %s",
				i, got[i].Method, got[i].Pattern, got[i].Handler, w.method, w.pattern, w.handler)
		}
		if got[i].Line == 0 {
			t.Errorf("route %d has no line number", i)
		}
	}
}

// The tool has to work while the project does not compile, because that is when
// an agent is most likely to be asking what exists.
func TestFindRoutesWorksOnCodeThatDoesNotCompile(t *testing.T) {
	dir := t.TempDir()

	routes := `package main

func (a *application) routes() *chi.Mux {
	a.App.HTTP.Router.Get("/health", a.Handlers.Health)
	undefinedHelper(thisDoesNotExist)
	return a.App.HTTP.Router
}
`
	if err := os.WriteFile(filepath.Join(dir, "routes.go"), []byte(routes), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := findRoutes(dir)
	if err != nil {
		t.Fatalf("parsing failed on a file that does not type-check: %v", err)
	}
	if len(got) != 1 || got[0].Pattern != "/health" {
		t.Fatalf("got %+v", got)
	}
}

func TestFindRoutesRejectsANonProject(t *testing.T) {
	if _, err := findRoutes(t.TempDir()); err == nil {
		t.Error("expected an error for a directory with no routes.go")
	}
	// The message has to say what to do about it, not just that something
	// failed -- an agent acting on "error" will retry the same call.
	if _, err := findRoutes(t.TempDir()); !strings.Contains(err.Error(), "Tjo project") {
		t.Errorf("unhelpful error: %v", err)
	}
}

// Secrets must not reach a transcript that is sent to a model provider.
func TestDescribeConfigRedactsSecrets(t *testing.T) {
	dir := t.TempDir()

	env := `APP_NAME=demo
PORT=4000
KEY=supersecretencryptionkey1234567
DATABASE_TYPE=sqlite
DATABASE_PASS=hunter2
TWILIO_API_KEY=ACxxxxxxxx
SOMETHING_CUSTOM=yes
# a comment
`
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(env), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := describeConfig(dir)
	if err != nil {
		t.Fatal(err)
	}

	for _, secret := range []string{"supersecretencryptionkey1234567", "hunter2", "ACxxxxxxxx"} {
		if strings.Contains(out, secret) {
			t.Errorf("a secret value reached the output: %q", secret)
		}
	}

	for _, want := range []string{
		"APP_NAME", "demo",
		"KEY", "(set, redacted)",
		"CORS_ALLOWED_ORIGINS", "(not set)",
		"SOMETHING_CUSTOM",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not mention %q", want)
		}
	}

	// An unrecognised key is worth surfacing: a typo in a variable name is
	// otherwise indistinguishable from a setting that does nothing.
	if !strings.Contains(out, "not recognised by the framework") {
		t.Error("unrecognised keys were not reported")
	}
}

func TestDescribeConfigRejectsAMissingEnv(t *testing.T) {
	if _, err := describeConfig(t.TempDir()); err == nil {
		t.Error("expected an error when there is no .env")
	}
}

// TestToolListIsDeterministicAndCacheable pins the two properties that make a
// cached tool list useful, carried over from #40 where they were deferred.
//
// A client caches tools/list and diffs it to notice a server quietly
// redefining a tool -- one of the ways MCP servers are attacked. That check is
// worthless if the list reorders itself between calls, which is what happens
// the moment anything in the registration path iterates a map.
func TestToolListIsDeterministicAndCacheable(t *testing.T) {
	ctx := context.Background()

	list := func() *mcp.ListToolsResult {
		t.Helper()

		serverTransport, clientTransport := mcp.NewInMemoryTransports()

		srv := mcp.NewServer(&mcp.Implementation{Name: "tjo", Version: "test"}, nil)
		registerTools(srv)

		ss, err := srv.Connect(ctx, serverTransport, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { ss.Close() })

		client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
		cs, err := client.Connect(ctx, clientTransport, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { cs.Close() })

		res, err := cs.ListTools(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}

	first, second := list(), list()

	var a, b []string
	for _, tool := range first.Tools {
		a = append(a, tool.Name)
	}
	for _, tool := range second.Tools {
		b = append(b, tool.Name)
	}

	if strings.Join(a, ",") != strings.Join(b, ",") {
		t.Errorf("tool order is not stable:\n  %v\n  %v", a, b)
	}
	if len(a) == 0 {
		t.Fatal("no tools registered")
	}

	// Every name namespaced, so a client merging several servers' tools does
	// not end up with two called "list".
	for _, name := range a {
		if !strings.HasPrefix(name, "tjo_") {
			t.Errorf("tool %q is not namespaced", name)
		}
	}

	if first.TTLMs <= 0 {
		t.Errorf("TTLMs = %d; a list with no freshness hint is immediately stale", first.TTLMs)
	}
	if first.CacheScope != "private" {
		t.Errorf("CacheScope = %q, want private", first.CacheScope)
	}
}
