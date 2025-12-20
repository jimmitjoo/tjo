package gemquick

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/jimmitjoo/gemquick/config"
)

func TestRoutes(t *testing.T) {
	g := &Gemquick{
		Routes: chi.NewRouter(),
		Debug:  false,
	}

	// Get routes
	routes := g.routes()

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
	g := &Gemquick{
		Routes: chi.NewRouter(),
		Debug:  true, // Enable debug mode
	}

	routes := g.routes()

	if routes == nil {
		t.Error("Expected routes to be initialized in debug mode")
	}
}

func TestRoutesMiddleware(t *testing.T) {
	g := &Gemquick{
		Routes: chi.NewRouter(),
		Debug:  false,
		Config: &config.Config{
			Cookie: config.CookieConfig{
				Secure: false,
				Domain: "localhost",
			},
		},
		InfoLog: createTestLogger(),
		Session: scs.New(),
	}

	// Get the router with middleware
	router := g.routes().(*chi.Mux)
	
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
	g := &Gemquick{
		Routes:   chi.NewRouter(),
		Debug:    false,
		RootPath: "./",
	}

	routes := g.routes()

	// Test static file route
	req := httptest.NewRequest("GET", "/public/test.css", nil)
	rr := httptest.NewRecorder()

	routes.ServeHTTP(rr, req)

	// Should get 404 if file doesn't exist
	if status := rr.Code; status != http.StatusNotFound {
		t.Errorf("Expected status 404 for non-existent file, got %v", status)
	}
}