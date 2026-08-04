package auth

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jimmitjoo/tjo/database"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

func TestOrganizationFromContext(t *testing.T) {
	if _, err := OrganizationFrom(context.Background()); !errors.Is(err, ErrNoActiveOrganization) {
		t.Errorf("a bare context returned %v", err)
	}

	ctx := WithOrganization(context.Background(), "org-1")
	got, err := OrganizationFrom(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != "org-1" {
		t.Errorf("got %q", got)
	}

	// An empty value must not count as an active organization, or setting it
	// from an empty session field silently unscopes every query.
	if _, err := OrganizationFrom(WithOrganization(context.Background(), "")); !errors.Is(err, ErrNoActiveOrganization) {
		t.Error("an empty organization id was treated as active")
	}
}

// The tenancy boundary is a WHERE clause, so the test has to be a real query
// against a real database. A mock that records the call proves the helper was
// invoked, not that the rows are actually separated.
func TestScopeToSeparatesTenantsInSQL(t *testing.T) {
	run := func(t *testing.T, db *sql.DB, dialect database.Dialect) {
		t.Helper()
		ctx := context.Background()

		db.Exec(`DROP TABLE IF EXISTS invoices`)
		// SQLite gives INTEGER PRIMARY KEY an implicit rowid; PostgreSQL does
		// not, so it needs an explicit sequence.
		idType := "INTEGER PRIMARY KEY"
		if dialect == database.DialectDollar {
			idType = "SERIAL PRIMARY KEY"
		}
		if _, err := db.Exec(`CREATE TABLE invoices (id ` + idType + `, organization_id TEXT NOT NULL, amount INT NOT NULL)`); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Exec(`DROP TABLE IF EXISTS invoices`) })

		for _, r := range []struct {
			org    string
			amount int
		}{{"org-a", 100}, {"org-a", 200}, {"org-b", 999}} {
			if _, err := db.Exec(rebindFor(dialect, `INSERT INTO invoices (organization_id, amount) VALUES (?, ?)`), r.org, r.amount); err != nil {
				t.Fatal(err)
			}
		}

		qb, err := ScopeTo(WithOrganization(ctx, "org-a"),
			database.NewQueryBuilder(db).WithDialect(dialect).Table("invoices"), "organization_id")
		if err != nil {
			t.Fatal(err)
		}

		rows, err := qb.Get()
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		defer rows.Close()

		var seen []int
		for rows.Next() {
			var id, amount int
			var org string
			if err := rows.Scan(&id, &org, &amount); err != nil {
				t.Fatal(err)
			}
			if org != "org-a" {
				t.Errorf("a row belonging to %q crossed the tenancy boundary", org)
			}
			seen = append(seen, amount)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}

		if len(seen) != 2 {
			t.Fatalf("got %d rows, want 2 -- org-b's invoice is visible", len(seen))
		}
		for _, amount := range seen {
			if amount == 999 {
				t.Error("org-b's invoice was returned to org-a")
			}
		}
	}

	t.Run("sqlite", func(t *testing.T) {
		db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "scope.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		run(t, db, database.DialectQuestion)
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
		defer db.Close()
		run(t, db, database.DialectDollar)
	})
}

func rebindFor(d database.Dialect, q string) string {
	if d != database.DialectDollar {
		return q
	}
	out := ""
	n := 0
	for _, r := range q {
		if r == '?' {
			n++
			out += "$" + string(rune('0'+n))
			continue
		}
		out += string(r)
	}
	return out
}

// Scoping answers "which rows", not "may this person see them". An account with
// no membership still produces a valid scoped query; this is what makes it
// return nothing on purpose rather than by accident.
func TestMustBelongRejectsNonMembers(t *testing.T) {
	store := newOrgStore()
	ctx := WithOrganization(context.Background(), "org-1")

	store.AddMember(ctx, &Membership{OrganizationID: "org-1", AccountID: "member", Role: RoleMember})

	if _, err := MustBelong(ctx, store, "member"); err != nil {
		t.Errorf("a member was rejected: %v", err)
	}
	if _, err := MustBelong(ctx, store, "stranger"); !errors.Is(err, ErrNotAMember) {
		t.Errorf("a non-member passed: %v", err)
	}
	if _, err := MustBelong(context.Background(), store, "member"); !errors.Is(err, ErrNoActiveOrganization) {
		t.Errorf("no active organization returned %v", err)
	}
}
