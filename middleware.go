package tjo

import (
	"net/http"
	"os"
	"strings"

	"github.com/justinas/nosurf"
)

func (g *Tjo) SessionLoad(next http.Handler) http.Handler {
	if g.Logging != nil && g.Logging.Info != nil {
		g.Logging.Info.Println("SessionLoad called")
	}
	return g.HTTP.Session.LoadAndSave(next)
}

// CrossOriginProtection rejects non-safe cross-origin browser requests using
// Sec-Fetch-Site, falling back to comparing Origin against Host.
//
// This sits in front of NoSurf rather than replacing it, and the distinction
// matters. Token CSRF only protects a form the application remembered to put a
// token in; this protects every state-changing request whether the template
// author got it right or not. Conversely this deliberately allows requests
// carrying neither Sec-Fetch-Site nor Origin, because they are either
// same-origin or not from a browser at all -- so it is not a replacement for
// tokens either. The two cover different halves.
//
// Trusted origins come from CORS_ALLOWED_ORIGINS, the variable the security
// package already documents for the same purpose, rather than inventing a
// second list that can disagree with the first.
func (g *Tjo) CrossOriginProtection(next http.Handler) http.Handler {
	protection := http.NewCrossOriginProtection()

	for _, origin := range strings.Split(os.Getenv("CORS_ALLOWED_ORIGINS"), ",") {
		origin = strings.TrimSpace(origin)
		if origin == "" {
			continue
		}
		if err := protection.AddTrustedOrigin(origin); err != nil && g.Logging != nil && g.Logging.Error != nil {
			// A malformed entry is a configuration error, not a reason to
			// serve without the origin allowed. Say so rather than swallowing
			// it: CORS_ALLOWED_ORIGINS being silently ignored was issue #16.
			g.Logging.Error.Printf("CORS_ALLOWED_ORIGINS: ignoring %q: %v", origin, err)
		}
	}

	protection.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("Accept"), "application/json") ||
			r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"cross-origin request rejected","code":"CSRF_ERROR"}`))
			return
		}
		http.Error(w, "cross-origin request rejected", http.StatusForbidden)
	}))

	return protection.Handler(next)
}

func (g *Tjo) NoSurf(next http.Handler) http.Handler {
	csrfHandler := nosurf.New(next)

	// Exempt API from CSRF protection:
	csrfHandler.ExemptRegexp("^/api/")

	csrfHandler.SetBaseCookie(http.Cookie{
		HttpOnly: true,
		Path:     "/",
		Secure:   g.Config.Cookie.Secure,
		SameSite: http.SameSiteStrictMode,
		Domain:   g.Config.Cookie.Domain,
	})

	return csrfHandler
}
