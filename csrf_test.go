package tjo

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/jimmitjoo/tjo/config"
)

// csrfApp returns a router with the real middleware chain, so these exercise
// what a request actually traverses rather than the middleware in isolation.
// The v0.8.0 rate-limit bypass survived precisely because it was verified at
// the function rather than through the router.
func csrfApp(t *testing.T) (*chi.Mux, *scs.SessionManager) {
	t.Helper()

	session := scs.New()
	g := &Tjo{
		HTTP:   &HTTPService{Router: chi.NewRouter(), Session: session},
		Config: &config.Config{},
	}

	mux, err := g.routes()
	if err != nil {
		t.Fatal(err)
	}
	mux.Post("/submit", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.Get("/form", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	return mux, session
}

// get issues a GET and returns the token the server minted plus the cookies it
// set, which is how a browser obtains one.
func get(t *testing.T, mux *chi.Mux) (string, []*http.Cookie) {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/form", nil)
	req.Host = "example.test"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	return rec.Header().Get("X-CSRF-Token"), rec.Result().Cookies()
}

func TestCSRFAcceptsAValidTokenAndRejectsEverythingElse(t *testing.T) {
	mux, _ := csrfApp(t)

	token, cookies := get(t, mux)
	if token == "" {
		t.Fatal("no token was issued; AJAX clients would have no way to obtain one")
	}

	post := func(field, header string) *httptest.ResponseRecorder {
		body := url.Values{}
		if field != "" {
			body.Set("csrf_token", field)
		}

		req := httptest.NewRequest(http.MethodPost, "/submit", strings.NewReader(body.Encode()))
		req.Host = "example.test"
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		if header != "" {
			req.Header.Set("X-CSRF-Token", header)
		}
		for _, c := range cookies {
			req.AddCookie(c)
		}

		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	t.Run("valid token in a form field", func(t *testing.T) {
		if got := post(token, "").Code; got != http.StatusOK {
			t.Errorf("status = %d, want 200", got)
		}
	})

	t.Run("valid token in a header", func(t *testing.T) {
		if got := post("", token).Code; got != http.StatusOK {
			t.Errorf("status = %d, want 200", got)
		}
	})

	t.Run("no token", func(t *testing.T) {
		if got := post("", "").Code; got != http.StatusForbidden {
			t.Errorf("status = %d, want 403", got)
		}
	})

	t.Run("forged token", func(t *testing.T) {
		if got := post("not-the-token", "").Code; got != http.StatusForbidden {
			t.Errorf("status = %d, want 403", got)
		}
	})

	// A token from a different session must not work here. This is what makes
	// it a CSRF token rather than a constant.
	t.Run("token from another session", func(t *testing.T) {
		otherMux, _ := csrfApp(t)
		otherToken, _ := get(t, otherMux)

		if otherToken == token {
			t.Fatal("two sessions were issued the same token")
		}
		if got := post(otherToken, "").Code; got != http.StatusForbidden {
			t.Errorf("status = %d, want 403", got)
		}
	})
}

// GET must never be checked; issue #18 was a CSRF layer 403ing ordinary traffic
// behind a standard nginx config.
func TestCSRFDoesNotCheckSafeMethods(t *testing.T) {
	mux, _ := csrfApp(t)

	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		req := httptest.NewRequest(method, "/form", nil)
		req.Host = "example.test"
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code == http.StatusForbidden {
			t.Errorf("%s was rejected by the CSRF check", method)
		}
	}
}

// Bearer-token API clients carry no ambient credential for an attacker to ride,
// so a CSRF token adds nothing and would break every non-browser client.
func TestCSRFExemptsAPIRoutes(t *testing.T) {
	mux, _ := csrfApp(t)
	mux.Post("/api/v1/things", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/things", nil)
	req.Host = "example.test"
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code == http.StatusForbidden {
		t.Error("an /api/ route was rejected for want of a CSRF token")
	}
}

// A token that survived session renewal would outlive the anonymous session it
// was minted for. `tjo make auth` renews on login and after 2FA.
func TestRotateCSRFTokenIssuesANewOne(t *testing.T) {
	session := scs.New()

	var before, after string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		before = CSRFToken(session, r)
		RotateCSRFToken(session, r)
		after = CSRFToken(session, r)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	session.LoadAndSave(h).ServeHTTP(httptest.NewRecorder(), req)

	if before == "" || after == "" {
		t.Fatalf("tokens were not issued: %q %q", before, after)
	}
	if before == after {
		t.Error("the token survived rotation")
	}
}

// The token is stable within a session, or every page load would invalidate the
// form on the previous one.
func TestCSRFTokenIsStableWithinASession(t *testing.T) {
	session := scs.New()

	var first, second string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		first = CSRFToken(session, r)
		second = CSRFToken(session, r)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	session.LoadAndSave(h).ServeHTTP(httptest.NewRecorder(), req)

	if first != second || first == "" {
		t.Errorf("token is not stable within a request: %q then %q", first, second)
	}
}
