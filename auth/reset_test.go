package auth

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// memStore is a ResetStore whose Consume is genuinely atomic, so the tests
// exercise the contract the interface documents rather than a weaker one.
type memStore struct {
	mu     sync.Mutex
	tokens map[string]*ResetToken // keyed by hex of hash
	used   map[string]bool
}

func newMemStore() *memStore {
	return &memStore{tokens: map[string]*ResetToken{}, used: map[string]bool{}}
}

func key(hash []byte) string {
	var b strings.Builder
	for _, c := range hash {
		b.WriteByte("0123456789abcdef"[c>>4])
		b.WriteByte("0123456789abcdef"[c&0xf])
	}
	return b.String()
}

func (m *memStore) Save(_ context.Context, t *ResetToken) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Storing the plaintext would be the bug this design exists to prevent, so
	// the fake refuses to keep it too.
	m.tokens[key(t.Hash)] = &ResetToken{
		Hash: t.Hash, UserID: t.UserID, Purpose: t.Purpose, Expiry: t.Expiry,
	}
	return nil
}

func (m *memStore) Consume(_ context.Context, hash []byte, purpose ResetPurpose) (*ResetToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	k := key(hash)
	t, ok := m.tokens[k]
	if !ok || m.used[k] || t.Purpose != purpose || !time.Now().Before(t.Expiry) {
		return nil, ErrInvalidReset
	}
	m.used[k] = true
	return t, nil
}

func (m *memStore) InvalidateUser(_ context.Context, userID string, purpose ResetPurpose) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, t := range m.tokens {
		if t.UserID == userID && t.Purpose == purpose {
			m.used[k] = true
		}
	}
	return nil
}

func TestResetTokenCarriesNoIdentity(t *testing.T) {
	tok, err := NewResetToken("user-42", PurposePasswordReset, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	// GHSA-44g2-5v2v-xh66 was a reset flow where the artifact presented to the
	// server *was* the identity, encrypted with malleable AES-CFB, so flipping
	// bits in it changed whose password was reset. The token must be an
	// unguessable lookup key and nothing else.
	if strings.Contains(tok.PlainText, "user-42") {
		t.Error("the user id is recoverable from the token")
	}
	if strings.Contains(tok.PlainText, "42") {
		t.Error("part of the user id appears in the token")
	}
}

func TestRedeemIsSingleUse(t *testing.T) {
	store := newMemStore()
	ctx := context.Background()

	tok, err := NewResetToken("user-1", PurposePasswordReset, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, tok); err != nil {
		t.Fatal(err)
	}

	got, err := Redeem(ctx, store, tok.PlainText, PurposePasswordReset)
	if err != nil {
		t.Fatalf("first redemption failed: %v", err)
	}
	if got != "user-1" {
		t.Errorf("redeemed for %q, want user-1", got)
	}

	if _, err := Redeem(ctx, store, tok.PlainText, PurposePasswordReset); !errors.Is(err, ErrInvalidReset) {
		t.Errorf("a consumed token was redeemed a second time: %v", err)
	}
}

// Two requests arriving together must not both succeed. This is why Consume is
// one operation in the interface rather than a read followed by a write.
func TestConcurrentRedemptionYieldsExactlyOneWinner(t *testing.T) {
	store := newMemStore()
	ctx := context.Background()

	tok, _ := NewResetToken("user-1", PurposePasswordReset, time.Hour)
	store.Save(ctx, tok)

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		wins    int
		results = map[string]int{}
	)

	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := Redeem(ctx, store, tok.PlainText, PurposePasswordReset)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				wins++
				results[id]++
			}
		}()
	}
	wg.Wait()

	if wins != 1 {
		t.Errorf("%d concurrent redemptions succeeded, want exactly 1", wins)
	}
}

// A token minted for activation must not be spendable on a password reset.
// Sharing a table between flows should not mean sharing an attack surface.
func TestPurposeIsEnforced(t *testing.T) {
	store := newMemStore()
	ctx := context.Background()

	tok, _ := NewResetToken("user-1", PurposeActivation, time.Hour)
	store.Save(ctx, tok)

	if _, err := Redeem(ctx, store, tok.PlainText, PurposePasswordReset); !errors.Is(err, ErrInvalidReset) {
		t.Errorf("an activation token was redeemed as a password reset: %v", err)
	}

	if _, err := Redeem(ctx, store, tok.PlainText, PurposeActivation); err != nil {
		t.Errorf("the token did not work for its own purpose: %v", err)
	}
}

func TestExpiredTokensAreRejected(t *testing.T) {
	store := newMemStore()
	ctx := context.Background()

	tok, _ := NewResetToken("user-1", PurposePasswordReset, -time.Second)
	store.Save(ctx, tok)

	if _, err := Redeem(ctx, store, tok.PlainText, PurposePasswordReset); !errors.Is(err, ErrInvalidReset) {
		t.Errorf("an expired token was redeemed: %v", err)
	}
}

// Even a store that forgot to filter on expiry must not produce a working
// token. Correctness should not depend on every implementation getting the SQL
// right.
func TestRedeemChecksExpiryEvenIfTheStoreDoesNot(t *testing.T) {
	store := &sloppyStore{inner: newMemStore()}
	ctx := context.Background()

	tok, _ := NewResetToken("user-1", PurposePasswordReset, -time.Hour)
	store.Save(ctx, tok)

	if _, err := Redeem(ctx, store, tok.PlainText, PurposePasswordReset); !errors.Is(err, ErrInvalidReset) {
		t.Errorf("an expired token passed because the store did not check: %v", err)
	}
}

// sloppyStore models the mistake a real implementation is most likely to make.
type sloppyStore struct{ inner *memStore }

func (s *sloppyStore) Save(ctx context.Context, t *ResetToken) error { return s.inner.Save(ctx, t) }

func (s *sloppyStore) Consume(_ context.Context, hash []byte, purpose ResetPurpose) (*ResetToken, error) {
	s.inner.mu.Lock()
	defer s.inner.mu.Unlock()
	t, ok := s.inner.tokens[key(hash)]
	if !ok {
		return nil, ErrInvalidReset
	}
	return t, nil // no expiry predicate, no used check
}

func (s *sloppyStore) InvalidateUser(ctx context.Context, u string, p ResetPurpose) error {
	return s.inner.InvalidateUser(ctx, u, p)
}

func TestResetPasswordInvalidatesOtherOutstandingTokens(t *testing.T) {
	store := newMemStore()
	ctx := context.Background()

	// A user who clicked "forgot password" three times has three live tokens.
	var tokens []*ResetToken
	for i := 0; i < 3; i++ {
		tok, _ := NewResetToken("user-1", PurposePasswordReset, time.Hour)
		store.Save(ctx, tok)
		tokens = append(tokens, tok)
	}

	userID, hash, err := ResetPassword(ctx, store, DefaultPasswordPolicy(), tokens[0].PlainText, "a long enough passphrase")
	if err != nil {
		t.Fatal(err)
	}
	if userID != "user-1" {
		t.Errorf("userID = %q", userID)
	}
	if err := VerifyPassword(hash, "a long enough passphrase"); err != nil {
		t.Errorf("the returned hash does not verify: %v", err)
	}

	// The other two must be dead. An old link sitting in an inbox should not
	// still work after the password has been changed.
	for i, tok := range tokens[1:] {
		if _, err := Redeem(ctx, store, tok.PlainText, PurposePasswordReset); !errors.Is(err, ErrInvalidReset) {
			t.Errorf("outstanding token %d still worked after a reset: %v", i+1, err)
		}
	}
}

func TestResetPasswordEnforcesThePolicy(t *testing.T) {
	store := newMemStore()
	ctx := context.Background()

	tok, _ := NewResetToken("user-1", PurposePasswordReset, time.Hour)
	store.Save(ctx, tok)

	if _, _, err := ResetPassword(ctx, store, DefaultPasswordPolicy(), tok.PlainText, "short"); err == nil {
		t.Error("a password below the policy minimum was accepted")
	}
}

func TestRedeemRejectsEmptyInput(t *testing.T) {
	if _, err := Redeem(context.Background(), newMemStore(), "", PurposePasswordReset); !errors.Is(err, ErrInvalidReset) {
		t.Errorf("an empty token returned %v", err)
	}
}
