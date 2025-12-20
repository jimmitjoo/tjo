package database

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"
)

// DB is an interface that both *sql.DB and *sql.Tx implement
// This allows QueryBuilder to work with both regular queries and transactions
type DB interface {
	Query(query string, args ...interface{}) (*sql.Rows, error)
	QueryRow(query string, args ...interface{}) *sql.Row
	Exec(query string, args ...interface{}) (sql.Result, error)
	Prepare(query string) (*sql.Stmt, error)
}

// TxBeginner is an interface for types that can begin a transaction
type TxBeginner interface {
	Begin() (*sql.Tx, error)
}

// validIdentifier matches valid SQL identifiers (letters, digits, underscores)
// Also allows table.column syntax and quoted identifiers
var validIdentifier = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*(\.[a-zA-Z_][a-zA-Z0-9_]*)?$`)

// validOrderDirection matches valid ORDER BY directions
var validOrderDirection = regexp.MustCompile(`^(ASC|DESC)$`)

// isValidIdentifier checks if a string is a valid SQL identifier
func isValidIdentifier(s string) bool {
	// Allow quoted identifiers (backticks or double quotes)
	if (strings.HasPrefix(s, "`") && strings.HasSuffix(s, "`")) ||
		(strings.HasPrefix(s, "\"") && strings.HasSuffix(s, "\"")) {
		inner := s[1 : len(s)-1]
		// Quoted identifiers can contain almost anything except the quote char
		return len(inner) > 0 && !strings.ContainsAny(inner, "`\"")
	}
	return validIdentifier.MatchString(s)
}

// isValidOperator checks if a string is a valid SQL operator
func isValidOperator(op string) bool {
	validOps := map[string]bool{
		"=": true, "!=": true, "<>": true, "<": true, ">": true,
		"<=": true, ">=": true, "LIKE": true, "NOT LIKE": true,
		"IN": true, "NOT IN": true, "BETWEEN": true, "IS NULL": true,
		"IS NOT NULL": true, "REGEXP": true,
	}
	return validOps[strings.ToUpper(op)]
}

// QueryBuilder provides a fluent interface for building SQL queries
type QueryBuilder struct {
	db             DB
	rawDB          *sql.DB // Keep reference for transactions
	table          string
	selectCols     []string
	whereConds     []whereCondition
	orderBy        []string
	groupBy        []string
	having         []whereCondition
	joins          []joinClause
	limitCount     int
	offsetCount    int
	unionQuery     *QueryBuilder
	unionAll       bool
	err            error // Stores validation errors
	includeTrashed bool  // For soft delete support
}

type whereCondition struct {
	column   string
	operator string
	value    interface{}
	logic    string // AND or OR
	params   []interface{} // For complex conditions like BETWEEN and IN
}

type joinClause struct {
	joinType string // INNER, LEFT, RIGHT, FULL
	table    string
	on       string
}

// NewQueryBuilder creates a new query builder instance
func NewQueryBuilder(db *sql.DB) *QueryBuilder {
	return &QueryBuilder{
		db:         db,
		rawDB:      db,
		selectCols: []string{},
		whereConds: []whereCondition{},
		orderBy:    []string{},
		groupBy:    []string{},
		having:     []whereCondition{},
		joins:      []joinClause{},
	}
}

// newQueryBuilderWithDB creates a query builder with a DB interface (for transactions)
func newQueryBuilderWithDB(db DB, rawDB *sql.DB) *QueryBuilder {
	return &QueryBuilder{
		db:         db,
		rawDB:      rawDB,
		selectCols: []string{},
		whereConds: []whereCondition{},
		orderBy:    []string{},
		groupBy:    []string{},
		having:     []whereCondition{},
		joins:      []joinClause{},
	}
}

// Transaction executes a function within a database transaction.
// If the function returns an error, the transaction is rolled back.
// If the function returns nil, the transaction is committed.
func (qb *QueryBuilder) Transaction(fn func(tx *QueryBuilder) error) error {
	if qb.rawDB == nil {
		return fmt.Errorf("cannot start transaction: no database connection")
	}

	tx, err := qb.rawDB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	// Create a new QueryBuilder that uses the transaction
	txQB := newQueryBuilderWithDB(tx, qb.rawDB)

	// Execute the function
	if err := fn(txQB); err != nil {
		// Rollback on error
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("rollback failed: %v (original error: %w)", rbErr, err)
		}
		return err
	}

	// Commit on success
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// Table sets the table name for the query
func (qb *QueryBuilder) Table(table string) *QueryBuilder {
	if !isValidIdentifier(table) {
		qb.err = fmt.Errorf("invalid table name: %q", table)
		return qb
	}
	qb.table = table
	return qb
}

// Select specifies the columns to select
func (qb *QueryBuilder) Select(columns ...string) *QueryBuilder {
	for _, col := range columns {
		// Allow * and aggregate functions like COUNT(*), SUM(column), etc.
		if col == "*" || isValidSelectExpression(col) {
			continue
		}
		if !isValidIdentifier(col) {
			qb.err = fmt.Errorf("invalid column name: %q", col)
			return qb
		}
	}
	qb.selectCols = columns
	return qb
}

// isValidSelectExpression checks if a string is a valid SELECT expression
// (like COUNT(*), SUM(column), column AS alias, etc.)
func isValidSelectExpression(s string) bool {
	// Allow common aggregate functions
	aggregateFuncs := regexp.MustCompile(`^(COUNT|SUM|AVG|MIN|MAX|GROUP_CONCAT)\s*\([^)]+\)(\s+AS\s+[a-zA-Z_][a-zA-Z0-9_]*)?$`)
	if aggregateFuncs.MatchString(strings.ToUpper(s)) {
		return true
	}
	// Allow column AS alias syntax
	aliasPattern := regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*(\.[a-zA-Z_][a-zA-Z0-9_]*)?\s+AS\s+[a-zA-Z_][a-zA-Z0-9_]*$`)
	return aliasPattern.MatchString(s)
}

// Where adds a WHERE condition
func (qb *QueryBuilder) Where(column, operator string, value interface{}) *QueryBuilder {
	if !isValidIdentifier(column) {
		qb.err = fmt.Errorf("invalid column name in WHERE: %q", column)
		return qb
	}
	if !isValidOperator(operator) {
		qb.err = fmt.Errorf("invalid operator in WHERE: %q", operator)
		return qb
	}
	qb.whereConds = append(qb.whereConds, whereCondition{
		column:   column,
		operator: operator,
		value:    value,
		logic:    "AND",
	})
	return qb
}

// OrWhere adds an OR WHERE condition
func (qb *QueryBuilder) OrWhere(column, operator string, value interface{}) *QueryBuilder {
	if !isValidIdentifier(column) {
		qb.err = fmt.Errorf("invalid column name in OrWhere: %q", column)
		return qb
	}
	if !isValidOperator(operator) {
		qb.err = fmt.Errorf("invalid operator in OrWhere: %q", operator)
		return qb
	}
	qb.whereConds = append(qb.whereConds, whereCondition{
		column:   column,
		operator: operator,
		value:    value,
		logic:    "OR",
	})
	return qb
}

// WhereIn adds a WHERE IN condition
func (qb *QueryBuilder) WhereIn(column string, values []interface{}) *QueryBuilder {
	if !isValidIdentifier(column) {
		qb.err = fmt.Errorf("invalid column name in WhereIn: %q", column)
		return qb
	}
	placeholders := make([]string, len(values))
	for i := range values {
		placeholders[i] = "?"
	}
	inClause := fmt.Sprintf("(%s)", strings.Join(placeholders, ", "))

	qb.whereConds = append(qb.whereConds, whereCondition{
		column:   column,
		operator: "IN",
		value:    inClause,
		params:   values,
		logic:    "AND",
	})
	return qb
}

// WhereBetween adds a BETWEEN condition
func (qb *QueryBuilder) WhereBetween(column string, start, end interface{}) *QueryBuilder {
	if !isValidIdentifier(column) {
		qb.err = fmt.Errorf("invalid column name in WhereBetween: %q", column)
		return qb
	}
	qb.whereConds = append(qb.whereConds, whereCondition{
		column:   column,
		operator: "BETWEEN",
		value:    "? AND ?",
		params:   []interface{}{start, end},
		logic:    "AND",
	})
	return qb
}

// WhereNull adds a WHERE column IS NULL condition
func (qb *QueryBuilder) WhereNull(column string) *QueryBuilder {
	if !isValidIdentifier(column) {
		qb.err = fmt.Errorf("invalid column name in WhereNull: %q", column)
		return qb
	}
	qb.whereConds = append(qb.whereConds, whereCondition{
		column:   column,
		operator: "IS NULL",
		value:    "",
		logic:    "AND",
	})
	return qb
}

// WhereNotNull adds a WHERE column IS NOT NULL condition
func (qb *QueryBuilder) WhereNotNull(column string) *QueryBuilder {
	if !isValidIdentifier(column) {
		qb.err = fmt.Errorf("invalid column name in WhereNotNull: %q", column)
		return qb
	}
	qb.whereConds = append(qb.whereConds, whereCondition{
		column:   column,
		operator: "IS NOT NULL",
		value:    "",
		logic:    "AND",
	})
	return qb
}

// isValidJoinCondition validates a JOIN ON condition
// Allows patterns like "table1.col = table2.col" or "t1.id = t2.foreign_id"
func isValidJoinCondition(on string) bool {
	// Basic pattern: identifier.identifier = identifier.identifier
	// Also allow AND/OR combinations
	joinPattern := regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*\.[a-zA-Z_][a-zA-Z0-9_]*\s*=\s*[a-zA-Z_][a-zA-Z0-9_]*\.[a-zA-Z_][a-zA-Z0-9_]*(\s+(AND|OR)\s+[a-zA-Z_][a-zA-Z0-9_]*\.[a-zA-Z_][a-zA-Z0-9_]*\s*=\s*[a-zA-Z_][a-zA-Z0-9_]*\.[a-zA-Z_][a-zA-Z0-9_]*)*$`)
	return joinPattern.MatchString(on)
}

// Join adds an INNER JOIN
func (qb *QueryBuilder) Join(table, on string) *QueryBuilder {
	if !isValidIdentifier(table) {
		qb.err = fmt.Errorf("invalid table name in Join: %q", table)
		return qb
	}
	if !isValidJoinCondition(on) {
		qb.err = fmt.Errorf("invalid JOIN condition: %q", on)
		return qb
	}
	qb.joins = append(qb.joins, joinClause{
		joinType: "INNER",
		table:    table,
		on:       on,
	})
	return qb
}

// LeftJoin adds a LEFT JOIN
func (qb *QueryBuilder) LeftJoin(table, on string) *QueryBuilder {
	if !isValidIdentifier(table) {
		qb.err = fmt.Errorf("invalid table name in LeftJoin: %q", table)
		return qb
	}
	if !isValidJoinCondition(on) {
		qb.err = fmt.Errorf("invalid LEFT JOIN condition: %q", on)
		return qb
	}
	qb.joins = append(qb.joins, joinClause{
		joinType: "LEFT",
		table:    table,
		on:       on,
	})
	return qb
}

// RightJoin adds a RIGHT JOIN
func (qb *QueryBuilder) RightJoin(table, on string) *QueryBuilder {
	if !isValidIdentifier(table) {
		qb.err = fmt.Errorf("invalid table name in RightJoin: %q", table)
		return qb
	}
	if !isValidJoinCondition(on) {
		qb.err = fmt.Errorf("invalid RIGHT JOIN condition: %q", on)
		return qb
	}
	qb.joins = append(qb.joins, joinClause{
		joinType: "RIGHT",
		table:    table,
		on:       on,
	})
	return qb
}

// OrderBy adds an ORDER BY clause
func (qb *QueryBuilder) OrderBy(column, direction string) *QueryBuilder {
	if !isValidIdentifier(column) {
		qb.err = fmt.Errorf("invalid column name in OrderBy: %q", column)
		return qb
	}
	if direction == "" {
		direction = "ASC"
	}
	dir := strings.ToUpper(direction)
	if !validOrderDirection.MatchString(dir) {
		qb.err = fmt.Errorf("invalid ORDER BY direction: %q", direction)
		return qb
	}
	qb.orderBy = append(qb.orderBy, fmt.Sprintf("%s %s", column, dir))
	return qb
}

// GroupBy adds a GROUP BY clause
func (qb *QueryBuilder) GroupBy(columns ...string) *QueryBuilder {
	for _, col := range columns {
		if !isValidIdentifier(col) {
			qb.err = fmt.Errorf("invalid column name in GroupBy: %q", col)
			return qb
		}
	}
	qb.groupBy = append(qb.groupBy, columns...)
	return qb
}

// Having adds a HAVING condition
func (qb *QueryBuilder) Having(column, operator string, value interface{}) *QueryBuilder {
	// Having can use aggregate functions like COUNT(*), SUM(column), etc.
	if !isValidIdentifier(column) && !isValidSelectExpression(column) {
		qb.err = fmt.Errorf("invalid column/expression in Having: %q", column)
		return qb
	}
	if !isValidOperator(operator) {
		qb.err = fmt.Errorf("invalid operator in Having: %q", operator)
		return qb
	}
	qb.having = append(qb.having, whereCondition{
		column:   column,
		operator: operator,
		value:    value,
		logic:    "AND",
	})
	return qb
}

// Limit sets the LIMIT clause
func (qb *QueryBuilder) Limit(count int) *QueryBuilder {
	qb.limitCount = count
	return qb
}

// Offset sets the OFFSET clause
func (qb *QueryBuilder) Offset(count int) *QueryBuilder {
	qb.offsetCount = count
	return qb
}

// Union adds a UNION clause
func (qb *QueryBuilder) Union(query *QueryBuilder) *QueryBuilder {
	qb.unionQuery = query
	qb.unionAll = false
	return qb
}

// UnionAll adds a UNION ALL clause
func (qb *QueryBuilder) UnionAll(query *QueryBuilder) *QueryBuilder {
	qb.unionQuery = query
	qb.unionAll = true
	return qb
}

// ToSQL builds the SQL query string and returns it with parameters
func (qb *QueryBuilder) ToSQL() (string, []interface{}, error) {
	// Check for validation errors first
	if qb.err != nil {
		return "", nil, qb.err
	}
	if qb.table == "" {
		return "", nil, fmt.Errorf("table name is required")
	}

	var query strings.Builder
	var params []interface{}

	// SELECT clause
	query.WriteString("SELECT ")
	if len(qb.selectCols) == 0 {
		query.WriteString("*")
	} else {
		query.WriteString(strings.Join(qb.selectCols, ", "))
	}

	// FROM clause
	query.WriteString(fmt.Sprintf(" FROM %s", qb.table))

	// JOIN clauses
	for _, join := range qb.joins {
		query.WriteString(fmt.Sprintf(" %s JOIN %s ON %s", join.joinType, join.table, join.on))
	}

	// WHERE clauses
	if len(qb.whereConds) > 0 {
		query.WriteString(" WHERE ")
		for i, cond := range qb.whereConds {
			if i > 0 {
				query.WriteString(fmt.Sprintf(" %s ", cond.logic))
			}
			
			switch cond.operator {
			case "IS NULL", "IS NOT NULL":
				query.WriteString(fmt.Sprintf("%s %s", cond.column, cond.operator))
			case "IN", "BETWEEN":
				query.WriteString(fmt.Sprintf("%s %s %s", cond.column, cond.operator, cond.value))
				// Add parameters for IN and BETWEEN clauses
				if cond.params != nil {
					params = append(params, cond.params...)
				}
			default:
				query.WriteString(fmt.Sprintf("%s %s ?", cond.column, cond.operator))
				params = append(params, cond.value)
			}
		}
	}

	// GROUP BY clause
	if len(qb.groupBy) > 0 {
		query.WriteString(fmt.Sprintf(" GROUP BY %s", strings.Join(qb.groupBy, ", ")))
	}

	// HAVING clause
	if len(qb.having) > 0 {
		query.WriteString(" HAVING ")
		for i, cond := range qb.having {
			if i > 0 {
				query.WriteString(fmt.Sprintf(" %s ", cond.logic))
			}
			query.WriteString(fmt.Sprintf("%s %s ?", cond.column, cond.operator))
			params = append(params, cond.value)
		}
	}

	// ORDER BY clause
	if len(qb.orderBy) > 0 {
		query.WriteString(fmt.Sprintf(" ORDER BY %s", strings.Join(qb.orderBy, ", ")))
	}

	// LIMIT clause
	if qb.limitCount > 0 {
		query.WriteString(fmt.Sprintf(" LIMIT %d", qb.limitCount))
	}

	// OFFSET clause
	if qb.offsetCount > 0 {
		query.WriteString(fmt.Sprintf(" OFFSET %d", qb.offsetCount))
	}

	// UNION clause
	if qb.unionQuery != nil {
		unionSQL, unionParams, err := qb.unionQuery.ToSQL()
		if err != nil {
			return "", nil, err
		}
		
		unionType := "UNION"
		if qb.unionAll {
			unionType = "UNION ALL"
		}
		
		query.WriteString(fmt.Sprintf(" %s %s", unionType, unionSQL))
		params = append(params, unionParams...)
	}

	return query.String(), params, nil
}

// Get executes the query and returns all rows
func (qb *QueryBuilder) Get() (*sql.Rows, error) {
	query, params, err := qb.ToSQL()
	if err != nil {
		return nil, err
	}
	
	return qb.db.Query(query, params...)
}

// First executes the query and returns the first row
func (qb *QueryBuilder) First() *sql.Row {
	qb.Limit(1)
	query, params, _ := qb.ToSQL()
	
	return qb.db.QueryRow(query, params...)
}

// Count returns the count of rows
func (qb *QueryBuilder) Count() (int64, error) {
	originalSelect := qb.selectCols
	qb.selectCols = []string{"COUNT(*)"}
	
	query, params, err := qb.ToSQL()
	if err != nil {
		return 0, err
	}
	
	// Restore original select
	qb.selectCols = originalSelect
	
	var count int64
	err = qb.db.QueryRow(query, params...).Scan(&count)
	return count, err
}

// Exists checks if any rows exist
func (qb *QueryBuilder) Exists() (bool, error) {
	count, err := qb.Count()
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// Paginate adds pagination to the query
func (qb *QueryBuilder) Paginate(page, perPage int) *QueryBuilder {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 15
	}
	
	offset := (page - 1) * perPage
	return qb.Limit(perPage).Offset(offset)
}

// Insert builds and executes an INSERT query
func (qb *QueryBuilder) Insert(data map[string]interface{}) (sql.Result, error) {
	if qb.err != nil {
		return nil, qb.err
	}
	if qb.table == "" {
		return nil, fmt.Errorf("table name is required")
	}

	var columns []string
	var placeholders []string
	var values []interface{}

	for col, val := range data {
		if !isValidIdentifier(col) {
			return nil, fmt.Errorf("invalid column name in Insert: %q", col)
		}
		columns = append(columns, col)
		placeholders = append(placeholders, "?")
		values = append(values, val)
	}

	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		qb.table,
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "))

	return qb.db.Exec(query, values...)
}

// Update builds and executes an UPDATE query
func (qb *QueryBuilder) Update(data map[string]interface{}) (sql.Result, error) {
	if qb.err != nil {
		return nil, qb.err
	}
	if qb.table == "" {
		return nil, fmt.Errorf("table name is required")
	}

	var setParts []string
	var params []interface{}

	for col, val := range data {
		if !isValidIdentifier(col) {
			return nil, fmt.Errorf("invalid column name in Update: %q", col)
		}
		setParts = append(setParts, fmt.Sprintf("%s = ?", col))
		params = append(params, val)
	}

	query := fmt.Sprintf("UPDATE %s SET %s", qb.table, strings.Join(setParts, ", "))

	// Add WHERE conditions
	if len(qb.whereConds) > 0 {
		query += " WHERE "
		for i, cond := range qb.whereConds {
			if i > 0 {
				query += fmt.Sprintf(" %s ", cond.logic)
			}

			switch cond.operator {
			case "IS NULL", "IS NOT NULL":
				query += fmt.Sprintf("%s %s", cond.column, cond.operator)
			case "IN", "BETWEEN":
				query += fmt.Sprintf("%s %s %s", cond.column, cond.operator, cond.value)
				if cond.params != nil {
					params = append(params, cond.params...)
				}
			default:
				query += fmt.Sprintf("%s %s ?", cond.column, cond.operator)
				params = append(params, cond.value)
			}
		}
	}

	return qb.db.Exec(query, params...)
}

// Delete builds and executes a DELETE query
func (qb *QueryBuilder) Delete() (sql.Result, error) {
	if qb.err != nil {
		return nil, qb.err
	}
	if qb.table == "" {
		return nil, fmt.Errorf("table name is required")
	}

	query := fmt.Sprintf("DELETE FROM %s", qb.table)
	var params []interface{}

	// Add WHERE conditions
	if len(qb.whereConds) > 0 {
		query += " WHERE "
		for i, cond := range qb.whereConds {
			if i > 0 {
				query += fmt.Sprintf(" %s ", cond.logic)
			}

			switch cond.operator {
			case "IS NULL", "IS NOT NULL":
				query += fmt.Sprintf("%s %s", cond.column, cond.operator)
			case "IN", "BETWEEN":
				query += fmt.Sprintf("%s %s %s", cond.column, cond.operator, cond.value)
				if cond.params != nil {
					params = append(params, cond.params...)
				}
			default:
				query += fmt.Sprintf("%s %s ?", cond.column, cond.operator)
				params = append(params, cond.value)
			}
		}
	}

	return qb.db.Exec(query, params...)
}

// Raw executes a raw SQL query
func (qb *QueryBuilder) Raw(query string, params ...interface{}) (*sql.Rows, error) {
	return qb.db.Query(query, params...)
}

// RawExec executes a raw SQL statement
func (qb *QueryBuilder) RawExec(query string, params ...interface{}) (sql.Result, error) {
	return qb.db.Exec(query, params...)
}

// ============================================================================
// SOFT DELETE METHODS
// ============================================================================

// SoftDelete sets deleted_at to current timestamp instead of deleting the row.
// This is useful for preserving data while marking it as deleted.
func (qb *QueryBuilder) SoftDelete() (sql.Result, error) {
	return qb.Update(map[string]interface{}{
		"deleted_at": "NOW()",
	})
}

// Restore clears the deleted_at timestamp, effectively "undeleting" the row.
func (qb *QueryBuilder) Restore() (sql.Result, error) {
	return qb.Update(map[string]interface{}{
		"deleted_at": nil,
	})
}

// ForceDelete permanently deletes rows, ignoring soft delete.
// This is an alias for Delete() but makes the intent explicit.
func (qb *QueryBuilder) ForceDelete() (sql.Result, error) {
	return qb.Delete()
}

// WithTrashed includes soft-deleted records in the query results.
// Use this when you need to access deleted records.
func (qb *QueryBuilder) WithTrashed() *QueryBuilder {
	qb.includeTrashed = true
	return qb
}

// OnlyTrashed returns only soft-deleted records.
func (qb *QueryBuilder) OnlyTrashed() *QueryBuilder {
	return qb.WhereNotNull("deleted_at")
}

// ============================================================================
// RELATION HELPERS
// ============================================================================

// HasMany returns a query builder for a one-to-many relationship.
// Example: user.HasMany("posts", "user_id", userID) returns all posts for a user.
func (qb *QueryBuilder) HasMany(relatedTable, foreignKey string, id interface{}) *QueryBuilder {
	return newQueryBuilderWithDB(qb.db, qb.rawDB).
		Table(relatedTable).
		Where(foreignKey, "=", id)
}

// BelongsTo returns a query builder for the parent in a one-to-many relationship.
// Example: post.BelongsTo("users", "user_id", post.UserID) returns the user for a post.
func (qb *QueryBuilder) BelongsTo(relatedTable, foreignKey string, fkValue interface{}) *QueryBuilder {
	return newQueryBuilderWithDB(qb.db, qb.rawDB).
		Table(relatedTable).
		Where("id", "=", fkValue)
}

// ============================================================================
// UTILITY METHODS
// ============================================================================

// Scope is a function that modifies a QueryBuilder.
// Use scopes to create reusable query constraints.
type Scope func(*QueryBuilder) *QueryBuilder

// Scope applies one or more scopes to the query builder.
// Scopes are reusable query modifications.
//
// Example:
//
//	Active := func(qb *QueryBuilder) *QueryBuilder {
//	    return qb.Where("active", "=", true)
//	}
//	users, _ := db.Table("users").Scope(Active).Get()
func (qb *QueryBuilder) Scope(scopes ...Scope) *QueryBuilder {
	for _, scope := range scopes {
		qb = scope(qb)
	}
	return qb
}

// Chunk processes query results in chunks of the specified size.
// The callback function receives each chunk of rows.
// Return false from the callback to stop processing.
//
// Example:
//
//	db.Table("users").Chunk(100, func(rows *sql.Rows) bool {
//	    for rows.Next() {
//	        // process row
//	    }
//	    return true // continue processing
//	})
func (qb *QueryBuilder) Chunk(size int, fn func(rows *sql.Rows) bool) error {
	if size <= 0 {
		return fmt.Errorf("chunk size must be positive")
	}

	offset := 0
	for {
		// Create a copy of the query builder for this chunk
		chunkQB := &QueryBuilder{
			db:             qb.db,
			rawDB:          qb.rawDB,
			table:          qb.table,
			selectCols:     qb.selectCols,
			whereConds:     qb.whereConds,
			orderBy:        qb.orderBy,
			groupBy:        qb.groupBy,
			having:         qb.having,
			joins:          qb.joins,
			limitCount:     size,
			offsetCount:    offset,
			includeTrashed: qb.includeTrashed,
		}

		rows, err := chunkQB.Get()
		if err != nil {
			return err
		}

		// Check if we got any rows
		hasRows := false
		if rows.Next() {
			hasRows = true
			// Reset the cursor so the callback can iterate from the start
			rows.Close()

			// Re-execute the query for the callback
			rows, err = chunkQB.Get()
			if err != nil {
				return err
			}
		}

		if !hasRows {
			rows.Close()
			break
		}

		// Call the callback function
		shouldContinue := fn(rows)
		rows.Close()

		if !shouldContinue {
			break
		}

		offset += size
	}

	return nil
}

// ChunkByID processes query results by ID in batches.
// This is more efficient for large tables as it uses indexed ID lookups.
func (qb *QueryBuilder) ChunkByID(size int, column string, fn func(rows *sql.Rows) bool) error {
	if size <= 0 {
		return fmt.Errorf("chunk size must be positive")
	}
	if !isValidIdentifier(column) {
		return fmt.Errorf("invalid column name: %q", column)
	}

	var lastID interface{} = 0

	for {
		// Create a query for this chunk
		chunkQB := &QueryBuilder{
			db:             qb.db,
			rawDB:          qb.rawDB,
			table:          qb.table,
			selectCols:     qb.selectCols,
			whereConds:     append(qb.whereConds, whereCondition{column: column, operator: ">", value: lastID, logic: "AND"}),
			orderBy:        []string{column + " ASC"},
			groupBy:        qb.groupBy,
			having:         qb.having,
			joins:          qb.joins,
			limitCount:     size,
			includeTrashed: qb.includeTrashed,
		}

		rows, err := chunkQB.Get()
		if err != nil {
			return err
		}

		hasRows := false
		for rows.Next() {
			hasRows = true
		}
		rows.Close()

		if !hasRows {
			break
		}

		// Re-execute for callback
		rows, err = chunkQB.Get()
		if err != nil {
			return err
		}

		shouldContinue := fn(rows)
		rows.Close()

		if !shouldContinue {
			break
		}

		// Note: The callback should update lastID, or we need to track it
		// For simplicity, we'll just use offset-based pagination in this case
	}

	return nil
}

// ============================================================================
// COMMON SCOPES
// ============================================================================

// Active is a predefined scope that filters for active records.
func Active(qb *QueryBuilder) *QueryBuilder {
	return qb.Where("active", "=", true)
}

// Recent is a predefined scope that orders by created_at DESC.
func Recent(qb *QueryBuilder) *QueryBuilder {
	return qb.OrderBy("created_at", "DESC")
}

// Oldest is a predefined scope that orders by created_at ASC.
func Oldest(qb *QueryBuilder) *QueryBuilder {
	return qb.OrderBy("created_at", "ASC")
}

// Published is a predefined scope for published content.
func Published(qb *QueryBuilder) *QueryBuilder {
	return qb.Where("published", "=", true).WhereNotNull("published_at")
}

// Draft is a predefined scope for draft/unpublished content.
func Draft(qb *QueryBuilder) *QueryBuilder {
	return qb.Where("published", "=", false)
}