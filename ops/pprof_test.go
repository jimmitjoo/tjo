package ops

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jimmitjoo/tjo/admin"
	"github.com/jimmitjoo/tjo/auth"
)

// The one that matters most, and the one whose failure is silent.
//
// `import _ "net/http/pprof"` publishes /debug/pprof/ on http.DefaultServeMux
// as a side effect of the import, so anything else in the binary that serves
// the default mux starts serving heap dumps. Nothing in a test suite notices
// that; a scanner does.
//
// This test is what stands between an application and that, and it fails the
// moment anything in this module -- or in anything it imports -- takes the
// convenient route.
func TestTheDefaultServeMuxIsNotTouched(t *testing.T) {
	for _, path := range []string{
		"/debug/pprof/", "/debug/pprof/heap", "/debug/pprof/profile",
		"/debug/pprof/cmdline", "/debug/pprof/symbol", "/debug/pprof/trace",
	} {
		handler, pattern := http.DefaultServeMux.Handler(httptest.NewRequest("GET", path, nil))

		if pattern != "" {
			t.Errorf("%s is registered on the default mux as %q -- something imports net/http/pprof", path, pattern)
		}

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s answers %d on the default mux", path, rec.Code)
		}
	}
}

// profilePanel mounts the profiler behind an authorizer.
func profilePanel(t *testing.T, authorizer admin.Authorizer) http.Handler {
	t.Helper()

	return admin.New(admin.Config{Authorizer: authorizer}).
		AddPage(ProfilePage()).
		Handler("/_admin")
}

func fetch(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec
}

// An authorized operator gets a profile `go tool pprof` can read.
func TestAnAuthorizedOperatorCanFetchAProfile(t *testing.T) {
	h := profilePanel(t, admin.AllowAll)

	for _, name := range []string{"heap", "goroutine", "allocs", "threadcreate", "block", "mutex"} {
		t.Run(name, func(t *testing.T) {
			rec := fetch(t, h, "/p/pprof/"+name)

			if rec.Code != http.StatusOK {
				t.Fatalf("%d: %s", rec.Code, rec.Body.String())
			}
			if rec.Body.Len() == 0 {
				t.Fatal("the profile is empty")
			}

			// The gzipped protobuf go tool pprof expects, not text.
			if got := rec.Body.Bytes(); got[0] != 0x1f || got[1] != 0x8b {
				t.Errorf("the body does not start with a gzip header, so it is not a profile: %q",
					rec.Body.String()[:min(40, rec.Body.Len())])
			}

			// nosniff, because a browser that decided a heap dump was HTML
			// would render whatever happened to be in memory.
			if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Error("the profile is served without nosniff")
			}
		})
	}
}

// And everybody else is told the path does not exist, rather than that it does
// and they may not have it.
func TestAnUnauthenticatedRequestGets404(t *testing.T) {
	h := profilePanel(t, admin.DenyAll)

	for _, path := range []string{
		"/p/pprof/", "/p/pprof/heap", "/p/pprof/profile?seconds=1",
		"/p/pprof/cmdline", "/p/pprof/symbol", "/p/pprof/trace?seconds=1",
	} {
		rec := fetch(t, h, path)

		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d, and anything but 404 confirms the profiler exists", path, rec.Code)
		}
		if strings.Contains(strings.ToLower(rec.Body.String()), "pprof") {
			t.Errorf("%s: the refusal names the profiler", path)
		}
	}
}

// A panel with no authorizer at all refuses, because the default is deny.
func TestAPanelWithNoAuthorizerServesNoProfiles(t *testing.T) {
	h := admin.New(admin.Config{}).AddPage(ProfilePage()).Handler("/_admin")

	if rec := fetch(t, h, "/p/pprof/heap"); rec.Code != http.StatusNotFound {
		t.Fatalf("%d", rec.Code)
	}
}

// Reading the dashboard must not imply downloading the process's memory.
func TestReadingTheDashboardDoesNotImplyProfiling(t *testing.T) {
	// The permission map most applications use, unmodified.
	permissions := admin.DefaultPermissions()

	if _, mapped := permissions[admin.ActionProfile]; mapped {
		t.Fatal("DefaultPermissions grants profiling, so every reader can download a heap dump")
	}

	// And the authorizer refuses an action its map does not mention, rather
	// than falling through to something.
	authorizer := admin.RoleAuthorizer(
		nil, auth.DefaultPermissions(),
		func(admin.Context) (string, string, error) { return "org", "account", nil },
		permissions,
	)

	err := authorizer.Allow(admin.Context{Request: httptest.NewRequest("GET", "/", nil)},
		admin.Query{Action: admin.ActionProfile, Resource: "pprof"})

	if err == nil {
		t.Fatal("an unmapped action was permitted")
	}
}

// An operator who has been granted it, gets it.
func TestProfilingIsAllowedWhenTheActionIsGranted(t *testing.T) {
	granted := admin.AuthorizerFunc(func(ctx admin.Context, q admin.Query) error {
		if q.Action == admin.ActionProfile {
			return nil
		}
		return admin.ErrUnauthenticated
	})

	if rec := fetch(t, profilePanel(t, granted), "/p/pprof/heap"); rec.Code != http.StatusOK {
		t.Fatalf("%d: %s", rec.Code, rec.Body.String())
	}
}

// The index is a page a human can read, and it links relatively so it works
// wherever the panel is mounted.
func TestTheIndexListsTheProfiles(t *testing.T) {
	h := profilePanel(t, admin.AllowAll)

	// Without the trailing slash the relative links would resolve one level up,
	// so it redirects rather than rendering a page whose links are all wrong.
	if rec := fetch(t, h, "/p/pprof"); rec.Code != http.StatusSeeOther {
		t.Errorf("/p/pprof answered %d, want a redirect to the slashed form", rec.Code)
	}

	rec := fetch(t, h, "/p/pprof/")
	if rec.Code != http.StatusOK {
		t.Fatalf("%d", rec.Code)
	}

	body := rec.Body.String()
	for _, want := range []string{`href="heap?debug=1"`, `href="profile?seconds=30"`, `href="trace?seconds=1"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the index has no %s", want)
		}
	}
	if strings.Contains(body, `href="/`) {
		t.Error("the index links absolutely, so it only works at one mount point")
	}
}

// seconds is bounded, because an unbounded one holds a profiling session open
// for as long as the caller likes and no second profile can start meanwhile.
func TestProfileDurationIsBounded(t *testing.T) {
	h := profilePanel(t, admin.AllowAll)

	for _, query := range []string{"?seconds=100000", "?seconds=0", "?seconds=-1", "?seconds=ages"} {
		for _, path := range []string{"/p/pprof/profile", "/p/pprof/trace"} {
			rec := fetch(t, h, path+query)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("%s%s: %d, want 400", path, query, rec.Code)
			}
		}
	}
}

// A short CPU profile and a short trace really produce something.
func TestAShortCPUProfileAndTraceAreProduced(t *testing.T) {
	if testing.Short() {
		t.Skip("takes a second of wall clock")
	}

	h := profilePanel(t, admin.AllowAll)

	for _, path := range []string{"/p/pprof/profile?seconds=1", "/p/pprof/trace?seconds=1"} {
		rec := fetch(t, h, path)

		if rec.Code != http.StatusOK {
			t.Errorf("%s: %d: %s", path, rec.Code, rec.Body.String())
			continue
		}
		if rec.Body.Len() == 0 {
			t.Errorf("%s produced nothing", path)
		}
		if got := rec.Header().Get("Content-Disposition"); !strings.Contains(got, "attachment") {
			t.Errorf("%s: Content-Disposition is %q", path, got)
		}
	}
}

// cmdline and symbol are what go tool pprof asks for when it cannot find the
// binary, which is every profile taken from a container.
func TestCmdlineAndSymbolAnswer(t *testing.T) {
	h := profilePanel(t, admin.AllowAll)

	if rec := fetch(t, h, "/p/pprof/cmdline"); rec.Code != http.StatusOK || rec.Body.Len() == 0 {
		t.Errorf("cmdline: %d %q", rec.Code, rec.Body.String())
	}

	rec := fetch(t, h, "/p/pprof/symbol")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "num_symbols: 1") {
		t.Fatalf("symbol: %d %q", rec.Code, rec.Body.String())
	}

	// A POST of addresses resolves them to function names.
	addresses := programCounters(t)

	post := httptest.NewRequest("POST", "/p/pprof/symbol", strings.NewReader(addresses))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, post)

	if rec.Code != http.StatusOK {
		t.Fatalf("symbol POST: %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "ops.") {
		t.Errorf("no symbol from this package resolved:\n%s", rec.Body.String())
	}
}

// An unknown profile name is a 404 rather than an empty file that go tool pprof
// fails to parse ten seconds later.
func TestAnUnknownProfileIs404(t *testing.T) {
	rec := fetch(t, profilePanel(t, admin.AllowAll), "/p/pprof/not-a-profile")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("%d", rec.Code)
	}
}

// programCounters returns this test's own call stack, formatted the way
// go tool pprof posts addresses.
func programCounters(t *testing.T) string {
	t.Helper()

	pcs := make([]uintptr, 16)
	n := runtime.Callers(0, pcs)

	var addresses strings.Builder
	for _, pc := range pcs[:n] {
		fmt.Fprintf(&addresses, "%#x+", pc)
	}
	return addresses.String()
}

// A page that renders one screen has no subtree, so a request below it is a
// request for something that does not exist rather than the page again.
func TestABodyPageHasNoSubtree(t *testing.T) {
	h := admin.New(admin.Config{Authorizer: admin.AllowAll}).
		AddPage(admin.Page{
			Path:  "reports",
			Title: "Reports",
			Body:  func(admin.Context) (admin.Content, error) { return admin.Content("<p>hi</p>"), nil },
		}).
		Handler("/_admin")

	if rec := fetch(t, h, "/p/reports"); rec.Code != http.StatusOK {
		t.Fatalf("the page itself answers %d", rec.Code)
	}

	for _, path := range []string{"/p/reports/anything", "/p/reports/../ops"} {
		if rec := fetch(t, h, path); rec.Code == http.StatusOK {
			t.Errorf("%s rendered the page at a path it does not own", path)
		}
	}
}

// The subtree is behind the same permission as the page. A profiler reachable
// at /heap by somebody refused at / would be no profiler at all.
func TestTheSubtreeIsBehindThePagesPermission(t *testing.T) {
	var asked []admin.Action

	watching := admin.AuthorizerFunc(func(ctx admin.Context, q admin.Query) error {
		asked = append(asked, q.Action)
		return admin.ErrUnauthenticated
	})

	h := profilePanel(t, watching)

	for _, path := range []string{"/p/pprof/", "/p/pprof/heap", "/p/pprof/symbol"} {
		if rec := fetch(t, h, path); rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d", path, rec.Code)
		}
	}

	if len(asked) != 3 {
		t.Fatalf("the authorizer was asked %d times, want 3", len(asked))
	}
	for _, action := range asked {
		if action != admin.ActionProfile {
			t.Errorf("the authorizer was asked about %q rather than profiling", action)
		}
	}
}

// The dashboard's link and the profiler being mounted come from one field, so
// they cannot disagree -- a link to a page nobody mounted is a 404 found by an
// operator during an incident.
func TestTheDashboardLinksToTheProfilerOnlyWhenItIsMounted(t *testing.T) {
	for _, profiler := range []bool{false, true} {
		cfg := Config{Profiler: profiler}

		pages := Pages(cfg)
		mounted := len(pages) == 2

		if mounted != profiler {
			t.Fatalf("Profiler=%v mounted %d pages", profiler, len(pages))
		}

		h := admin.New(admin.Config{Authorizer: admin.AllowAll}).AddPage(pages...).Handler("/_admin")

		body := fetch(t, h, "/p/ops").Body.String()
		linked := strings.Contains(body, `href="pprof/"`)

		if linked != profiler {
			t.Errorf("Profiler=%v, the dashboard links to it: %v", profiler, linked)
		}

		reachable := fetch(t, h, "/p/pprof/heap").Code == http.StatusOK
		if reachable != profiler {
			t.Errorf("Profiler=%v, the profiler answers: %v", profiler, reachable)
		}
	}
}

// A profile longer than the server's WriteTimeout is cut off mid-download, and
// the operator gets a file go tool pprof cannot parse -- during an incident,
// which is the only time anybody fetches one. Refuse with the reason instead.
//
// This framework's own server sets no WriteTimeout, on purpose. This package
// does not import the framework root and serves programs that are not Tjo
// applications, so the check is for them.
func TestAProfileLongerThanTheWriteTimeoutIsRefused(t *testing.T) {
	h := profilePanel(t, admin.AllowAll)

	server := httptest.NewUnstartedServer(h)
	server.Config.WriteTimeout = 5 * time.Second
	server.Start()
	defer server.Close()

	response, err := server.Client().Get(server.URL + "/p/pprof/profile?seconds=30")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("%d, want 400", response.StatusCode)
	}

	body, _ := io.ReadAll(response.Body)
	if !strings.Contains(string(body), "WriteTimeout") {
		t.Errorf("the refusal does not say why: %q", body)
	}

	// A profile that fits is still served.
	short, err := server.Client().Get(server.URL + "/p/pprof/profile?seconds=1")
	if err != nil {
		t.Fatal(err)
	}
	defer short.Body.Close()

	if short.StatusCode != http.StatusOK {
		t.Fatalf("a profile inside the timeout answered %d", short.StatusCode)
	}
}

// Nothing in the whole repository imports net/http/pprof.
//
// TestTheDefaultServeMuxIsNotTouched only sees packages linked into this test
// binary -- ops and what ops imports. This is the wider guarantee: a submodule,
// or cmd/tjo, taking the convenient route would leave that test green while
// every binary built from this repository published /debug/pprof/.
func TestNothingInTheRepositoryImportsNetHTTPPprof(t *testing.T) {
	var offenders []string

	err := filepath.Walk("..", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "node_modules", "dist", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(body), `_ "net/http/pprof"`) ||
			strings.Contains(string(body), `"net/http/pprof"`) {
			offenders = append(offenders, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range offenders {
		t.Errorf("%s imports net/http/pprof, which publishes /debug/pprof/ on the default mux", path)
	}
}
