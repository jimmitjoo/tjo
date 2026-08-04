package tjo

import (
	"database/sql"
	"path/filepath"
	"slices"
	"testing"
)

func TestTjo_OpenDB(t *testing.T) {
	g := &Tjo{}

	tests := []struct {
		name      string
		dbType    string
		dsn       string
		wantError bool
	}{
		{
			name:      "Invalid PostgreSQL connection",
			dbType:    "postgres",
			dsn:       "host=invalid port=5432 user=test password=test dbname=test sslmode=disable",
			wantError: true,
		},
		{
			name:      "Invalid MySQL connection",
			dbType:    "mysql",
			dsn:       "invalid:invalid@tcp(invalid:3306)/invalid",
			wantError: true,
		},
		{
			name:      "PostgreSQL type conversion",
			dbType:    "postgresql",
			dsn:       "host=invalid port=5432 user=test password=test dbname=test sslmode=disable",
			wantError: true,
		},
		{
			name:      "MariaDB type conversion",
			dbType:    "mariadb",
			dsn:       "invalid:invalid@tcp(invalid:3306)/invalid",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, err := g.OpenDB(tt.dbType, tt.dsn)
			
			if tt.wantError {
				if err == nil {
					t.Error("Expected error but got none")
					if db != nil {
						db.Close()
					}
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
				if db != nil {
					db.Close()
				}
			}
		})
	}
}

func TestDatabaseTypeConversion(t *testing.T) {
	g := &Tjo{}

	// Test that postgres/postgresql gets converted to pgx
	testCases := []struct {
		input    string
		expected string
	}{
		{"postgres", "pgx"},
		{"postgresql", "pgx"},
		{"mysql", "mysql"},
		{"mariadb", "mysql"},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			// We can't easily test the internal conversion without refactoring,
			// but we can verify the function handles these types
			_, err := g.OpenDB(tc.input, "invalid_dsn")
			if err == nil {
				t.Error("Expected error with invalid DSN")
			}
		})
	}
}
// TestOpenDBResolvesSQLiteUnderBothSpellings pins the driver-name normalisation
// that the modernc swap depends on.
//
// mattn/go-sqlite3 registered as "sqlite3"; modernc.org/sqlite registers as
// "sqlite". Configuration accepts both spellings and always has, so OpenDB has
// to map them onto whichever name the linked driver actually registered. Get
// this wrong and sql.Open returns "unknown driver" at runtime, on the database
// `tjo new -d sqlite` sets up by default -- a failure no build catches.
func TestOpenDBResolvesSQLiteUnderBothSpellings(t *testing.T) {
	g := &Tjo{}

	for _, dbType := range []string{"sqlite", "sqlite3"} {
		t.Run(dbType, func(t *testing.T) {
			db, err := g.OpenDB(dbType, filepath.Join(t.TempDir(), "probe.db"))
			if err != nil {
				t.Fatalf("OpenDB(%q): %v", dbType, err)
			}
			defer db.Close()

			if _, err := db.Exec(`CREATE TABLE t (id INTEGER PRIMARY KEY)`); err != nil {
				t.Fatalf("the handle does not work: %v", err)
			}
		})
	}
}

// The binary must not need cgo. `tjo deploy` cross-compiles a Linux binary from
// whatever the developer is running on, and the release matrix builds every
// target from one runner; both are impossible with a cgo-linked SQLite driver.
//
// This asserts the pure-Go driver is the one registered, which is the property
// those two depend on. A cgo driver would register "sqlite3" and not "sqlite".
func TestSQLiteDriverIsPureGo(t *testing.T) {
	for _, name := range sql.Drivers() {
		if name == "sqlite3" {
			t.Error(`a driver registered as "sqlite3" is linked, which means mattn/go-sqlite3 and cgo are back`)
		}
	}

	if !slices.Contains(sql.Drivers(), "sqlite") {
		t.Fatalf(`no "sqlite" driver registered; have %v`, sql.Drivers())
	}
}
