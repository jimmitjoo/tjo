package admin

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

// Article is the model under test: one of every field kind the panel infers.
type Article struct {
	ID          int       `db:"id,omitempty"`
	Title       string    `db:"title"`
	Body        string    `db:"body"`
	AuthorID    int       `db:"author_id" admin:"belongsTo=authors.id:name"`
	Views       int       `db:"views"`
	Rating      float64   `db:"rating"`
	Published   bool      `db:"published"`
	Secret      string    `db:"secret"`
	PublishedAt time.Time `db:"published_at"`
	CreatedAt   time.Time `db:"created_at"`
}

type Author struct {
	ID   int    `db:"id,omitempty"`
	Name string `db:"name"`
}

// eachDatabase runs fn against SQLite and PostgreSQL. The PostgreSQL half skips
// unless TJO_TEST_POSTGRES_DSN is set; CI sets it.
func eachDatabase(t *testing.T, fn func(t *testing.T, db *sql.DB, driver string)) {
	t.Helper()

	t.Run("sqlite", func(t *testing.T) {
		db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "admin.db"))
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(1)
		t.Cleanup(func() { db.Close() })

		mustExec(t, db, `CREATE TABLE authors (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL)`)
		mustExec(t, db, `CREATE TABLE articles (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT NOT NULL, body TEXT, author_id INTEGER,
			views INTEGER DEFAULT 0, rating REAL DEFAULT 0,
			published INTEGER DEFAULT 0, secret TEXT,
			published_at DATETIME, created_at DATETIME)`)

		fn(t, db, "sqlite")
	})

	t.Run("postgres", func(t *testing.T) {
		dsn := os.Getenv("TJO_TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("TJO_TEST_POSTGRES_DSN is not set")
		}

		db, err := sql.Open("pgx", dsn)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Ping(); err != nil {
			t.Fatalf("ping: %v", err)
		}
		t.Cleanup(func() {
			db.Exec(`DROP TABLE IF EXISTS articles`)
			db.Exec(`DROP TABLE IF EXISTS authors`)
			db.Exec(`DROP TABLE IF EXISTS tjo_admin_audit`)
			db.Close()
		})

		mustExec(t, db, `DROP TABLE IF EXISTS articles`)
		mustExec(t, db, `DROP TABLE IF EXISTS authors`)
		mustExec(t, db, `DROP TABLE IF EXISTS tjo_admin_audit`)
		mustExec(t, db, `CREATE TABLE authors (id SERIAL PRIMARY KEY, name TEXT NOT NULL)`)
		mustExec(t, db, `CREATE TABLE articles (
			id SERIAL PRIMARY KEY,
			title TEXT NOT NULL, body TEXT, author_id INTEGER,
			views INTEGER DEFAULT 0, rating DOUBLE PRECISION DEFAULT 0,
			published BOOLEAN DEFAULT false, secret TEXT,
			published_at TIMESTAMPTZ, created_at TIMESTAMPTZ)`)

		fn(t, db, "postgres")
	})
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

// seed inserts a handful of rows and returns the author id.
func seed(t *testing.T, db *sql.DB, driver string) int {
	t.Helper()

	ph := func(n int) string {
		if driver == "postgres" {
			return "$" + itoa(n)
		}
		return "?"
	}

	var authorID int
	if driver == "postgres" {
		if err := db.QueryRow(`INSERT INTO authors (name) VALUES ($1) RETURNING id`, "Ada").Scan(&authorID); err != nil {
			t.Fatal(err)
		}
	} else {
		res, err := db.Exec(`INSERT INTO authors (name) VALUES (?)`, "Ada")
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		authorID = int(id)
	}

	for i, title := range []string{"First post", "Second post", "Third post"} {
		mustExec(t, db,
			`INSERT INTO articles (title, body, author_id, views, rating, published, secret, created_at)
			 VALUES (`+ph(1)+`, `+ph(2)+`, `+ph(3)+`, `+ph(4)+`, `+ph(5)+`, `+ph(6)+`, `+ph(7)+`, `+ph(8)+`)`,
			title, "body of "+title, authorID, i*10, 4.5, i == 0, "top-secret-value", time.Now().UTC())
	}

	return authorID
}

func itoa(n int) string { return strconv.Itoa(n) }

// testPanel is the three-line registration from the package documentation.
func testPanel(db *sql.DB, driver string, authorizer Authorizer) *Panel {
	panel := New(Config{DB: db, Driver: driver, Authorizer: authorizer, Title: "Test Admin"})
	panel.Register(
		Resource{Model: Article{}, Table: "articles", HasMany: []HasMany{{Table: "articles", ForeignKey: "author_id"}}},
		Resource{Model: Author{}, Table: "authors"},
	)
	return panel
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec
}

func post(t *testing.T, h http.Handler, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// The panel's cross-origin protection reads these; a browser sends them and
	// so must the test.
	r.Header.Set("Sec-Fetch-Site", "same-origin")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

// The definition of done: a model registered in three lines produces a working
// list and edit screen.
func TestThreeLinesProduceAListAndAnEditScreen(t *testing.T) {
	eachDatabase(t, func(t *testing.T, db *sql.DB, driver string) {
		seed(t, db, driver)

		h := testPanel(db, driver, AllowAll).Handler("/admin")

		list := get(t, h, "/r/articles")
		if list.Code != http.StatusOK {
			t.Fatalf("list: %d\n%s", list.Code, list.Body.String())
		}
		body := list.Body.String()
		for _, want := range []string{"First post", "Second post", "Third post", "3 records"} {
			if !strings.Contains(body, want) {
				t.Errorf("the list does not contain %q", want)
			}
		}

		edit := get(t, h, "/r/articles/1")
		if edit.Code != http.StatusOK {
			t.Fatalf("edit: %d\n%s", edit.Code, edit.Body.String())
		}
		form := edit.Body.String()

		// The inputs were chosen from the Go types, without configuration.
		for _, want := range []string{
			`name="title"`,
			`<textarea id="body"`,      // string named body -> textarea
			`type="number" step="1"`,   // int
			`type="number" step="any"`, // float
			`type="checkbox"`,          // bool
			`type="datetime-local"`,    // time.Time
			`<select id="author_id"`,   // belongs-to
			`<option value="1"`,        // ... populated from the related table
		} {
			if !strings.Contains(form, want) {
				t.Errorf("the form does not contain %q", want)
			}
		}
	})
}

// A column holding a credential is not rendered by a screen whose whole job is
// rendering whatever is in the table.
func TestSensitiveColumnsAreNotRendered(t *testing.T) {
	eachDatabase(t, func(t *testing.T, db *sql.DB, driver string) {
		seed(t, db, driver)

		h := testPanel(db, driver, AllowAll).Handler("/admin")

		for _, path := range []string{"/r/articles", "/r/articles/1"} {
			body := get(t, h, path).Body.String()
			if strings.Contains(body, "top-secret-value") {
				t.Errorf("%s renders the secret column", path)
			}
			if strings.Contains(body, `name="secret"`) {
				t.Errorf("%s offers the secret column for editing", path)
			}
		}
	})
}

// Per-record authorization, enforced on the server. Hiding the button is not
// the same as refusing the request, and this is the difference.
func TestPerRecordAuthorizationIsEnforcedNotHidden(t *testing.T) {
	eachDatabase(t, func(t *testing.T, db *sql.DB, driver string) {
		seed(t, db, driver)

		// May read everything, may only change the published article.
		rules := AuthorizerFunc(func(ctx Context, q Query) error {
			switch q.Action {
			case ActionList, ActionView, ActionCreate:
				return nil
			}
			if q.Record != nil && parseBool(display(q.Record["published"])) {
				return nil
			}
			return ErrForbidden
		})

		h := testPanel(db, driver, rules).Handler("/admin")

		// Article 1 is published; 2 is not.
		allowed := post(t, h, "/r/articles/1", url.Values{"title": {"Edited"}})
		if allowed.Code != http.StatusSeeOther {
			t.Fatalf("permitted update: %d\n%s", allowed.Code, allowed.Body.String())
		}

		refused := post(t, h, "/r/articles/2", url.Values{"title": {"Should not happen"}})
		if refused.Code != http.StatusForbidden {
			t.Fatalf("refused update: %d, want 403", refused.Code)
		}

		var title string
		if err := db.QueryRow(`SELECT title FROM articles WHERE id = 2`).Scan(&title); err != nil {
			t.Fatal(err)
		}
		if title != "Second post" {
			t.Fatalf("the refused update was applied anyway: title is %q", title)
		}

		// And the delete, which is the one that cannot be undone.
		refusedDelete := post(t, h, "/r/articles/2/delete", nil)
		if refusedDelete.Code != http.StatusForbidden {
			t.Fatalf("refused delete: %d, want 403", refusedDelete.Code)
		}

		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM articles`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 3 {
			t.Fatalf("%d articles remain, want 3", count)
		}
	})
}

// An unauthenticated visitor is told nothing, including whether any of this
// exists. A known account is told no.
func TestAnonymousGets404AndAKnownAccountGets403(t *testing.T) {
	eachDatabase(t, func(t *testing.T, db *sql.DB, driver string) {
		seed(t, db, driver)

		anonymous := testPanel(db, driver, DenyAll).Handler("/admin")
		for _, path := range []string{"/", "/r/articles", "/r/articles/1", "/r/nosuch"} {
			if code := get(t, anonymous, path).Code; code != http.StatusNotFound {
				t.Errorf("anonymous %s: %d, want 404", path, code)
			}
		}

		known := testPanel(db, driver, AuthorizerFunc(func(Context, Query) error {
			return ErrForbidden
		})).Handler("/admin")
		if code := get(t, known, "/r/articles").Code; code != http.StatusForbidden {
			t.Errorf("known account: %d, want 403", code)
		}
	})
}

// A panel with no authorizer at all serves nothing. The alternative default is
// a CRUD interface to the whole database, published by someone who meant to
// configure it later.
func TestAPanelWithNoAuthorizerRefusesEverything(t *testing.T) {
	eachDatabase(t, func(t *testing.T, db *sql.DB, driver string) {
		seed(t, db, driver)

		panel := New(Config{DB: db, Driver: driver})
		panel.Register(Resource{Model: Article{}, Table: "articles"})
		h := panel.Handler("/admin")

		for _, path := range []string{"/", "/r/articles", "/r/articles/1"} {
			if code := get(t, h, path).Code; code != http.StatusNotFound {
				t.Errorf("%s: %d, want 404", path, code)
			}
		}
	})
}

func TestCreateUpdateAndDelete(t *testing.T) {
	eachDatabase(t, func(t *testing.T, db *sql.DB, driver string) {
		authorID := seed(t, db, driver)

		h := testPanel(db, driver, AllowAll).Handler("/admin")

		created := post(t, h, "/r/articles", url.Values{
			"title":     {"Fourth post"},
			"body":      {"written by the test"},
			"author_id": {display(authorID)},
			"views":     {"7"},
			"rating":    {"3.5"},
			"published": {"true"},
		})
		if created.Code != http.StatusSeeOther {
			t.Fatalf("create: %d\n%s", created.Code, created.Body.String())
		}

		var (
			views     int
			rating    float64
			published bool
		)
		row := db.QueryRow(`SELECT views, rating, published FROM articles WHERE title = 'Fourth post'`)
		if err := row.Scan(&views, &rating, &published); err != nil {
			t.Fatal(err)
		}
		if views != 7 || rating != 3.5 || !published {
			t.Fatalf("stored views=%d rating=%v published=%v, want 7 / 3.5 / true", views, rating, published)
		}

		updated := post(t, h, "/r/articles/1", url.Values{"title": {"Renamed"}, "author_id": {display(authorID)}})
		if updated.Code != http.StatusSeeOther {
			t.Fatalf("update: %d\n%s", updated.Code, updated.Body.String())
		}

		var title string
		if err := db.QueryRow(`SELECT title FROM articles WHERE id = 1`).Scan(&title); err != nil {
			t.Fatal(err)
		}
		if title != "Renamed" {
			t.Fatalf("title = %q after update", title)
		}

		deleted := post(t, h, "/r/articles/3/delete", nil)
		if deleted.Code != http.StatusSeeOther {
			t.Fatalf("delete: %d", deleted.Code)
		}

		var count int
		db.QueryRow(`SELECT COUNT(*) FROM articles`).Scan(&count)
		if count != 3 {
			t.Fatalf("%d articles after creating one and deleting one, want 3", count)
		}
	})
}

// A required field that is empty comes back as a form with a message, not as a
// database error.
func TestValidationRedisplaysTheForm(t *testing.T) {
	eachDatabase(t, func(t *testing.T, db *sql.DB, driver string) {
		seed(t, db, driver)

		panel := New(Config{DB: db, Driver: driver, Authorizer: AllowAll})
		panel.Register(Resource{
			Model: Article{}, Table: "articles",
			FieldOverrides: map[string]Field{
				"title": {Label: "Title", Kind: KindText, Required: true, Searchable: true},
			},
		})
		h := panel.Handler("/admin")

		out := post(t, h, "/r/articles", url.Values{"title": {""}})
		if out.Code != http.StatusUnprocessableEntity {
			t.Fatalf("create with no title: %d, want 422", out.Code)
		}
		if !strings.Contains(out.Body.String(), "is required") {
			t.Fatalf("the form does not say what was wrong:\n%s", out.Body.String())
		}
	})
}

func TestSearchFilterSortAndPaginate(t *testing.T) {
	eachDatabase(t, func(t *testing.T, db *sql.DB, driver string) {
		seed(t, db, driver)

		h := testPanel(db, driver, AllowAll).Handler("/admin")

		search := get(t, h, "/r/articles?q=Second").Body.String()
		if !strings.Contains(search, "Second post") || strings.Contains(search, "First post") {
			t.Error("search did not narrow the list")
		}

		filter := get(t, h, "/r/articles?f_title=Third").Body.String()
		if !strings.Contains(filter, "Third post") || strings.Contains(filter, "First post") {
			t.Error("the column filter did not narrow the list")
		}

		ascending := get(t, h, "/r/articles?sort=title&dir=asc").Body.String()
		if strings.Index(ascending, "First post") > strings.Index(ascending, "Third post") {
			t.Error("ascending sort did not order by title")
		}

		paged := get(t, h, "/r/articles?per_page=2").Body.String()
		if rows := strings.Count(paged, `name="ids"`); rows != 2 {
			t.Errorf("per_page=2 produced %d rows", rows)
		}
	})
}

// Sort and filter columns reach SQL as text, because a column name cannot be a
// parameter. They are matched against the resource's own fields first, and
// anything else is ignored rather than passed on.
func TestUnknownSortAndFilterColumnsAreIgnored(t *testing.T) {
	eachDatabase(t, func(t *testing.T, db *sql.DB, driver string) {
		seed(t, db, driver)

		h := testPanel(db, driver, AllowAll).Handler("/admin")

		out := get(t, h, "/r/articles?sort=id%3B+DROP+TABLE+articles&f_nosuch=1&f_secret=x")
		if out.Code != http.StatusOK {
			t.Fatalf("%d\n%s", out.Code, out.Body.String())
		}

		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM articles`).Scan(&count); err != nil {
			t.Fatalf("the articles table did not survive: %v", err)
		}
		if count != 3 {
			t.Fatalf("%d rows, want 3", count)
		}
	})
}

// Bulk actions authorize every selected record, or they are a way around
// per-record permissions: tick the box next to a row you may not touch.
func TestBulkDeleteChecksEveryRecord(t *testing.T) {
	eachDatabase(t, func(t *testing.T, db *sql.DB, driver string) {
		seed(t, db, driver)

		rules := AuthorizerFunc(func(ctx Context, q Query) error {
			if q.Action == ActionDelete && q.Record != nil && display(q.Record["id"]) == "2" {
				return ErrForbidden
			}
			return nil
		})

		h := testPanel(db, driver, rules).Handler("/admin")

		out := post(t, h, "/r/articles/bulk", url.Values{"action": {"delete"}, "ids": {"1", "2"}})
		if out.Code != http.StatusForbidden {
			t.Fatalf("bulk delete including a refused record: %d, want 403", out.Code)
		}

		var count int
		db.QueryRow(`SELECT COUNT(*) FROM articles`).Scan(&count)
		if count != 3 {
			t.Fatalf("%d articles remain, want 3 -- part of a refused bulk action was applied", count)
		}

		// The permitted subset works.
		ok := post(t, h, "/r/articles/bulk", url.Values{"action": {"delete"}, "ids": {"1", "3"}})
		if ok.Code != http.StatusSeeOther {
			t.Fatalf("permitted bulk delete: %d", ok.Code)
		}
		db.QueryRow(`SELECT COUNT(*) FROM articles`).Scan(&count)
		if count != 1 {
			t.Fatalf("%d articles remain, want 1", count)
		}
	})
}

// The audit trail exists so "who deleted that" has an answer.
func TestAuditTrailRecordsWrites(t *testing.T) {
	eachDatabase(t, func(t *testing.T, db *sql.DB, driver string) {
		seed(t, db, driver)

		panel := New(Config{
			DB: db, Driver: driver, Authorizer: AllowAll,
			Actor: func(Context) string { return "alex@example.com" },
		})
		panel.Register(Resource{Model: Article{}, Table: "articles"})

		if err := panel.audit.Migrate(context.Background()); err != nil {
			t.Fatal(err)
		}

		h := panel.Handler("/admin")

		if code := post(t, h, "/r/articles/1", url.Values{"title": {"Audited"}}).Code; code != http.StatusSeeOther {
			t.Fatalf("update: %d", code)
		}

		entries, err := panel.audit.For(context.Background(), "articles", "1", 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 {
			t.Fatalf("%d audit entries, want 1", len(entries))
		}
		if entries[0].Actor != "alex@example.com" || entries[0].Action != ActionUpdate {
			t.Fatalf("entry = %+v", entries[0])
		}
		if _, changed := entries[0].Changes["title"]; !changed {
			t.Fatalf("the entry does not record the changed column: %+v", entries[0].Changes)
		}

		// And it does not record the columns the panel refuses to display.
		if _, leaked := entries[0].Changes["secret"]; leaked {
			t.Fatal("the audit trail holds a copy of a hidden column")
		}
	})
}

// A custom page is half the value of an admin panel: it is where the internal
// tooling ends up.
func TestCustomPage(t *testing.T) {
	eachDatabase(t, func(t *testing.T, db *sql.DB, driver string) {
		panel := testPanel(db, driver, AllowAll)
		panel.AddPage(Page{
			Path:  "reports",
			Title: "Reports",
			Body: func(ctx Context) (Content, error) {
				return Content(`<p id="report">All good</p>`), nil
			},
		})
		h := panel.Handler("/admin")

		out := get(t, h, "/p/reports")
		if out.Code != http.StatusOK {
			t.Fatalf("%d", out.Code)
		}
		body := out.Body.String()
		if !strings.Contains(body, `id="report"`) {
			t.Error("the page body was not rendered")
		}
		if !strings.Contains(body, "Reports") || !strings.Contains(body, "Test Admin") {
			t.Error("the page was not wrapped in the panel's chrome")
		}
	})
}

// A cross-origin POST is refused whether or not the application mounted its own
// CSRF middleware.
func TestCrossOriginWritesAreRejected(t *testing.T) {
	eachDatabase(t, func(t *testing.T, db *sql.DB, driver string) {
		seed(t, db, driver)

		h := testPanel(db, driver, AllowAll).Handler("/admin")

		r := httptest.NewRequest("POST", "/r/articles/1/delete", nil)
		r.Header.Set("Sec-Fetch-Site", "cross-site")

		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("cross-origin delete: %d, want 403", rec.Code)
		}

		var count int
		db.QueryRow(`SELECT COUNT(*) FROM articles`).Scan(&count)
		if count != 3 {
			t.Fatal("a cross-origin request deleted a record")
		}
	})
}

// A refused field is ignored when it arrives, not merely absent from the form.
// A POST body is whatever the client felt like sending.
func TestAFieldTheAuthorizerRefusesIsNotWritten(t *testing.T) {
	eachDatabase(t, func(t *testing.T, db *sql.DB, driver string) {
		seed(t, db, driver)

		rules := AuthorizerFunc(func(ctx Context, q Query) error {
			if q.Field == "views" && q.Action == ActionUpdate {
				return ErrForbidden
			}
			return nil
		})

		h := testPanel(db, driver, rules).Handler("/admin")

		out := post(t, h, "/r/articles/1", url.Values{"title": {"Fine"}, "views": {"9999"}})
		if out.Code != http.StatusSeeOther {
			t.Fatalf("%d\n%s", out.Code, out.Body.String())
		}

		var (
			title string
			views int
		)
		if err := db.QueryRow(`SELECT title, views FROM articles WHERE id = 1`).Scan(&title, &views); err != nil {
			t.Fatal(err)
		}
		if title != "Fine" {
			t.Fatalf("the permitted field was not written: %q", title)
		}
		if views == 9999 {
			t.Fatal("a field the authorizer refused was written anyway")
		}
	})
}

func TestReadOnlyResourceRefusesWrites(t *testing.T) {
	eachDatabase(t, func(t *testing.T, db *sql.DB, driver string) {
		seed(t, db, driver)

		panel := New(Config{DB: db, Driver: driver, Authorizer: AllowAll})
		panel.Register(Resource{Model: Article{}, Table: "articles", ReadOnly: true})
		h := panel.Handler("/admin")

		if code := get(t, h, "/r/articles").Code; code != http.StatusOK {
			t.Fatalf("list of a read-only resource: %d", code)
		}
		if code := post(t, h, "/r/articles/1", url.Values{"title": {"nope"}}).Code; code != http.StatusNotFound {
			t.Fatalf("update of a read-only resource: %d, want 404", code)
		}
		if code := post(t, h, "/r/articles/1/delete", nil).Code; code != http.StatusNotFound {
			t.Fatalf("delete from a read-only resource: %d, want 404", code)
		}
	})
}

// A resource that cannot work is reported at registration, not as a broken
// screen later.
func TestInvalidResourcesAreRefused(t *testing.T) {
	type NoTag struct{ Whatever string }

	for name, resource := range map[string]Resource{
		"no table":       {Model: Article{}},
		"not a struct":   {Model: "articles", Table: "articles"},
		"bad table name": {Model: Article{}, Table: "articles; DROP TABLE users"},
		"no primary key": {Model: NoTag{}, Table: "things"},
	} {
		panel := New(Config{Authorizer: AllowAll})
		panel.Register(resource)
		if panel.Err() == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}
