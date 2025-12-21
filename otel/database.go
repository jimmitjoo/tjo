package otel

import (
	"context"
	"database/sql"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
)

// TracedDB wraps a sql.DB to add tracing to all database operations.
type TracedDB struct {
	db     *sql.DB
	tracer trace.Tracer
	dbName string
	dbType string
}

// WrapDB wraps a sql.DB with tracing capabilities.
func WrapDB(db *sql.DB, dbType, dbName string) *TracedDB {
	return &TracedDB{
		db:     db,
		tracer: otel.Tracer("tjo/database"),
		dbName: dbName,
		dbType: dbType,
	}
}

// WrapDBWithTracer wraps a sql.DB with a specific tracer.
func WrapDBWithTracer(db *sql.DB, tracer trace.Tracer, dbType, dbName string) *TracedDB {
	return &TracedDB{
		db:     db,
		tracer: tracer,
		dbName: dbName,
		dbType: dbType,
	}
}

// DB returns the underlying sql.DB.
func (t *TracedDB) DB() *sql.DB {
	return t.db
}

// baseAttributes returns common database attributes.
func (t *TracedDB) baseAttributes() []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		semconv.DBSystemKey.String(t.dbType),
	}
	if t.dbName != "" {
		attrs = append(attrs, semconv.DBName(t.dbName))
	}
	return attrs
}

// Query executes a query and traces it.
func (t *TracedDB) Query(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	ctx, span := t.tracer.Start(ctx, "db.query",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(t.baseAttributes()...),
		trace.WithAttributes(
			semconv.DBStatement(truncateQuery(query)),
			attribute.String("db.operation", "query"),
		),
	)
	defer span.End()

	rows, err := t.db.QueryContext(ctx, query, args...)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return rows, err
}

// QueryRow executes a query that returns a single row and traces it.
func (t *TracedDB) QueryRow(ctx context.Context, query string, args ...interface{}) *sql.Row {
	ctx, span := t.tracer.Start(ctx, "db.query_row",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(t.baseAttributes()...),
		trace.WithAttributes(
			semconv.DBStatement(truncateQuery(query)),
			attribute.String("db.operation", "query_row"),
		),
	)
	defer span.End()

	return t.db.QueryRowContext(ctx, query, args...)
}

// Exec executes a statement and traces it.
func (t *TracedDB) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	operation := detectOperation(query)

	ctx, span := t.tracer.Start(ctx, "db."+operation,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(t.baseAttributes()...),
		trace.WithAttributes(
			semconv.DBStatement(truncateQuery(query)),
			attribute.String("db.operation", operation),
		),
	)
	defer span.End()

	result, err := t.db.ExecContext(ctx, query, args...)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else if result != nil {
		if rowsAffected, err := result.RowsAffected(); err == nil {
			span.SetAttributes(attribute.Int64("db.rows_affected", rowsAffected))
		}
	}
	return result, err
}

// Prepare creates a prepared statement and traces it.
func (t *TracedDB) Prepare(ctx context.Context, query string) (*TracedStmt, error) {
	ctx, span := t.tracer.Start(ctx, "db.prepare",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(t.baseAttributes()...),
		trace.WithAttributes(
			semconv.DBStatement(truncateQuery(query)),
			attribute.String("db.operation", "prepare"),
		),
	)
	defer span.End()

	stmt, err := t.db.PrepareContext(ctx, query)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	return &TracedStmt{
		stmt:   stmt,
		tracer: t.tracer,
		query:  query,
		attrs:  t.baseAttributes(),
	}, nil
}

// Begin starts a transaction and traces it.
func (t *TracedDB) Begin(ctx context.Context) (*TracedTx, error) {
	ctx, span := t.tracer.Start(ctx, "db.transaction",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(t.baseAttributes()...),
		trace.WithAttributes(attribute.String("db.operation", "begin")),
	)

	tx, err := t.db.BeginTx(ctx, nil)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		span.End()
		return nil, err
	}

	return &TracedTx{
		tx:     tx,
		tracer: t.tracer,
		span:   span,
		attrs:  t.baseAttributes(),
	}, nil
}

// Ping verifies the database connection.
func (t *TracedDB) Ping(ctx context.Context) error {
	ctx, span := t.tracer.Start(ctx, "db.ping",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(t.baseAttributes()...),
	)
	defer span.End()

	err := t.db.PingContext(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return err
}

// Close closes the database connection.
func (t *TracedDB) Close() error {
	return t.db.Close()
}

// TracedStmt is a traced prepared statement.
type TracedStmt struct {
	stmt   *sql.Stmt
	tracer trace.Tracer
	query  string
	attrs  []attribute.KeyValue
}

// Query executes the prepared statement with tracing.
func (s *TracedStmt) Query(ctx context.Context, args ...interface{}) (*sql.Rows, error) {
	ctx, span := s.tracer.Start(ctx, "db.stmt.query",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(s.attrs...),
		trace.WithAttributes(semconv.DBStatement(truncateQuery(s.query))),
	)
	defer span.End()

	rows, err := s.stmt.QueryContext(ctx, args...)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return rows, err
}

// Exec executes the prepared statement with tracing.
func (s *TracedStmt) Exec(ctx context.Context, args ...interface{}) (sql.Result, error) {
	ctx, span := s.tracer.Start(ctx, "db.stmt.exec",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(s.attrs...),
		trace.WithAttributes(semconv.DBStatement(truncateQuery(s.query))),
	)
	defer span.End()

	result, err := s.stmt.ExecContext(ctx, args...)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return result, err
}

// Close closes the prepared statement.
func (s *TracedStmt) Close() error {
	return s.stmt.Close()
}

// TracedTx is a traced transaction.
type TracedTx struct {
	tx     *sql.Tx
	tracer trace.Tracer
	span   trace.Span
	attrs  []attribute.KeyValue
}

// Query executes a query within the transaction.
func (t *TracedTx) Query(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	ctx, span := t.tracer.Start(ctx, "db.tx.query",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(t.attrs...),
		trace.WithAttributes(semconv.DBStatement(truncateQuery(query))),
	)
	defer span.End()

	rows, err := t.tx.QueryContext(ctx, query, args...)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return rows, err
}

// Exec executes a statement within the transaction.
func (t *TracedTx) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	operation := detectOperation(query)

	ctx, span := t.tracer.Start(ctx, "db.tx."+operation,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(t.attrs...),
		trace.WithAttributes(
			semconv.DBStatement(truncateQuery(query)),
			attribute.String("db.operation", operation),
		),
	)
	defer span.End()

	result, err := t.tx.ExecContext(ctx, query, args...)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return result, err
}

// Commit commits the transaction.
func (t *TracedTx) Commit() error {
	t.span.SetAttributes(attribute.String("db.transaction.status", "commit"))
	err := t.tx.Commit()
	if err != nil {
		t.span.RecordError(err)
		t.span.SetStatus(codes.Error, err.Error())
	}
	t.span.End()
	return err
}

// Rollback rolls back the transaction.
func (t *TracedTx) Rollback() error {
	t.span.SetAttributes(attribute.String("db.transaction.status", "rollback"))
	err := t.tx.Rollback()
	if err != nil {
		t.span.RecordError(err)
		t.span.SetStatus(codes.Error, err.Error())
	}
	t.span.End()
	return err
}

// truncateQuery limits query length to avoid huge spans.
const maxQueryLength = 2048

func truncateQuery(query string) string {
	if len(query) <= maxQueryLength {
		return query
	}
	return query[:maxQueryLength] + "..."
}

// detectOperation detects the SQL operation type from the query.
func detectOperation(query string) string {
	// Simple detection based on first word
	for i := 0; i < len(query); i++ {
		if query[i] != ' ' && query[i] != '\t' && query[i] != '\n' {
			// Found first non-whitespace
			end := i
			for end < len(query) && query[end] != ' ' && query[end] != '\t' && query[end] != '\n' {
				end++
			}
			word := query[i:end]
			switch {
			case len(word) >= 6 && (word[:6] == "SELECT" || word[:6] == "select"):
				return "select"
			case len(word) >= 6 && (word[:6] == "INSERT" || word[:6] == "insert"):
				return "insert"
			case len(word) >= 6 && (word[:6] == "UPDATE" || word[:6] == "update"):
				return "update"
			case len(word) >= 6 && (word[:6] == "DELETE" || word[:6] == "delete"):
				return "delete"
			default:
				return "exec"
			}
		}
	}
	return "exec"
}
