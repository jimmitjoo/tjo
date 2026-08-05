package admin

import (
	"fmt"
	"reflect"
	"strings"
	"time"
	"unicode"
)

// FieldKind is how a column is rendered and parsed.
type FieldKind string

const (
	KindText     FieldKind = "text"
	KindLongText FieldKind = "longtext"
	KindNumber   FieldKind = "number"
	KindDecimal  FieldKind = "decimal"
	KindBool     FieldKind = "bool"
	KindTime     FieldKind = "time"
	KindEmail    FieldKind = "email"
	KindURL      FieldKind = "url"
	KindSelect   FieldKind = "select"
	KindFile     FieldKind = "file"
)

// Field is one column of a resource.
type Field struct {
	// Name is the database column.
	Name string

	// Label is what a person sees. Derived from the column unless overridden.
	Label string

	Kind FieldKind

	// PrimaryKey marks the identifying column. Never editable.
	PrimaryKey bool

	// ReadOnly shows the value but does not accept a new one.
	//
	// Enforced when the form is processed, not only when it is drawn. A
	// disabled input is a suggestion to the browser and nothing at all to
	// anything else.
	ReadOnly bool

	// Hidden keeps the column out of every screen.
	Hidden bool

	// Required rejects an empty value.
	Required bool

	// Searchable includes the column in the search box's LIKE.
	Searchable bool

	// Relation is set for a belongs-to column: the values offered are the
	// related table's rows.
	Relation *BelongsTo

	// Choices turns a column into a fixed dropdown.
	Choices []Choice
}

// Choice is one option in a select.
type Choice struct {
	Value string
	Label string
}

// BelongsTo describes a foreign key.
type BelongsTo struct {
	// Table is the table the key points at.
	Table string

	// Key is the column in that table, usually its primary key.
	Key string

	// Label is the column to show a person. An id in a dropdown is a puzzle.
	Label string
}

// HasMany describes rows in another table that point back at this one.
type HasMany struct {
	// Title is the heading above the list.
	Title string

	// Table holds the related rows.
	Table string

	// ForeignKey is the column in Table pointing at this record.
	ForeignKey string

	// Columns are shown, in order. Empty means the first three the table has.
	Columns []string

	// Limit caps how many are listed. Zero means DefaultHasManyLimit.
	Limit int
}

// DefaultHasManyLimit bounds an inline list, because "show every related row"
// is how an admin page for a popular record becomes a database incident.
const DefaultHasManyLimit = 25

// BulkAction is something a person can do to several selected rows at once.
type BulkAction struct {
	// Name appears in the URL and must be unique within the resource.
	Name string

	// Label is the button.
	Label string

	// Confirm asks first. Use it for anything that destroys data.
	Confirm bool

	// Run receives the selected primary keys.
	//
	// Authorization has already run: the panel checks ActionUpdate on every
	// selected record before calling this, so a bulk action cannot be a way
	// around per-record permissions.
	Run func(ctx Context, ids []string) error
}

// Resource is a table exposed in the panel.
//
// The minimum is a model and a table:
//
//	panel.Register(admin.Resource{Model: User{}, Table: "users"})
//
// Everything else -- columns, labels, input types, which fields are searchable
// -- is read off the struct.
type Resource struct {
	// Model is any value of the struct type. Its fields are reflected over;
	// it is never stored or mutated.
	Model any

	// Table is the database table.
	Table string

	// Name is the URL segment and heading. Derived from Table when empty.
	Name string

	// Singular and Plural override the headings.
	Singular string
	Plural   string

	// PrimaryKey defaults to "id".
	PrimaryKey string

	// ListColumns limits the list view. Empty shows every visible field, up to
	// MaxListColumns -- a table with forty columns is not a useful screen.
	ListColumns []string

	// DefaultSort is the column the list is ordered by. Defaults to the
	// primary key, descending.
	DefaultSort string
	DefaultDesc bool

	// PerPage defaults to DefaultPerPage.
	PerPage int

	// HasMany renders related rows on the edit screen.
	HasMany []HasMany

	// BulkActions appear on the list when rows are selected.
	BulkActions []BulkAction

	// FieldOverrides adjusts what reflection worked out. The key is the column.
	FieldOverrides map[string]Field

	// ReadOnly forbids create, update and delete for everyone, regardless of
	// what the authorizer says. For tables that are reports.
	ReadOnly bool

	fields []Field
}

const (
	// DefaultPerPage is a page of rows.
	DefaultPerPage = 25

	// MaxPerPage bounds what a URL may ask for. Without it, ?per_page=1000000
	// is a denial of service one query string long.
	MaxPerPage = 200

	// MaxListColumns keeps a wide table readable, and keeps the row's own
	// actions on screen rather than off the right-hand edge.
	MaxListColumns = 6
)

// sensitiveColumns are hidden unless the application explicitly overrides them.
//
// An admin panel is a screen that renders whatever is in a table, and these
// columns hold credentials. Defaulting them to visible would mean the first
// person to register a users model publishes every password hash to whoever can
// reach the panel -- which is exactly the sort of thing an admin panel is used
// to do by accident.
var sensitiveColumns = map[string]bool{
	"password":       true,
	"password_hash":  true,
	"token":          true,
	"token_hash":     true,
	"secret":         true,
	"totp_secret":    true,
	"remember_token": true,
	"api_key":        true,
	"private_key":    true,
	"session_data":   true,
}

// Fields returns the resource's columns, working them out on first use.
func (r *Resource) Fields() []Field {
	if r.fields == nil {
		r.fields = r.reflectFields()
	}
	return r.fields
}

// Field returns one column by name.
func (r *Resource) Field(name string) (Field, bool) {
	for _, f := range r.Fields() {
		if f.Name == name {
			return f, true
		}
	}
	return Field{}, false
}

// VisibleFields are the ones a screen may show.
func (r *Resource) VisibleFields() []Field {
	var out []Field
	for _, f := range r.Fields() {
		if !f.Hidden {
			out = append(out, f)
		}
	}
	return out
}

// EditableFields are the ones a form may write.
func (r *Resource) EditableFields() []Field {
	var out []Field
	for _, f := range r.VisibleFields() {
		if f.PrimaryKey || f.ReadOnly {
			continue
		}
		out = append(out, f)
	}
	return out
}

// ListFields are the columns of the list view.
func (r *Resource) ListFields() []Field {
	visible := r.VisibleFields()

	if len(r.ListColumns) > 0 {
		var out []Field
		for _, name := range r.ListColumns {
			if f, ok := r.Field(name); ok && !f.Hidden {
				out = append(out, f)
			}
		}
		return out
	}

	if len(visible) > MaxListColumns {
		return visible[:MaxListColumns]
	}
	return visible
}

// SearchFields are the text columns the search box matches against.
func (r *Resource) SearchFields() []Field {
	var out []Field
	for _, f := range r.VisibleFields() {
		if f.Searchable {
			out = append(out, f)
		}
	}
	return out
}

func (r *Resource) key() string {
	if r.PrimaryKey != "" {
		return r.PrimaryKey
	}
	return "id"
}

func (r *Resource) slug() string {
	if r.Name != "" {
		return r.Name
	}
	return r.Table
}

func (r *Resource) title() string {
	if r.Plural != "" {
		return r.Plural
	}
	return humanise(r.slug())
}

func (r *Resource) singular() string {
	if r.Singular != "" {
		return r.Singular
	}
	return strings.TrimSuffix(humanise(r.slug()), "s")
}

func (r *Resource) perPage(requested int) int {
	switch {
	case requested > 0 && requested <= MaxPerPage:
		return requested
	case r.PerPage > 0 && r.PerPage <= MaxPerPage:
		return r.PerPage
	default:
		return DefaultPerPage
	}
}

// reflectFields reads the model struct.
func (r *Resource) reflectFields() []Field {
	t := reflect.TypeOf(r.Model)
	for t != nil && t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct {
		return nil
	}

	var out []Field

	for i := range t.NumField() {
		sf := t.Field(i)
		if !sf.IsExported() {
			continue
		}

		column := columnName(sf)
		if column == "-" {
			continue
		}

		field := Field{
			Name:  column,
			Label: humanise(column),
			Kind:  inferKind(sf.Type, column),
		}

		field.PrimaryKey = column == r.key()
		field.Hidden = sensitiveColumns[column]
		field.Searchable = !field.Hidden && (field.Kind == KindText || field.Kind == KindLongText || field.Kind == KindEmail)

		// A column the database maintains is shown, not edited.
		if column == "created_at" || column == "updated_at" || column == "deleted_at" {
			field.ReadOnly = true
		}

		applyTag(&field, sf.Tag.Get("admin"))

		if override, ok := r.FieldOverrides[column]; ok {
			field = merge(field, override)
		}

		out = append(out, field)
	}

	return out
}

// columnName reads the db tag the framework's models already carry, falling
// back to the Go field name in snake case.
func columnName(sf reflect.StructField) string {
	tag := sf.Tag.Get("db")
	if tag == "" {
		tag = sf.Tag.Get("json")
	}

	name, _, _ := strings.Cut(tag, ",")
	name = strings.TrimSpace(name)

	switch name {
	case "":
		return snake(sf.Name)
	case "-":
		return "-"
	default:
		return name
	}
}

func inferKind(t reflect.Type, column string) FieldKind {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	if t == reflect.TypeOf(time.Time{}) {
		return KindTime
	}

	switch t.Kind() {
	case reflect.Bool:
		return KindBool
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return KindNumber
	case reflect.Float32, reflect.Float64:
		return KindDecimal
	case reflect.String:
		switch {
		case column == "email" || strings.HasSuffix(column, "_email"):
			return KindEmail
		case column == "url" || strings.HasSuffix(column, "_url"):
			return KindURL
		case strings.HasSuffix(column, "_body") || column == "body" ||
			column == "content" || column == "description" || column == "notes":
			return KindLongText
		default:
			return KindText
		}
	default:
		return KindText
	}
}

// applyTag reads `admin:"..."`, a comma-separated list of flags and key=value
// pairs: hidden, readonly, required, show, search, label=..., widget=...,
// belongsTo=table.key:label, choices=a|b|c.
func applyTag(f *Field, tag string) {
	if tag == "" {
		return
	}

	for _, part := range strings.Split(tag, ",") {
		part = strings.TrimSpace(part)
		key, value, hasValue := strings.Cut(part, "=")

		switch {
		case !hasValue && key == "hidden":
			f.Hidden = true
		case !hasValue && key == "show":
			// The opt-in that overrides the sensitive-column default.
			f.Hidden = false
		case !hasValue && key == "readonly":
			f.ReadOnly = true
		case !hasValue && key == "required":
			f.Required = true
		case !hasValue && key == "search":
			f.Searchable = true
		case !hasValue && key == "nosearch":
			f.Searchable = false
		case !hasValue && key == "file":
			f.Kind = KindFile
		case key == "label":
			f.Label = value
		case key == "widget":
			f.Kind = FieldKind(value)
		case key == "choices":
			f.Kind = KindSelect
			for _, c := range strings.Split(value, "|") {
				f.Choices = append(f.Choices, Choice{Value: c, Label: humanise(c)})
			}
		case key == "belongsTo":
			f.Kind = KindSelect
			f.Relation = parseBelongsTo(value)
		}
	}
}

// parseBelongsTo reads "table.key:label", "table.key" or "table".
func parseBelongsTo(spec string) *BelongsTo {
	rest, label, _ := strings.Cut(spec, ":")
	table, key, _ := strings.Cut(rest, ".")

	if key == "" {
		key = "id"
	}
	if label == "" {
		label = key
	}
	return &BelongsTo{Table: table, Key: key, Label: label}
}

// merge lets an override set only what it cares about.
func merge(base, override Field) Field {
	if override.Label != "" {
		base.Label = override.Label
	}
	if override.Kind != "" {
		base.Kind = override.Kind
	}
	if override.Relation != nil {
		base.Relation = override.Relation
	}
	if len(override.Choices) > 0 {
		base.Choices = override.Choices
	}
	// Booleans are taken as written: an override exists to say "no, this one
	// is different", and a false that could not be expressed would make
	// un-hiding a sensitive column impossible.
	base.Hidden = override.Hidden
	base.ReadOnly = override.ReadOnly || base.PrimaryKey
	base.Required = override.Required
	base.Searchable = override.Searchable

	return base
}

// humanise turns a column name into a label.
func humanise(s string) string {
	s = strings.ReplaceAll(s, "_", " ")
	if s == "" {
		return s
	}

	words := strings.Fields(s)
	for i, w := range words {
		switch strings.ToLower(w) {
		case "id":
			words[i] = "ID"
		case "url":
			words[i] = "URL"
		default:
			r := []rune(w)
			r[0] = unicode.ToUpper(r[0])
			words[i] = string(r)
		}
	}
	return strings.Join(words, " ")
}

// snake converts a Go field name to a column name.
func snake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(unicode.ToLower(r))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// validate reports a resource that cannot work, at registration time rather
// than at the first request.
func (r *Resource) validate() error {
	if r.Table == "" {
		return fmt.Errorf("admin: resource %T has no table", r.Model)
	}
	if !safeIdentifier(r.Table) {
		return fmt.Errorf("admin: %q is not a usable table name", r.Table)
	}
	if len(r.Fields()) == 0 {
		return fmt.Errorf("admin: resource %q has no usable fields -- is Model a struct?", r.Table)
	}
	if _, ok := r.Field(r.key()); !ok {
		return fmt.Errorf("admin: resource %q has no %q column; set PrimaryKey", r.Table, r.key())
	}

	for _, f := range r.Fields() {
		if !safeIdentifier(f.Name) {
			return fmt.Errorf("admin: %q is not a usable column name in %q", f.Name, r.Table)
		}
		if f.Relation != nil {
			if !safeIdentifier(f.Relation.Table) || !safeIdentifier(f.Relation.Key) || !safeIdentifier(f.Relation.Label) {
				return fmt.Errorf("admin: relation on %q.%q names an unusable identifier", r.Table, f.Name)
			}
		}
	}

	for _, h := range r.HasMany {
		if !safeIdentifier(h.Table) || !safeIdentifier(h.ForeignKey) {
			return fmt.Errorf("admin: has-many on %q names an unusable identifier", r.Table)
		}
		for _, c := range h.Columns {
			if !safeIdentifier(c) {
				return fmt.Errorf("admin: has-many on %q names an unusable column %q", r.Table, c)
			}
		}
	}

	return nil
}

// safeIdentifier reports whether s can be interpolated into SQL.
//
// Table and column names cannot be parameters, so they are the one thing in
// this package that reaches a query as text. Everything that does is either
// checked here or matched against the resource's own field list first; nothing
// arrives from a request without going through one of the two.
func safeIdentifier(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}
