package database

import (
	"database/sql"
	"fmt"
)

// Model represents a database table with configuration options.
// It provides a type-safe way to define and query database tables
// without runtime reflection.
type Model struct {
	table      string
	primaryKey string
	softDelete bool
}

// NewModel creates a new Model for the given table name.
// The table name is validated to prevent SQL injection.
func NewModel(table string) *Model {
	return &Model{
		table:      table,
		primaryKey: "id",
		softDelete: false,
	}
}

// WithPrimaryKey sets a custom primary key column name.
func (m *Model) WithPrimaryKey(pk string) *Model {
	m.primaryKey = pk
	return m
}

// WithSoftDelete enables soft delete support for this model.
// When enabled, Query() will automatically exclude soft-deleted records
// by adding a WHERE deleted_at IS NULL condition.
func (m *Model) WithSoftDelete() *Model {
	m.softDelete = true
	return m
}

// Table returns the table name.
func (m *Model) Table() string {
	return m.table
}

// PrimaryKey returns the primary key column name.
func (m *Model) PrimaryKey() string {
	return m.primaryKey
}

// HasSoftDelete returns whether soft delete is enabled.
func (m *Model) HasSoftDelete() bool {
	return m.softDelete
}

// Query creates a new QueryBuilder for this model.
// If soft delete is enabled, it automatically adds WHERE deleted_at IS NULL.
func (m *Model) Query(db *sql.DB) *QueryBuilder {
	if !isValidIdentifier(m.table) {
		qb := NewQueryBuilder(db)
		qb.err = fmt.Errorf("invalid table name: %q", m.table)
		return qb
	}

	qb := NewQueryBuilder(db).Table(m.table)

	// Automatically exclude soft-deleted records if enabled
	if m.softDelete {
		qb = qb.WhereNull("deleted_at")
	}

	return qb
}

// Find retrieves a single record by its primary key.
func (m *Model) Find(db *sql.DB, id interface{}) *sql.Row {
	return m.Query(db).Where(m.primaryKey, "=", id).First()
}

// All retrieves all records from the table.
func (m *Model) All(db *sql.DB) (*sql.Rows, error) {
	return m.Query(db).Get()
}

// Create inserts a new record and returns the result.
func (m *Model) Create(db *sql.DB, data map[string]interface{}) (sql.Result, error) {
	return NewQueryBuilder(db).Table(m.table).Insert(data)
}

// Update updates records matching the given ID.
func (m *Model) Update(db *sql.DB, id interface{}, data map[string]interface{}) (sql.Result, error) {
	return NewQueryBuilder(db).Table(m.table).Where(m.primaryKey, "=", id).Update(data)
}

// Delete performs a hard delete for the given ID.
// For soft delete, use SoftDelete instead.
func (m *Model) Delete(db *sql.DB, id interface{}) (sql.Result, error) {
	return NewQueryBuilder(db).Table(m.table).Where(m.primaryKey, "=", id).Delete()
}
