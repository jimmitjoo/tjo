package auth

import (
	"context"
	"errors"
)

// ErrNotActivated is returned when credentials are correct but the account has
// not been activated.
//
// Distinct from ErrBadCredentials on purpose, and it is the one distinction
// worth making: it is only ever returned *after* the password has been
// verified, so it reveals nothing to someone who does not already know the
// password.
var ErrNotActivated = errors.New("auth: account is not activated")

// Account is the minimum a stored user must expose for authentication.
//
// Deliberately tiny. Everything else -- names, avatars, preferences, whatever
// the application actually cares about -- stays in the application's own type,
// which embeds or wraps this. A library that insisted on owning the user record
// is the thing Val Town left Clerk over: an app that needs to join on users
// ends up with two users tables.
type Account interface {
	// AuthID is a stable identifier for this account, used as the subject of
	// sessions and reset tokens. It must not change when the email does.
	AuthID() string

	// PasswordHash returns the stored hash, or nil if the account has no
	// password -- which is normal for an account that only uses a passkey or
	// a social provider.
	PasswordHash() []byte

	// Activated reports whether the account may sign in.
	Activated() bool
}

// AccountStore looks up accounts for authentication.
type AccountStore interface {
	// ByEmail returns the account for an address.
	//
	// It must return (nil, nil) when there is no such account rather than an
	// error, so Authenticate can do the timing-equalising dummy comparison.
	// Returning an error here reintroduces the enumeration oracle, because a
	// missing user then costs a round trip and a present one costs bcrypt.
	ByEmail(ctx context.Context, email string) (Account, error)
}

// Authenticate verifies an email and password.
//
// The lookup and the verification happen unconditionally, in that order, so a
// login attempt for an address that does not exist costs the same as one that
// does. Short-circuiting on "no such user" is the single most common way to
// turn a login form into a user-enumeration oracle, and it is why
// VerifyPassword accepts a nil hash rather than the caller checking first.
//
// It returns ErrBadCredentials for both a missing account and a wrong password,
// and never says which.
func Authenticate(ctx context.Context, store AccountStore, email, password string) (Account, error) {
	account, err := store.ByEmail(ctx, email)
	if err != nil {
		return nil, err
	}

	var hash []byte
	if account != nil {
		hash = account.PasswordHash()
	}

	if err := VerifyPassword(hash, password); err != nil {
		return nil, ErrBadCredentials
	}

	// Reached only with a correct password, so it tells an attacker nothing
	// they did not already have.
	if !account.Activated() {
		return nil, ErrNotActivated
	}

	return account, nil
}

// AuthenticateAndUpgrade is Authenticate plus an opportunistic rehash.
//
// Login is the only moment the plaintext password exists, so it is the only
// moment a hash made at an outdated cost can be upgraded. Without this, a
// password hashed in 2019 stays at 2019's cost forever.
//
// upgrade is called with the new hash when one was produced. It is the caller's
// job to store it, and a failure to do so must not fail the login -- the user
// authenticated correctly, and a storage problem is not their problem.
func AuthenticateAndUpgrade(ctx context.Context, store AccountStore, email, password string, upgrade func(ctx context.Context, account Account, newHash []byte)) (Account, error) {
	account, err := Authenticate(ctx, store, email, password)
	if err != nil {
		return nil, err
	}

	if upgrade != nil && NeedsRehash(account.PasswordHash()) {
		if newHash, err := HashPassword(password); err == nil {
			upgrade(ctx, account, newHash)
		}
	}

	return account, nil
}
