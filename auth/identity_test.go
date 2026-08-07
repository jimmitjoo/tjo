package auth

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

// testAccount is the minimum an application's user type has to be.
type testAccount struct {
	id        string
	hash      []byte
	activated bool
}

func (a testAccount) AuthID() string       { return a.id }
func (a testAccount) PasswordHash() []byte { return a.hash }
func (a testAccount) Activated() bool      { return a.activated }

// accountsByEmail is an AccountStore over a map.
type accountsByEmail map[string]Account

func (a accountsByEmail) ByEmail(ctx context.Context, email string) (Account, error) {
	account, ok := a[email]
	if !ok {
		return nil, nil
	}
	return account, nil
}

func identityStore(t *testing.T) *SQLIdentityStore {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	store := NewSQLIdentityStore(db, DialectSQLite)
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return store
}

func googleIdentity(subject, email string, verified bool) *Identity {
	return &Identity{
		Provider: "google", Subject: subject,
		Email: email, EmailVerified: verified, Name: "Ada",
	}
}

// A returning user: the subject is known, so it is their account, no questions.
func TestAKnownIdentitySignsIn(t *testing.T) {
	ctx := context.Background()
	store := identityStore(t)

	if err := Link(ctx, store, "acct-1", googleIdentity("g-1", "ada@example.com", true)); err != nil {
		t.Fatal(err)
	}

	resolution, err := Resolve(ctx, store, googleIdentity("g-1", "ada@example.com", true), ResolveOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if resolution.Outcome != SignedIn || resolution.AccountID != "acct-1" {
		t.Fatalf("outcome %d, account %q", resolution.Outcome, resolution.AccountID)
	}
}

// The account-takeover test. Somebody registers a provider account with the
// victim's address and signs in; they must not be handed the victim's account.
func TestAnUnauthenticatedIdentityNeverMergesByEmail(t *testing.T) {
	ctx := context.Background()
	store := identityStore(t)

	victim := testAccount{id: "victim", hash: []byte("hash"), activated: true}
	accounts := accountsByEmail{"ada@example.com": victim}

	// Even with the provider asserting the address is verified. Verified means
	// the provider checked it, not that this is the same person -- and a flow
	// that merges on it is only as safe as every provider it is ever
	// configured with.
	for _, verified := range []bool{true, false} {
		resolution, err := Resolve(ctx, store,
			googleIdentity("g-attacker", "ada@example.com", verified),
			ResolveOptions{Accounts: accounts})
		if err != nil {
			t.Fatal(err)
		}

		if resolution.Outcome != NeedsLogin {
			t.Errorf("verified=%v: outcome %d, want NeedsLogin", verified, resolution.Outcome)
		}
		if resolution.AccountID != "" {
			t.Errorf("verified=%v: handed out account %q", verified, resolution.AccountID)
		}
	}

	// And nothing was written: a refused sign-in must not leave a link behind
	// for the next attempt to find.
	if linked, err := store.IdentityBySubject(ctx, "google", "g-attacker"); err != nil || linked != "" {
		t.Errorf("the refused identity was linked to %q anyway (%v)", linked, err)
	}
}

// The same flow, once the person has proved the account is theirs.
func TestASignedInPersonLinksAProvider(t *testing.T) {
	ctx := context.Background()
	store := identityStore(t)

	resolution, err := Resolve(ctx, store,
		googleIdentity("g-1", "ada@example.com", true),
		ResolveOptions{CurrentAccountID: "acct-1"})
	if err != nil {
		t.Fatal(err)
	}

	if resolution.Outcome != SignedIn || resolution.AccountID != "acct-1" {
		t.Fatalf("outcome %d, account %q", resolution.Outcome, resolution.AccountID)
	}

	// And the link persists, so the next sign-in needs no password.
	linked, err := store.IdentityBySubject(ctx, "google", "g-1")
	if err != nil || linked != "acct-1" {
		t.Fatalf("linked to %q (%v)", linked, err)
	}
}

// A provider identity belongs to one account. Attaching a victim's Google
// identity to an attacker's account would take the victim's way in with it.
func TestAnIdentityCannotBeMovedToAnotherAccount(t *testing.T) {
	ctx := context.Background()
	store := identityStore(t)

	if err := Link(ctx, store, "victim", googleIdentity("g-1", "ada@example.com", true)); err != nil {
		t.Fatal(err)
	}

	_, err := Resolve(ctx, store,
		googleIdentity("g-1", "ada@example.com", true),
		ResolveOptions{CurrentAccountID: "attacker"})

	if !errors.Is(err, ErrLinkedElsewhere) {
		t.Fatalf("%v, want ErrLinkedElsewhere", err)
	}

	if linked, _ := store.IdentityBySubject(ctx, "google", "g-1"); linked != "victim" {
		t.Fatalf("the identity moved to %q", linked)
	}
}

// A first-time visitor with no matching account. The caller creates one.
func TestAnUnknownIdentityWithNoMatchNeedsAnAccount(t *testing.T) {
	ctx := context.Background()
	store := identityStore(t)

	resolution, err := Resolve(ctx, store,
		googleIdentity("g-1", "new@example.com", true),
		ResolveOptions{Accounts: accountsByEmail{}})
	if err != nil {
		t.Fatal(err)
	}

	if resolution.Outcome != NoAccount {
		t.Fatalf("outcome %d, want NoAccount", resolution.Outcome)
	}
	if resolution.Identity == nil || resolution.Identity.Email != "new@example.com" {
		t.Fatal("the identity was not handed back for the caller to create an account with")
	}
}

// Without an AccountStore there is nothing to match against, which is the
// documented behaviour and worth pinning: it must not silently sign somebody in.
func TestResolveWithoutAnAccountStoreCreatesRatherThanMerges(t *testing.T) {
	ctx := context.Background()
	store := identityStore(t)

	resolution, err := Resolve(ctx, store, googleIdentity("g-1", "ada@example.com", true), ResolveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Outcome != NoAccount {
		t.Fatalf("outcome %d, want NoAccount", resolution.Outcome)
	}
}

// Signing in again refreshes the profile, because a name is the provider's to
// change and a stale copy is only ever noticed by the person it is wrong about.
func TestSigningInRefreshesTheStoredProfile(t *testing.T) {
	ctx := context.Background()
	store := identityStore(t)

	if err := Link(ctx, store, "acct-1", &Identity{
		Provider: "google", Subject: "g-1", Email: "ada@example.com", Name: "Ada Byron",
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := Resolve(ctx, store, &Identity{
		Provider: "google", Subject: "g-1", Email: "ada@example.com", Name: "Ada Lovelace",
	}, ResolveOptions{}); err != nil {
		t.Fatal(err)
	}

	identities, err := store.IdentitiesFor(ctx, "acct-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(identities) != 1 || identities[0].Name != "Ada Lovelace" {
		t.Fatalf("stored identities are %+v", identities)
	}
}

// Unlinking the last way into an account locks its owner out permanently, and
// support cannot fix it because there is nothing left to verify them with.
func TestUnlinkingTheLastIdentityOfAPasswordlessAccountIsRefused(t *testing.T) {
	ctx := context.Background()
	store := identityStore(t)

	account := testAccount{id: "acct-1", activated: true}
	if err := Link(ctx, store, account.id, googleIdentity("g-1", "ada@example.com", true)); err != nil {
		t.Fatal(err)
	}

	if err := Unlink(ctx, store, account, "google", 0); !errors.Is(err, ErrLastIdentity) {
		t.Fatalf("%v, want ErrLastIdentity", err)
	}

	if linked, _ := store.IdentityBySubject(ctx, "google", "g-1"); linked != "acct-1" {
		t.Fatal("the identity was removed anyway")
	}
}

// The same unlink, when there is another way in.
func TestUnlinkingIsAllowedWhenSomethingElseRemains(t *testing.T) {
	ctx := context.Background()

	withPassword := testAccount{id: "acct-1", hash: []byte("hash"), activated: true}
	passwordless := testAccount{id: "acct-2", activated: true}

	cases := []struct {
		name             string
		account          Account
		second           *Identity
		otherCredentials int
	}{
		{"a password remains", withPassword, nil, 0},
		{"another provider remains", passwordless, &Identity{Provider: "github", Subject: "gh-1"}, 0},
		{"a passkey remains", passwordless, nil, 1},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store := identityStore(t)

			if err := Link(ctx, store, c.account.AuthID(), googleIdentity("g-1", "ada@example.com", true)); err != nil {
				t.Fatal(err)
			}
			if c.second != nil {
				if err := Link(ctx, store, c.account.AuthID(), c.second); err != nil {
					t.Fatal(err)
				}
			}

			if err := Unlink(ctx, store, c.account, "google", c.otherCredentials); err != nil {
				t.Fatalf("refused a safe unlink: %v", err)
			}
			if linked, _ := store.IdentityBySubject(ctx, "google", "g-1"); linked != "" {
				t.Fatal("the identity is still linked")
			}
		})
	}
}

// Relinking the same provider replaces rather than duplicates: somebody who
// unlinks Google and links a different Google account has one, not two.
func TestLinkingTheSameProviderTwiceReplaces(t *testing.T) {
	ctx := context.Background()
	store := identityStore(t)

	if err := Link(ctx, store, "acct-1", googleIdentity("g-1", "ada@example.com", true)); err != nil {
		t.Fatal(err)
	}
	if err := Link(ctx, store, "acct-1", googleIdentity("g-2", "ada@work.example", true)); err != nil {
		t.Fatal(err)
	}

	identities, err := store.IdentitiesFor(ctx, "acct-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(identities) != 1 || identities[0].Subject != "g-2" {
		t.Fatalf("stored identities are %+v", identities)
	}

	// The old subject no longer resolves, so the previous Google account has
	// genuinely lost access rather than quietly keeping it.
	if linked, _ := store.IdentityBySubject(ctx, "google", "g-1"); linked != "" {
		t.Fatalf("the replaced identity still resolves to %q", linked)
	}
}

// Unlinking something that was never linked is not an error, so a double
// submit does not produce a stack trace.
func TestUnlinkingSomethingThatIsNotThereIsNotAnError(t *testing.T) {
	ctx := context.Background()
	store := identityStore(t)

	account := testAccount{id: "acct-1", hash: []byte("hash"), activated: true}
	if err := Unlink(ctx, store, account, "google", 0); err != nil {
		t.Fatal(err)
	}
}

// Resolving nothing is a programming error, not a sign-in.
func TestResolvingAnEmptyIdentityIsRefused(t *testing.T) {
	ctx := context.Background()
	store := identityStore(t)

	for _, identity := range []*Identity{nil, {}, {Provider: "google"}, {Subject: "g-1"}} {
		if _, err := Resolve(ctx, store, identity, ResolveOptions{}); err == nil {
			t.Errorf("%+v resolved", identity)
		}
	}
}
