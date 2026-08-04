package auth

import (
	"errors"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestHashAndVerify(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}

	if err := VerifyPassword(hash, "correct horse battery staple"); err != nil {
		t.Errorf("the right password did not verify: %v", err)
	}
	if err := VerifyPassword(hash, "wrong"); !errors.Is(err, ErrBadCredentials) {
		t.Errorf("the wrong password returned %v, want ErrBadCredentials", err)
	}
}

// bcrypt silently truncates at 72 bytes. Accepting a longer password and only
// comparing the first 72 tells the user their passphrase is stronger than it
// is, so this refuses instead.
func TestPasswordsLongerThanBcryptCanHandleAreRejected(t *testing.T) {
	long := strings.Repeat("a", MaxPasswordBytes+1)

	if _, err := HashPassword(long); !errors.Is(err, ErrPasswordTooLong) {
		t.Errorf("HashPassword accepted %d bytes: %v", len(long), err)
	}
	if err := DefaultPasswordPolicy().Validate(long); !errors.Is(err, ErrPasswordTooLong) {
		t.Errorf("Validate accepted %d bytes: %v", len(long), err)
	}
}

// A missing user must cost the same as a present one, or "does this email have
// an account" is answerable with a stopwatch.
func TestVerifyWithNoHashStillPaysForBcrypt(t *testing.T) {
	hash, err := HashPassword("some password")
	if err != nil {
		t.Fatal(err)
	}

	measure := func(f func()) time.Duration {
		start := time.Now()
		f()
		return time.Since(start)
	}

	real := measure(func() { VerifyPassword(hash, "wrong password") })
	missing := measure(func() { VerifyPassword(nil, "wrong password") })

	if err := VerifyPassword(nil, "anything"); !errors.Is(err, ErrBadCredentials) {
		t.Fatalf("a nil hash returned %v, want ErrBadCredentials", err)
	}

	// Not asserting they are equal -- bcrypt timing varies and a strict
	// comparison would be flaky. Asserting the missing-user path is not
	// obviously cheaper, which is the property that matters: an early return
	// would be orders of magnitude faster, not 20% faster.
	if missing < real/4 {
		t.Errorf("a missing user took %v against %v for a real one; the timing oracle is open", missing, real)
	}
}

func TestNeedsRehash(t *testing.T) {
	weak, err := bcrypt.GenerateFromPassword([]byte("x"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if !NeedsRehash(weak) {
		t.Error("a hash below the default cost was not flagged for upgrade")
	}

	current, err := HashPassword("x")
	if err != nil {
		t.Fatal(err)
	}
	if NeedsRehash(current) {
		t.Error("a freshly made hash was flagged for upgrade")
	}

	// Garbage should be replaced, not trusted.
	if !NeedsRehash([]byte("not a bcrypt hash")) {
		t.Error("an unreadable hash was not flagged for upgrade")
	}
}

func TestPasswordPolicy(t *testing.T) {
	p := DefaultPasswordPolicy()

	if err := p.Validate("short"); err == nil {
		t.Error("a five-character password was accepted")
	}
	if err := p.Validate("a reasonable passphrase"); err != nil {
		t.Errorf("a long passphrase was rejected: %v", err)
	}

	// Length is counted in runes, so a passphrase in a non-Latin script is not
	// penalised for its encoding. Twelve Japanese characters is twelve
	// characters, not thirty-six bytes' worth of credit.
	t.Run("counts runes rather than bytes", func(t *testing.T) {
		twelve := "パスワードのテストです１２"
		if err := p.Validate(twelve); err != nil {
			t.Errorf("a 12-rune passphrase was rejected: %v", err)
		}

		eleven := "パスワードのテストで１"
		if err := p.Validate(eleven); err == nil {
			t.Error("an 11-rune passphrase was accepted")
		}
	})

	// No composition rules, deliberately. NIST SP 800-63B recommends against
	// them -- they produce Password1! -- so this must not start requiring a
	// digit or a symbol.
	t.Run("no composition rules", func(t *testing.T) {
		if err := p.Validate("all lowercase letters here"); err != nil {
			t.Errorf("a long all-lowercase passphrase was rejected: %v", err)
		}
	})
}

func TestConstantTimeEquals(t *testing.T) {
	if !ConstantTimeEquals([]byte("same"), []byte("same")) {
		t.Error("equal values compared unequal")
	}
	if ConstantTimeEquals([]byte("same"), []byte("different")) {
		t.Error("unequal values compared equal")
	}
	if ConstantTimeEquals([]byte("a"), nil) {
		t.Error("a value compared equal to nil")
	}
}
