package database

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "github.com/mattn/go-sqlite3"
)

func TestSQLiteIntegration(t *testing.T) {
	// Create a temporary SQLite database file
	tmpFile := "/tmp/test_tjo.db"
	defer os.Remove(tmpFile)

	t.Run("SQLite connection", func(t *testing.T) {
		db, err := sql.Open("sqlite3", tmpFile)
		require.NoError(t, err)
		defer db.Close()

		err = db.Ping()
		require.NoError(t, err)
	})

	t.Run("SQLite with query builder", func(t *testing.T) {
		db, err := sql.Open("sqlite3", tmpFile)
		require.NoError(t, err)
		defer db.Close()

		// Create a test table and clear any existing data
		_, err = db.Exec(`DROP TABLE IF EXISTS users`)
		require.NoError(t, err)
		
		_, err = db.Exec(`
			CREATE TABLE users (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				name TEXT NOT NULL,
				email TEXT NOT NULL,
				age INTEGER,
				created_at DATETIME DEFAULT CURRENT_TIMESTAMP
			)
		`)
		require.NoError(t, err)

		qb := NewQueryBuilder(db)

		// Test insert
		result, err := qb.Table("users").
			Insert(map[string]interface{}{
				"name":  "John Doe",
				"email": "john@example.com",
				"age":   30,
			})
		require.NoError(t, err)

		id, err := result.LastInsertId()
		require.NoError(t, err)
		assert.Greater(t, id, int64(0))

		// Test select
		rows, err := qb.Table("users").
			Where("email", "=", "john@example.com").
			Get()
		require.NoError(t, err)
		defer rows.Close()

		var users []map[string]interface{}
		columns, err := rows.Columns()
		require.NoError(t, err)

		for rows.Next() {
			user := make(map[string]interface{})
			values := make([]interface{}, len(columns))
			pointers := make([]interface{}, len(columns))
			for i := range values {
				pointers[i] = &values[i]
			}

			err = rows.Scan(pointers...)
			require.NoError(t, err)

			for i, col := range columns {
				user[col] = values[i]
			}
			users = append(users, user)
		}

		assert.Len(t, users, 1)
		nameValue := users[0]["name"]
		var name string
		if b, ok := nameValue.([]byte); ok {
			name = string(b)
		} else if s, ok := nameValue.(string); ok {
			name = s
		}
		assert.Equal(t, "John Doe", name)

		// Test update
		result, err = qb.Table("users").
			Where("email", "=", "john@example.com").
			Update(map[string]interface{}{
				"age": 31,
			})
		require.NoError(t, err)

		rowsAffected, err := result.RowsAffected()
		require.NoError(t, err)
		assert.Equal(t, int64(1), rowsAffected)
	})

	t.Run("SQLite with health checker", func(t *testing.T) {
		db, err := sql.Open("sqlite3", tmpFile)
		require.NoError(t, err)
		defer db.Close()

		hc := NewHealthChecker(db, 5*time.Second)

		ctx := context.Background()
		status := hc.Check(ctx)

		assert.Equal(t, "healthy", status.Status)
		assert.Empty(t, status.Errors)
		assert.Equal(t, "SQLite", status.DatabaseType)
		assert.NotEmpty(t, status.Version)
		assert.Equal(t, "passed", status.Checks["ping"])
		assert.Equal(t, "passed", status.Checks["query_execution"])
		assert.Equal(t, "passed", status.Checks["database_info"])
	})

	t.Run("SQLite with seeder", func(t *testing.T) {
		db, err := sql.Open("sqlite3", tmpFile)
		require.NoError(t, err)
		defer db.Close()

		// Create a test table
		_, err = db.Exec(`
			CREATE TABLE IF NOT EXISTS test_products (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				name TEXT NOT NULL,
				price DECIMAL(10,2),
				category TEXT,
				in_stock BOOLEAN DEFAULT 1
			)
		`)
		require.NoError(t, err)

		// Create a seeder for products
		seeder := &BaseSeeder{TableName: "test_products"}

		// Test bulk insert with SQLite
		testData := []map[string]interface{}{
			{"name": "Laptop", "price": 999.99, "category": "Electronics", "in_stock": true},
			{"name": "Mouse", "price": 29.99, "category": "Electronics", "in_stock": true},
			{"name": "Desk", "price": 299.99, "category": "Furniture", "in_stock": false},
		}

		err = seeder.BulkInsert(db, testData)
		require.NoError(t, err)

		// Verify data was inserted
		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM test_products").Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 3, count)

		// Test with factory
		factory := NewFactory()
		faker := NewFaker()

		template := map[string]interface{}{
			"name":     faker.Sequence("Product-%d"),
			"price":    faker.RandomInt(10, 1000),
			"category": "Generated",
			"in_stock": faker.Boolean(),
		}

		factory.Create(5, template)
		factoryData := factory.GetData()

		err = seeder.BulkInsert(db, factoryData)
		require.NoError(t, err)

		// Verify total count
		err = db.QueryRow("SELECT COUNT(*) FROM test_products").Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 8, count) // 3 manual + 5 generated
	})

	t.Run("SQLite in-memory database", func(t *testing.T) {
		db, err := sql.Open("sqlite3", ":memory:")
		require.NoError(t, err)
		defer db.Close()

		// Create test table
		_, err = db.Exec(`
			CREATE TABLE memory_test (
				id INTEGER PRIMARY KEY,
				data TEXT
			)
		`)
		require.NoError(t, err)

		qb := NewQueryBuilder(db)

		// Test insert and select
		_, err = qb.Table("memory_test").
			Insert(map[string]interface{}{
				"data": "test data",
			})
		require.NoError(t, err)

		rows, err := qb.Table("memory_test").Get()
		require.NoError(t, err)
		defer rows.Close()

		var results []map[string]interface{}
		columns, err := rows.Columns()
		require.NoError(t, err)

		for rows.Next() {
			result := make(map[string]interface{})
			values := make([]interface{}, len(columns))
			pointers := make([]interface{}, len(columns))
			for i := range values {
				pointers[i] = &values[i]
			}

			err = rows.Scan(pointers...)
			require.NoError(t, err)

			for i, col := range columns {
				result[col] = values[i]
			}
			results = append(results, result)
		}

		assert.Len(t, results, 1)
		dataValue := results[0]["data"]
		var data string
		if b, ok := dataValue.([]byte); ok {
			data = string(b)
		} else if s, ok := dataValue.(string); ok {
			data = s
		}
		assert.Equal(t, "test data", data)

		// Test health check on in-memory DB
		hc := NewHealthChecker(db, 5*time.Second)
		ctx := context.Background()
		status := hc.Check(ctx)

		assert.Equal(t, "healthy", status.Status)
		assert.Equal(t, "SQLite", status.DatabaseType)
	})
}

func TestSQLiteConnectionPooling(t *testing.T) {
	tmpFile := "/tmp/test_pool_tjo.db"
	defer os.Remove(tmpFile)

	db, err := sql.Open("sqlite3", tmpFile+"?cache=shared")
	require.NoError(t, err)
	defer db.Close()

	// Configure connection pool
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(time.Hour)

	t.Run("connection pool stats", func(t *testing.T) {
		cm := NewConnectionMonitor(db, 10, 0.8)
		stats := cm.GetConnectionStats()

		assert.GreaterOrEqual(t, stats.MaxOpenConnections, 0)
		assert.GreaterOrEqual(t, stats.OpenConnections, 0)
		assert.LessOrEqual(t, stats.UtilizationPercent, 100.0)
	})

	t.Run("concurrent SQLite operations", func(t *testing.T) {
		// Create test table
		_, err := db.Exec(`
			CREATE TABLE IF NOT EXISTS concurrent_test (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				thread_id INTEGER,
				data TEXT,
				created_at DATETIME DEFAULT CURRENT_TIMESTAMP
			)
		`)
		require.NoError(t, err)

		// Run concurrent inserts
		done := make(chan bool, 5)
		for i := 0; i < 5; i++ {
			go func(threadID int) {
				qb := NewQueryBuilder(db)
				_, err := qb.Table("concurrent_test").
					Insert(map[string]interface{}{
						"thread_id": threadID,
						"data":      "concurrent data",
					})
				assert.NoError(t, err)
				done <- true
			}(i)
		}

		// Wait for all goroutines to complete
		for i := 0; i < 5; i++ {
			<-done
		}

		// Verify all inserts completed
		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM concurrent_test").Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 5, count)
	})
}