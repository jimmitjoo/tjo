package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "github.com/mattn/go-sqlite3"
)

// MockTjo simulates the Tjo struct for testing
type MockTjo struct {
	RootPath string
}

// OpenDB mirrors the OpenDB function from driver.go for testing
func (g *MockTjo) OpenDB(dbType, dsn string) (*sql.DB, error) {
	if dbType == "postgres" || dbType == "postgresql" {
		dbType = "pgx"
	} else if dbType == "mysql" || dbType == "mariadb" {
		dbType = "mysql"
	} else if dbType == "sqlite" || dbType == "sqlite3" {
		dbType = "sqlite3"
	}

	db, err := sql.Open(dbType, dsn)
	if err != nil {
		return nil, err
	}

	err = db.Ping()
	if err != nil {
		return nil, err
	}

	return db, nil
}

// BuildDSN mirrors the BuildDSN function from tjo.go for testing SQLite support
func (g *MockTjo) BuildDSN() string {
	var dsn string
	dbType := os.Getenv("DATABASE_TYPE")

	switch dbType {
	case "sqlite", "sqlite3":
		dbPath := os.Getenv("DATABASE_NAME")
		if dbPath == "" {
			dbPath = "test.db"
		}
		
		if !strings.HasPrefix(dbPath, "/") && !strings.Contains(dbPath, ":") {
			// Relative path - put it in the data directory
			dsn = fmt.Sprintf("%s/data/%s", g.RootPath, dbPath)
		} else {
			// Absolute path or special SQLite DSN (like :memory:)
			dsn = dbPath
		}
	default:
		// For this test, we only care about SQLite
	}

	return dsn
}

func TestTjoSQLiteIntegration(t *testing.T) {
	// Create a temporary directory structure
	tempDir := "/tmp/tjo_test"
	dataDir := tempDir + "/data"
	
	// Clean up after test
	defer func() {
		os.RemoveAll(tempDir)
	}()

	// Create directories
	err := os.MkdirAll(dataDir, 0755)
	require.NoError(t, err)

	tjo := &MockTjo{
		RootPath: tempDir,
	}

	t.Run("SQLite file database integration", func(t *testing.T) {
		// Set environment variables
		os.Setenv("DATABASE_TYPE", "sqlite3")
		os.Setenv("DATABASE_NAME", "integration_test.db")
		defer func() {
			os.Unsetenv("DATABASE_TYPE")
			os.Unsetenv("DATABASE_NAME")
		}()

		// Test DSN building
		dsn := tjo.BuildDSN()
		expectedDSN := tempDir + "/data/integration_test.db"
		assert.Equal(t, expectedDSN, dsn)

		// Test database connection
		db, err := tjo.OpenDB("sqlite3", dsn)
		require.NoError(t, err)
		defer db.Close()

		// Test that the database file was created
		_, err = os.Stat(expectedDSN)
		assert.NoError(t, err)

		// Test basic operations with all our database components
		t.Run("query builder integration", func(t *testing.T) {
			qb := NewQueryBuilder(db)
			
			// Create table
			_, err := db.Exec(`CREATE TABLE integration_users (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				name TEXT NOT NULL,
				email TEXT UNIQUE NOT NULL,
				created_at DATETIME DEFAULT CURRENT_TIMESTAMP
			)`)
			require.NoError(t, err)

			// Insert with query builder
			result, err := qb.Table("integration_users").Insert(map[string]interface{}{
				"name":  "Integration User",
				"email": "integration@example.com",
			})
			require.NoError(t, err)
			
			id, err := result.LastInsertId()
			require.NoError(t, err)
			assert.Greater(t, id, int64(0))
		})

		t.Run("health checker integration", func(t *testing.T) {
			hc := NewHealthChecker(db, 5*time.Second)
			ctx := context.Background()
			
			status := hc.Check(ctx)
			assert.Equal(t, "healthy", status.Status)
			assert.Equal(t, "SQLite", status.DatabaseType)
			assert.Empty(t, status.Errors)
		})

		t.Run("seeder integration", func(t *testing.T) {
			// Create test table for seeding
			_, err := db.Exec(`CREATE TABLE IF NOT EXISTS integration_products (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				name TEXT NOT NULL,
				price DECIMAL(10,2),
				category TEXT
			)`)
			require.NoError(t, err)

			// Test seeder
			seeder := &BaseSeeder{TableName: "integration_products"}
			
			// Use factory to create test data
			factory := NewFactory()
			faker := NewFaker()
			
			template := map[string]interface{}{
				"name":     faker.Sequence("Product-%d"),
				"price":    faker.RandomInt(10, 100),
				"category": "Test Category",
			}
			
			factory.Create(3, template)
			data := factory.GetData()
			
			err = seeder.BulkInsert(db, data)
			require.NoError(t, err)
			
			// Verify data was inserted
			var count int
			err = db.QueryRow("SELECT COUNT(*) FROM integration_products").Scan(&count)
			require.NoError(t, err)
			assert.Equal(t, 3, count)
		})
	})

	t.Run("SQLite memory database integration", func(t *testing.T) {
		// Set environment for in-memory database
		os.Setenv("DATABASE_TYPE", "sqlite")
		os.Setenv("DATABASE_NAME", ":memory:")
		defer func() {
			os.Unsetenv("DATABASE_TYPE")
			os.Unsetenv("DATABASE_NAME")
		}()

		// Test DSN building for in-memory database
		dsn := tjo.BuildDSN()
		assert.Equal(t, ":memory:", dsn)

		// Test database connection
		db, err := tjo.OpenDB("sqlite", dsn)
		require.NoError(t, err)
		defer db.Close()

		// Test that we can perform operations on in-memory database
		_, err = db.Exec(`CREATE TABLE memory_test (id INTEGER PRIMARY KEY, data TEXT)`)
		require.NoError(t, err)

		qb := NewQueryBuilder(db)
		result, err := qb.Table("memory_test").Insert(map[string]interface{}{
			"data": "memory test data",
		})
		require.NoError(t, err)

		id, err := result.LastInsertId()
		require.NoError(t, err)
		assert.Greater(t, id, int64(0))
	})

	t.Run("SQLite absolute path integration", func(t *testing.T) {
		absolutePath := "/tmp/absolute_test.db"
		defer os.Remove(absolutePath)

		// Set environment for absolute path database
		os.Setenv("DATABASE_TYPE", "sqlite3")
		os.Setenv("DATABASE_NAME", absolutePath)
		defer func() {
			os.Unsetenv("DATABASE_TYPE")
			os.Unsetenv("DATABASE_NAME")
		}()

		// Test DSN building for absolute path
		dsn := tjo.BuildDSN()
		assert.Equal(t, absolutePath, dsn)

		// Test database connection
		db, err := tjo.OpenDB("sqlite3", dsn)
		require.NoError(t, err)
		defer db.Close()

		// Verify the file was created at the absolute path
		_, err = os.Stat(absolutePath)
		assert.NoError(t, err)
	})
}