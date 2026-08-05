package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jimmitjoo/tjo/database"
)

// Audit records who changed what.
//
// The question an admin panel eventually produces is "who deleted that", and it
// can only be answered if the answer was written down before it was asked. One
// row per write, with the changed columns and nothing else -- storing whole
// records would turn the trail into a second copy of every table, including the
// columns this package refuses to display.
type Audit struct {
	db      *sql.DB
	driver  string
	dialect database.Dialect
	table   string

	// Retain bounds how long entries are kept. Zero keeps them forever, which
	// is a decision rather than a default: an audit trail that a cleanup job
	// truncates is not one.
	Retain time.Duration
}

// NewAudit returns an audit trail on the default table.
//
// driver is the DATABASE_TYPE value, because the DDL below differs between
// MySQL and SQLite in ways the placeholder dialect does not distinguish -- both
// use ? and only one of them spells it AUTOINCREMENT.
func NewAudit(db *sql.DB, driver string) *Audit {
	return &Audit{
		db:      db,
		driver:  normaliseDriver(driver),
		dialect: database.DialectFor(driver),
		table:   "tjo_admin_audit",
	}
}

// normaliseDriver maps the DATABASE_TYPE values the framework accepts onto the
// three schema flavours.
func normaliseDriver(driver string) string {
	switch strings.ToLower(strings.TrimSpace(driver)) {
	case "postgres", "postgresql", "pgx":
		return "postgres"
	case "mysql", "mariadb":
		return "mysql"
	default:
		return "sqlite"
	}
}

// WithTable overrides the table name.
func (a *Audit) WithTable(name string) *Audit {
	a.table = name
	return a
}

// Entry is one recorded change.
type Entry struct {
	At       time.Time
	Actor    string
	Action   Action
	Resource string
	RecordID string

	// Changes holds only what differed, as column -> [before, after]. A create
	// records the values it set; a delete records the record as it was.
	Changes map[string]any
}

// Migrate creates the table if it does not exist.
func (a *Audit) Migrate(ctx context.Context) error {
	if a == nil || a.db == nil {
		return nil
	}

	var ddl string
	switch a.driver {
	case "postgres":
		ddl = `CREATE TABLE IF NOT EXISTS ` + a.table + ` (
			id        BIGSERIAL PRIMARY KEY,
			at        TIMESTAMPTZ NOT NULL,
			actor     TEXT,
			action    TEXT NOT NULL,
			resource  TEXT NOT NULL,
			record_id TEXT,
			changes   TEXT
		)`
	case "mysql":
		ddl = `CREATE TABLE IF NOT EXISTS ` + a.table + ` (
			id        BIGINT AUTO_INCREMENT PRIMARY KEY,
			at        DATETIME NOT NULL,
			actor     VARCHAR(191),
			action    VARCHAR(32) NOT NULL,
			resource  VARCHAR(191) NOT NULL,
			record_id VARCHAR(191),
			changes   TEXT
		)`
	default:
		ddl = `CREATE TABLE IF NOT EXISTS ` + a.table + ` (
			id        INTEGER PRIMARY KEY AUTOINCREMENT,
			at        DATETIME NOT NULL,
			actor     TEXT,
			action    TEXT NOT NULL,
			resource  TEXT NOT NULL,
			record_id TEXT,
			changes   TEXT
		)`
	}

	if _, err := a.db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("admin: migrate audit: %w", err)
	}

	// The trail is read by record ("what happened to this row") far more often
	// than by anything else.
	_, err := a.db.ExecContext(ctx,
		`CREATE INDEX IF NOT EXISTS `+a.table+`_record ON `+a.table+` (resource, record_id)`)
	return err
}

// Record writes an entry.
func (a *Audit) Record(ctx context.Context, e Entry) error {
	if a == nil || a.db == nil {
		return nil
	}

	changes, err := json.Marshal(e.Changes)
	if err != nil {
		return err
	}

	query := `INSERT INTO ` + a.table + ` (at, actor, action, resource, record_id, changes) VALUES (?, ?, ?, ?, ?, ?)`
	if a.dialect == database.DialectDollar {
		query = `INSERT INTO ` + a.table + ` (at, actor, action, resource, record_id, changes) VALUES ($1, $2, $3, $4, $5, $6)`
	}

	_, err = a.db.ExecContext(ctx, query,
		time.Now().UTC(), e.Actor, string(e.Action), e.Resource, e.RecordID, string(changes))
	return err
}

// For returns the entries about one record, newest first.
func (a *Audit) For(ctx context.Context, resource, recordID string, limit int) ([]Entry, error) {
	if a == nil || a.db == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}

	query := `SELECT at, actor, action, record_id, changes FROM ` + a.table +
		` WHERE resource = ? AND record_id = ? ORDER BY at DESC LIMIT ?`
	if a.dialect == database.DialectDollar {
		query = `SELECT at, actor, action, record_id, changes FROM ` + a.table +
			` WHERE resource = $1 AND record_id = $2 ORDER BY at DESC LIMIT $3`
	}

	rows, err := a.db.QueryContext(ctx, query, resource, recordID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Entry
	for rows.Next() {
		var (
			e       Entry
			actor   sql.NullString
			changes sql.NullString
			action  string
		)
		if err := rows.Scan(&e.At, &actor, &action, &e.RecordID, &changes); err != nil {
			return nil, err
		}
		e.Actor = actor.String
		e.Action = Action(action)
		if changes.String != "" {
			_ = json.Unmarshal([]byte(changes.String), &e.Changes)
		}
		e.Resource = resource
		out = append(out, e)
	}
	return out, rows.Err()
}

// Prune deletes entries older than Retain. Run it from the scheduler.
func (a *Audit) Prune(ctx context.Context) (int64, error) {
	if a == nil || a.db == nil || a.Retain <= 0 {
		return 0, nil
	}

	query := `DELETE FROM ` + a.table + ` WHERE at < ?`
	if a.dialect == database.DialectDollar {
		query = `DELETE FROM ` + a.table + ` WHERE at < $1`
	}

	res, err := a.db.ExecContext(ctx, query, time.Now().UTC().Add(-a.Retain))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// diff reports what changed between two versions of a record.
//
// Only the columns that differ, and only the ones the panel was allowed to
// show: a trail that recorded every column would hold copies of the password
// hashes this package goes out of its way not to display.
func diff(before, after map[string]any, fields []Field) map[string]any {
	changes := map[string]any{}

	for _, f := range fields {
		if f.Hidden {
			continue
		}

		old, hadOld := before[f.Name]
		nw, hasNew := after[f.Name]

		switch {
		case hadOld && hasNew:
			if fmt.Sprint(old) != fmt.Sprint(nw) {
				changes[f.Name] = []any{fmt.Sprint(old), fmt.Sprint(nw)}
			}
		case hasNew:
			changes[f.Name] = []any{nil, fmt.Sprint(nw)}
		case hadOld:
			changes[f.Name] = []any{fmt.Sprint(old), nil}
		}
	}

	return changes
}
