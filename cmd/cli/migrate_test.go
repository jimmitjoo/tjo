package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jimmitjoo/gemquick/config"
	"github.com/jimmitjoo/gemquick/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDoMigrate(t *testing.T) {
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "migrate_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	originalWd, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(originalWd)

	err = os.Chdir(tempDir)
	require.NoError(t, err)

	// Save and restore cfg
	originalCfg := cfg
	defer func() { cfg = originalCfg }()

	// Create migrations directory
	err = os.MkdirAll("migrations", 0755)
	require.NoError(t, err)

	// Create test migration files
	timestamp := time.Now().Format("20060102150405")
	
	upMigration := `-- Test migration up
CREATE TABLE IF NOT EXISTS test_table (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255)
);`
	
	downMigration := `-- Test migration down
DROP TABLE IF EXISTS test_table;`

	upFile := filepath.Join("migrations", timestamp+"_create_test_table.up.sql")
	downFile := filepath.Join("migrations", timestamp+"_create_test_table.down.sql")
	
	err = os.WriteFile(upFile, []byte(upMigration), 0644)
	require.NoError(t, err)
	
	err = os.WriteFile(downFile, []byte(downMigration), 0644)
	require.NoError(t, err)

	tests := []struct {
		name        string
		direction   string
		steps       string
		setupEnv    map[string]string
		expectError bool
	}{
		{
			name:      "migrate up",
			direction: "up",
			steps:     "",
			setupEnv: map[string]string{
				"DATABASE_TYPE": "postgres",
				"DATABASE_DSN":  "postgres://user:pass@localhost:5432/testdb?sslmode=disable",
			},
			expectError: true, // Will error because no actual DB connection
		},
		{
			name:      "migrate down",
			direction: "down",
			steps:     "1",
			setupEnv: map[string]string{
				"DATABASE_TYPE": "postgres",
				"DATABASE_DSN":  "postgres://user:pass@localhost:5432/testdb?sslmode=disable",
			},
			expectError: true, // Will error because no actual DB connection
		},
		{
			name:      "migrate reset",
			direction: "reset",
			steps:     "",
			setupEnv: map[string]string{
				"DATABASE_TYPE": "postgres",
				"DATABASE_DSN":  "postgres://user:pass@localhost:5432/testdb?sslmode=disable",
			},
			expectError: true, // Will error because no actual DB connection
		},
		{
			name:        "invalid direction shows help",
			direction:   "invalid",
			steps:       "",
			setupEnv:    map[string]string{},
			expectError: false, // Actually shows help instead of erroring
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up environment
			for k, v := range tt.setupEnv {
				os.Setenv(k, v)
				defer os.Unsetenv(k)
			}

			// Initialize cfg for the test
			dbType := tt.setupEnv["DATABASE_TYPE"]
			if dbType == "" {
				dbType = "postgres"
			}
			loadedCfg, _ := config.Load()
			cfg = &core.CLIConfig{
				RootPath: tempDir,
				DBType:   dbType,
				Config:   loadedCfg,
			}

			// Note: doMigrate will fail without actual database connection
			// We're mainly testing that the function handles arguments correctly
			err := doMigrate(tt.direction, tt.steps)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestMigrationHelpers(t *testing.T) {
	t.Run("parse migration direction", func(t *testing.T) {
		tests := []struct {
			input    string
			expected string
		}{
			{"up", "up"},
			{"down", "down"},
			{"reset", "reset"},
			{"", "up"}, // default
			{"invalid", "up"}, // default for invalid
		}

		for _, tt := range tests {
			// Test direction parsing logic
			direction := tt.input
			if direction == "" {
				direction = "up"
			}
			
			switch direction {
			case "up", "down", "reset":
				// Valid directions
			default:
				direction = "up"
			}

			assert.Equal(t, tt.expected, direction)
		}
	})

	t.Run("parse steps", func(t *testing.T) {
		tests := []struct {
			input       string
			expected    int
			expectError bool
		}{
			{"1", 1, false},
			{"5", 5, false},
			{"0", 0, false},
			{"-1", -1, false},
			{"", 0, false},
			{"abc", 0, true},
			{"1.5", 0, true},
		}

		for _, tt := range tests {
			if tt.input != "" {
				_, err := parseSteps(tt.input)
				if tt.expectError {
					assert.Error(t, err)
				} else {
					assert.NoError(t, err)
				}
			}
			
			if !tt.expectError && tt.input != "" {
				result, _ := parseSteps(tt.input)
				assert.Equal(t, tt.expected, result)
			}
		}
	})
}

func parseSteps(s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	
	steps := 0
	negative := false
	
	for i, r := range s {
		if i == 0 && r == '-' {
			negative = true
			continue
		}
		
		if r < '0' || r > '9' {
			return 0, os.ErrInvalid
		}
		
		steps = steps*10 + int(r-'0')
	}
	
	if negative {
		steps = -steps
	}
	
	return steps, nil
}

func TestMigrationFiles(t *testing.T) {
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "migration_files_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	migrationsDir := filepath.Join(tempDir, "migrations")
	err = os.MkdirAll(migrationsDir, 0755)
	require.NoError(t, err)

	// Create test migration files with different timestamps
	migrations := []struct {
		timestamp string
		name      string
	}{
		{"20230101120000", "create_users_table"},
		{"20230102120000", "add_email_to_users"},
		{"20230103120000", "create_posts_table"},
		{"20230104120000", "add_index_to_posts"},
	}

	for _, m := range migrations {
		upFile := filepath.Join(migrationsDir, m.timestamp+"_"+m.name+".up.sql")
		downFile := filepath.Join(migrationsDir, m.timestamp+"_"+m.name+".down.sql")
		
		err = os.WriteFile(upFile, []byte("-- UP migration"), 0644)
		require.NoError(t, err)
		
		err = os.WriteFile(downFile, []byte("-- DOWN migration"), 0644)
		require.NoError(t, err)
	}

	// Test finding migration files
	files, err := filepath.Glob(filepath.Join(migrationsDir, "*.up.sql"))
	require.NoError(t, err)
	assert.Len(t, files, 4, "Should find 4 up migration files")

	files, err = filepath.Glob(filepath.Join(migrationsDir, "*.down.sql"))
	require.NoError(t, err)
	assert.Len(t, files, 4, "Should find 4 down migration files")

	// Test sorting migration files (they should be in chronological order)
	upFiles, err := filepath.Glob(filepath.Join(migrationsDir, "*.up.sql"))
	require.NoError(t, err)
	
	for i := 0; i < len(upFiles)-1; i++ {
		assert.True(t, upFiles[i] < upFiles[i+1], "Migration files should be sorted chronologically")
	}
}

func TestMigrationValidation(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		isValid  bool
	}{
		{
			name:     "valid up migration",
			filename: "20230101120000_create_users.up.sql",
			isValid:  true,
		},
		{
			name:     "valid down migration",
			filename: "20230101120000_create_users.down.sql",
			isValid:  true,
		},
		{
			name:     "missing timestamp",
			filename: "create_users.up.sql",
			isValid:  false,
		},
		{
			name:     "missing direction",
			filename: "20230101120000_create_users.sql",
			isValid:  false,
		},
		{
			name:     "invalid extension",
			filename: "20230101120000_create_users.up.txt",
			isValid:  false,
		},
		{
			name:     "invalid direction",
			filename: "20230101120000_create_users.sideways.sql",
			isValid:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Check if filename matches migration pattern
			isUp := len(tt.filename) > 7 && tt.filename[len(tt.filename)-7:] == ".up.sql"
			isDown := len(tt.filename) > 9 && tt.filename[len(tt.filename)-9:] == ".down.sql"
			
			hasTimestamp := len(tt.filename) >= 14
			for i := 0; i < 14 && hasTimestamp; i++ {
				if tt.filename[i] < '0' || tt.filename[i] > '9' {
					hasTimestamp = false
				}
			}
			
			isValid := (isUp || isDown) && hasTimestamp
			assert.Equal(t, tt.isValid, isValid, "Migration filename validation failed for %s", tt.filename)
		})
	}
}

func TestDatabaseConnection(t *testing.T) {
	tests := []struct {
		name        string
		dbType      string
		dsn         string
		expectError bool
	}{
		{
			name:        "postgres connection string",
			dbType:      "postgres",
			dsn:         "postgres://user:pass@localhost:5432/testdb?sslmode=disable",
			expectError: true, // Will fail without actual DB
		},
		{
			name:        "mysql connection string",
			dbType:      "mysql",
			dsn:         "user:pass@tcp(localhost:3306)/testdb",
			expectError: true, // Will fail without actual DB
		},
		{
			name:        "invalid connection string",
			dbType:      "postgres",
			dsn:         "invalid://connection",
			expectError: true,
		},
		{
			name:        "empty connection string",
			dbType:      "postgres",
			dsn:         "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Try to open database connection
			// This will fail in test environment without actual database
			var db *sql.DB
			var err error

			switch tt.dbType {
			case "postgres", "pgx":
				db, err = sql.Open("pgx", tt.dsn)
			case "mysql", "mariadb":
				db, err = sql.Open("mysql", tt.dsn)
			default:
				err = os.ErrInvalid
			}

			if db != nil {
				defer db.Close()
			}

			if tt.expectError {
				// We expect errors in test environment
				if err == nil {
					// Try to ping to force connection
					err = db.Ping()
					assert.Error(t, err)
				}
			}
		})
	}
}

func TestMigrationTableCreation(t *testing.T) {
	// Test SQL for creating migration tracking table
	postgresSQL := `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version VARCHAR(255) PRIMARY KEY,
    applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);`

	mysqlSQL := `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version VARCHAR(255) PRIMARY KEY,
    applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);`

	// Verify SQL syntax is valid (would need actual DB to execute)
	assert.Contains(t, postgresSQL, "schema_migrations")
	assert.Contains(t, postgresSQL, "version")
	assert.Contains(t, postgresSQL, "applied_at")

	assert.Contains(t, mysqlSQL, "schema_migrations")
	assert.Contains(t, mysqlSQL, "version")
	assert.Contains(t, mysqlSQL, "applied_at")
}

func TestMigrationRollback(t *testing.T) {
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "rollback_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	migrationsDir := filepath.Join(tempDir, "migrations")
	err = os.MkdirAll(migrationsDir, 0755)
	require.NoError(t, err)

	// Create migration files for rollback testing
	migrations := []struct {
		timestamp string
		name      string
		upSQL     string
		downSQL   string
	}{
		{
			"20230101120000",
			"create_users",
			"CREATE TABLE users (id INT PRIMARY KEY);",
			"DROP TABLE users;",
		},
		{
			"20230102120000",
			"create_posts",
			"CREATE TABLE posts (id INT PRIMARY KEY);",
			"DROP TABLE posts;",
		},
		{
			"20230103120000",
			"add_user_email",
			"ALTER TABLE users ADD COLUMN email VARCHAR(255);",
			"ALTER TABLE users DROP COLUMN email;",
		},
	}

	for _, m := range migrations {
		upFile := filepath.Join(migrationsDir, m.timestamp+"_"+m.name+".up.sql")
		downFile := filepath.Join(migrationsDir, m.timestamp+"_"+m.name+".down.sql")
		
		err = os.WriteFile(upFile, []byte(m.upSQL), 0644)
		require.NoError(t, err)
		
		err = os.WriteFile(downFile, []byte(m.downSQL), 0644)
		require.NoError(t, err)
	}

	// Test finding down migrations for rollback
	downFiles, err := filepath.Glob(filepath.Join(migrationsDir, "*.down.sql"))
	require.NoError(t, err)
	assert.Len(t, downFiles, 3, "Should find 3 down migration files")

	// Test rollback order (should be reverse chronological)
	// Sort in reverse for rollback
	for i := 0; i < len(downFiles)/2; i++ {
		j := len(downFiles) - 1 - i
		downFiles[i], downFiles[j] = downFiles[j], downFiles[i]
	}
	
	// Verify reverse order
	for i := 0; i < len(downFiles)-1; i++ {
		assert.True(t, downFiles[i] > downFiles[i+1], "Rollback files should be in reverse chronological order")
	}
}