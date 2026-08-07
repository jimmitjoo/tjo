package admin

import (
	"embed"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jimmitjoo/tjo/i18n"
)

//go:embed templates/*.html
var templateFiles embed.FS

// Content is HTML a custom page produced. It is inserted without escaping, so
// a page that renders user input must escape it -- html/template is right there.
type Content = template.HTML

var templates = template.Must(template.New("admin").Funcs(template.FuncMap{
	"display": display,
	"inputValue": func(f Field, v any) string {
		if t, ok := v.(time.Time); ok && f.Kind == KindTime {
			return t.Format("2006-01-02T15:04")
		}
		return display(v)
	},
	"add":  func(a, b int) int { return a + b },
	"itoa": strconv.Itoa,
	"sortURL": func(l listPage, column string) string {
		dir := "asc"
		if l.Params.Sort == column && !l.Params.Desc {
			dir = "desc"
		}
		return l.BaseURL + l.queryString(map[string]string{"sort": column, "dir": dir, "page": ""})
	},
	"pageURL": func(l listPage, page int) string {
		return l.BaseURL + l.queryString(map[string]string{"page": strconv.Itoa(page)})
	},
	"sortIndicator": func(l listPage, column string) string {
		if l.Params.Sort != column {
			return ""
		}
		if l.Params.Desc {
			return "▾"
		}
		return "▴"
	},
	"filterValue": func(l listPage, column string) string { return l.Params.Filters[column] },
	"pageNumbers": pageNumbers,
	"isChecked":   func(v any) bool { return parseBool(display(v)) },
}).ParseFS(templateFiles, "templates/*.html"))

// localised gives a template the request's language.
//
// Embedded in every page struct rather than reached through a function in the
// FuncMap, because html/template binds its FuncMap at parse time and the
// language is per request. `{{.T "admin.new"}}` in a template is a method call
// on the data, which is the shape that works without cloning the template set
// on every request.
type localised struct {
	p *i18n.Printer
}

// T translates a key.
func (l localised) T(key string, args ...any) string { return l.p.T(key, args...) }

// N translates a key with a count, choosing the locale's plural form.
//
// The count is any integer type rather than int, because html/template does
// not convert between them: a page holding a row count as int64 would
// otherwise fail to render with "wrong type for value", at runtime, in the
// template.
func (l localised) N(key string, count any, args ...any) string {
	return l.p.N(key, toInt(count), args...)
}

func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int8:
		return int(n)
	case int16:
		return int(n)
	case int32:
		return int(n)
	case int64:
		return int(n)
	case uint:
		return int(n)
	case uint8:
		return int(n)
	case uint16:
		return int(n)
	case uint32:
		return int(n)
	case uint64:
		return int(n)
	case float32:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

// Label translates a field's heading.
//
// Reflection derives labels with humanise(), which is an English word-splitter:
// `first_name` becomes "First Name" whatever language the operator reads. So
// the derived label is a fallback, and a catalogue may override it per table or
// across the application:
//
//	"admin.field.users.first_name": "Förnamn"
//	"admin.field.email":            "E-post"
//
// The specific key wins, so two tables with a `status` column can disagree
// about what status means without either of them being wrong.
func (l localised) Label(r *Resource, f Field) string {
	for _, key := range []string{
		"admin.field." + r.slug() + "." + f.Name,
		"admin.field." + f.Name,
	} {
		if l.p.Has(key) {
			return l.p.T(key)
		}
	}
	return f.Label
}

// Plural and Singular translate a resource's name, falling back to the one
// derived from the table.
func (l localised) Plural(r *Resource) string {
	if key := "admin.resource." + r.slug(); l.p.Has(key) {
		return l.p.T(key)
	}
	return r.title()
}

func (l localised) Singular(r *Resource) string {
	if key := "admin.resource." + r.slug() + ".singular"; l.p.Has(key) {
		return l.p.T(key)
	}
	return r.singular()
}

// Lang is the BCP 47 tag, for the html lang attribute.
func (l localised) Lang() string { return l.p.Tag().String() }

// Dir is "ltr" or "rtl", for the html dir attribute. An Arabic or Hebrew
// operator gets a mirrored layout rather than a translated left-to-right one.
func (l localised) Dir() string { return string(l.p.Dir()) }

// nav is the sidebar.
type nav struct {
	localised

	Title     string
	Mount     string
	Resources []navItem
	Pages     []navItem
}

type navItem struct {
	Label string
	URL   string
}

type dashboardPage struct {
	localised

	Nav nav
}

type listRow struct {
	ID        string
	Cells     []string
	CanUpdate bool
	CanDelete bool
}

type listPage struct {
	localised

	Nav      nav
	Resource *Resource
	Fields   []Field
	Rows     []listRow
	Params   listParams
	Total    int64
	Pages    int
	CanWrite bool
	BaseURL  string
}

type input struct {
	Field   Field
	Value   any
	Choices []Choice
}

type relatedBlock struct {
	Title   string
	Columns []string
	Rows    []map[string]any
}

type formPage struct {
	localised

	Nav       nav
	Resource  *Resource
	ID        string
	Creating  bool
	Inputs    []input
	Problems  map[string]string
	Related   []relatedBlock
	History   []Entry
	CanDelete bool
	Action    string
	BackURL   string
	HasFiles  bool
}

type customPage struct {
	localised

	Nav   nav
	Title string
	Body  Content
}

func (dashboardPage) templateName() string { return "dashboard.html" }
func (listPage) templateName() string      { return "list.html" }
func (formPage) templateName() string      { return "form.html" }
func (customPage) templateName() string    { return "page.html" }

type page interface{ templateName() string }

func (p *Panel) render(w http.ResponseWriter, r *http.Request, data page) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// An admin panel has no business being framed or indexed.
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.Header().Set("Referrer-Policy", "same-origin")

	if err := templates.ExecuteTemplate(w, data.templateName(), data); err != nil {
		// The response has already begun, so this cannot become a 500. Saying
		// so in the body beats a silently truncated page.
		w.Write([]byte("\n<!-- admin: " + template.HTMLEscapeString(err.Error()) + " -->"))
	}
}

// navFor lists what this visitor may open.
func (p *Panel) navFor(ctx Context) nav {
	out := nav{localised: localised{p: i18n.From(ctx)}, Title: p.title, Mount: p.mount}

	for _, r := range p.Resources() {
		if p.allow(ctx, Query{Action: ActionList, Resource: r.slug()}) != nil {
			continue
		}
		out.Resources = append(out.Resources, navItem{Label: r.title(), URL: p.url("r", r.slug())})
	}

	p.mu.RLock()
	pages := append([]*Page(nil), p.pages...)
	p.mu.RUnlock()

	for _, page := range pages {
		if p.allow(ctx, Query{Action: page.Action, Resource: page.Path}) != nil {
			continue
		}
		out.Pages = append(out.Pages, navItem{Label: page.Title, URL: p.url("p", page.Path)})
	}

	return out
}

// formPage assembles the create or edit screen.
func (p *Panel) formPage(ctx Context, resource *Resource, record map[string]any, id string, values map[string]any, problems map[string]string) (formPage, error) {
	creating := id == ""

	action := ActionUpdate
	if creating {
		action = ActionCreate
	}

	navigation := p.navFor(ctx)

	out := formPage{
		localised: navigation.localised,
		Nav:       navigation,
		Resource:  resource,
		ID:        id,
		Creating:  creating,
		Problems:  problems,
		BackURL:   p.url("r", resource.slug()),
		CanDelete: !creating && !resource.ReadOnly && p.allow(ctx, Query{Action: ActionDelete, Resource: resource.slug(), Record: record}) == nil,
	}

	if creating {
		out.Action = p.url("r", resource.slug())
	} else {
		out.Action = p.url("r", resource.slug(), id)
	}

	for _, field := range resource.VisibleFields() {
		if !p.allowField(ctx, ActionView, resource.slug(), field.Name, record) {
			continue
		}

		// A field the authorizer will not let this person write is shown as it
		// is: readable, not editable.
		if !field.ReadOnly && !field.PrimaryKey && !p.allowField(ctx, action, resource.slug(), field.Name, record) {
			field.ReadOnly = true
		}
		if resource.ReadOnly {
			field.ReadOnly = true
		}

		in := input{Field: field}

		switch {
		case values != nil:
			in.Value = values[field.Name]
		case record != nil:
			in.Value = record[field.Name]
		}

		if field.Kind == KindFile {
			out.HasFiles = true
		}

		if field.Relation != nil {
			choices, err := p.options(ctx, field.Relation)
			if err != nil {
				return out, err
			}
			in.Choices = choices
		} else if len(field.Choices) > 0 {
			in.Choices = field.Choices
		}

		out.Inputs = append(out.Inputs, in)
	}

	if !creating {
		for _, h := range resource.HasMany {
			rows, columns, err := p.related(ctx, h, id)
			if err != nil {
				return out, err
			}
			title := h.Title
			if title == "" {
				title = humanise(h.Table)
			}
			out.Related = append(out.Related, relatedBlock{Title: title, Columns: columns, Rows: rows})
		}

		if p.audit != nil {
			history, err := p.audit.For(ctx, resource.slug(), id, 10)
			if err == nil {
				out.History = history
			}
		}
	}

	return out, nil
}

// renderFormWithProblems redraws a form the person just submitted, keeping what
// they typed.
func (p *Panel) renderFormWithProblems(w http.ResponseWriter, r *http.Request, ctx Context, resource *Resource, values map[string]any, id string, problems map[string]string) {
	var record map[string]any
	if id != "" {
		record, _ = p.find(ctx, resource, id)
	}

	page, err := p.formPage(ctx, resource, record, id, values, problems)
	if err != nil {
		p.fail(w, err)
		return
	}

	w.WriteHeader(http.StatusUnprocessableEntity)
	p.render(w, r, page)
}

// pageNumbers returns the pagination window: the first, the last, and a few
// either side of the current one.
func pageNumbers(current, total int) []int {
	if total <= 1 {
		return nil
	}

	want := map[int]bool{1: true, total: true, current: true}
	for i := 1; i <= 2; i++ {
		if current-i >= 1 {
			want[current-i] = true
		}
		if current+i <= total {
			want[current+i] = true
		}
	}

	var out []int
	for i := 1; i <= total; i++ {
		if want[i] {
			out = append(out, i)
		}
	}
	return out
}

// changeSummary renders an audit entry's changes for the history list.
func (e Entry) Summary() string {
	if len(e.Changes) == 0 {
		return ""
	}

	parts := make([]string, 0, len(e.Changes))
	for column, change := range e.Changes {
		pair, ok := change.([]any)
		if !ok || len(pair) != 2 {
			parts = append(parts, column)
			continue
		}
		parts = append(parts, column+": "+display(pair[0])+" → "+display(pair[1]))
	}

	return strings.Join(parts, ", ")
}
