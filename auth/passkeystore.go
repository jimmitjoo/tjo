package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// SQLPasskeyStore is a PasskeyStore over database/sql.
//
// One row per credential, and the credential itself is one opaque column. There
// is no column for the public key, the attestation format or the signature
// counter, and that is the whole design: decomposing the credential into fields
// would freeze this library's idea of a credential into the schema, which is
// exactly the migration the record format exists to avoid.
type SQLPasskeyStore struct {
	db      *sql.DB
	dialect Dialect
	table   string
}

// NewSQLPasskeyStore returns a store using the default table name.
func NewSQLPasskeyStore(db *sql.DB, dialect Dialect) *SQLPasskeyStore {
	return &SQLPasskeyStore{db: db, dialect: dialect, table: "tjo_passkeys"}
}

// WithTable overrides the table name.
func (s *SQLPasskeyStore) WithTable(name string) *SQLPasskeyStore {
	s.table = name
	return s
}

func (s *SQLPasskeyStore) rebind(q string) string {
	if s.dialect != DialectPostgres {
		return q
	}
	return rebindPostgres(q)
}

// Migrate creates the table if it does not exist.
func (s *SQLPasskeyStore) Migrate(ctx context.Context) error {
	var ddl string
	switch s.dialect {
	case DialectPostgres:
		ddl = `CREATE TABLE IF NOT EXISTS ` + s.table + ` (
			credential_id BYTEA PRIMARY KEY,
			account_id    TEXT NOT NULL,
			record        TEXT NOT NULL,
			label         TEXT,
			created_at    TIMESTAMPTZ NOT NULL
		)`
	case DialectMySQL:
		ddl = `CREATE TABLE IF NOT EXISTS ` + s.table + ` (
			credential_id VARBINARY(255) PRIMARY KEY,
			account_id    VARCHAR(191) NOT NULL,
			record        TEXT NOT NULL,
			label         VARCHAR(191),
			created_at    DATETIME NOT NULL
		)`
	default:
		ddl = `CREATE TABLE IF NOT EXISTS ` + s.table + ` (
			credential_id BLOB PRIMARY KEY,
			account_id    TEXT NOT NULL,
			record        TEXT NOT NULL,
			label         TEXT,
			created_at    DATETIME NOT NULL
		)`
	}

	if _, err := s.db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("auth: migrate passkeys: %w", err)
	}

	// Every registration ceremony lists an account's credentials to build the
	// exclude list, so this lookup is on the hot path of both ceremonies.
	_, err := s.db.ExecContext(ctx,
		`CREATE INDEX IF NOT EXISTS `+s.table+`_account ON `+s.table+` (account_id)`)
	return err
}

// AddPasskey stores a credential.
//
// The credential id is the primary key, so registering the same credential
// twice is a conflict rather than a duplicate row -- an authenticator that
// somehow re-registers an existing key does not silently produce two rows that
// revocation then has to remove one at a time.
func (s *SQLPasskeyStore) AddPasskey(ctx context.Context, accountID string, rec *PasskeyRecord, label string) error {
	if accountID == "" || rec == nil || len(rec.CredentialID) == 0 {
		return errors.New("auth: passkey needs an account and a credential id")
	}

	_, err := s.db.ExecContext(ctx, s.rebind(
		`INSERT INTO `+s.table+` (credential_id, account_id, record, label, created_at) VALUES (?, ?, ?, ?, ?)`),
		rec.CredentialID, accountID, rec.Record, label, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("auth: store passkey: %w", err)
	}
	return nil
}

// PasskeysFor returns every credential of an account, newest first.
func (s *SQLPasskeyStore) PasskeysFor(ctx context.Context, accountID string) ([]*PasskeyRecord, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(
		`SELECT credential_id, record FROM `+s.table+` WHERE account_id = ? ORDER BY created_at DESC`),
		accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*PasskeyRecord
	for rows.Next() {
		var (
			credentialID []byte
			record       string
		)
		if err := rows.Scan(&credentialID, &record); err != nil {
			return nil, err
		}
		rec, err := hydrate(credentialID, record)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// PasskeyByCredentialID finds the account a credential belongs to.
func (s *SQLPasskeyStore) PasskeyByCredentialID(ctx context.Context, credentialID []byte) (string, *PasskeyRecord, error) {
	var (
		accountID string
		record    string
	)
	err := s.db.QueryRowContext(ctx, s.rebind(
		`SELECT account_id, record FROM `+s.table+` WHERE credential_id = ?`), credentialID).
		Scan(&accountID, &record)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		// Not an error. An unknown credential is a normal thing for an
		// authenticator to present -- a revoked key, a different deployment --
		// and the ceremony turns it into one indistinguishable failure.
		return "", nil, nil
	case err != nil:
		return "", nil, err
	}

	rec, err := hydrate(credentialID, record)
	if err != nil {
		return "", nil, err
	}
	return accountID, rec, nil
}

// RevokePasskey removes one credential from an account.
//
// Scoped by account as well as credential id, so a caller that mixes up whose
// credential it is holding removes nothing rather than someone else's key.
func (s *SQLPasskeyStore) RevokePasskey(ctx context.Context, accountID string, credentialID []byte) error {
	_, err := s.db.ExecContext(ctx, s.rebind(
		`DELETE FROM `+s.table+` WHERE account_id = ? AND credential_id = ?`), accountID, credentialID)
	return err
}

// hydrate fills in the fields that are derivable from the record, so nothing
// downstream is tempted to keep a second copy of them in a column.
func hydrate(credentialID []byte, record string) (*PasskeyRecord, error) {
	var payload json.RawMessage
	transports, err := DecodePasskey(record, &payload)
	if err != nil {
		return nil, err
	}
	return &PasskeyRecord{Record: record, CredentialID: credentialID, Transports: transports}, nil
}
