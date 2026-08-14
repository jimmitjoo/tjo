package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// Signing in with an identity provider, and the rules that make it safe.
//
// The token exchange is mechanical and takes a day. What takes the thought is
// what happens when somebody signs in with Google and an account with that
// email already exists -- see Resolve, which is where the account-takeover
// vulnerability in this feature lives.
//
// # Why go-oidc rather than hand-rolled verification
//
// Verifying an ID token means fetching a JWKS, selecting the right key,
// refusing algorithms the provider did not advertise, surviving key rotation,
// and checking issuer, audience, expiry and nonce. Every one of those is a
// place to be subtly wrong, and being subtly wrong means accepting a forged
// identity.
//
// This project's rule is to prefer the standard library and it has removed four
// dependencies by doing so. This is the case where that rule does not apply:
// two of its four published advisories were in hand-written authentication
// code. go-oidc costs one module the graph did not already have.
//
// # This does not create a session
//
// Consistent with the rest of the package. The flow reports who signed in; the
// caller renews the session and rotates the CSRF token, which is the same three
// lines every other login path in this framework ends with. Skipping it is
// session fixation.

// Errors from the sign-in flow. Each one is a different thing for the caller to
// do, which is why they are distinguishable -- unlike the credential errors
// elsewhere in this package, none of these is a probing oracle: they are only
// ever returned to somebody who has just completed an OAuth ceremony.
var (
	// ErrStateMismatch means the callback did not carry the state this
	// ceremony issued. It is login CSRF: somebody is trying to complete their
	// own authorization in this browser's session.
	ErrStateMismatch = errors.New("auth: oauth state does not match")

	// ErrNonceMismatch means the ID token was not minted for this request. A
	// token replayed from another ceremony verifies its signature perfectly.
	ErrNonceMismatch = errors.New("auth: id token nonce does not match")

	// ErrNoIDToken means an OIDC provider returned no id_token.
	ErrNoIDToken = errors.New("auth: provider returned no id token")

	// ErrEmailNotVerified means the provider did not assert that it had
	// verified the address, so it cannot be used to find an existing account.
	ErrEmailNotVerified = errors.New("auth: the provider did not verify this email address")

	// ErrAccountExists means an account already uses this email and the person
	// signing in has not proved they own it.
	ErrAccountExists = errors.New("auth: an account with this email already exists; sign in first, then link")

	// ErrLastIdentity means unlinking would leave an account with no way in.
	ErrLastIdentity = errors.New("auth: refusing to unlink the last identity of an account with no password")
)

// Provider is one identity provider.
type Provider struct {
	// Name is stored on the identity and must be stable forever: it is half
	// the key that maps a person to their account. Renaming it orphans every
	// identity stored under the old one.
	Name string

	ClientID     string
	ClientSecret string

	// RedirectURL must match what is registered with the provider, exactly.
	RedirectURL string

	// Issuer is the OIDC issuer URL. Discovery reads its
	// .well-known/openid-configuration, so nothing else needs configuring.
	//
	// Leave empty for a provider that is OAuth 2.0 but not OIDC -- GitHub --
	// and fill in the three endpoints below instead.
	Issuer string

	// AuthURL, TokenURL and UserInfoURL are for non-OIDC providers only.
	AuthURL     string
	TokenURL    string
	UserInfoURL string

	// EmailsURL is read when the profile carries no email address, and is
	// expected to return GitHub's shape: a list of {email, primary, verified}.
	//
	// It exists because most GitHub users keep their address private, so the
	// profile endpoint returns none -- and an application that creates
	// accounts from these identities then has an account it cannot mail and,
	// the second time it happens, a unique constraint on an empty string.
	EmailsURL string

	// Scopes beyond the defaults. openid, profile and email are added for OIDC
	// providers; a caller asking for more is asking for access to something,
	// which is a different feature with a different consent screen.
	Scopes []string
}

// Google returns a configured Google provider.
func Google(clientID, clientSecret, redirectURL string) Provider {
	return Provider{
		Name:         "google",
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Issuer:       "https://accounts.google.com",
	}
}

// consumerTenant is Entra's stable tenant id for personal Microsoft accounts.
//
// A constant rather than the "consumers" alias because Entra's discovery
// documents name the tenant *id* as the issuer however the tenant was
// addressed in the URL, so configuring ".../consumers/v2.0" produces an issuer
// mismatch. Substituting it here is what makes the friendly name work.
const consumerTenant = "9188040d-6c67-4c5b-b112-36a304b66dad"

// Microsoft returns a provider for a Microsoft Entra tenant.
//
// tenant is a tenant id -- the GUID -- or "consumers" for personal Microsoft
// accounts. There is no default.
//
// It is specifically not a domain like "contoso.onmicrosoft.com", and not
// "common" or "organizations". Entra will serve a discovery document for all
// three, and none of them can be used:
//
//   - A domain resolves to a document whose issuer is the tenant's GUID, which
//     does not match the URL it was fetched from.
//   - "common" and "organizations" serve the literal placeholder
//     "https://login.microsoftonline.com/{tenantid}/v2.0", because the real
//     issuer depends on which tenant the person turns out to belong to.
//
// NewOAuth says so, with the tenant id in hand -- see validateEntraTenant.
// Failing there beats failing at discovery with a string comparison, and it
// beats what somebody does on seeing an issuer mismatch, which is to stop
// checking the issuer.
func Microsoft(tenant, clientID, clientSecret, redirectURL string) Provider {
	if tenant == "consumers" {
		tenant = consumerTenant
	}
	return Provider{
		Name:         "microsoft",
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Issuer:       "https://login.microsoftonline.com/" + tenant + "/v2.0",
	}
}

// GitHub returns a GitHub provider.
//
// GitHub is OAuth 2.0 and not OIDC: there is no id_token and no issuer to
// discover, so the identity comes from the user API instead. That is why it
// needs the endpoints spelled out where the others do not.
func GitHub(clientID, clientSecret, redirectURL string) Provider {
	return Provider{
		Name:         "github",
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		AuthURL:      "https://github.com/login/oauth/authorize",
		TokenURL:     "https://github.com/login/oauth/access_token",
		UserInfoURL:  "https://api.github.com/user",
		EmailsURL:    "https://api.github.com/user/emails",
		Scopes:       []string{"read:user", "user:email"},
	}
}

// OIDC returns a provider discovered from an issuer URL, which is what makes
// this work with a corporate identity provider without shipping a driver for
// each one.
func OIDC(name, issuer, clientID, clientSecret, redirectURL string) Provider {
	return Provider{
		Name:         name,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Issuer:       issuer,
	}
}

// OAuth runs the sign-in ceremony for one provider.
type OAuth struct {
	provider Provider
	config   *oauth2.Config
	verifier *oidc.IDTokenVerifier

	// client is used for discovery and for the token exchange, so a test can
	// point the whole thing at an httptest server.
	client *http.Client
}

// NewOAuth prepares a provider, performing OIDC discovery when there is an
// issuer.
//
// Discovery happens once, here, rather than per request: it is a network call
// and doing it on every sign-in makes the identity provider's availability a
// dependency of every login rather than of start-up.
func NewOAuth(ctx context.Context, p Provider, opts ...OAuthOption) (*OAuth, error) {
	if p.Name == "" {
		return nil, errors.New("auth: a provider needs a name")
	}
	if p.ClientID == "" || p.RedirectURL == "" {
		return nil, fmt.Errorf("auth: provider %q needs a client id and a redirect url", p.Name)
	}

	o := &OAuth{provider: p, client: http.DefaultClient}
	for _, opt := range opts {
		opt(o)
	}

	if err := validateEntraTenant(p.Issuer); err != nil {
		return nil, err
	}

	ctx = oidc.ClientContext(ctx, o.client)

	if p.Issuer != "" {
		discovered, err := oidc.NewProvider(ctx, p.Issuer)
		if err != nil {
			return nil, fmt.Errorf("auth: discovering %s: %w", p.Name, err)
		}

		o.config = &oauth2.Config{
			ClientID:     p.ClientID,
			ClientSecret: p.ClientSecret,
			RedirectURL:  p.RedirectURL,
			Endpoint:     discovered.Endpoint(),
			Scopes:       append([]string{oidc.ScopeOpenID, "profile", "email"}, p.Scopes...),
		}
		o.verifier = discovered.Verifier(&oidc.Config{ClientID: p.ClientID})

		return o, nil
	}

	if p.AuthURL == "" || p.TokenURL == "" || p.UserInfoURL == "" {
		return nil, fmt.Errorf("auth: provider %q has no issuer, so it needs auth, token and user-info urls", p.Name)
	}

	o.config = &oauth2.Config{
		ClientID:     p.ClientID,
		ClientSecret: p.ClientSecret,
		RedirectURL:  p.RedirectURL,
		Endpoint:     oauth2.Endpoint{AuthURL: p.AuthURL, TokenURL: p.TokenURL},
		Scopes:       p.Scopes,
	}

	return o, nil
}

// validateEntraTenant refuses the Entra issuer URLs whose discovery document
// names a different issuer than the URL it was fetched from.
//
// Entra reports the tenant *id* as the issuer however the tenant was addressed
// -- so a GUID is the only form that matches itself. A domain resolves to the
// GUID, and "common" and "organizations" resolve to the literal placeholder
// "{tenantid}", because their real issuer depends on who signs in.
//
// Checked here, offline and with the tenant in hand, rather than left to
// discovery: the discovery failure is a string comparison that tells nobody
// what to do, and what somebody does on seeing an issuer mismatch is stop
// checking the issuer, which accepts tokens from every Entra tenant that
// exists.
//
// Real multi-tenant sign-in means checking the issuer against a rule about
// which organizations may sign in. That is an application's policy, not
// something this package can guess, so it is out of scope rather than
// approximated.
func validateEntraTenant(issuer string) error {
	const entra = "https://login.microsoftonline.com/"

	if !strings.HasPrefix(issuer, entra) {
		return nil
	}

	tenant, _, _ := strings.Cut(strings.TrimPrefix(issuer, entra), "/")
	if isGUID(tenant) {
		return nil
	}

	return fmt.Errorf("auth: %q does not name an Entra tenant id, and Entra's discovery document reports the tenant id as the issuer however the tenant is addressed -- so this cannot verify; use the GUID (a domain's is in the \"issuer\" field of https://login.microsoftonline.com/<domain>/v2.0/.well-known/openid-configuration), or \"consumers\" for personal accounts", issuer)
}

// isGUID reports whether s is 8-4-4-4-12 hex.
func isGUID(s string) bool {
	groups := strings.Split(s, "-")
	if len(groups) != 5 {
		return false
	}

	for i, want := range []int{8, 4, 4, 4, 12} {
		if len(groups[i]) != want {
			return false
		}
		for _, c := range groups[i] {
			if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
				return false
			}
		}
	}

	return true
}

// OAuthOption configures an OAuth.
type OAuthOption func(*OAuth)

// WithHTTPClient sets the client used for discovery, the token exchange and
// the user-info call. For tests, and for deployments behind an egress proxy.
func WithHTTPClient(c *http.Client) OAuthOption {
	return func(o *OAuth) { o.client = c }
}

// Name is the provider's stable name.
func (o *OAuth) Name() string { return o.provider.Name }

// OAuthCeremony is a sign-in in flight.
type OAuthCeremony struct {
	// URL is where to send the browser.
	URL string

	// State is opaque and belongs in the user's server-side session until the
	// callback. It carries the state token, the PKCE verifier and the OIDC
	// nonce; all three are secrets for the duration of one ceremony, and a
	// client that can choose them can complete somebody else's sign-in.
	State []byte
}

// oauthState is what State holds.
type oauthState struct {
	Provider string    `json:"p"`
	State    string    `json:"s"`
	Verifier string    `json:"v"`
	Nonce    string    `json:"n"`
	Issued   time.Time `json:"i"`
}

// OAuthCeremonyTTL bounds how long a sign-in may take.
//
// Not unlimited: a state blob that never expires is a replay window that never
// closes, and nobody takes twenty minutes to click one button.
const OAuthCeremonyTTL = 15 * time.Minute

// Begin starts a sign-in.
//
// State, PKCE and nonce are always sent and are not configurable. State is
// login CSRF protection; PKCE is required for public clients and harmless for
// confidential ones; the nonce binds the ID token to this request. A knob that
// turned any of them off would only ever be turned off by mistake.
func (o *OAuth) Begin(ctx context.Context) (*OAuthCeremony, error) {
	state, err := randomToken()
	if err != nil {
		return nil, err
	}
	verifier, err := randomToken()
	if err != nil {
		return nil, err
	}
	nonce, err := randomToken()
	if err != nil {
		return nil, err
	}

	challenge := sha256.Sum256([]byte(verifier))

	options := []oauth2.AuthCodeOption{
		oauth2.SetAuthURLParam("code_challenge", base64.RawURLEncoding.EncodeToString(challenge[:])),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	}
	if o.verifier != nil {
		options = append(options, oidc.Nonce(nonce))
	}

	blob, err := json.Marshal(oauthState{
		Provider: o.provider.Name,
		State:    state,
		Verifier: verifier,
		Nonce:    nonce,
		Issued:   time.Now().UTC(),
	})
	if err != nil {
		return nil, err
	}

	return &OAuthCeremony{
		URL:   o.config.AuthCodeURL(state, options...),
		State: blob,
	}, nil
}

// Identity is who signed in.
type Identity struct {
	// Provider and Subject together identify a person, forever. The subject is
	// the provider's stable id for them.
	Provider string
	Subject  string

	Email string

	// EmailVerified is whether the *provider* asserts it verified the address.
	// It is the difference between an email that may be used to find an
	// existing account and one that may not.
	EmailVerified bool

	Name      string
	AvatarURL string
}

// Finish completes a sign-in and reports who it was.
//
// It does not create a session, look up an account or write anything. Resolve
// does the account part, separately, because the rules there are the ones worth
// reading on their own.
func (o *OAuth) Finish(ctx context.Context, state []byte, r *http.Request) (*Identity, error) {
	var ceremony oauthState
	if err := json.Unmarshal(state, &ceremony); err != nil {
		return nil, fmt.Errorf("auth: ceremony state: %w", err)
	}

	if ceremony.Provider != o.provider.Name {
		return nil, fmt.Errorf("auth: ceremony was begun for %q, not %q", ceremony.Provider, o.provider.Name)
	}
	if time.Since(ceremony.Issued) > OAuthCeremonyTTL {
		return nil, errors.New("auth: this sign-in took too long; start again")
	}

	// Constant time, because the state is a secret and comparing it with ==
	// leaks it one byte at a time to anyone willing to measure.
	if !ConstantTimeEquals([]byte(r.URL.Query().Get("state")), []byte(ceremony.State)) {
		return nil, ErrStateMismatch
	}

	// A provider reporting an error reports it here rather than by failing the
	// exchange, and "access_denied" is a person clicking Cancel.
	if failure := r.URL.Query().Get("error"); failure != "" {
		return nil, fmt.Errorf("auth: %s refused: %s", o.provider.Name, failure)
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		return nil, errors.New("auth: callback carried no authorization code")
	}

	ctx = context.WithValue(ctx, oauth2.HTTPClient, o.client)

	token, err := o.config.Exchange(ctx, code,
		oauth2.SetAuthURLParam("code_verifier", ceremony.Verifier))
	if err != nil {
		return nil, fmt.Errorf("auth: exchanging the code with %s: %w", o.provider.Name, err)
	}

	if o.verifier != nil {
		return o.identityFromIDToken(ctx, token, ceremony.Nonce)
	}
	return o.identityFromUserInfo(ctx, token)
}

// identityFromIDToken reads the identity out of a verified OIDC token.
func (o *OAuth) identityFromIDToken(ctx context.Context, token *oauth2.Token, nonce string) (*Identity, error) {
	raw, ok := token.Extra("id_token").(string)
	if !ok || raw == "" {
		return nil, ErrNoIDToken
	}

	// Verify checks the signature against the provider's JWKS, the issuer, the
	// audience and the expiry. Everything below trusts the claims only because
	// this line succeeded.
	verified, err := o.verifier.Verify(ctx, raw)
	if err != nil {
		return nil, fmt.Errorf("auth: verifying the id token from %s: %w", o.provider.Name, err)
	}

	// The nonce binds this token to the request that asked for it. Without the
	// check, a token captured from another ceremony verifies perfectly.
	if verified.Nonce != nonce {
		return nil, ErrNonceMismatch
	}

	var claims struct {
		Subject       string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified any    `json:"email_verified"`
		Name          string `json:"name"`
		Picture       string `json:"picture"`
	}
	if err := verified.Claims(&claims); err != nil {
		return nil, fmt.Errorf("auth: reading id token claims: %w", err)
	}
	if claims.Subject == "" {
		return nil, errors.New("auth: the id token has no subject")
	}

	return &Identity{
		Provider:      o.provider.Name,
		Subject:       claims.Subject,
		Email:         strings.ToLower(strings.TrimSpace(claims.Email)),
		EmailVerified: truthy(claims.EmailVerified),
		Name:          claims.Name,
		AvatarURL:     claims.Picture,
	}, nil
}

// identityFromUserInfo reads the identity from a non-OIDC provider's API.
func (o *OAuth) identityFromUserInfo(ctx context.Context, token *oauth2.Token) (*Identity, error) {
	request, err := http.NewRequestWithContext(ctx, "GET", o.provider.UserInfoURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+token.AccessToken)
	request.Header.Set("Accept", "application/vnd.github+json")

	response, err := o.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("auth: reading the profile from %s: %w", o.provider.Name, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("auth: %s returned %s for the profile", o.provider.Name, response.Status)
	}

	var profile struct {
		ID        json.Number `json:"id"`
		NodeID    string      `json:"node_id"`
		Login     string      `json:"login"`
		Name      string      `json:"name"`
		Email     string      `json:"email"`
		AvatarURL string      `json:"avatar_url"`
	}
	if err := json.NewDecoder(response.Body).Decode(&profile); err != nil {
		return nil, fmt.Errorf("auth: decoding the profile from %s: %w", o.provider.Name, err)
	}

	subject := profile.ID.String()
	if subject == "" {
		subject = profile.NodeID
	}
	if subject == "" {
		return nil, fmt.Errorf("auth: %s returned a profile with no id", o.provider.Name)
	}

	identity := &Identity{
		Provider: o.provider.Name,
		Subject:  subject,
		Email:    strings.ToLower(strings.TrimSpace(profile.Email)),
		// The profile endpoint returns the *public* address, which its owner
		// typed into a form and nobody checked. Treating it as verified would
		// be treating a self-declared string as proof of ownership, which is
		// the account-takeover path this package exists to refuse.
		EmailVerified: false,
		Name:          cmpFirst(profile.Name, profile.Login),
		AvatarURL:     profile.AvatarURL,
	}

	// Most GitHub users keep their address private, so the profile has none.
	// The addresses endpoint has them, along with which one is primary and
	// which GitHub has verified -- and that verification is the provider's own
	// assertion, unlike the public field above.
	if identity.Email == "" && o.provider.EmailsURL != "" {
		address, verified, err := o.primaryEmail(ctx, token)
		if err != nil {
			// Not fatal. The identity is complete without it, and a caller
			// that needs an address can say so more usefully than this can.
			return identity, nil
		}
		identity.Email, identity.EmailVerified = address, verified
	}

	return identity, nil
}

// primaryEmail reads the account's primary address from a GitHub-shaped
// addresses endpoint.
func (o *OAuth) primaryEmail(ctx context.Context, token *oauth2.Token) (string, bool, error) {
	request, err := http.NewRequestWithContext(ctx, "GET", o.provider.EmailsURL, nil)
	if err != nil {
		return "", false, err
	}
	request.Header.Set("Authorization", "Bearer "+token.AccessToken)
	request.Header.Set("Accept", "application/vnd.github+json")

	response, err := o.client.Do(request)
	if err != nil {
		return "", false, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("auth: %s returned %s for the addresses", o.provider.Name, response.Status)
	}

	var addresses []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := json.NewDecoder(response.Body).Decode(&addresses); err != nil {
		return "", false, err
	}

	// The primary one, and only if it is verified. An unverified address is
	// not evidence of anything, and taking a non-primary one would mean
	// guessing which of somebody's addresses they meant.
	for _, address := range addresses {
		if address.Primary && address.Verified {
			return strings.ToLower(strings.TrimSpace(address.Email)), true, nil
		}
	}

	return "", false, errors.New("auth: no verified primary address")
}

// randomToken returns 256 bits, base64url.
func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// truthy reads email_verified, which arrives as a bool from most providers and
// as the string "true" from some.
func truthy(v any) bool {
	switch value := v.(type) {
	case bool:
		return value
	case string:
		return value == "true"
	default:
		return false
	}
}

func cmpFirst(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
