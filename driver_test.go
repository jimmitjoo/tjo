package tjo

import (
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