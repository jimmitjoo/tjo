package ops

import (
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jimmitjoo/tjo/admin"
)

// The profiler, behind the admin panel's authorizer.
//
// When a process is using four gigabytes or burning CPU, the answer should not
// be "rebuild it with net/http/pprof imported and deploy that", which is the
// worst possible moment to be changing the binary.
//
// # Why this does not import net/http/pprof
//
// That package's init registers its handlers on http.DefaultServeMux,
// unconditionally, as a side effect of the import. Anything else in the binary
// that serves the default mux -- a library's metrics endpoint, a health server
// somebody wired up in six lines -- would then be serving /debug/pprof/ to
// whoever can reach it. A heap dump is whatever was in memory: session
// identifiers, tokens, request bodies, decrypted secrets. That is not a debug
// convenience, it is an exfiltration endpoint that happens to be documented,
// and a framework must not add one to a program as a side effect of being
// imported.
//
// So the handlers here are written over runtime/pprof and runtime/trace, which
// is what net/http/pprof does, and the default mux is never touched. There is a
// test for that, because the failure mode is silent and total.
//
// # Why its own permission
//
// admin.ActionProfile, which is absent from admin.DefaultPermissions.
// Downloading the process's memory is a different question from reading a
// table, and RoleAuthorizer refuses actions its map does not mention -- so
// granting it is something somebody wrote down rather than something they
// inherited.

// ProfilePage returns the profiler as an admin page.
//
//	panel := admin.New(admin.Config{Authorizer: authorizer}).
//	    AddPage(ops.Page(cfg), ops.ProfilePage())
//
// With the panel mounted at /_admin, an authorized operator runs
//
//	go tool pprof http://host/_admin/p/pprof/heap
//
// and everybody else gets a 404 -- not a 403, which would confirm that the path
// is a profiler to somebody who was guessing.
func ProfilePage() admin.Page {
	return admin.Page{
		Path:    "pprof",
		Title:   "Profiler",
		Action:  admin.ActionProfile,
		Handler: http.HandlerFunc(serveProfile),
	}
}

// MaxProfileSeconds bounds a CPU profile or a trace.
//
// Unbounded, ?seconds= is a request that holds a connection and a profiling
// session open for as long as the caller likes, which is a denial of service
// available to anybody who has the permission -- and profiling is on in the
// meantime, so a second request cannot start one.
const MaxProfileSeconds = 120

// serveProfile dispatches on the path after the page.
func serveProfile(w http.ResponseWriter, r *http.Request) {
	name := strings.Trim(r.PathValue("rest"), "/")

	switch name {
	case "":
		profileIndex(w, r)

	case "cmdline":
		text(w)
		fmt.Fprint(w, strings.Join(os.Args, "\x00"))

	case "profile":
		cpuProfile(w, r)

	case "trace":
		executionTrace(w, r)

	case "symbol":
		symbol(w, r)

	default:
		namedProfile(w, r, name)
	}
}

// namedProfile writes one of the profiles the runtime keeps.
func namedProfile(w http.ResponseWriter, r *http.Request, name string) {
	profile := pprof.Lookup(name)
	if profile == nil {
		text(w)
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, "Unknown profile: %s\n", name)
		return
	}

	debug, _ := strconv.Atoi(r.URL.Query().Get("debug"))

	// gc=1 before a heap profile, because otherwise it reports what has not
	// been collected yet rather than what is actually retained -- which is the
	// difference between "we leak" and "the collector has not run".
	if name == "heap" && r.URL.Query().Get("gc") != "" {
		runtime.GC()
	}

	if debug > 0 {
		text(w)
	} else {
		binary(w, name)
	}

	if err := profile.WriteTo(w, debug); err != nil {
		// The header is written; there is nowhere to report this but the body,
		// and go tool pprof will fail to parse it, which is the correct
		// outcome for a truncated profile.
		fmt.Fprintf(w, "\nprofile %q failed: %v\n", name, err)
	}
}

// cpuProfile samples the CPU for a while.
func cpuProfile(w http.ResponseWriter, r *http.Request) {
	seconds, err := profileSeconds(r, 30)
	if err != nil {
		badRequest(w, err)
		return
	}

	binary(w, "profile")

	if err := pprof.StartCPUProfile(w); err != nil {
		// Already profiling. Reported before anything is written, so the
		// caller sees a message rather than an empty file.
		w.Header().Del("Content-Disposition")
		badRequest(w, fmt.Errorf("could not start a CPU profile: %w", err))
		return
	}

	sleep(r, seconds)
	pprof.StopCPUProfile()
}

// executionTrace records a runtime trace.
func executionTrace(w http.ResponseWriter, r *http.Request) {
	seconds, err := profileSeconds(r, 1)
	if err != nil {
		badRequest(w, err)
		return
	}

	binary(w, "trace")

	if err := trace.Start(w); err != nil {
		w.Header().Del("Content-Disposition")
		badRequest(w, fmt.Errorf("could not start a trace: %w", err))
		return
	}

	sleep(r, seconds)
	trace.Stop()
}

// symbol resolves program counters to function names.
//
// go tool pprof asks for this when it cannot find the binary a profile came
// from, which is the normal case for a profile taken from a container.
func symbol(w http.ResponseWriter, r *http.Request) {
	text(w)

	if r.Method == http.MethodGet && r.URL.RawQuery == "" {
		fmt.Fprintf(w, "num_symbols: 1\n")
		return
	}

	addresses := r.URL.RawQuery
	if r.Method == http.MethodPost {
		// Bounded, because this is a list of addresses and a caller that sends
		// a gigabyte of them is not asking a question.
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			return
		}
		addresses = string(body)
	}

	fmt.Fprintf(w, "num_symbols: 1\n")

	for _, field := range strings.FieldsFunc(addresses, func(c rune) bool {
		return c == '+' || c == ' ' || c == '\n' || c == '\r'
	}) {
		pc, err := strconv.ParseUint(strings.TrimPrefix(field, "0x"), 16, 64)
		if err != nil {
			continue
		}
		if fn := runtime.FuncForPC(uintptr(pc)); fn != nil {
			fmt.Fprintf(w, "%#x %s\n", pc, fn.Name())
		}
	}
}

// profileIndex lists what can be fetched.
func profileIndex(w http.ResponseWriter, r *http.Request) {
	type entry struct {
		Name  string
		Count int
		Href  string
	}

	var entries []entry
	for _, profile := range pprof.Profiles() {
		entries = append(entries, entry{
			Name:  profile.Name(),
			Count: profile.Count(),
			Href:  profile.Name() + "?debug=1",
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })

	entries = append(entries,
		entry{Name: "profile", Href: "profile?seconds=30"},
		entry{Name: "trace", Href: "trace?seconds=1"},
		entry{Name: "cmdline", Href: "cmdline"},
	)

	// Relative hrefs, so the page works wherever the panel is mounted. The
	// request has to end in a slash for them to resolve; the link on the ops
	// dashboard includes one.
	if !strings.HasSuffix(r.URL.Path, "/") {
		http.Redirect(w, r, r.URL.Path+"/", http.StatusSeeOther)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	profileIndexTemplate.Execute(w, entries)
}

var profileIndexTemplate = template.Must(template.New("pprof").Parse(`<!doctype html>
<meta charset="utf-8">
<title>Profiler</title>
<style>
  body { font: 15px/1.5 system-ui, sans-serif; margin: 2rem; max-width: 46rem; }
  table { border-collapse: collapse; width: 100%; }
  td, th { text-align: start; padding: .35rem .75rem .35rem 0; border-block-end: 1px solid #ddd; }
  code { background: #f4f4f5; padding: .1rem .3rem; border-radius: 3px; }
</style>
<h1>Profiler</h1>
<p>Fetch these with <code>go tool pprof</code> rather than reading them here:</p>
<pre><code>go tool pprof &lt;this url&gt;heap</code></pre>
<table>
  <tr><th>Profile</th><th>Count</th></tr>
  {{range .}}<tr><td><a href="{{.Href}}">{{.Name}}</a></td><td>{{if .Count}}{{.Count}}{{end}}</td></tr>
  {{end}}
</table>
<p>A profile is a copy of what is in this process's memory. Treat a downloaded
one the way you would treat the database.</p>
`))

// profileSeconds reads ?seconds=, bounded twice.
//
// The second bound is the server's WriteTimeout, which is an absolute deadline
// from the start of the request: a profile longer than it has its connection
// cut mid-write, and the operator gets a truncated file that go tool pprof
// cannot parse. During an incident, which is the only time anybody asks for
// one. Refusing up front with the reason beats producing that.
//
// This framework's own server sets no WriteTimeout, deliberately, so the check
// is for the callers this package also serves -- it does not import the
// framework root, and works from a program that is not a Tjo application.
func profileSeconds(r *http.Request, fallback int) (int, error) {
	raw := r.URL.Query().Get("seconds")

	seconds := fallback
	if raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			return 0, fmt.Errorf("seconds must be a positive whole number")
		}
		if parsed > MaxProfileSeconds {
			return 0, fmt.Errorf("seconds must be at most %d", MaxProfileSeconds)
		}
		seconds = parsed
	}

	if limit, ok := writeTimeout(r); ok && time.Duration(seconds)*time.Second >= limit {
		return 0, fmt.Errorf(
			"seconds must be less than the server's WriteTimeout of %s, or the profile is cut off mid-download",
			limit)
	}

	return seconds, nil
}

// writeTimeout returns the serving server's WriteTimeout, if there is one.
func writeTimeout(r *http.Request) (time.Duration, bool) {
	srv, ok := r.Context().Value(http.ServerContextKey).(*http.Server)
	if !ok || srv == nil || srv.WriteTimeout <= 0 {
		return 0, false
	}
	return srv.WriteTimeout, true
}

// sleep waits, or returns early when the caller goes away -- so a cancelled
// download stops profiling rather than holding it open for the full duration.
func sleep(r *http.Request, seconds int) {
	timer := time.NewTimer(time.Duration(seconds) * time.Second)
	defer timer.Stop()

	select {
	case <-timer.C:
	case <-r.Context().Done():
	}
}

func text(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

func binary(w http.ResponseWriter, name string) {
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
}

func badRequest(w http.ResponseWriter, err error) {
	text(w)
	w.WriteHeader(http.StatusBadRequest)
	fmt.Fprintln(w, err)
}
