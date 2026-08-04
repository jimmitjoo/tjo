package tjo

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/alexedwards/scs/v2"
)

// Session-backed CSRF tokens, replacing nosurf.
//
// nosurf is not abandoned but it is thin: its design predates Sec-Fetch-Site
// entirely, and v1.1.1 shipped a same-origin check that compared r.URL.Scheme,
// which is empty on a server-side request, so it never ran at all
// (CVE-2025-46721). v0.8.0 upgraded rather than replaced it, because writing new
// token code during a security release -- in the area that had already produced
// three advisories -- was the wrong trade at the time.
//
// The replacement is small because the session is already on every request that
// has a token. A token stored in the session and compared against the submitted
// one needs no masking: the double-submit-cookie pattern's complexity exists to
// defend a token the client can read and modify, which is not the situation
// here.
//
// This sits behind CrossOriginProtection, which is the primary gate. The two
// cover different halves and neither subsumes the other -- see middleware.go.

const (
	csrfSessionKey = "_csrf_token"
	csrfFormField  = "csrf_token"
	csrfHeader     = "X-CSRF-Token"
	csrfTokenBytes = 32
)

// csrfSafeMethods never carry state changes, so they are never checked. This
// is the same list every implementation uses, and the reason applications must
// not perform state changes on GET.
var csrfSafeMethods = map[string]bool{
	http.MethodGet:     true,
	http.MethodHead:    true,
	http.MethodOptions: true,
	http.MethodTrace:   true,
}

// CSRFToken returns the token for this request's session, creating one if
// needed. Templates render it; AJAX clients read it from the response header.
func CSRFToken(session *scs.SessionManager, r *http.Request) string {
	if session == nil {
		return ""
	}

	if tok := session.GetString(r.Context(), csrfSessionKey); tok != "" {
		return tok
	}

	buf := make([]byte, csrfTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand does not fail on any supported platform; returning an
		// empty token here would silently disable protection, so this refuses
		// to produce one at all and the check below rejects the request.
		return ""
	}

	tok := base64.RawURLEncoding.EncodeToString(buf)
	session.Put(r.Context(), csrfSessionKey, tok)
	return tok
}

// RotateCSRFToken issues a fresh token, discarding the previous one.
//
// Call it whenever the session's identity changes. `tjo make auth` renews the
// session ID on login and after 2FA verification; a token that survived that
// rotation would outlive the anonymous session it was minted for.
func RotateCSRFToken(session *scs.SessionManager, r *http.Request) {
	if session == nil {
		return
	}
	session.Remove(r.Context(), csrfSessionKey)
	CSRFToken(session, r)
}

// CSRF verifies the submitted token against the session's.
//
// It publishes the token as a response header from inside the handler chain
// rather than around it. That distinction was a real bug: reading the token
// from a wrapper outside the CSRF middleware always yielded "", which left AJAX
// and SPA clients with no way to obtain one.
func (g *Tjo) CSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session := g.HTTP.Session

		token := CSRFToken(session, r)
		if token != "" {
			w.Header().Set(csrfHeader, token)
		}

		if csrfSafeMethods[r.Method] || g.csrfExempt(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		submitted := r.Header.Get(csrfHeader)
		if submitted == "" {
			// ParseForm consumes the body. Doing it here means a handler that
			// calls it again sees the parsed values rather than an empty form,
			// which is how the sanitised-output bug in #22 presented.
			_ = r.ParseForm()
			submitted = r.PostFormValue(csrfFormField)
		}

		if token == "" || submitted == "" ||
			subtle.ConstantTimeCompare([]byte(token), []byte(submitted)) != 1 {
			g.csrfFailed(w, r)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// csrfExempt reports whether path is outside CSRF checking.
//
// /api/ is exempt because token CSRF is meaningless for clients that
// authenticate with a bearer token rather than a cookie -- there is no ambient
// credential for an attacker to ride. Those routes are covered by
// CrossOriginProtection and by the API's own auth.
func (g *Tjo) csrfExempt(path string) bool {
	return strings.HasPrefix(path, "/api/")
}

func (g *Tjo) csrfFailed(w http.ResponseWriter, r *http.Request) {
	if g.Logging != nil && g.Logging.Info != nil {
		g.Logging.Info.Printf("CSRF check failed for %s %s", r.Method, r.URL.Path)
	}

	if strings.Contains(r.Header.Get("Accept"), "application/json") ||
		r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"CSRF token mismatch","code":"CSRF_ERROR"}`))
		return
	}

	http.Error(w, "CSRF token mismatch", http.StatusForbidden)
}
