package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// SQLResetStore is a ResetStore over database/sql.
//
// It exists because Consume has to be atomic, and that is exactly the
// requirement an application implementing the interface by hand will get wrong:
// a SELECT to check the token followed by an UPDATE to mark it used lets two
// concurrent requests both see it as unused, and the prize for winning that
// race is somebody else's account.
//
// Shipping a correct implementation is cheaper than documenting the hazard and
// hoping.
type SQLResetStore struct {
	db      *sql.DB
	dialect Dialect
	table   string
}

// Dialect selects placeholder syntax and the atomic-claim strategy.
type Dialect int

const (
	// DialectPostgres uses $1 placeholders and UPDATE ... RETURNING.
	DialectPostgres Dialect = iota
	// DialectMySQL uses ? placeholders and a transaction with SELECT ... FOR UPDATE.
	DialectMySQL
	// DialectSQLite uses ? placeholders and a transaction; SQLite serialises
	// writers, which provides the same exclusivity.
	DialectSQLite
)

// NewSQLResetStore returns a store using the default table name.
func NewSQLResetStore(db *sql.DB, dialect Dialect) *SQLResetStore {
	return &SQLResetStore{db: db, dialect: dialect, table: "tjo_reset_tokens"}
}

// WithTable overrides the table name.
func (s *SQLResetStore) WithTable(name string) *SQLResetStore {
	s.table = name
	return s
}

func (s *SQLResetStore) rebind(q string) string {
	if s.dialect != DialectPostgres {
		return q
	}
	var b strings.Builder
	n := 0
	for _, r := range q {
		if r == '?' {
			n++
			fmt.Fprintf(&b, "$%d", n)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Migrate creates the table if it does not exist.
func (s *SQLResetStore) Migrate(ctx context.Context) error {
	var ddl string
	switch s.dialect {
	case DialectPostgres:
		ddl = `CREATE TABLE IF NOT EXISTS ` + s.table + ` (
			hash      BYTEA PRIMARY KEY,
			user_id   TEXT NOT NULL,
			purpose   TEXT NOT NULL,
			expiry    TIMESTAMPTZ NOT NULL,
			used_at   TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL
		)`
	case DialectMySQL:
		ddl = `CREATE TABLE IF NOT EXISTS ` + s.table + ` (
			hash      VARBINARY(64) PRIMARY KEY,
			user_id   VARCHAR(191) NOT NULL,
			purpose   VARCHAR(64) NOT NULL,
			expiry    DATETIME NOT NULL,
			used_at   DATETIME NULL,
			created_at DATETIME NOT NULL
		)`
	default:
		ddl = `CREATE TABLE IF NOT EXISTS ` + s.table + ` (
			hash      BLOB PRIMARY KEY,
			user_id   TEXT NOT NULL,
			purpose   TEXT NOT NULL,
			expiry    DATETIME NOT NULL,
			used_at   DATETIME,
			created_at DATETIME NOT NULL
		)`
	}

	if _, err := s.db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("auth: migrate reset tokens: %w", err)
	}

	// InvalidateUser filters on both, so the index has to cover both or every
	// password change scans the table.
	_, err := s.db.ExecContext(ctx,
		`CREATE INDEX IF NOT EXISTS `+s.table+`_user ON `+s.table+` (user_id, purpose)`)
	return err
}

func (s *SQLResetStore) Save(ctx context.Context, t *ResetToken) error {
	// Only the hash is written. The plaintext exists in the emailed link and
	// nowhere else, so a database read gives an attacker nothing redeemable.
	_, err := s.db.ExecContext(ctx, s.rebind(
		`INSERT INTO `+s.table+` (hash, user_id, purpose, expiry, created_at) VALUES (?, ?, ?, ?, ?)`),
		t.Hash, t.UserID, string(t.Purpose), t.Expiry.UTC(), time.Now().UTC())
	return err
}

// Consume claims a token atomically.
func (s *SQLResetStore) Consume(ctx context.Context, hash []byte, purpose ResetPurpose) (*ResetToken, error) {
	now := time.Now().UTC()

	if s.dialect == DialectPostgres {
		// One statement: the UPDATE both checks and claims, so two concurrent
		// requests cannot both see used_at IS NULL.
		row := s.db.QueryRowContext(ctx, s.rebind(
			`UPDATE `+s.table+`
			 SET used_at = ?
			 WHERE hash = ? AND purpose = ? AND used_at IS NULL AND expiry > ?
			 RETURNING user_id, purpose, expiry`),
			now, hash, string(purpose), now)
		return scanReset(row, hash)
	}

	// MySQL and SQLite: a transaction around the same logic. RowsAffected on
	// the UPDATE is what proves this caller won, rather than a prior SELECT
	// that another caller could also have satisfied.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`UPDATE `+s.table+`
		 SET used_at = ?
		 WHERE hash = ? AND purpose = ? AND used_at IS NULL AND expiry > ?`,
		now, hash, string(purpose), now)
	if err != nil {
		return nil, err
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		if err != nil {
			return nil, err
		}
		return nil, ErrInvalidReset
	}

	row := tx.QueryRowContext(ctx,
		`SELECT user_id, purpose, expiry FROM `+s.table+` WHERE hash = ?`, hash)

	token, err := scanReset(row, hash)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return token, nil
}

func scanReset(row *sql.Row, hash []byte) (*ResetToken, error) {
	var (
		userID, purpose string
		expiry          time.Time
	)
	switch err := row.Scan(&userID, &purpose, &expiry); {
	case errors.Is(err, sql.ErrNoRows):
		return nil, ErrInvalidReset
	case err != nil:
		return nil, err
	}

	return &ResetToken{
		Hash:    hash,
		UserID:  userID,
		Purpose: ResetPurpose(purpose),
		Expiry:  expiry,
	}, nil
}

func (s *SQLResetStore) InvalidateUser(ctx context.Context, userID string, purpose ResetPurpose) error {
	_, err := s.db.ExecContext(ctx, s.rebind(
		`UPDATE `+s.table+` SET used_at = ? WHERE user_id = ? AND purpose = ? AND used_at IS NULL`),
		time.Now().UTC(), userID, string(purpose))
	return err
}

// DeleteExpired removes spent and expired rows.
//
// Without this the table grows forever: every password-reset request anyone
// ever made stays in it. Run it from the scheduler.
func (s *SQLResetStore) DeleteExpired(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-olderThan)

	res, err := s.db.ExecContext(ctx, s.rebind(
		`DELETE FROM `+s.table+` WHERE expiry < ? OR (used_at IS NOT NULL AND used_at < ?)`),
		cutoff, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
