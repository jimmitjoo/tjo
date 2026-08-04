package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"time"
)

// ErrInvalidReset is returned for a reset token that is unknown, already used,
// or expired.
//
// One error for all three. Telling a caller which it was hands an attacker a
// way to probe whether a token existed, and there is nothing a legitimate user
// can do differently with the distinction anyway.
var ErrInvalidReset = errors.New("auth: invalid or expired reset token")

// ResetPurpose distinguishes what a single-use token is for, so one minted for
// account activation cannot be spent on a password reset.
//
// Without it, two flows sharing a table share an attack surface: an activation
// link mailed to an unverified address would be redeemable against the password
// endpoint.
type ResetPurpose string

const (
	PurposePasswordReset ResetPurpose = "password_reset"
	PurposeActivation    ResetPurpose = "activation"
	PurposeEmailChange   ResetPurpose = "email_change"
)

// ResetToken is a single-use credential bound to a user.
//
// It carries no identity of its own. That is the correction to
// GHSA-44g2-5v2v-xh66, where the scaffolded flow decrypted an email address out
// of a form field using unauthenticated AES-CFB -- so an attacker could bit-flip
// their own token's ciphertext into another address and reset that account's
// password. The identity now lives in the row this token's hash points at, and
// the token is nothing but an unguessable lookup key.
type ResetToken struct {
	// PlainText is set only by NewResetToken. It goes in the emailed link and
	// is never stored.
	PlainText string

	// Hash is what the row keeps. A database read gives an attacker nothing
	// they can redeem.
	Hash []byte

	UserID  string
	Purpose ResetPurpose
	Expiry  time.Time
}

// resetTokenBytes is 32, so a token is 256 bits. Larger than an API token
// because a reset link is often the single strongest credential in the system:
// possession of one is possession of the account.
const resetTokenBytes = 32

// NewResetToken mints a token for userID valid for ttl.
func NewResetToken(userID string, purpose ResetPurpose, ttl time.Duration) (*ResetToken, error) {
	if userID == "" {
		return nil, errors.New("auth: reset token needs a user")
	}

	buf := make([]byte, resetTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}

	plain := base64.RawURLEncoding.EncodeToString(buf)
	sum := sha256.Sum256([]byte(plain))

	return &ResetToken{
		PlainText: plain,
		Hash:      sum[:],
		UserID:    userID,
		Purpose:   purpose,
		Expiry:    time.Now().Add(ttl),
	}, nil
}

// HashResetToken returns the stored form, for looking a token up.
func HashResetToken(plain string) []byte {
	sum := sha256.Sum256([]byte(plain))
	return sum[:]
}

// ResetStore is the storage the caller provides.
//
// Consume is deliberately one operation rather than a read followed by a
// delete. A reset token that can be read, validated and then redeemed twice
// concurrently is a race with account takeover at the end of it, and pushing
// the atomicity requirement into the interface is the only way a library can
// insist on it.
type ResetStore interface {
	// Save persists a token. Only Hash, UserID, Purpose and Expiry are stored;
	// PlainText must not be.
	Save(ctx context.Context, t *ResetToken) error

	// Consume atomically finds the token with this hash and purpose, marks it
	// used, and returns it. It must return ErrInvalidReset if no unused,
	// unexpired token matches -- and must not report which condition failed.
	//
	// Implement it as a single UPDATE ... WHERE hash = ? AND used_at IS NULL
	// RETURNING, or as a transaction. A SELECT followed by an UPDATE lets two
	// requests both see the token as unused.
	Consume(ctx context.Context, hash []byte, purpose ResetPurpose) (*ResetToken, error)

	// InvalidateUser marks every outstanding token for a user as used.
	InvalidateUser(ctx context.Context, userID string, purpose ResetPurpose) error
}

// Redeem consumes a token and returns the user it was minted for.
//
// The expiry check happens here rather than being left to the store, so every
// implementation gets it. Stores are still expected to filter on expiry in SQL
// for the sake of the index, but correctness does not depend on them doing so.
func Redeem(ctx context.Context, store ResetStore, plain string, purpose ResetPurpose) (string, error) {
	if plain == "" {
		return "", ErrInvalidReset
	}

	token, err := store.Consume(ctx, HashResetToken(plain), purpose)
	if err != nil {
		return "", ErrInvalidReset
	}
	if token == nil {
		return "", ErrInvalidReset
	}

	// Belt and braces: a store that forgot the expiry predicate must not turn
	// an old token into a working one.
	if !time.Now().Before(token.Expiry) {
		return "", ErrInvalidReset
	}

	// A store that returned a token for a different purpose than asked would
	// let an activation link reset a password.
	if token.Purpose != purpose {
		return "", ErrInvalidReset
	}

	if token.UserID == "" {
		return "", ErrInvalidReset
	}

	return token.UserID, nil
}

// ResetPassword redeems a token and returns the new password hash to store.
//
// It does not write anything. The caller updates the password and marks the
// token used in one transaction -- which is why Consume is part of the store
// interface rather than something this function calls on its own schedule.
//
// Every other outstanding reset token for the user is invalidated, because a
// password change means the previous ones were either used or should not be.
func ResetPassword(ctx context.Context, store ResetStore, policy PasswordPolicy, plainToken, newPassword string) (userID string, hash []byte, err error) {
	userID, err = Redeem(ctx, store, plainToken, PurposePasswordReset)
	if err != nil {
		return "", nil, err
	}

	if err := policy.Validate(newPassword); err != nil {
		return "", nil, err
	}

	hash, err = HashPassword(newPassword)
	if err != nil {
		return "", nil, err
	}

	if err := store.InvalidateUser(ctx, userID, PurposePasswordReset); err != nil {
		return "", nil, fmt.Errorf("auth: could not invalidate outstanding reset tokens: %w", err)
	}

	return userID, hash, nil
}

// EqualHash compares two token hashes in constant time.
func EqualHash(a, b []byte) bool {
	return subtle.ConstantTimeCompare(a, b) == 1
}
