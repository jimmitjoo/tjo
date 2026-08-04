package tjo

import (
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/jimmitjoo/tjo/config"
)

// newRoutableApp returns a Tjo with just enough wired up for routes() to
// succeed: a session manager, which is what SessionLoad and NoSurf need.
func newRoutableApp(debug bool) *Tjo {
	return &Tjo{
		HTTP: &HTTPService{
			Router:  chi.NewRouter(),
			Session: scs.New(),
		},
		Debug: debug,
	}
}

// TestRoutesRequiresASessionManager pins the contract that replaced a silent
// degradation. routes() used to skip SessionLoad and NoSurf when the session
// manager was missing and return a router anyway, which is how every
// application ended up serving requests with no CSRF protection at all --
// GHSA-9m5v-pvgv-cv8j. It refuses now.
func TestRoutesRequiresASessionManager(t *testing.T) {
	t.Run("no HTTP service", func(t *testing.T) {
		g := &Tjo{}
		if _, err := g.routes(); err == nil {
			t.Error("expected an error rather than an unprotected router")
		}
	})

	t.Run("no session manager", func(t *testing.T) {
		g := &Tjo{HTTP: &HTTPService{Router: chi.NewRouter()}}
		if _, err := g.routes(); err == nil {
			t.Error("expected an error rather than a router missing SessionLoad and NoSurf")
		}
	})

	t.Run("session manager present", func(t *testing.T) {
		if _, err := newRoutableApp(false).routes(); err != nil {
			t.Errorf("expected success, got %v", err)
		}
	})
}

func TestRoutes(t *testing.T) {
	g := newRoutableApp(false)

	routes, err := g.routes()
	if err != nil {
		t.Fatal(err)
	}

	// Test that routes is not nil
	if routes == nil {
		t.Error("Expected routes to be initialized")
	}

	// Test middleware is applied
	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()

	routes.ServeHTTP(rr, req)

	// Check that we get a 404 (no routes defined, but middleware should work)
	if status := rr.Code; status != http.StatusNotFound {
		t.Errorf("Expected status 404, got %v", status)
	}
}

func TestRoutesWithDebug(t *testing.T) {
	g := newRoutableApp(true)

	routes, err := g.routes()
	if err != nil {
		t.Fatal(err)
	}

	if routes == nil {
		t.Error("Expected routes to be initialized in debug mode")
	}
}

func TestRoutesMiddleware(t *testing.T) {
	g := &Tjo{
		HTTP: &HTTPService{
			Router:  chi.NewRouter(),
			Session: scs.New(),
		},
		Logging: &LoggingService{
			Info: log.New(os.Stdout, "INFO\t", log.Ldate|log.Ltime),
		},
		Debug: false,
		Config: &config.Config{
			Cookie: config.CookieConfig{
				Secure: false,
				Domain: "localhost",
			},
		},
	}

	// Get the router with middleware
	router, err := g.routes()
	if err != nil {
		t.Fatal(err)
	}

	// Add a test route
	router.Get("/test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test"))
	})

	// Test the route
	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	// Check response
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("Expected status 200, got %v", status)
	}

	if body := rr.Body.String(); body != "test" {
		t.Errorf("Expected body 'test', got %s", body)
	}
}

func TestStaticFileServing(t *testing.T) {
	g := newRoutableApp(false)
	g.RootPath = "./"

	routes, err := g.routes()
	if err != nil {
		t.Fatal(err)
	}

	// Test static file route
	req := httptest.NewRequest("GET", "/public/test.css", nil)
	rr := httptest.NewRecorder()

	routes.ServeHTTP(rr, req)

	// Should get 404 if file doesn't exist
	if status := rr.Code; status != http.StatusNotFound {
		t.Errorf("Expected status 404 for non-existent file, got %v", status)
	}
}

// TestRouterDoesNotTrustForwardedHeadersForRemoteAddr pins the fix for a bypass
// that survived GHSA-hm83-wmj9-52fm.
//
// v0.7.0 corrected IPThrottler.getRealIP so that proxy headers are consulted
// only when the peer is a configured trusted proxy. But routes() mounted chi's
// middleware.RealIP two lines above, and that middleware rewrites
// r.RemoteAddr from X-Forwarded-For unconditionally. getRealIP therefore
// inspected a peer address the attacker had chosen, found it was not a trusted
// proxy, and returned it -- one fresh rate-limit bucket, penalty record and
// blacklist entry per forged header value.
//
// Without the fix this test sees RemoteAddr rewritten to 1.2.3.4.
func TestRouterDoesNotTrustForwardedHeadersForRemoteAddr(t *testing.T) {
	g := newRoutableApp(false)
	g.Config = &config.Config{}

	mux, err := g.routes()
	if err != nil {
		t.Fatal(err)
	}

	const peer = "192.0.2.10:44321"

	var seen string
	mux.Get("/", func(w http.ResponseWriter, r *http.Request) {
		seen = r.RemoteAddr
	})

	for _, header := range []string{"X-Forwarded-For", "X-Real-IP"} {
		t.Run(header, func(t *testing.T) {
			seen = ""

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = peer
			req.Header.Set(header, "1.2.3.4")

			mux.ServeHTTP(httptest.NewRecorder(), req)

			if seen != peer {
				t.Errorf("RemoteAddr = %q, want %q -- a client-supplied %s reached the handler as the peer address", seen, peer, header)
			}
		})
	}
}
