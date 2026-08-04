package tjo

import (
	"database/sql"

	// Only stdlib performs the sql.Register call, so pgconn and pgx itself do
	// not need blank-importing alongside it. pgx v4 was a dead end: GO-2026-5004
	// is a SQL injection with no fix in the v4 line, and pgproto3/v2 carries
	// GO-2026-4518 on the same terms. v5/stdlib registers under the same "pgx"
	// driver name, so the string dispatch elsewhere is unaffected.
	_ "github.com/jackc/pgx/v5/stdlib"

	// modernc.org/sqlite rather than mattn/go-sqlite3: pure Go, no cgo.
	//
	// This costs measurable performance. Benchmarked on darwin/arm64, one write
	// connection, WAL, synchronous=NORMAL:
	//
	//	insert   mattn  6699 ns/op   modernc   8273 ns/op   1.2x slower
	//	select   mattn 32388 ns/op   modernc 100475 ns/op   3.1x slower
	//
	// It is bought deliberately. cgo makes `GOOS=linux CGO_ENABLED=0 go build`
	// impossible, and that cross-compile is not a convenience -- it is what
	// `tjo deploy` does, and what lets the release matrix be one runner instead
	// of five with a native builder per target. It also makes reproducible
	// builds nearly free: -trimpath plus a fixed build date, with no C toolchain
	// varying underneath.
	//
	// For a request doing a handful of point lookups the difference is tens of
	// microseconds against template rendering and network. If that stops being
	// true for someone, the answer is Postgres, which this framework supports
	// and which is the right call at that point anyway.
	_ "modernc.org/sqlite"
)

func (g *Tjo) OpenDB(dbType, dsn string) (*sql.DB, error) {
	if dbType == "postgres" || dbType == "postgresql" {
		dbType = "pgx"
	} else if dbType == "mysql" || dbType == "mariadb" {
		dbType = "mysql"
	} else if dbType == "sqlite" || dbType == "sqlite3" {
		// modernc registers as "sqlite"; mattn registered as "sqlite3". Both
		// spellings are accepted from configuration and normalised here, so
		// SQLITE_* settings written against either keep working.
		dbType = "sqlite"
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
