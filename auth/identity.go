package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Linking a provider identity to an account.
//
// This file is the security-relevant half of social login. The token exchange
// in oauth.go is mechanical; the question of *which account* a verified
// identity signs into is where this feature gets people breached, and the
// answer is one function, Resolve, with its rules written down.

// IdentityStore persists the mapping from a provider identity to an account.
//
// It does not own accounts. The application's users table is the
// application's, exactly as with Account and PasskeyStore -- an authentication
// library that insists on owning the user record leaves an application with
// two users tables and a join it cannot write.
type IdentityStore interface {
	// IdentityBySubject returns the account linked to a provider identity, or
	// ("", nil) when there is none.
	//
	// The pair (provider, subject) is the key, never the email. An email
	// changes; a subject does not, and keying on the email means a person who
	// changes their address at the provider signs into a stranger's account.
	IdentityBySubject(ctx context.Context, provider, subject string) (accountID string, err error)

	// LinkIdentity attaches an identity to an account, replacing any previous
	// identity from the same provider.
	LinkIdentity(ctx context.Context, accountID string, identity *Identity) error

	// UnlinkIdentity removes one provider from an account. Removing something
	// that is not there is not an error.
	UnlinkIdentity(ctx context.Context, accountID, provider string) error

	// IdentitiesFor returns every identity linked to an account.
	IdentitiesFor(ctx context.Context, accountID string) ([]*Identity, error)
}

// Outcome is what Resolve concluded.
type Outcome int

const (
	// SignedIn means AccountID is who this is. Renew the session.
	SignedIn Outcome = iota

	// NoAccount means nobody has this identity and nothing matched. Create an
	// account, then call Link.
	//
	// Do not mark the new account's email address verified unless
	// Identity.EmailVerified is set. GitHub, for one, returns an address its
	// owner typed and nobody checked.
	NoAccount

	// NeedsLogin means an account already uses this email address and the
	// person signing in has not proved they own it.
	//
	// Send them to the password or passkey form. Once they are signed in, the
	// same flow run again resolves to SignedIn, because Resolve links a new
	// identity to whoever is already authenticated.
	NeedsLogin
)

// Resolution is Resolve's answer.
type Resolution struct {
	Outcome   Outcome
	AccountID string

	// Identity is the identity that was resolved, for the NoAccount case where
	// the caller needs the email and name to create an account with.
	Identity *Identity
}

// ErrLinkedElsewhere means this provider identity already belongs to a
// different account than the one currently signed in.
//
// Silently moving it would be an account takeover with extra steps: anyone who
// can sign into a throwaway account could then attach a victim's Google
// identity and lose the victim their way in.
var ErrLinkedElsewhere = errors.New("auth: this provider account is already linked to a different account")

// ResolveOptions configures the linking policy.
type ResolveOptions struct {
	// CurrentAccountID is the account already signed in, or empty.
	//
	// This is the field that makes linking safe. A person who is signed in has
	// proved they own the account, so attaching a new provider to it is their
	// decision. A person who is not has proved nothing.
	CurrentAccountID string

	// Accounts finds accounts by email, so that a first-time social sign-in
	// with an address that already has an account leads to NeedsLogin rather
	// than to a second, orphaned account.
	//
	// Optional. Nil means never look, which yields NoAccount instead --
	// correct, but it gives the person two accounts and no explanation.
	Accounts AccountStore
}

// Resolve decides which account a verified identity signs into.
//
// The rules, in order:
//
//  1. The identity is already linked -- that account, and only that account. If
//     somebody else is signed in, ErrLinkedElsewhere.
//  2. Somebody is signed in -- link the identity to them. This is the only path
//     that attaches a provider to an existing account, and it requires proof of
//     ownership, which being signed in is.
//  3. Nobody is signed in and the email belongs to an existing account --
//     NeedsLogin. Never an automatic merge.
//  4. Otherwise -- NoAccount.
//
// Rule 3 is the one worth arguing about, because merging automatically is
// convenient and is how accounts get taken over: register an identity provider
// account with the victim's address, sign in, and be handed their account. Some
// providers verify addresses and some do not, and a flow that trusts the
// verified ones is a flow whose safety depends on every provider it is ever
// configured with. Requiring a login makes it depend on none of them.
func Resolve(ctx context.Context, store IdentityStore, identity *Identity, opts ResolveOptions) (Resolution, error) {
	if identity == nil || identity.Provider == "" || identity.Subject == "" {
		return Resolution{}, errors.New("auth: resolving an identity with no provider or subject")
	}

	linked, err := store.IdentityBySubject(ctx, identity.Provider, identity.Subject)
	if err != nil {
		return Resolution{}, err
	}

	if linked != "" {
		if opts.CurrentAccountID != "" && opts.CurrentAccountID != linked {
			return Resolution{}, ErrLinkedElsewhere
		}
		// Refreshed on every sign-in: the name and avatar are the provider's to
		// change, and a stale copy is the sort of thing nobody notices until a
		// person has been showing their old surname for a year.
		if err := store.LinkIdentity(ctx, linked, identity); err != nil {
			return Resolution{}, err
		}
		return Resolution{Outcome: SignedIn, AccountID: linked, Identity: identity}, nil
	}

	if opts.CurrentAccountID != "" {
		if err := store.LinkIdentity(ctx, opts.CurrentAccountID, identity); err != nil {
			return Resolution{}, err
		}
		return Resolution{Outcome: SignedIn, AccountID: opts.CurrentAccountID, Identity: identity}, nil
	}

	if opts.Accounts != nil && identity.Email != "" {
		existing, err := opts.Accounts.ByEmail(ctx, identity.Email)
		if err != nil {
			return Resolution{}, err
		}
		if existing != nil {
			// Whether the provider verified the address does not change the
			// answer, only the reason. Verified: this is probably the same
			// person, and proving it costs them one password. Unverified: this
			// may well not be, and the address is worth nothing as evidence.
			return Resolution{Outcome: NeedsLogin, Identity: identity}, nil
		}
	}

	return Resolution{Outcome: NoAccount, Identity: identity}, nil
}

// Link attaches an identity to an account, for the NoAccount path once the
// caller has created one.
func Link(ctx context.Context, store IdentityStore, accountID string, identity *Identity) error {
	if accountID == "" {
		return errors.New("auth: linking an identity to no account")
	}
	return store.LinkIdentity(ctx, accountID, identity)
}

// Unlink removes a provider from an account, refusing to remove the last way
// in.
//
// otherCredentials is how many other ways the account can be signed into that
// this package cannot see -- passkeys, typically. Counting them is the caller's
// job for the same reason RevokePasskey leaves the equivalent check to its
// caller: guessing means either blocking a legitimate unlink or locking
// somebody out of their own account. Pass 0 when there are none.
func Unlink(ctx context.Context, store IdentityStore, account Account, provider string, otherCredentials int) error {
	if account == nil {
		return errors.New("auth: unlinking from no account")
	}

	identities, err := store.IdentitiesFor(ctx, account.AuthID())
	if err != nil {
		return err
	}

	var remaining int
	for _, existing := range identities {
		if existing.Provider != provider {
			remaining++
		}
	}

	if remaining == 0 && otherCredentials == 0 && len(account.PasswordHash()) == 0 {
		return ErrLastIdentity
	}

	return store.UnlinkIdentity(ctx, account.AuthID(), provider)
}

// SQLIdentityStore is an IdentityStore over database/sql.
type SQLIdentityStore struct {
	db      *sql.DB
	dialect Dialect
	table   string
}

// NewSQLIdentityStore returns a store using the default table name.
func NewSQLIdentityStore(db *sql.DB, dialect Dialect) *SQLIdentityStore {
	return &SQLIdentityStore{db: db, dialect: dialect, table: "tjo_identities"}
}

// WithTable overrides the table name.
func (s *SQLIdentityStore) WithTable(name string) *SQLIdentityStore {
	s.table = name
	return s
}

func (s *SQLIdentityStore) rebind(q string) string {
	if s.dialect != DialectPostgres {
		return q
	}
	return rebindPostgres(q)
}

// Migrate creates the table if it does not exist.
func (s *SQLIdentityStore) Migrate(ctx context.Context) error {
	var ddl string
	switch s.dialect {
	case DialectPostgres:
		ddl = `CREATE TABLE IF NOT EXISTS ` + s.table + ` (
			provider       TEXT NOT NULL,
			subject        TEXT NOT NULL,
			account_id     TEXT NOT NULL,
			email          TEXT NOT NULL DEFAULT '',
			email_verified BOOLEAN NOT NULL DEFAULT FALSE,
			name           TEXT NOT NULL DEFAULT '',
			avatar_url     TEXT NOT NULL DEFAULT '',
			created_at     TIMESTAMPTZ NOT NULL,
			PRIMARY KEY (provider, subject)
		)`
	case DialectMySQL:
		ddl = `CREATE TABLE IF NOT EXISTS ` + s.table + ` (
			provider       VARCHAR(64) NOT NULL,
			subject        VARCHAR(191) NOT NULL,
			account_id     VARCHAR(191) NOT NULL,
			email          VARCHAR(191) NOT NULL DEFAULT '',
			email_verified BOOLEAN NOT NULL DEFAULT FALSE,
			name           VARCHAR(191) NOT NULL DEFAULT '',
			avatar_url     TEXT,
			created_at     DATETIME NOT NULL,
			PRIMARY KEY (provider, subject)
		)`
	default:
		ddl = `CREATE TABLE IF NOT EXISTS ` + s.table + ` (
			provider       TEXT NOT NULL,
			subject        TEXT NOT NULL,
			account_id     TEXT NOT NULL,
			email          TEXT NOT NULL DEFAULT '',
			email_verified BOOLEAN NOT NULL DEFAULT 0,
			name           TEXT NOT NULL DEFAULT '',
			avatar_url     TEXT NOT NULL DEFAULT '',
			created_at     DATETIME NOT NULL,
			PRIMARY KEY (provider, subject)
		)`
	}

	if _, err := s.db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("auth: migrate identities: %w", err)
	}

	// One identity per provider per account, so that "unlink Google" is
	// unambiguous and so that two Google accounts cannot quietly share one
	// application account.
	if _, err := s.db.ExecContext(ctx,
		`CREATE UNIQUE INDEX IF NOT EXISTS `+s.table+`_account_provider ON `+s.table+` (account_id, provider)`); err != nil {
		return fmt.Errorf("auth: index identities: %w", err)
	}

	return nil
}

// IdentityBySubject implements IdentityStore.
func (s *SQLIdentityStore) IdentityBySubject(ctx context.Context, provider, subject string) (string, error) {
	var accountID string

	err := s.db.QueryRowContext(ctx, s.rebind(
		`SELECT account_id FROM `+s.table+` WHERE provider = ? AND subject = ?`),
		provider, subject).Scan(&accountID)

	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("auth: identity by subject: %w", err)
	}
	return accountID, nil
}

// LinkIdentity implements IdentityStore.
//
// It replaces any previous identity from the same provider on the same account,
// which is what happens when somebody unlinks Google and links a different
// Google account: the old row is gone and the new one takes the slot.
func (s *SQLIdentityStore) LinkIdentity(ctx context.Context, accountID string, identity *Identity) error {
	if identity == nil || identity.Provider == "" || identity.Subject == "" {
		return errors.New("auth: linking an identity with no provider or subject")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("auth: link identity: %w", err)
	}
	defer tx.Rollback()

	// Both deletes and the insert in one transaction: between them the account
	// has no identity from this provider, and a reader arriving there would see
	// an account that cannot sign in.
	if _, err := tx.ExecContext(ctx, s.rebind(
		`DELETE FROM `+s.table+` WHERE account_id = ? AND provider = ?`),
		accountID, identity.Provider); err != nil {
		return fmt.Errorf("auth: link identity: %w", err)
	}

	if _, err := tx.ExecContext(ctx, s.rebind(
		`DELETE FROM `+s.table+` WHERE provider = ? AND subject = ?`),
		identity.Provider, identity.Subject); err != nil {
		return fmt.Errorf("auth: link identity: %w", err)
	}

	if _, err := tx.ExecContext(ctx, s.rebind(
		`INSERT INTO `+s.table+`
		 (provider, subject, account_id, email, email_verified, name, avatar_url, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`),
		identity.Provider, identity.Subject, accountID,
		strings.ToLower(identity.Email), identity.EmailVerified,
		identity.Name, identity.AvatarURL, time.Now().UTC()); err != nil {
		return fmt.Errorf("auth: link identity: %w", err)
	}

	return tx.Commit()
}

// UnlinkIdentity implements IdentityStore.
func (s *SQLIdentityStore) UnlinkIdentity(ctx context.Context, accountID, provider string) error {
	_, err := s.db.ExecContext(ctx, s.rebind(
		`DELETE FROM `+s.table+` WHERE account_id = ? AND provider = ?`),
		accountID, provider)
	if err != nil {
		return fmt.Errorf("auth: unlink identity: %w", err)
	}
	return nil
}

// IdentitiesFor implements IdentityStore.
func (s *SQLIdentityStore) IdentitiesFor(ctx context.Context, accountID string) ([]*Identity, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(
		`SELECT provider, subject, email, email_verified, name, avatar_url
		 FROM `+s.table+` WHERE account_id = ? ORDER BY provider`), accountID)
	if err != nil {
		return nil, fmt.Errorf("auth: identities for account: %w", err)
	}
	defer rows.Close()

	var identities []*Identity
	for rows.Next() {
		var identity Identity
		if err := rows.Scan(&identity.Provider, &identity.Subject, &identity.Email,
			&identity.EmailVerified, &identity.Name, &identity.AvatarURL); err != nil {
			return nil, fmt.Errorf("auth: identities for account: %w", err)
		}
		identities = append(identities, &identity)
	}

	return identities, rows.Err()
}
