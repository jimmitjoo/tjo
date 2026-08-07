package auth

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

// A test OpenID provider.
//
// The alternative is mocking the verifier, which would test everything except
// the part that matters. This serves real discovery, a real JWKS and really
// signed tokens, so the tests below exercise signature verification, issuer and
// audience checks and key selection -- and can therefore also serve a *wrong*
// token and prove it is rejected.
type testIssuer struct {
	server *httptest.Server
	key    *rsa.PrivateKey
	keyID  string

	// codes maps an authorization code to the claims its token will carry.
	codes map[string]map[string]any

	// verifiers maps a code to the PKCE challenge it was issued against, so
	// the token endpoint can check the verifier the way a real one does.
	verifiers map[string]string

	// lastForm is what the client last posted to /token.
	lastForm url.Values
}

func newTestIssuer(t *testing.T) *testIssuer {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	issuer := &testIssuer{
		key:       key,
		keyID:     "test-key",
		codes:     map[string]map[string]any{},
		verifiers: map[string]string{},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"issuer":                                issuer.URL(),
			"authorization_endpoint":                issuer.URL() + "/authorize",
			"token_endpoint":                        issuer.URL() + "/token",
			"jwks_uri":                              issuer.URL() + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"keys": []any{issuer.jwk()}})
	})
	mux.HandleFunc("/token", issuer.token)

	issuer.server = httptest.NewServer(mux)
	t.Cleanup(issuer.server.Close)

	return issuer
}

func (i *testIssuer) URL() string {
	if i.server == nil {
		// Discovery is served before the server exists only in the closure
		// above, which runs after Start; this is unreachable in practice.
		return ""
	}
	return i.server.URL
}

func (i *testIssuer) client() *http.Client { return i.server.Client() }

// issue registers an authorization code and the claims its ID token will carry.
func (i *testIssuer) issue(code string, claims map[string]any) {
	i.codes[code] = claims
}

// bindPKCE records the challenge a code was issued against.
func (i *testIssuer) bindPKCE(code, verifier string) {
	sum := sha256.Sum256([]byte(verifier))
	i.verifiers[code] = base64.RawURLEncoding.EncodeToString(sum[:])
}

func (i *testIssuer) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	i.lastForm = r.Form

	code := r.Form.Get("code")
	claims, ok := i.codes[code]
	if !ok {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant"})
		return
	}

	if want, bound := i.verifiers[code]; bound {
		sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if base64.RawURLEncoding.EncodeToString(sum[:]) != want {
			writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant"})
			return
		}
	}

	writeJSON(w, map[string]any{
		"access_token": "at-" + code,
		"token_type":   "Bearer",
		"expires_in":   3600,
		"id_token":     i.sign(claims),
	})
}

// sign mints an RS256 JWT, filling in iss, aud, iat and exp when the caller has
// not overridden them.
func (i *testIssuer) sign(claims map[string]any) string {
	full := map[string]any{
		"iss": i.URL(),
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(time.Hour).Unix(),
	}
	for k, v := range claims {
		full[k] = v
	}

	header, _ := json.Marshal(map[string]any{"alg": "RS256", "typ": "JWT", "kid": i.keyID})
	payload, _ := json.Marshal(full)

	signingInput := base64.RawURLEncoding.EncodeToString(header) + "." +
		base64.RawURLEncoding.EncodeToString(payload)

	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, i.key, crypto.SHA256, digest[:])
	if err != nil {
		panic(err)
	}

	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)
}

// jwk is the public half, as the JWKS endpoint serves it.
func (i *testIssuer) jwk() map[string]any {
	public := i.key.Public().(*rsa.PublicKey)
	return map[string]any{
		"kty": "RSA",
		"alg": "RS256",
		"use": "sig",
		"kid": i.keyID,
		"n":   base64.RawURLEncoding.EncodeToString(public.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(bigEndian(public.E)),
	}
}

// bigEndian encodes the exponent, which is 65537 in every key this generates
// but is encoded properly anyway.
func bigEndian(n int) []byte {
	var out []byte
	for n > 0 {
		out = append([]byte{byte(n & 0xff)}, out...)
		n >>= 8
	}
	return out
}

func writeJSON(w http.ResponseWriter, v any) { writeJSONStatus(w, http.StatusOK, v) }

func writeJSONStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// callback builds the request the browser would make, carrying a code and the
// state from a ceremony.
func callback(t *testing.T, ceremony *OAuthCeremony, code string) *http.Request {
	t.Helper()

	authorize, err := url.Parse(ceremony.URL)
	if err != nil {
		t.Fatal(err)
	}

	return httptest.NewRequest("GET", "/callback?"+url.Values{
		"code":  {code},
		"state": {authorize.Query().Get("state")},
	}.Encode(), nil)
}

// nonceFrom reads the nonce a ceremony sent, so a test can mint a token that
// matches -- or deliberately one that does not.
func nonceFrom(t *testing.T, ceremony *OAuthCeremony) string {
	t.Helper()

	authorize, err := url.Parse(ceremony.URL)
	if err != nil {
		t.Fatal(err)
	}
	return authorize.Query().Get("nonce")
}
