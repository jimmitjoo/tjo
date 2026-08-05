package admin

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jimmitjoo/tjo/database"
)

// handleDashboard lists the resources this visitor may see.
//
// Filtered by the authorizer rather than listing everything and refusing on
// click: a nav entry for a resource somebody cannot open is a disclosure of
// what exists, and an annoyance.
func (p *Panel) handleDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := p.context(r)

	nav := p.navFor(ctx)
	if len(nav.Resources) == 0 && len(nav.Pages) == 0 {
		p.deny(w, p.allow(ctx, Query{Action: ActionList, Resource: ""}))
		return
	}

	p.render(w, r, dashboardPage{Nav: nav})
}

func (p *Panel) handleList(w http.ResponseWriter, r *http.Request) {
	ctx := p.context(r)

	resource := p.resource(r.PathValue("resource"))
	if resource == nil {
		p.deny(w, nil)
		return
	}
	if err := p.allow(ctx, Query{Action: ActionList, Resource: resource.slug()}); err != nil {
		p.deny(w, err)
		return
	}

	params := parseListParams(resource, r.URL.Query())

	rows, total, err := p.list(ctx, resource, params)
	if err != nil {
		p.fail(w, err)
		return
	}

	// Per-record authorization, applied to the page that was fetched. Filtering
	// after the query is the honest version: the alternative is asking the
	// authorizer to express itself as SQL, which most cannot, and a count that
	// includes rows the viewer may not see is a smaller lie than a screen that
	// shows them.
	visible := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if p.allow(ctx, Query{Action: ActionView, Resource: resource.slug(), Record: row}) == nil {
			visible = append(visible, row)
		}
	}

	fields := p.readableFields(ctx, resource, resource.ListFields(), nil)

	// A belongs-to column holds an id, and an id in a table is the same puzzle
	// it is in a dropdown. The related labels are loaded once per page rather
	// than once per row, which is the difference between two queries and
	// twenty-six.
	labels := map[string]map[string]string{}
	for _, f := range fields {
		if f.Relation == nil {
			continue
		}
		choices, err := p.options(ctx, f.Relation)
		if err != nil {
			continue
		}
		lookup := make(map[string]string, len(choices))
		for _, c := range choices {
			lookup[c.Value] = c.Label
		}
		labels[f.Name] = lookup
	}

	page := listPage{
		Nav:      p.navFor(ctx),
		Resource: resource,
		Fields:   fields,
		Rows:     make([]listRow, 0, len(visible)),
		Params:   params,
		Total:    total,
		Pages:    pageCount(total, params.PerPage),
		CanWrite: !resource.ReadOnly && p.allow(ctx, Query{Action: ActionCreate, Resource: resource.slug()}) == nil,
		BaseURL:  p.url("r", resource.slug()),
	}

	for _, row := range visible {
		cells := make([]string, 0, len(fields))
		for _, f := range fields {
			value := display(row[f.Name])
			if lookup, ok := labels[f.Name]; ok {
				if label, found := lookup[value]; found && label != "" {
					value = label
				}
			}
			cells = append(cells, value)
		}
		page.Rows = append(page.Rows, listRow{
			ID:        display(row[resource.key()]),
			Cells:     cells,
			CanUpdate: !resource.ReadOnly && p.allow(ctx, Query{Action: ActionUpdate, Resource: resource.slug(), Record: row}) == nil,
			CanDelete: !resource.ReadOnly && p.allow(ctx, Query{Action: ActionDelete, Resource: resource.slug(), Record: row}) == nil,
		})
	}

	p.render(w, r, page)
}

// handleForm renders the create or edit screen.
func (p *Panel) handleForm(w http.ResponseWriter, r *http.Request) {
	ctx := p.context(r)

	resource := p.resource(r.PathValue("resource"))
	if resource == nil {
		p.deny(w, nil)
		return
	}

	id := r.PathValue("id")
	creating := id == ""

	var (
		record map[string]any
		err    error
	)

	if creating {
		if resource.ReadOnly {
			p.deny(w, nil)
			return
		}
		if err := p.allow(ctx, Query{Action: ActionCreate, Resource: resource.slug()}); err != nil {
			p.deny(w, err)
			return
		}
	} else {
		record, err = p.find(ctx, resource, id)
		if err != nil {
			p.fail(w, err)
			return
		}
		if record == nil {
			p.deny(w, nil)
			return
		}
		if err := p.allow(ctx, Query{Action: ActionView, Resource: resource.slug(), Record: record}); err != nil {
			p.deny(w, err)
			return
		}
	}

	page, err := p.formPage(ctx, resource, record, id, nil, nil)
	if err != nil {
		p.fail(w, err)
		return
	}

	p.render(w, r, page)
}

func (p *Panel) handleCreate(w http.ResponseWriter, r *http.Request) {
	ctx := p.context(r)

	resource := p.resource(r.PathValue("resource"))
	if resource == nil {
		p.deny(w, nil)
		return
	}
	if resource.ReadOnly {
		p.deny(w, nil)
		return
	}
	if err := p.allow(ctx, Query{Action: ActionCreate, Resource: resource.slug()}); err != nil {
		p.deny(w, err)
		return
	}

	values, problems, err := p.readForm(ctx, r, resource, nil)
	if err != nil {
		p.fail(w, err)
		return
	}

	if len(problems) > 0 {
		p.renderFormWithProblems(w, r, ctx, resource, values, "", problems)
		return
	}

	result, err := database.NewQueryBuilder(p.db).WithDialect(p.dialect).
		Table(resource.Table).Insert(values)
	if err != nil {
		p.renderFormWithProblems(w, r, ctx, resource, values, "", map[string]string{"": err.Error()})
		return
	}

	id := ""
	if inserted, idErr := result.LastInsertId(); idErr == nil && inserted > 0 {
		id = strconv.FormatInt(inserted, 10)
	} else if v, ok := values[resource.key()]; ok {
		id = display(v)
	}

	p.record(ctx, ActionCreate, resource, id, diff(nil, values, resource.Fields()))

	http.Redirect(w, r, p.url("r", resource.slug()), http.StatusSeeOther)
}

func (p *Panel) handleUpdate(w http.ResponseWriter, r *http.Request) {
	ctx := p.context(r)

	resource := p.resource(r.PathValue("resource"))
	if resource == nil {
		p.deny(w, nil)
		return
	}

	id := r.PathValue("id")

	existing, err := p.find(ctx, resource, id)
	if err != nil {
		p.fail(w, err)
		return
	}
	if existing == nil {
		p.deny(w, nil)
		return
	}
	if resource.ReadOnly {
		p.deny(w, nil)
		return
	}

	// The record is loaded before the check, because a per-record rule cannot
	// be evaluated without the record. This is the difference between hiding
	// the button and enforcing the permission.
	if err := p.allow(ctx, Query{Action: ActionUpdate, Resource: resource.slug(), Record: existing}); err != nil {
		p.deny(w, err)
		return
	}

	values, problems, err := p.readForm(ctx, r, resource, existing)
	if err != nil {
		p.fail(w, err)
		return
	}

	if len(problems) > 0 {
		p.renderFormWithProblems(w, r, ctx, resource, values, id, problems)
		return
	}

	if len(values) > 0 {
		_, err = database.NewQueryBuilder(p.db).WithDialect(p.dialect).
			Table(resource.Table).Where(resource.key(), "=", id).Update(values)
		if err != nil {
			p.renderFormWithProblems(w, r, ctx, resource, values, id, map[string]string{"": err.Error()})
			return
		}
	}

	p.record(ctx, ActionUpdate, resource, id, diff(existing, values, resource.Fields()))

	http.Redirect(w, r, p.url("r", resource.slug()), http.StatusSeeOther)
}

func (p *Panel) handleDelete(w http.ResponseWriter, r *http.Request) {
	ctx := p.context(r)

	resource := p.resource(r.PathValue("resource"))
	if resource == nil {
		p.deny(w, nil)
		return
	}

	id := r.PathValue("id")

	existing, err := p.find(ctx, resource, id)
	if err != nil {
		p.fail(w, err)
		return
	}
	if existing == nil {
		p.deny(w, nil)
		return
	}
	if resource.ReadOnly {
		p.deny(w, nil)
		return
	}
	if err := p.allow(ctx, Query{Action: ActionDelete, Resource: resource.slug(), Record: existing}); err != nil {
		p.deny(w, err)
		return
	}

	if _, err := database.NewQueryBuilder(p.db).WithDialect(p.dialect).
		Table(resource.Table).Where(resource.key(), "=", id).Delete(); err != nil {
		p.fail(w, err)
		return
	}

	p.record(ctx, ActionDelete, resource, id, diff(existing, nil, resource.Fields()))

	http.Redirect(w, r, p.url("r", resource.slug()), http.StatusSeeOther)
}

// handleBulk runs an action over the selected rows.
func (p *Panel) handleBulk(w http.ResponseWriter, r *http.Request) {
	ctx := p.context(r)

	resource := p.resource(r.PathValue("resource"))
	if resource == nil || resource.ReadOnly {
		p.deny(w, nil)
		return
	}
	if err := r.ParseForm(); err != nil {
		p.fail(w, err)
		return
	}

	name := r.Form.Get("action")
	ids := r.Form["ids"]

	if len(ids) == 0 {
		http.Redirect(w, r, p.url("r", resource.slug()), http.StatusSeeOther)
		return
	}
	if len(ids) > MaxPerPage {
		// The form can only offer a page's worth, so more than that is not a
		// person clicking checkboxes.
		p.deny(w, nil)
		return
	}

	action := ActionDelete
	var custom *BulkAction

	if name != "delete" {
		for i := range resource.BulkActions {
			if resource.BulkActions[i].Name == name {
				custom = &resource.BulkActions[i]
				action = ActionUpdate
				break
			}
		}
		if custom == nil {
			p.deny(w, nil)
			return
		}
	}

	// Every selected record is authorized individually before anything runs.
	// A bulk action that checked once would be a way around per-record
	// permissions -- tick the box next to a row you may not touch.
	for _, id := range ids {
		existing, err := p.find(ctx, resource, id)
		if err != nil {
			p.fail(w, err)
			return
		}
		if existing == nil {
			p.deny(w, nil)
			return
		}
		if err := p.allow(ctx, Query{Action: action, Resource: resource.slug(), Record: existing}); err != nil {
			p.deny(w, err)
			return
		}
	}

	if custom != nil {
		if err := custom.Run(ctx, ids); err != nil {
			p.fail(w, err)
			return
		}
		for _, id := range ids {
			p.record(ctx, ActionUpdate, resource, id, map[string]any{"bulk_action": custom.Name})
		}
	} else {
		values := make([]any, 0, len(ids))
		for _, id := range ids {
			values = append(values, id)
		}
		if _, err := database.NewQueryBuilder(p.db).WithDialect(p.dialect).
			Table(resource.Table).WhereIn(resource.key(), values).Delete(); err != nil {
			p.fail(w, err)
			return
		}
		for _, id := range ids {
			p.record(ctx, ActionDelete, resource, id, map[string]any{"bulk": true})
		}
	}

	http.Redirect(w, r, p.url("r", resource.slug()), http.StatusSeeOther)
}

func (p *Panel) handlePage(w http.ResponseWriter, r *http.Request) {
	ctx := p.context(r)

	page := p.page(r.PathValue("page"))
	if page == nil {
		p.deny(w, nil)
		return
	}
	if err := p.allow(ctx, Query{Action: page.Action, Resource: page.Path}); err != nil {
		p.deny(w, err)
		return
	}

	if page.Handler != nil {
		page.Handler.ServeHTTP(w, r)
		return
	}
	if page.Body == nil {
		p.fail(w, fmt.Errorf("admin: page %q has neither Body nor Handler", page.Path))
		return
	}

	body, err := page.Body(ctx)
	if err != nil {
		p.fail(w, err)
		return
	}

	p.render(w, r, customPage{Nav: p.navFor(ctx), Title: page.Title, Body: body})
}

// readForm turns a submitted form into column values.
//
// Only fields that are editable *and* permitted are read. A field the
// authorizer refuses is not merely absent from the form -- it is ignored when
// it arrives, because a form is a suggestion and a POST body is whatever the
// client felt like sending.
func (p *Panel) readForm(ctx Context, r *http.Request, resource *Resource, existing map[string]any) (map[string]any, map[string]string, error) {
	if err := r.ParseMultipartForm(MaxUploadBytes); err != nil && !strings.Contains(err.Error(), "not multipart") {
		if err := r.ParseForm(); err != nil {
			return nil, nil, err
		}
	}

	action := ActionCreate
	if existing != nil {
		action = ActionUpdate
	}

	values := map[string]any{}
	problems := map[string]string{}

	for _, field := range resource.EditableFields() {
		if !p.allowField(ctx, action, resource.slug(), field.Name, existing) {
			continue
		}

		if field.Kind == KindFile {
			value, err := p.readFile(ctx, r, field)
			if err != nil {
				problems[field.Name] = err.Error()
				continue
			}
			if value == "" {
				// No new file: leave whatever is stored alone rather than
				// blanking it, which is what an empty file input means.
				continue
			}
			values[field.Name] = value
			continue
		}

		raw := strings.TrimSpace(r.Form.Get(field.Name))

		if field.Kind == KindBool {
			values[field.Name] = parseBool(raw) || r.Form.Get(field.Name) == "on"
			continue
		}

		if raw == "" {
			if field.Required {
				problems[field.Name] = field.Label + " is required"
				continue
			}
			values[field.Name] = nil
			continue
		}

		value, err := coerce(field, raw)
		if err != nil {
			problems[field.Name] = err.Error()
			continue
		}
		values[field.Name] = value
	}

	return values, problems, nil
}

func (p *Panel) readFile(ctx Context, r *http.Request, field Field) (string, error) {
	if r.MultipartForm == nil {
		return strings.TrimSpace(r.Form.Get(field.Name)), nil
	}

	file, header, err := r.FormFile(field.Name)
	if err != nil {
		// No file chosen. Fall back to whatever the text input carried, which
		// is how a path is edited when there is no uploader configured.
		return strings.TrimSpace(r.Form.Get(field.Name)), nil
	}
	defer file.Close()

	if p.uploads == nil {
		return "", fmt.Errorf("admin: no uploader is configured, so %s cannot accept a file", field.Label)
	}

	return p.uploads.Upload(ctx, field.Name, file, header)
}

// coerce converts a form string to the type the column wants.
func coerce(field Field, raw string) (any, error) {
	switch field.Kind {
	case KindNumber:
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%s must be a whole number", field.Label)
		}
		return n, nil

	case KindDecimal:
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, fmt.Errorf("%s must be a number", field.Label)
		}
		return f, nil

	case KindTime:
		for _, layout := range []string{"2006-01-02T15:04", "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02"} {
			if t, err := time.Parse(layout, raw); err == nil {
				return t.UTC(), nil
			}
		}
		return nil, fmt.Errorf("%s must be a date and time", field.Label)

	default:
		return raw, nil
	}
}

// record writes an audit entry, and does not fail the request if it cannot.
//
// A trail that could block a delete would be a trail people turn off.
func (p *Panel) record(ctx Context, action Action, resource *Resource, id string, changes map[string]any) {
	if p.audit == nil {
		return
	}

	actor := ""
	if p.actor != nil {
		actor = p.actor(ctx)
	}

	_ = p.audit.Record(ctx, Entry{
		Actor:    actor,
		Action:   action,
		Resource: resource.slug(),
		RecordID: id,
		Changes:  changes,
	})
}

// readableFields drops the columns the authorizer refuses for this record.
func (p *Panel) readableFields(ctx Context, resource *Resource, fields []Field, record map[string]any) []Field {
	out := make([]Field, 0, len(fields))
	for _, f := range fields {
		if p.allowField(ctx, ActionView, resource.slug(), f.Name, record) {
			out = append(out, f)
		}
	}
	return out
}

func pageCount(total int64, perPage int) int {
	if perPage <= 0 {
		return 1
	}
	pages := int((total + int64(perPage) - 1) / int64(perPage))
	if pages < 1 {
		return 1
	}
	return pages
}

// queryString rebuilds the list URL with one parameter changed, so paging and
// sorting keep the filters.
func (l listPage) queryString(overrides map[string]string) string {
	q := url.Values{}

	if l.Params.Search != "" {
		q.Set("q", l.Params.Search)
	}
	if l.Params.Sort != "" {
		q.Set("sort", l.Params.Sort)
		if l.Params.Desc {
			q.Set("dir", "desc")
		}
	}
	if l.Params.Page > 1 {
		q.Set("page", strconv.Itoa(l.Params.Page))
	}
	for column, value := range l.Params.Filters {
		q.Set("f_"+column, value)
	}

	for key, value := range overrides {
		if value == "" {
			q.Del(key)
			continue
		}
		q.Set(key, value)
	}

	if len(q) == 0 {
		return ""
	}
	return "?" + q.Encode()
}
