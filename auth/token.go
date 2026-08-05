// Package auth is authentication as a library: verbs and interfaces, no server,
// no tables of its own.
//
// It exists because this project published four advisories and two of them were
// in generated authentication code -- a password reset that verified nothing,
// and API tokens stored in plaintext. Generated code cannot be unit-tested,
// which is precisely why those defects reached users. Everything here can be,
// and the tests are the argument for the design rather than a formality.
//
// # Storage stays yours
//
// Every flow takes a store interface and returns what to persist; nothing here
// owns a user record, a session or a schema. That is deliberate: a library that
// insists on owning the users table is the thing applications end up with two
// of, and it is why swapping the backend later means a migration.
//
// # What is here
//
//   - Passwords: bcrypt with cost upgrade on login, a rune-counted policy, and
//     a timing-equalised failure path.
//   - Login: lookup and comparison unconditional and in that order.
//   - Single-use tokens: password reset, activation, recovery codes and
//     remember-me, separated by purpose and consumed atomically.
//   - Two-factor: TOTP per RFC 6238, with replay rejection, and recovery codes.
//   - Passkeys: both ceremonies, stored in an opaque interoperable record.
//   - Organizations: memberships, roles, permissions, invitations, scoping.
//   - API tokens: minted once, stored as a hash.
//
// # What is not, and will not be
//
// Sessions and the HTTP layer. A session belongs to the framework the
// application already chose, and the one decision this package insists on
// instead of implementing is that the caller renews the session at every point
// where authentication completes -- password login, 2FA, passkey and
// remember-me. Skipping it is session fixation, and it is the mistake generated
// code kept making.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"errors"
	"strings"
	"time"
)

// ErrInvalidToken is returned when a token is malformed, unknown or expired.
//
// One error for all three, deliberately: distinguishing "no such token" from
// "expired token" tells an attacker which of their guesses existed.
var ErrInvalidToken = errors.New("auth: invalid token")

// tokenBytes is the entropy behind each token. 16 bytes is 128 bits, which is
// the floor for something guessable at internet scale.
const tokenBytes = 16

// Token is an API token: a plaintext value shown to the user exactly once, and
// a hash that is all the server retains.
type Token struct {
	// PlainText is populated only by NewToken. It is never read back from
	// storage, because storage never has it.
	//
	// v0.7.0 fixed exactly this: tokens were persisted in plaintext and
	// serialised into JSON responses, so a database read or a logged response
	// body handed over working credentials.
	PlainText string `json:"-"`

	// Hash is what gets stored and compared.
	Hash []byte `json:"-"`

	Expiry time.Time `json:"expiry"`
}

// NewToken mints a token valid for ttl.
//
// The plaintext exists only in the returned value. Show it to the user once and
// store the hash; there is no way to recover it afterwards, which is the point.
func NewToken(ttl time.Duration) (*Token, error) {
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}

	// base32 without padding: case-insensitive, no characters that need
	// escaping in a URL or a header, and no padding for a user to lose when
	// copying it out of a terminal.
	plain := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf)
	sum := sha256.Sum256([]byte(plain))

	return &Token{
		PlainText: plain,
		Hash:      sum[:],
		Expiry:    time.Now().Add(ttl),
	}, nil
}

// HashToken returns the stored form of a plaintext token.
//
// SHA-256 rather than bcrypt, and that is not an oversight. A password is
// low-entropy and chosen by a human, so it needs a slow hash to survive being
// guessed. A token is 128 bits from crypto/rand, so there is nothing to guess,
// and a slow hash would only mean every authenticated API request pays for a
// key-derivation function.
func HashToken(plain string) []byte {
	sum := sha256.Sum256([]byte(plain))
	return sum[:]
}

// Verify reports whether plain matches this token and has not expired.
//
// Constant-time comparison: one that returns on the first differing byte leaks
// the stored hash to anyone willing to measure, one byte at a time.
func (t *Token) Verify(plain string, now time.Time) error {
	if t == nil || len(t.Hash) == 0 {
		return ErrInvalidToken
	}

	sum := sha256.Sum256([]byte(plain))
	if subtle.ConstantTimeCompare(t.Hash, sum[:]) != 1 {
		return ErrInvalidToken
	}

	// Checked after the comparison, so an expired token and an unknown one take
	// the same path and the same time.
	if !now.Before(t.Expiry) {
		return ErrInvalidToken
	}

	return nil
}

// FromAuthorizationHeader extracts a bearer token.
//
// It rejects anything that is not exactly "Bearer <token>" of the right length,
// rather than accepting a prefix match. A header of "Bearer" with no value, or
// with extra fields after the token, is a malformed request rather than
// something to interpret generously.
func FromAuthorizationHeader(header string) (string, error) {
	if header == "" {
		return "", ErrInvalidToken
	}

	scheme, value, found := strings.Cut(header, " ")
	if !found {
		return "", ErrInvalidToken
	}

	// The scheme is case-insensitive per RFC 7235; the token is not.
	if !strings.EqualFold(scheme, "Bearer") {
		return "", ErrInvalidToken
	}

	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, " \t") {
		return "", ErrInvalidToken
	}

	// Length is fixed by construction, so a value of any other length cannot be
	// one of ours and does not need hashing to find out.
	if len(value) != base32.StdEncoding.WithPadding(base32.NoPadding).EncodedLen(tokenBytes) {
		return "", ErrInvalidToken
	}

	return value, nil
}
