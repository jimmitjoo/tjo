package auth

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

// ErrBadCredentials is returned when an email or password does not match.
//
// One error for both, deliberately. Distinguishing "no such user" from "wrong
// password" turns the login form into a user-enumeration oracle, and every
// application that has ever leaked its user list to a signup form has done it
// by being helpful here.
var ErrBadCredentials = errors.New("auth: invalid credentials")

// MaxPasswordBytes is bcrypt's input limit.
//
// bcrypt hashes at most 72 bytes. Historically it truncated silently, which
// meant a library accepting a 200-byte passphrase and comparing the first 72
// was telling the user their password is stronger than it is. x/crypto now
// returns an error instead, but the limit still has to be checked before
// hashing so validation can report it rather than discovering it at write time.
//
// golang.org/x/crypto/bcrypt does not export this as a constant.
const MaxPasswordBytes = 72

// ErrPasswordTooLong is returned for input bcrypt cannot hash.
var ErrPasswordTooLong = fmt.Errorf("auth: password exceeds %d bytes", MaxPasswordBytes)

// dummyHash is compared against when no user was found, so a login attempt for
// an unknown address costs the same as one for a known address.
//
// Without it, "does this email have an account" is answerable with a stopwatch:
// a missing user returns immediately, a real one pays for bcrypt. Generated
// once at init from a value nothing can log in with.
var dummyHash []byte

func init() {
	h, err := bcrypt.GenerateFromPassword([]byte("dummy-password-for-constant-time-login"), bcrypt.DefaultCost)
	if err != nil {
		panic("auth: cannot generate the timing-defence hash: " + err.Error())
	}
	dummyHash = h
}

// HashPassword returns a bcrypt hash suitable for storage.
func HashPassword(plain string) ([]byte, error) {
	if len(plain) > MaxPasswordBytes {
		return nil, ErrPasswordTooLong
	}
	return bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
}

// VerifyPassword reports whether plain matches hash.
//
// Pass a nil or empty hash when the user was not found. It still performs a
// full bcrypt comparison against a dummy value, so the timing of a failed login
// does not reveal whether the account exists. That only holds if the caller
// keeps doing the lookup-then-verify sequence unconditionally -- returning
// early on "user not found" puts the oracle back.
func VerifyPassword(hash []byte, plain string) error {
	if len(hash) == 0 {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(plain))
		return ErrBadCredentials
	}

	if err := bcrypt.CompareHashAndPassword(hash, []byte(plain)); err != nil {
		return ErrBadCredentials
	}

	return nil
}

// NeedsRehash reports whether a stored hash was made with a weaker cost than
// the current default, so it can be upgraded on the next successful login.
//
// Without this, a password hashed at cost 10 in 2019 is still at cost 10 today.
// The upgrade is free: at login the plaintext is in hand for the only moment it
// ever will be.
func NeedsRehash(hash []byte) bool {
	cost, err := bcrypt.Cost(hash)
	if err != nil {
		// Unreadable hash: treat as needing replacement rather than as fine.
		return true
	}
	return cost < bcrypt.DefaultCost
}

// PasswordPolicy is the minimum a password must satisfy.
//
// Deliberately just a length floor. Composition rules -- one uppercase, one
// digit, one symbol -- push people towards Password1! and are recommended
// against by NIST SP 800-63B, which asks for length and a breached-password
// check instead. The breach check is the caller's to make; this library will
// not ship a wordlist.
type PasswordPolicy struct {
	// MinRunes is measured in runes, not bytes, so a passphrase in a
	// non-Latin script is not penalised for its encoding.
	MinRunes int
}

// DefaultPasswordPolicy is 12 runes, following NIST SP 800-63B's guidance to
// favour length over composition.
func DefaultPasswordPolicy() PasswordPolicy {
	return PasswordPolicy{MinRunes: 12}
}

// Validate reports whether plain satisfies the policy.
func (p PasswordPolicy) Validate(plain string) error {
	if len(plain) > MaxPasswordBytes {
		return ErrPasswordTooLong
	}

	min := p.MinRunes
	if min == 0 {
		min = DefaultPasswordPolicy().MinRunes
	}

	if utf8.RuneCountInString(plain) < min {
		return fmt.Errorf("auth: password must be at least %d characters", min)
	}

	return nil
}

// ConstantTimeEquals compares two secrets without leaking their contents
// through timing.
//
// Exported because callers keep needing it for their own tokens and keep
// reaching for == instead.
func ConstantTimeEquals(a, b []byte) bool {
	return subtle.ConstantTimeCompare(a, b) == 1
}

// hashAtCost is used by tests to produce hashes at a chosen cost. It is not
// exported: callers should never choose a cost, and a knob that only exists to
// make hashes weaker is a knob worth not offering.
func hashAtCost(plain string, cost int) ([]byte, error) {
	if len(plain) > MaxPasswordBytes {
		return nil, ErrPasswordTooLong
	}
	return bcrypt.GenerateFromPassword([]byte(plain), cost)
}
