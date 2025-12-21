package data

import (
	"database/sql"
	"fmt"
	"os"

	up "github.com/upper/db/v4"
	"github.com/upper/db/v4/adapter/mysql"
	"github.com/upper/db/v4/adapter/postgresql"
	"github.com/upper/db/v4/adapter/sqlite"
)

// upper is the database session used by all models
var upper up.Session

// InitDB initializes the upper/db session from an existing *sql.DB connection.
// This should be called during application startup after the database connection is established.
func InitDB(db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("database connection is nil")
	}

	var err error
	dbType := os.Getenv("DATABASE_TYPE")

	switch dbType {
	case "postgres", "postgresql", "pgx":
		upper, err = postgresql.New(db)
	case "mysql", "mariadb":
		upper, err = mysql.New(db)
	case "sqlite", "sqlite3":
		upper, err = sqlite.New(db)
	default:
		return fmt.Errorf("unsupported database type: %s", dbType)
	}

	if err != nil {
		return fmt.Errorf("failed to initialize upper/db session: %w", err)
	}

	return nil
}

// getInsertID extracts the integer ID from an insert result.
// upper/db returns different types depending on the database driver,
// so this helper normalizes the result.
func getInsertID(id interface{}) int {
	switch v := id.(type) {
	case int64:
		return int(v)
	case int:
		return v
	case uint64:
		return int(v)
	case uint:
		return int(v)
	default:
		return 0
	}
}
