package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jimmitjoo/gemquick/config"
	"github.com/jimmitjoo/gemquick/core"
	"github.com/stretchr/testify/assert"
)

func TestGetDSN(t *testing.T) {
	tests := []struct {
		name     string
		dbType   string
		env      map[string]string
		expected string
	}{
		{
			name:   "PostgreSQL with password",
			dbType: "postgres",
			env: map[string]string{
				"DATABASE_USER":     "user",
				"DATABASE_PASS":     "pass",
				"DATABASE_HOST":     "localhost",
				"DATABASE_PORT":     "5432",
				"DATABASE_NAME":     "testdb",
				"DATABASE_SSL_MODE": "disable",
			},
			expected: "postgres://user:pass@localhost:5432/testdb?sslmode=disable&timezone=UTC&connect_timeout=5",
		},
		{
			name:   "PostgreSQL without password",
			dbType: "postgres",
			env: map[string]string{
				"DATABASE_USER":     "user",
				"DATABASE_HOST":     "localhost",
				"DATABASE_PORT":     "5432",
				"DATABASE_NAME":     "testdb",
				"DATABASE_SSL_MODE": "disable",
			},
			expected: "postgres://user@localhost:5432/testdb?sslmode=disable&timezone=UTC&connect_timeout=5",
		},
		{
			name:   "PGX type",
			dbType: "pgx",
			env: map[string]string{
				"DATABASE_USER":     "user",
				"DATABASE_PASS":     "pass",
				"DATABASE_HOST":     "localhost",
				"DATABASE_PORT":     "5432",
				"DATABASE_NAME":     "testdb",
				"DATABASE_SSL_MODE": "disable",
			},
			expected: "postgres://user:pass@localhost:5432/testdb?sslmode=disable&timezone=UTC&connect_timeout=5",
		},
		{
			name:   "MySQL",
			dbType: "mysql",
			env: map[string]string{
				"DATABASE_TYPE": "mysql",
				"DATABASE_USER": "root",
				"DATABASE_PASS": "pass",
				"DATABASE_HOST": "localhost",
				"DATABASE_PORT": "3306",
				"DATABASE_NAME": "testdb",
			},
			expected: "mysql://root:pass@tcp(localhost:3306)/testdb?collation=utf8mb4_unicode_ci&parseTime=true&loc=UTC&timeout=5s",
		},
	}

	// Save and restore cfg
	originalCfg := cfg
	defer func() { cfg = originalCfg }()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save and restore environment
			oldEnv := make(map[string]string)
			for k := range tt.env {
				oldEnv[k] = os.Getenv(k)
			}
			defer func() {
				for k, v := range oldEnv {
					if v == "" {
						os.Unsetenv(k)
					} else {
						os.Setenv(k, v)
					}
				}
			}()

			// Set test environment
			for k, v := range tt.env {
				os.Setenv(k, v)
			}

			// Load config after setting environment variables
			loadedCfg, err := config.Load()
			if err != nil {
				t.Fatalf("Failed to load config: %v", err)
			}
			cfg = &core.CLIConfig{
				RootPath: ".",
				DBType:   tt.dbType,
				Config:   loadedCfg,
			}

			result := getDSN()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSetup(t *testing.T) {
	tests := []struct {
		name string
		arg1 string
		arg2 string
	}{
		{
			name: "New command",
			arg1: "new",
			arg2: "myapp",
		},
		{
			name: "Version command",
			arg1: "version",
			arg2: "",
		},
		{
			name: "Help command",
			arg1: "help",
			arg2: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// These commands should not try to load .env
			setup(tt.arg1, tt.arg2)
		})
	}
}

func TestUpdateSourceFiles(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "cli_test")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create a test Go file
	testFile := filepath.Join(tempDir, "test.go")
	content := []byte("package main\n// myapp placeholder")
	err = os.WriteFile(testFile, content, 0644)
	assert.NoError(t, err)

	// Set appUrl for the test
	appUrl = "testapp"

	// Test updateSourceFiles
	info, err := os.Stat(testFile)
	assert.NoError(t, err)

	err = updateSourceFiles(testFile, info, nil)
	assert.NoError(t, err)

	// Read the updated file
	updated, err := os.ReadFile(testFile)
	assert.NoError(t, err)
	assert.Contains(t, string(updated), "testapp")
	assert.NotContains(t, string(updated), "myapp")
}

func TestUpdateSourceFiles_Directory(t *testing.T) {
	// Create a temporary directory
	tempDir, err := os.MkdirTemp("", "cli_test_dir")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	info, err := os.Stat(tempDir)
	assert.NoError(t, err)

	// Should return nil for directories
	err = updateSourceFiles(tempDir, info, nil)
	assert.NoError(t, err)
}

func TestUpdateSourceFiles_Error(t *testing.T) {
	// Test with an error passed in
	err := updateSourceFiles("", nil, os.ErrNotExist)
	assert.Error(t, err)
	assert.Equal(t, os.ErrNotExist, err)
}

func init() {
	// Initialize cfg for tests
	cfg = &core.CLIConfig{
		RootPath: ".",
		DBType:   "",
		Config:   nil,
	}
}