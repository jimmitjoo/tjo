package database

import (
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// The driver swap from pgx v4 to v5 (#37) shipped against zero integration
// coverage: every test in this package runs on SQLite or on mocks, so nothing
// in the suite would have noticed if PostgreSQL had stopped working entirely.
//
// A driver is exactly the component where that gap matters. These tests connect
// to a real server when TJO_TEST_POSTGRES_DSN is set and skip when it is not,
// which keeps them free of a dockertest dependency in the module that just
// spent this release shedding dependencies.
//
//	docker run -d --name tjo-pgtest -e POSTGRES_PASSWORD=secret \
//	  -e POSTGRES_USER=tjo -e POSTGRES_DB=tjotest -p 55432:5432 postgres:16-alpine
//	TJO_TEST_POSTGRES_DSN='postgres://tjo:secret@localhost:55432/tjotest?sslmode=disable' go test ./database/...
func postgresDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := os.Getenv("TJO_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TJO_TEST_POSTGRES_DSN is not set")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	return db
}

// pgx/v5/stdlib must keep registering under the same "pgx" name v4 used.
// OpenDB and five other call sites dispatch on that string, so a rename would
// break PostgreSQL support without breaking the build.
func TestPostgresDriverIsRegisteredAsPgx(t *testing.T) {
	db := postgresDB(t)

	var one int
	if err := db.QueryRow("SELECT 1").Scan(&one); err != nil {
		t.Fatalf("SELECT 1: %v", err)
	}
	if one != 1 {
		t.Fatalf("SELECT 1 returned %d", one)
	}
}

// PostgreSQL's wire protocol accepts only $1, $2, ... placeholders, which is
// why DialectDollar exists. Placeholder rewriting is the part of a driver
// migration that fails silently rather than loudly.
func TestQueryBuilderDollarPlaceholdersAgainstPostgres(t *testing.T) {
	db := postgresDB(t)

	if _, err := db.Exec(`DROP TABLE IF EXISTS tjo_qb_widgets`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE tjo_qb_widgets (id serial primary key, name text not null, qty int not null)`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DROP TABLE IF EXISTS tjo_qb_widgets`) })

	for _, r := range []struct {
		name string
		qty  int
	}{{"alpha", 1}, {"beta", 5}, {"gamma", 9}} {
		if _, err := db.Exec(`INSERT INTO tjo_qb_widgets (name, qty) VALUES ($1, $2)`, r.name, r.qty); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := NewQueryBuilder(db).
		WithDialect(DialectDollar).
		Table("tjo_qb_widgets").
		Where("qty", ">", 1).
		OrderBy("qty", "asc").
		Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var id, qty int
		var name string
		if err := rows.Scan(&id, &name, &qty); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	if len(names) != 2 || names[0] != "beta" || names[1] != "gamma" {
		t.Fatalf("got %v, want [beta gamma]", names)
	}
}
