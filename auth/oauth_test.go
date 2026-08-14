package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func testOAuth(t *testing.T, issuer *testIssuer) *OAuth {
	t.Helper()

	o, err := NewOAuth(context.Background(),
		OIDC("test", issuer.URL(), "client-id", "secret", "https://app.example/callback"),
		WithHTTPClient(issuer.client()))
	if err != nil {
		t.Fatal(err)
	}
	return o
}

// The happy path, end to end against a real issuer: discovery, an authorization
// URL, a token exchange, a signed ID token, a verified identity.
func TestSignInWithOIDC(t *testing.T) {
	issuer := newTestIssuer(t)
	o := testOAuth(t, issuer)

	ceremony, err := o.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	issuer.issue("code-1", map[string]any{
		"sub":            "1234",
		"aud":            "client-id",
		"nonce":          nonceFrom(t, ceremony),
		"email":          "Ada@Example.com",
		"email_verified": true,
		"name":           "Ada Lovelace",
		"picture":        "https://example.com/ada.png",
	})

	identity, err := o.Finish(context.Background(), ceremony.State, callback(t, ceremony, "code-1"))
	if err != nil {
		t.Fatal(err)
	}

	if identity.Provider != "test" || identity.Subject != "1234" {
		t.Errorf("identity is %s/%s", identity.Provider, identity.Subject)
	}
	// Lower-cased, because an address that differs only in case is the same
	// address, and a store keyed on it would otherwise hold two.
	if identity.Email != "ada@example.com" {
		t.Errorf("email is %q", identity.Email)
	}
	if !identity.EmailVerified {
		t.Error("the provider verified the address and the identity does not say so")
	}
	if identity.Name != "Ada Lovelace" {
		t.Errorf("name is %q", identity.Name)
	}
}

// State, PKCE and nonce are not optional and not configurable. This asserts all
// three are on the wire, because each of them silently missing is a different
// vulnerability that nothing else in the flow would notice.
func TestEveryCeremonyCarriesStatePKCEAndNonce(t *testing.T) {
	issuer := newTestIssuer(t)
	o := testOAuth(t, issuer)

	ceremony, err := o.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	authorize, err := url.Parse(ceremony.URL)
	if err != nil {
		t.Fatal(err)
	}
	q := authorize.Query()

	for _, param := range []string{"state", "nonce", "code_challenge"} {
		if q.Get(param) == "" {
			t.Errorf("the authorization url has no %s", param)
		}
	}
	if q.Get("code_challenge_method") != "S256" {
		t.Errorf("code_challenge_method is %q, and plain PKCE protects nothing", q.Get("code_challenge_method"))
	}

	// Two ceremonies must not share any of them, or the whole point is lost.
	second, err := o.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	secondQuery, _ := url.Parse(second.URL)
	for _, param := range []string{"state", "nonce", "code_challenge"} {
		if q.Get(param) == secondQuery.Query().Get(param) {
			t.Errorf("two ceremonies share a %s", param)
		}
	}

	// And the verifier really is sent at the exchange, not merely generated.
	issuer.issue("code-1", map[string]any{"sub": "1", "aud": "client-id", "nonce": nonceFrom(t, ceremony)})
	if _, err := o.Finish(context.Background(), ceremony.State, callback(t, ceremony, "code-1")); err != nil {
		t.Fatal(err)
	}
	if issuer.lastForm.Get("code_verifier") == "" {
		t.Error("the token exchange sent no code_verifier, so PKCE was theatre")
	}
}

// A callback whose state is not this ceremony's is login CSRF: somebody else's
// authorization being completed in this browser.
func TestACallbackWithTheWrongStateIsRejected(t *testing.T) {
	issuer := newTestIssuer(t)
	o := testOAuth(t, issuer)

	ceremony, err := o.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	issuer.issue("code-1", map[string]any{"sub": "1", "aud": "client-id", "nonce": nonceFrom(t, ceremony)})

	for _, state := range []string{"", "not-the-state", strings.Repeat("a", 43)} {
		r := httptest.NewRequest("GET", "/callback?code=code-1&state="+url.QueryEscape(state), nil)

		if _, err := o.Finish(context.Background(), ceremony.State, r); !errors.Is(err, ErrStateMismatch) {
			t.Errorf("state %q: %v, want ErrStateMismatch", state, err)
		}
	}
}

// A token minted for a different ceremony has a perfect signature. The nonce is
// the only thing that catches it.
func TestAnIDTokenFromAnotherCeremonyIsRejected(t *testing.T) {
	issuer := newTestIssuer(t)
	o := testOAuth(t, issuer)

	victim, err := o.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	other, err := o.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// A genuine, correctly signed token -- for the other ceremony.
	issuer.issue("code-1", map[string]any{
		"sub": "1", "aud": "client-id", "nonce": nonceFrom(t, other),
	})

	if _, err := o.Finish(context.Background(), victim.State, callback(t, victim, "code-1")); !errors.Is(err, ErrNonceMismatch) {
		t.Fatalf("%v, want ErrNonceMismatch", err)
	}
}

// The audience check is go-oidc's, and this asserts it is actually wired up:
// a token for a different client is a token this application must not accept.
func TestAnIDTokenForAnotherClientIsRejected(t *testing.T) {
	issuer := newTestIssuer(t)
	o := testOAuth(t, issuer)

	ceremony, err := o.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	issuer.issue("code-1", map[string]any{
		"sub": "1", "aud": "someone-elses-client", "nonce": nonceFrom(t, ceremony),
	})

	if _, err := o.Finish(context.Background(), ceremony.State, callback(t, ceremony, "code-1")); err == nil {
		t.Fatal("a token minted for another client was accepted")
	}
}

// An expired token, for the same reason: the check belongs to the library, and
// this proves the library is doing it.
func TestAnExpiredIDTokenIsRejected(t *testing.T) {
	issuer := newTestIssuer(t)
	o := testOAuth(t, issuer)

	ceremony, err := o.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	issuer.issue("code-1", map[string]any{
		"sub": "1", "aud": "client-id", "nonce": nonceFrom(t, ceremony),
		"exp": time.Now().Add(-time.Hour).Unix(),
	})

	if _, err := o.Finish(context.Background(), ceremony.State, callback(t, ceremony, "code-1")); err == nil {
		t.Fatal("an expired token was accepted")
	}
}

// A token signed by a key the issuer never published. This is the one that
// would let anybody sign in as anybody, so it gets its own test rather than
// being trusted to the dependency.
func TestAnIDTokenSignedByAStrangerIsRejected(t *testing.T) {
	issuer := newTestIssuer(t)
	o := testOAuth(t, issuer)

	ceremony, err := o.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// A second issuer with its own key, minting a token that claims to come
	// from the first. Everything about it is right except who signed it.
	forger := newTestIssuer(t)
	forged := forger.sign(map[string]any{
		"iss": issuer.URL(), "sub": "1", "aud": "client-id", "nonce": nonceFrom(t, ceremony),
	})

	// Hand it back from a token endpoint of its own: what is under test is
	// verification, not how the token arrived.
	hostile := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"access_token": "at", "token_type": "Bearer", "id_token": forged,
		})
	}))
	defer hostile.Close()
	o.config.Endpoint.TokenURL = hostile.URL

	if _, err := o.Finish(context.Background(), ceremony.State, callback(t, ceremony, "code-1")); err == nil {
		t.Fatal("a token signed by a key the issuer never published was accepted")
	} else if !strings.Contains(err.Error(), "signature") {
		// Rejected for the right reason. Without this the test would still pass
		// if the signature check vanished and something incidental -- a nonce,
		// a claim -- happened to fail instead.
		t.Errorf("rejected, but not for its signature: %v", err)
	}
}

// A ceremony that has been sitting in a session for an hour is a replay window.
func TestAStaleCeremonyIsRejected(t *testing.T) {
	issuer := newTestIssuer(t)
	o := testOAuth(t, issuer)

	ceremony, err := o.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	var blob oauthState
	if err := json.Unmarshal(ceremony.State, &blob); err != nil {
		t.Fatal(err)
	}
	blob.Issued = time.Now().UTC().Add(-OAuthCeremonyTTL - time.Minute)

	stale, err := json.Marshal(blob)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := o.Finish(context.Background(), stale, callback(t, ceremony, "code-1")); err == nil {
		t.Fatal("a ceremony older than the ttl was accepted")
	}
}

// State issued for one provider must not complete a ceremony at another, or a
// misconfigured application lets an attacker choose which provider vouches for
// them.
func TestStateFromAnotherProviderIsRejected(t *testing.T) {
	issuer := newTestIssuer(t)
	o := testOAuth(t, issuer)

	other, err := NewOAuth(context.Background(),
		OIDC("other", issuer.URL(), "client-id", "secret", "https://app.example/callback"),
		WithHTTPClient(issuer.client()))
	if err != nil {
		t.Fatal(err)
	}

	ceremony, err := other.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := o.Finish(context.Background(), ceremony.State, callback(t, ceremony, "code-1")); err == nil {
		t.Fatal("a ceremony begun at another provider was completed here")
	}
}

// The provider reporting a refusal is a person clicking Cancel, and it must not
// look like a successful sign-in.
func TestAProviderRefusalIsAnError(t *testing.T) {
	issuer := newTestIssuer(t)
	o := testOAuth(t, issuer)

	ceremony, err := o.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	authorize, _ := url.Parse(ceremony.URL)
	r := httptest.NewRequest("GET", "/callback?"+url.Values{
		"state": {authorize.Query().Get("state")},
		"error": {"access_denied"},
	}.Encode(), nil)

	if _, err := o.Finish(context.Background(), ceremony.State, r); err == nil {
		t.Fatal("a refused authorization looked like a sign-in")
	}
}

// GitHub is OAuth 2.0 without OIDC, so the identity comes from its user API --
// and the address it returns is one its owner typed, so it is never verified.
func TestANonOIDCProviderNeverReportsAVerifiedEmail(t *testing.T) {
	var api *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"access_token": "at", "token_type": "Bearer"})
	})
	mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer at" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		writeJSON(w, map[string]any{
			"id": 42, "login": "ada", "name": "Ada", "email": "ADA@example.com",
			"avatar_url": "https://example.com/ada.png",
		})
	})
	mux.HandleFunc("/user/emails", func(w http.ResponseWriter, r *http.Request) {
		t.Error("the addresses endpoint was called although the profile had an address")
	})
	api = httptest.NewServer(mux)
	defer api.Close()

	o, err := NewOAuth(context.Background(), Provider{
		Name: "github", ClientID: "id", ClientSecret: "secret",
		RedirectURL: "https://app.example/callback",
		AuthURL:     api.URL + "/authorize",
		TokenURL:    api.URL + "/token",
		UserInfoURL: api.URL + "/user",
		// Set, so that the /user/emails handler's t.Error is reachable: the
		// endpoint must go unread because the profile had an address, not
		// because there was nowhere to read it from.
		EmailsURL: api.URL + "/user/emails",
	}, WithHTTPClient(api.Client()))
	if err != nil {
		t.Fatal(err)
	}

	ceremony, err := o.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	identity, err := o.Finish(context.Background(), ceremony.State, callback(t, ceremony, "code-1"))
	if err != nil {
		t.Fatal(err)
	}

	if identity.Subject != "42" || identity.Name != "Ada" {
		t.Errorf("identity is %+v", identity)
	}
	if identity.Email != "ada@example.com" {
		t.Errorf("email is %q", identity.Email)
	}
	if identity.EmailVerified {
		t.Error("a self-declared address was reported as verified, which is the account-takeover path")
	}
	// A non-OIDC provider has no id_token, so a nonce would be a promise the
	// flow cannot keep. It must not be sent.
	authorize, _ := url.Parse(ceremony.URL)
	if authorize.Query().Get("nonce") != "" {
		t.Error("a nonce was sent to a provider that cannot echo it")
	}
}

// Misconfiguration must fail at start-up, where somebody is watching, rather
// than at the first sign-in.
func TestAProviderWithoutEnoughConfigurationIsRejected(t *testing.T) {
	cases := map[string]Provider{
		"no name":     {ClientID: "id", RedirectURL: "https://x/cb", Issuer: "https://accounts.example"},
		"no client":   {Name: "x", RedirectURL: "https://x/cb", Issuer: "https://accounts.example"},
		"no redirect": {Name: "x", ClientID: "id", Issuer: "https://accounts.example"},
		"no endpoints": {
			Name: "x", ClientID: "id", RedirectURL: "https://x/cb",
		},
	}

	for name, provider := range cases {
		if _, err := NewOAuth(context.Background(), provider); err == nil {
			t.Errorf("%s: configured anyway", name)
		}
	}
}

// The shipped providers point where they should. Cheap, and it is the sort of
// typo that is only ever found in production.
func TestTheShippedProvidersAreConfigured(t *testing.T) {
	if g := Google("id", "secret", "cb"); g.Issuer != "https://accounts.google.com" {
		t.Errorf("google issuer is %q", g.Issuer)
	}
	const tenant = "72f988bf-86f1-41af-91ab-2d7cd011db47"
	if m := Microsoft(tenant, "id", "secret", "cb"); !strings.Contains(m.Issuer, "/"+tenant+"/") {
		t.Errorf("microsoft tenant is %q", m.Issuer)
	}
	// "consumers" is substituted for the tenant it stands for, because Entra
	// reports the id as the issuer however the tenant was addressed -- so the
	// friendly name would otherwise fail the issuer check it is meant to pass.
	if m := Microsoft("consumers", "id", "secret", "cb"); !strings.Contains(m.Issuer, consumerTenant) {
		t.Errorf("consumers became %q rather than its tenant id", m.Issuer)
	}
	if g := GitHub("id", "secret", "cb"); g.EmailsURL == "" {
		t.Error("github requests the user:email scope and has nowhere to spend it")
	}
	if g := GitHub("id", "secret", "cb"); g.Issuer != "" || g.UserInfoURL == "" {
		t.Error("github is configured as if it were an OIDC provider")
	}
}

// Entra reports the tenant id as the issuer however the tenant was addressed,
// so a GUID is the only form that verifies. The aliases and the domain form all
// serve a discovery document, and none of them can be used -- which has to fail
// here, offline, at start-up, rather than with a string-comparison error at the
// first sign-in that somebody fixes by disabling the issuer check.
func TestEntraTenantsThatCannotVerifyAreRefused(t *testing.T) {
	for _, tenant := range []string{"", "common", "organizations", "contoso.onmicrosoft.com"} {
		_, err := NewOAuth(context.Background(),
			Microsoft(tenant, "id", "secret", "https://app.example/cb"))

		if err == nil {
			t.Errorf("tenant %q: configured, and it cannot work", tenant)
			continue
		}
		if !strings.Contains(err.Error(), "tenant id") {
			t.Errorf("tenant %q: %v, which does not say what to do instead", tenant, err)
		}
	}
}

// Most GitHub users keep their address private, so the profile carries none.
// Without this the generated sign-up creates an account nobody can mail and,
// the second time it happens, violates the unique constraint on users.email --
// while holding a user:email scope it never spent.
func TestAPrivateGitHubAddressIsReadFromTheAddressesEndpoint(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"access_token": "at", "token_type": "Bearer"})
	})
	mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"id": 42, "login": "ada", "name": "Ada"})
	})
	mux.HandleFunc("/user/emails", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer at" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		writeJSON(w, []any{
			// Not primary. Taking this one would be guessing which of
			// somebody's addresses they meant.
			map[string]any{"email": "old@example.com", "primary": false, "verified": true},
			// Primary but unverified: not evidence of anything.
			map[string]any{"email": "unconfirmed@example.com", "primary": false, "verified": false},
			map[string]any{"email": "Ada@Example.com", "primary": true, "verified": true},
		})
	})

	api := httptest.NewServer(mux)
	defer api.Close()

	identity := githubIdentity(t, api, api.URL+"/user/emails")

	if identity.Email != "ada@example.com" {
		t.Errorf("email is %q, want the verified primary one", identity.Email)
	}
	// GitHub really did verify this one, unlike the public profile field.
	if !identity.EmailVerified {
		t.Error("an address GitHub verified is not marked verified")
	}
}

// A private address with no verified primary, and an endpoint that refuses.
// Neither is fatal: the identity is complete without an address, and failing
// the whole sign-in over a profile detail would be worse than the gap.
func TestAGitHubIdentityWithNoReadableAddressStillSignsIn(t *testing.T) {
	for _, name := range []string{"unverified", "refused"} {
		t.Run(name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, map[string]any{"access_token": "at", "token_type": "Bearer"})
			})
			mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, map[string]any{"id": 42, "login": "ada"})
			})
			mux.HandleFunc("/user/emails", func(w http.ResponseWriter, r *http.Request) {
				if name == "refused" {
					http.Error(w, "forbidden", http.StatusForbidden)
					return
				}
				writeJSON(w, []any{
					map[string]any{"email": "ada@example.com", "primary": true, "verified": false},
				})
			})

			api := httptest.NewServer(mux)
			defer api.Close()

			identity := githubIdentity(t, api, api.URL+"/user/emails")

			if identity.Subject != "42" {
				t.Fatalf("the sign-in did not complete: %+v", identity)
			}
			if identity.Email != "" || identity.EmailVerified {
				t.Errorf("kept an address it should not trust: %q verified=%v", identity.Email, identity.EmailVerified)
			}
		})
	}
}

// githubIdentity runs a whole non-OIDC ceremony against a test API.
func githubIdentity(t *testing.T, api *httptest.Server, emailsURL string) *Identity {
	t.Helper()

	o, err := NewOAuth(context.Background(), Provider{
		Name: "github", ClientID: "id", ClientSecret: "secret",
		RedirectURL: "https://app.example/callback",
		AuthURL:     api.URL + "/authorize",
		TokenURL:    api.URL + "/token",
		UserInfoURL: api.URL + "/user",
		EmailsURL:   emailsURL,
	}, WithHTTPClient(api.Client()))
	if err != nil {
		t.Fatal(err)
	}

	ceremony, err := o.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	identity, err := o.Finish(context.Background(), ceremony.State, callback(t, ceremony, "code-1"))
	if err != nil {
		t.Fatal(err)
	}
	return identity
}
