package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

type account struct {
	id        string
	hash      []byte
	activated bool
}

func (a *account) AuthID() string       { return a.id }
func (a *account) PasswordHash() []byte { return a.hash }
func (a *account) Activated() bool      { return a.activated }

type accountStore struct {
	byEmail map[string]*account
	lookups int
}

func (s *accountStore) ByEmail(_ context.Context, email string) (Account, error) {
	s.lookups++
	a, ok := s.byEmail[email]
	if !ok {
		// Deliberately (nil, nil): returning an error here would let
		// Authenticate short-circuit and reopen the enumeration oracle.
		return nil, nil
	}
	return a, nil
}

func newStore(t *testing.T) *accountStore {
	t.Helper()

	hash, err := HashPassword("a long enough passphrase")
	if err != nil {
		t.Fatal(err)
	}
	return &accountStore{byEmail: map[string]*account{
		"real@example.com":     {id: "u1", hash: hash, activated: true},
		"inactive@example.com": {id: "u2", hash: hash, activated: false},
	}}
}

func TestAuthenticate(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	t.Run("correct credentials", func(t *testing.T) {
		a, err := Authenticate(ctx, store, "real@example.com", "a long enough passphrase")
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if a.AuthID() != "u1" {
			t.Errorf("AuthID = %q", a.AuthID())
		}
	})

	t.Run("wrong password", func(t *testing.T) {
		if _, err := Authenticate(ctx, store, "real@example.com", "wrong"); !errors.Is(err, ErrBadCredentials) {
			t.Errorf("err = %v, want ErrBadCredentials", err)
		}
	})

	// The same error for a missing account as for a wrong password, or the
	// login form answers "does this address have an account".
	t.Run("unknown account is indistinguishable from a wrong password", func(t *testing.T) {
		_, missing := Authenticate(ctx, store, "nobody@example.com", "whatever")
		_, wrong := Authenticate(ctx, store, "real@example.com", "whatever")

		if !errors.Is(missing, ErrBadCredentials) {
			t.Errorf("missing account returned %v", missing)
		}
		if missing.Error() != wrong.Error() {
			t.Errorf("errors differ: %q vs %q", missing, wrong)
		}
	})

	// Only returned after the password is verified, so it discloses nothing to
	// someone who does not already have it.
	t.Run("inactive account with the right password", func(t *testing.T) {
		if _, err := Authenticate(ctx, store, "inactive@example.com", "a long enough passphrase"); !errors.Is(err, ErrNotActivated) {
			t.Errorf("err = %v, want ErrNotActivated", err)
		}
	})

	t.Run("inactive account with the wrong password says nothing about activation", func(t *testing.T) {
		if _, err := Authenticate(ctx, store, "inactive@example.com", "wrong"); !errors.Is(err, ErrBadCredentials) {
			t.Errorf("err = %v, want ErrBadCredentials -- activation state leaked before the password was checked", err)
		}
	})
}

// The lookup must happen for every attempt, including ones that cannot
// succeed. A store that is never consulted for unknown addresses is a store
// whose timing gives the answer away.
func TestAuthenticateAlwaysConsultsTheStore(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	before := store.lookups
	Authenticate(ctx, store, "nobody@example.com", "whatever")
	if store.lookups != before+1 {
		t.Error("no lookup was performed for an unknown address")
	}
}

// A missing account must not be measurably cheaper than a present one.
func TestUnknownAccountStillPaysForBcrypt(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	measure := func(email string) time.Duration {
		start := time.Now()
		Authenticate(ctx, store, email, "whatever")
		return time.Since(start)
	}

	present := measure("real@example.com")
	missing := measure("nobody@example.com")

	if missing < present/4 {
		t.Errorf("unknown address took %v against %v for a known one; the timing oracle is open", missing, present)
	}
}

func TestAuthenticateAndUpgradeRehashesWeakHashes(t *testing.T) {
	ctx := context.Background()

	// A hash made at a cost below today's default, as an account created years
	// ago would have.
	weak, err := hashAtCost("a long enough passphrase", 4)
	if err != nil {
		t.Fatal(err)
	}

	store := &accountStore{byEmail: map[string]*account{
		"old@example.com": {id: "u3", hash: weak, activated: true},
	}}

	var upgraded []byte
	a, err := AuthenticateAndUpgrade(ctx, store, "old@example.com", "a long enough passphrase",
		func(_ context.Context, _ Account, newHash []byte) { upgraded = newHash })
	if err != nil {
		t.Fatal(err)
	}
	if a.AuthID() != "u3" {
		t.Errorf("AuthID = %q", a.AuthID())
	}

	if upgraded == nil {
		t.Fatal("a hash below the default cost was not upgraded; login is the only moment the plaintext exists")
	}
	if NeedsRehash(upgraded) {
		t.Error("the upgraded hash still needs upgrading")
	}
	if err := VerifyPassword(upgraded, "a long enough passphrase"); err != nil {
		t.Errorf("the upgraded hash does not verify: %v", err)
	}
}

func TestAuthenticateAndUpgradeLeavesCurrentHashesAlone(t *testing.T) {
	store := newStore(t)

	var called bool
	if _, err := AuthenticateAndUpgrade(context.Background(), store, "real@example.com", "a long enough passphrase",
		func(_ context.Context, _ Account, _ []byte) { called = true }); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Error("a current hash was rehashed for no reason")
	}
}
