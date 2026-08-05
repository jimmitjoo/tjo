package admin

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jimmitjoo/tjo/database"
)

// listParams is a decoded list request.
//
// Every column named here has been matched against the resource's own fields
// before it gets this far. Column names cannot be query parameters, so they are
// the one thing that reaches SQL as text; matching them against a known list is
// what makes ?sort=id;DROP+TABLE a 400 rather than an incident.
type listParams struct {
	Page    int
	PerPage int
	Sort    string
	Desc    bool
	Search  string
	Filters map[string]string
}

func parseListParams(r *Resource, q url.Values) listParams {
	p := listParams{
		Page:    1,
		PerPage: r.perPage(atoiOr(q.Get("per_page"), 0)),
		Search:  strings.TrimSpace(q.Get("q")),
		Filters: map[string]string{},
	}

	if n := atoiOr(q.Get("page"), 1); n > 0 {
		p.Page = n
	}

	// Sort: only a column this resource actually has.
	if sort := q.Get("sort"); sort != "" {
		if f, ok := r.Field(sort); ok && !f.Hidden {
			p.Sort = f.Name
		}
	}
	if p.Sort == "" {
		p.Sort = r.DefaultSort
	}
	if p.Sort == "" {
		p.Sort = r.key()
		p.Desc = r.DefaultDesc || q.Get("dir") == ""
	}
	if dir := q.Get("dir"); dir != "" {
		p.Desc = strings.EqualFold(dir, "desc")
	}

	// Filters arrive as f_<column>=value, and unknown columns are dropped
	// rather than passed through.
	for key, values := range q {
		column, found := strings.CutPrefix(key, "f_")
		if !found || len(values) == 0 || values[0] == "" {
			continue
		}
		if f, ok := r.Field(column); ok && !f.Hidden {
			p.Filters[f.Name] = values[0]
		}
	}

	return p
}

// query builds the list query with the filters applied but no paging, so the
// same conditions can be counted and fetched.
func (p *Panel) query(r *Resource, params listParams) *database.QueryBuilder {
	qb := database.NewQueryBuilder(p.db).WithDialect(p.dialect).Table(r.Table)

	for column, value := range params.Filters {
		field, _ := r.Field(column)

		switch field.Kind {
		case KindText, KindLongText, KindEmail, KindURL:
			qb = qb.Where(column, "LIKE", "%"+value+"%")
		case KindBool:
			qb = qb.Where(column, "=", parseBool(value))
		default:
			qb = qb.Where(column, "=", value)
		}
	}

	if params.Search != "" {
		searchable := r.SearchFields()
		for i, f := range searchable {
			if i == 0 {
				qb = qb.Where(f.Name, "LIKE", "%"+params.Search+"%")
				continue
			}
			qb = qb.OrWhere(f.Name, "LIKE", "%"+params.Search+"%")
		}
	}

	return qb
}

// list fetches one page and the total.
func (p *Panel) list(ctx context.Context, r *Resource, params listParams) (rows []map[string]any, total int64, err error) {
	total, err = p.query(r, params).Count()
	if err != nil {
		return nil, 0, err
	}

	direction := "ASC"
	if params.Desc {
		direction = "DESC"
	}

	result, err := p.query(r, params).
		OrderBy(params.Sort, direction).
		Paginate(params.Page, params.PerPage).
		Get()
	if err != nil {
		return nil, 0, err
	}
	defer result.Close()

	rows, err = scanRows(result)
	return rows, total, err
}

// find returns one record, or (nil, nil) when there is none.
func (p *Panel) find(ctx context.Context, r *Resource, id string) (map[string]any, error) {
	result, err := database.NewQueryBuilder(p.db).WithDialect(p.dialect).
		Table(r.Table).Where(r.key(), "=", id).Limit(1).Get()
	if err != nil {
		return nil, err
	}
	defer result.Close()

	rows, err := scanRows(result)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

// options loads the choices for a belongs-to select.
//
// Bounded: a dropdown of every row in a million-row table is a page that never
// finishes loading, and the honest fix for that is a picker, not a bigger limit.
func (p *Panel) options(ctx context.Context, rel *BelongsTo) ([]Choice, error) {
	const limit = 500

	result, err := database.NewQueryBuilder(p.db).WithDialect(p.dialect).
		Table(rel.Table).Select(rel.Key, rel.Label).OrderBy(rel.Label, "ASC").Limit(limit).Get()
	if err != nil {
		return nil, err
	}
	defer result.Close()

	rows, err := scanRows(result)
	if err != nil {
		return nil, err
	}

	out := make([]Choice, 0, len(rows))
	for _, row := range rows {
		value := display(row[rel.Key])
		label := display(row[rel.Label])
		if label == "" {
			label = value
		}
		out = append(out, Choice{Value: value, Label: label})
	}
	return out, nil
}

// related loads the rows of a has-many block.
//
// The columns are chosen rather than selected with *, because * on a table
// nobody described is how an inline list of "the user's sessions" ends up
// printing a column of session tokens next to them.
func (p *Panel) related(ctx context.Context, h HasMany, id string) ([]map[string]any, []string, error) {
	limit := h.Limit
	if limit <= 0 {
		limit = DefaultHasManyLimit
	}

	columns := h.Columns
	if len(columns) == 0 {
		// A registered resource for the same table already knows which of its
		// columns are fit to show.
		if related := p.resource(h.Table); related != nil {
			for _, f := range related.ListFields() {
				columns = append(columns, f.Name)
			}
		}
	}

	qb := database.NewQueryBuilder(p.db).WithDialect(p.dialect).Table(h.Table)
	if len(columns) > 0 {
		qb = qb.Select(columns...)
	}

	result, err := qb.Where(h.ForeignKey, "=", id).Limit(limit).Get()
	if err != nil {
		return nil, nil, err
	}
	defer result.Close()

	found, err := result.Columns()
	if err != nil {
		return nil, nil, err
	}

	rows, err := scanRows(result)
	if err != nil {
		return nil, nil, err
	}

	// Last defence, for a table this panel knows nothing about: a column whose
	// name says it holds a credential is dropped whatever the query returned.
	safe := make([]string, 0, len(found))
	for _, column := range found {
		if sensitiveColumns[column] {
			for _, row := range rows {
				delete(row, column)
			}
			continue
		}
		safe = append(safe, column)
	}

	return rows, safe, nil
}

// scanRows turns a result set into maps, which is what a panel that does not
// know its tables at compile time has to work with.
func scanRows(rows *sql.Rows) ([]map[string]any, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var out []map[string]any

	for rows.Next() {
		cells := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range cells {
			pointers[i] = &cells[i]
		}

		if err := rows.Scan(pointers...); err != nil {
			return nil, err
		}

		record := make(map[string]any, len(columns))
		for i, column := range columns {
			record[column] = normalise(cells[i])
		}
		out = append(out, record)
	}

	return out, rows.Err()
}

// normalise turns driver types into ones a template can render.
//
// []byte in particular: every string column comes back as bytes from some
// drivers and as a string from others, and a template printing the first
// renders "[104 101 108 108 111]".
func normalise(v any) any {
	switch value := v.(type) {
	case []byte:
		return string(value)
	default:
		return v
	}
}

// display renders a cell as text.
func display(v any) string {
	switch value := v.(type) {
	case nil:
		return ""
	case string:
		return value
	case []byte:
		return string(value)
	case time.Time:
		return value.Format("2006-01-02 15:04")
	case bool:
		if value {
			return "yes"
		}
		return "no"
	default:
		return fmt.Sprint(value)
	}
}

func atoiOr(s string, fallback int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return fallback
}

func parseBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
