// Package admin is a model-driven CRUD panel: register a struct, get a working
// list and edit screen.
//
// Django's admin is still the canonical reason people pick Django, and Go has
// had nothing maintained -- the largest project in the space had not been
// pushed to in fourteen months when this was written. This is the useful 90%
// of it: reflection over the structs an application already has, rendered
// server-side with the query builder and the standard library.
//
// # Three lines
//
//	panel := admin.New(admin.Config{DB: db, Driver: "sqlite", Authorizer: myRules})
//	panel.Register(admin.Resource{Model: User{}, Table: "users"})
//	mux.Mount("/admin", panel.Handler("/admin"))
//
// That is a list with search, filters, sorting and pagination, and a form with
// an input per column chosen from the field's type.
//
// # It refuses to work until you say who may use it
//
// The zero value of Config has no authorizer, and a panel without one answers
// every request with 404. There is no permissive default, because the failure
// mode of one is a CRUD interface to the entire database published to the
// internet by someone who was going to configure it later.
//
// 404 rather than 403, and for an unauthenticated visitor rather than 403 with
// a login prompt: an admin panel that confirms its own existence has told a
// scanner exactly where to point the credential stuffing.
//
// # What it deliberately is not
//
// Filament is a framework inside a framework -- form builders, widgets, charts,
// a plugin ecosystem -- and that is both why it has three million installs and
// why it is somebody's full-time job. This is model-driven CRUD plus a slot for
// custom pages, and it stops there. The custom-page slot is half the value: the
// admin is where internal tooling ends up living.
//
// # No build step
//
// Server-rendered HTML with inline CSS, and the only JavaScript is a handful of
// lines for the select-all checkbox. No npm, no bundler, no CDN -- which also
// means no CSP exception and nothing to keep up to date.
//
// Rendering is html/template rather than the framework's Jet. Jet escapes, but
// html/template escapes *contextually* -- differently inside an attribute, a
// URL and a script -- and this package renders arbitrary database content into
// all three. That is the whole argument; it is also one fewer dependency.
package admin

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/jimmitjoo/tjo/database"
)

// Config configures a panel.
type Config struct {
	// DB is the database the resources live in.
	DB *sql.DB

	// Driver is the application's DATABASE_TYPE: "sqlite", "mysql",
	// "mariadb", "postgres". It selects both the placeholder syntax and the
	// audit table's schema.
	Driver string

	// Authorizer decides who may do what. Without one nothing is permitted.
	Authorizer Authorizer

	// Title is shown in the header. Defaults to "Admin".
	Title string

	// Actor names the person behind a request, for the audit trail. Optional;
	// without it the trail records the action and not who took it, which is
	// half a trail.
	Actor func(ctx Context) string

	// Audit records who changed what. Nil disables it.
	//
	// Enabled by default when a DB is present: an admin panel is where someone
	// eventually asks "who deleted that", and the answer has to have been
	// recorded before the question.
	Audit *Audit

	// Uploads receives files from file fields. Without it, a file field falls
	// back to a plain text input for a path.
	Uploads Uploader
}

// Panel is a mounted admin.
type Panel struct {
	db         *sql.DB
	dialect    database.Dialect
	authorizer Authorizer
	title      string
	actor      func(ctx Context) string
	audit      *Audit
	uploads    Uploader

	mu        sync.RWMutex
	resources []*Resource
	byName    map[string]*Resource
	pages     []*Page
	byPath    map[string]*Page

	// mount is the path the panel is served under, so links are absolute.
	mount string

	// registerErr is the first registration failure, reported by Handler
	// rather than panicking at init.
	registerErr error
}

// Page is a custom screen: the half of an admin panel that is not CRUD.
type Page struct {
	// Path is the URL segment, e.g. "reports".
	Path string

	// Title is the nav entry and heading.
	Title string

	// Body renders the page's content, which the panel wraps in its chrome.
	//
	// Returning HTML rather than writing to the ResponseWriter is what makes
	// the page look like the rest of the panel without every page having to
	// reproduce the layout.
	Body func(ctx Context) (Content, error)

	// Handler takes the whole request instead, for a page that streams, or
	// redirects, or is not HTML. Set one or the other; Handler wins.
	Handler http.Handler

	// Post handles a form submitted from this page and returns where to send
	// the browser next. An empty redirect returns to the page itself.
	//
	// A page without one accepts no writes at all: an internal tool that only
	// reads should not have a POST endpoint just because pages can have them.
	Post func(ctx Context) (redirect string, err error)

	// Action is the permission checked before the page is shown. Defaults to
	// ActionList.
	Action Action

	// PostAction is the permission checked before Post runs. Defaults to
	// ActionUpdate, so a page that reads with one role and writes with another
	// gets that without configuration.
	PostAction Action
}

// New returns a panel.
func New(cfg Config) *Panel {
	title := cfg.Title
	if title == "" {
		title = "Admin"
	}

	authorizer := cfg.Authorizer
	if authorizer == nil {
		authorizer = DenyAll
	}

	audit := cfg.Audit
	if audit == nil && cfg.DB != nil {
		audit = NewAudit(cfg.DB, cfg.Driver)
	}

	return &Panel{
		db:         cfg.DB,
		dialect:    database.DialectFor(cfg.Driver),
		authorizer: authorizer,
		title:      title,
		actor:      cfg.Actor,
		audit:      audit,
		uploads:    cfg.Uploads,
		byName:     map[string]*Resource{},
		byPath:     map[string]*Page{},
	}
}

// Register adds a resource. The first invalid one is reported by Handler.
func (p *Panel) Register(resources ...Resource) *Panel {
	p.mu.Lock()
	defer p.mu.Unlock()

	for i := range resources {
		r := resources[i]

		if err := r.validate(); err != nil {
			if p.registerErr == nil {
				p.registerErr = err
			}
			continue
		}
		if _, taken := p.byName[r.slug()]; taken {
			if p.registerErr == nil {
				p.registerErr = fmt.Errorf("admin: two resources are both called %q", r.slug())
			}
			continue
		}

		p.resources = append(p.resources, &r)
		p.byName[r.slug()] = &r
	}

	return p
}

// AddPage registers a custom screen.
func (p *Panel) AddPage(pages ...Page) *Panel {
	p.mu.Lock()
	defer p.mu.Unlock()

	for i := range pages {
		page := pages[i]

		if page.Path == "" || strings.ContainsAny(page.Path, "/?#") {
			if p.registerErr == nil {
				p.registerErr = fmt.Errorf("admin: page path %q must be a single URL segment", page.Path)
			}
			continue
		}
		if page.Action == "" {
			page.Action = ActionList
		}
		if page.PostAction == "" {
			page.PostAction = ActionUpdate
		}

		p.pages = append(p.pages, &page)
		p.byPath[page.Path] = &page
	}

	return p
}

// Err reports a registration problem, if there was one.
func (p *Panel) Err() error { return p.registerErr }

// Audit returns the trail, so an application can migrate it, prune it, or read
// it from a page of its own.
func (p *Panel) Audit() *Audit { return p.audit }

// Handler returns the panel's routes, to be mounted at mount.
//
// mount is needed because every link the panel writes is absolute: a panel that
// guessed its own prefix from the request would break the moment it was mounted
// behind a path-rewriting proxy, and guessing wrong means every link is broken
// rather than one.
func (p *Panel) Handler(mount string) http.Handler {
	p.mu.Lock()
	p.mount = strings.TrimSuffix(mount, "/")
	err := p.registerErr
	p.mu.Unlock()

	if err != nil {
		// Refusing to serve is the right outcome: a panel that dropped the
		// resource it could not validate would be a panel quietly missing a
		// screen someone is relying on.
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "admin: "+err.Error(), http.StatusInternalServerError)
		})
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", p.handleDashboard)
	mux.HandleFunc("GET /r/{resource}", p.handleList)
	mux.HandleFunc("GET /r/{resource}/new", p.handleForm)
	mux.HandleFunc("POST /r/{resource}", p.handleCreate)
	mux.HandleFunc("POST /r/{resource}/bulk", p.handleBulk)
	mux.HandleFunc("GET /r/{resource}/{id}", p.handleForm)
	mux.HandleFunc("POST /r/{resource}/{id}", p.handleUpdate)
	mux.HandleFunc("POST /r/{resource}/{id}/delete", p.handleDelete)
	mux.HandleFunc("GET /p/{page}", p.handlePage)
	mux.HandleFunc("POST /p/{page}", p.handlePagePost)

	// Cross-origin protection covers every mutating request whether or not the
	// application mounted its own CSRF middleware. It is not a replacement for
	// token CSRF -- it deliberately allows requests carrying neither
	// Sec-Fetch-Site nor Origin -- but a panel that is safe standalone is worth
	// more than a panel with a deployment note.
	protection := http.NewCrossOriginProtection()
	protection.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "cross-origin request rejected", http.StatusForbidden)
	}))

	return protection.Handler(mux)
}

// Resources returns the registered resources, for a nav or a test.
func (p *Panel) Resources() []*Resource {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return append([]*Resource(nil), p.resources...)
}

func (p *Panel) resource(name string) *Resource {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.byName[name]
}

func (p *Panel) page(path string) *Page {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.byPath[path]
}

// context wraps a request.
func (p *Panel) context(r *http.Request) Context {
	return Context{Context: r.Context(), Request: r}
}

// deny answers a refused request.
//
// 403 when the authorizer refused a known account, 404 for everything else --
// an anonymous visitor, a resource that does not exist, a record that does not.
// Those three are indistinguishable on purpose: each of them would otherwise
// confirm something to somebody who has not identified themselves.
func (p *Panel) deny(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrForbidden) {
		http.Error(w, "403 forbidden", http.StatusForbidden)
		return
	}
	http.Error(w, "404 page not found", http.StatusNotFound)
}

// fail reports a server-side problem without leaking the query that caused it.
func (p *Panel) fail(w http.ResponseWriter, err error) {
	http.Error(w, "admin: "+err.Error(), http.StatusInternalServerError)
}

func (p *Panel) url(parts ...string) string {
	return p.mount + "/" + strings.Join(parts, "/")
}
