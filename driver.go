package tjo

import (
	"database/sql"

	// Only stdlib performs the sql.Register call, so pgconn and pgx itself do
	// not need blank-importing alongside it. pgx v4 was a dead end: GO-2026-5004
	// is a SQL injection with no fix in the v4 line, and pgproto3/v2 carries
	// GO-2026-4518 on the same terms. v5/stdlib registers under the same "pgx"
	// driver name, so the string dispatch elsewhere is unaffected.
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "github.com/mattn/go-sqlite3"
)

func (g *Tjo) OpenDB(dbType, dsn string) (*sql.DB, error) {
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
