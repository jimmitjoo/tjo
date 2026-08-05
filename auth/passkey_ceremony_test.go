package auth

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testRPID   = "example.com"
	testOrigin = "https://example.com"
)

func sqlitePasskeys(t *testing.T) *SQLPasskeyStore {
	t.Helper()

	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "passkeys.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })

	s := NewSQLPasskeyStore(db, DialectSQLite)
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s
}

func testPasskeys(t *testing.T, store PasskeyStore) *Passkeys {
	t.Helper()

	p, err := NewPasskeys(PasskeyConfig{
		RelyingPartyID:   testRPID,
		RelyingPartyName: "Example",
		Origins:          []string{testOrigin},
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// register runs a full registration ceremony and returns the stored record.
func register(t *testing.T, p *Passkeys, a *virtualAuthenticator, accountID, label string) *PasskeyRecord {
	t.Helper()
	ctx := context.Background()

	ceremony, err := p.BeginRegistration(ctx, accountID, "alex@example.com", "Alex")
	if err != nil {
		t.Fatal(err)
	}

	body := a.Register(t, challengeOf(t, ceremony.Options))
	r := httptest.NewRequest("POST", "/passkeys/register", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")

	record, err := p.FinishRegistration(ctx, accountID, label, ceremony.State, r)
	if err != nil {
		t.Fatalf("FinishRegistration: %v", err)
	}
	return record
}

// The whole feature, end to end: a credential created by an authenticator is
// stored, and the same authenticator signs in with it.
func TestRegisterThenSignIn(t *testing.T) {
	ctx := context.Background()
	store := sqlitePasskeys(t)
	p := testPasskeys(t, store)

	authenticator := newAuthenticator(t, testRPID, testOrigin)
	record := register(t, p, authenticator, "account-1", "MacBook")

	// Stored in the opaque format, not decomposed into columns.
	if !strings.HasPrefix(record.Record, "$webauthn$v=1$") {
		t.Fatalf("stored record = %q, want the opaque record format", record.Record)
	}

	ceremony, err := p.BeginLogin(ctx)
	if err != nil {
		t.Fatal(err)
	}

	body := authenticator.Assert(t, challengeOf(t, ceremony.Options), []byte("account-1"))
	r := httptest.NewRequest("POST", "/passkeys/login", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")

	login, err := p.FinishLogin(ctx, ceremony.State, r)
	if err != nil {
		t.Fatalf("FinishLogin: %v", err)
	}

	if login.AccountID != "account-1" {
		t.Fatalf("signed in as %q, want account-1", login.AccountID)
	}
	if !bytes.Equal(login.CredentialID, authenticator.credentialID) {
		t.Fatal("the login reports a different credential than the one that signed")
	}
}

// A challenge is single-use replay protection. Signing the challenge from one
// ceremony and presenting it to another must fail, or a captured response is a
// reusable password.
func TestASignatureForAnotherChallengeIsRejected(t *testing.T) {
	ctx := context.Background()
	store := sqlitePasskeys(t)
	p := testPasskeys(t, store)

	authenticator := newAuthenticator(t, testRPID, testOrigin)
	register(t, p, authenticator, "account-1", "MacBook")

	stale, err := p.BeginLogin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := p.BeginLogin(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Signed against the first ceremony, presented to the second.
	body := authenticator.Assert(t, challengeOf(t, stale.Options), []byte("account-1"))
	r := httptest.NewRequest("POST", "/passkeys/login", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")

	if _, err := p.FinishLogin(ctx, fresh.State, r); err == nil {
		t.Fatal("a response signed for a different challenge was accepted")
	}
}

// An authenticator from a different relying party must not be able to sign in
// here, even holding a credential id this application knows.
func TestASignatureFromAnotherRelyingPartyIsRejected(t *testing.T) {
	ctx := context.Background()
	store := sqlitePasskeys(t)
	p := testPasskeys(t, store)

	authenticator := newAuthenticator(t, testRPID, testOrigin)
	register(t, p, authenticator, "account-1", "MacBook")

	ceremony, err := p.BeginLogin(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Same key and credential id, but the authenticator now believes it is
	// talking to attacker.example, so the rpIdHash it signs is a different one.
	authenticator.rpID = "attacker.example"

	body := authenticator.Assert(t, challengeOf(t, ceremony.Options), []byte("account-1"))
	r := httptest.NewRequest("POST", "/passkeys/login", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")

	if _, err := p.FinishLogin(ctx, ceremony.State, r); err == nil {
		t.Fatal("a response for a different relying party was accepted")
	}
}

// Finishing a registration against a different account than the one the
// ceremony was begun for would let someone register their own key on somebody
// else's account.
func TestRegistrationIsBoundToTheAccountThatBeganIt(t *testing.T) {
	ctx := context.Background()
	store := sqlitePasskeys(t)
	p := testPasskeys(t, store)

	authenticator := newAuthenticator(t, testRPID, testOrigin)

	ceremony, err := p.BeginRegistration(ctx, "attacker", "attacker@example.com", "Attacker")
	if err != nil {
		t.Fatal(err)
	}

	body := authenticator.Register(t, challengeOf(t, ceremony.Options))
	r := httptest.NewRequest("POST", "/passkeys/register", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")

	if _, err := p.FinishRegistration(ctx, "victim", "stolen", ceremony.State, r); err == nil {
		t.Fatal("a ceremony begun for one account was finished against another")
	}

	if keys, err := store.PasskeysFor(ctx, "victim"); err != nil || len(keys) != 0 {
		t.Fatalf("victim has %d passkeys, want 0 (err %v)", len(keys), err)
	}
}

// A credential this application has never seen is one indistinguishable
// failure, not "unknown credential" as opposed to "wrong signature".
func TestAnUnknownCredentialFailsWithoutSayingSo(t *testing.T) {
	ctx := context.Background()
	store := sqlitePasskeys(t)
	p := testPasskeys(t, store)

	stranger := newAuthenticator(t, testRPID, testOrigin)

	ceremony, err := p.BeginLogin(ctx)
	if err != nil {
		t.Fatal(err)
	}

	body := stranger.Assert(t, challengeOf(t, ceremony.Options), []byte("account-1"))
	r := httptest.NewRequest("POST", "/passkeys/login", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")

	_, err = p.FinishLogin(ctx, ceremony.State, r)
	if !errors.Is(err, ErrUnknownCredential) {
		t.Fatalf("err = %v, want ErrUnknownCredential", err)
	}
}

// An account may have several passkeys -- a laptop and a phone -- and each is
// revocable on its own.
func TestSeveralCredentialsPerAccountAndRevocation(t *testing.T) {
	ctx := context.Background()
	store := sqlitePasskeys(t)
	p := testPasskeys(t, store)

	laptop := newAuthenticator(t, testRPID, testOrigin)
	phone := newAuthenticator(t, testRPID, testOrigin)

	register(t, p, laptop, "account-1", "MacBook")
	register(t, p, phone, "account-1", "iPhone")

	keys, err := store.PasskeysFor(ctx, "account-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 {
		t.Fatalf("%d passkeys, want 2", len(keys))
	}

	// Transports survive the round trip, and are derived from the record rather
	// than kept in a second column that could disagree with it.
	if len(keys[0].Transports) != 2 {
		t.Fatalf("transports = %v, want the two the authenticator reported", keys[0].Transports)
	}

	if err := p.Revoke(ctx, "account-1", phone.credentialID, true); err != nil {
		t.Fatal(err)
	}

	keys, err = store.PasskeysFor(ctx, "account-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || bytes.Equal(keys[0].CredentialID, phone.credentialID) {
		t.Fatal("revocation removed the wrong credential")
	}

	// The revoked credential can no longer sign in.
	ceremony, err := p.BeginLogin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	body := phone.Assert(t, challengeOf(t, ceremony.Options), []byte("account-1"))
	r := httptest.NewRequest("POST", "/passkeys/login", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")

	if _, err := p.FinishLogin(ctx, ceremony.State, r); !errors.Is(err, ErrUnknownCredential) {
		t.Fatalf("a revoked credential signed in: %v", err)
	}
}

// Revoking the only way into an account is a lockout with no recovery path
// short of a database edit.
func TestRevokingTheLastPasskeyOfAPasswordlessAccountIsRefused(t *testing.T) {
	ctx := context.Background()
	store := sqlitePasskeys(t)
	p := testPasskeys(t, store)

	only := newAuthenticator(t, testRPID, testOrigin)
	register(t, p, only, "account-1", "MacBook")

	if err := p.Revoke(ctx, "account-1", only.credentialID, false); err == nil {
		t.Fatal("revoked the last passkey of an account with no password")
	}

	// With a password set it is allowed: the account still has a way in.
	if err := p.Revoke(ctx, "account-1", only.credentialID, true); err != nil {
		t.Fatal(err)
	}
}

// An authenticator that already holds a credential for this account is told so
// by the exclude list, rather than silently creating a second one.
func TestRegistrationExcludesCredentialsTheAccountAlreadyHas(t *testing.T) {
	ctx := context.Background()
	store := sqlitePasskeys(t)
	p := testPasskeys(t, store)

	existing := newAuthenticator(t, testRPID, testOrigin)
	register(t, p, existing, "account-1", "MacBook")

	ceremony, err := p.BeginRegistration(ctx, "account-1", "alex@example.com", "Alex")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(ceremony.Options), b64(existing.credentialID)) {
		t.Fatalf("the existing credential is not in the exclude list: %s", ceremony.Options)
	}
}

func TestPasskeyConfigRefusesToRunWithoutARelyingParty(t *testing.T) {
	store := sqlitePasskeys(t)

	if _, err := NewPasskeys(PasskeyConfig{Origins: []string{testOrigin}}, store); err == nil {
		t.Fatal("accepted a config with no relying party id")
	}
	if _, err := NewPasskeys(PasskeyConfig{RelyingPartyID: testRPID}, store); err == nil {
		t.Fatal("accepted a config with no origins")
	}
}

var _ PasskeyStore = (*SQLPasskeyStore)(nil)

// The user handle the authenticator asserts must agree with the account the
// credential is stored against. Without that agreement a credential could be
// replayed to claim a different account.
func TestAMismatchedUserHandleIsRejected(t *testing.T) {
	ctx := context.Background()
	store := sqlitePasskeys(t)
	p := testPasskeys(t, store)

	authenticator := newAuthenticator(t, testRPID, testOrigin)
	register(t, p, authenticator, "account-1", "MacBook")

	ceremony, err := p.BeginLogin(ctx)
	if err != nil {
		t.Fatal(err)
	}

	body := authenticator.Assert(t, challengeOf(t, ceremony.Options), []byte("account-2"))
	r := httptest.NewRequest("POST", "/passkeys/login", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")

	login, err := p.FinishLogin(ctx, ceremony.State, r)
	if err == nil {
		t.Fatalf("a credential owned by account-1 signed in as %q", login.AccountID)
	}
}
