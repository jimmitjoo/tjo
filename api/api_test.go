package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestNew(t *testing.T) {
	// Test with nil config (should use defaults)
	api := New(nil)
	if api.Config == nil {
		t.Fatal("New() with nil config should create default config")
	}
	if api.Config.Version != "v1" {
		t.Errorf("New() default version = %v, want v1", api.Config.Version)
	}
	if api.Router == nil {
		t.Fatal("New() should create router")
	}
	if api.VersionRouter == nil {
		t.Fatal("New() should create version router")
	}

	// Test with custom config
	config := &APIConfig{
		Version:         "v2",
		RateLimitPerMin: 100,
		EnableCORS:      false,
		Debug:           true,
	}
	api = New(config)
	if api.Config.Version != "v2" {
		t.Errorf("New() custom version = %v, want v2", api.Config.Version)
	}
	if api.RateLimiter == nil {
		t.Error("New() should create rate limiter when RateLimitPerMin > 0")
	}
}

func TestAPIUse(t *testing.T) {
	api := New(nil)
	
	// Add custom middleware
	called := false
	customMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			next.ServeHTTP(w, r)
		})
	}
	
	api.Use(customMiddleware)
	
	// Test that middleware is applied
	api.Router.Get("/test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test", nil)
	
	api.ServeHTTP(w, r)
	
	if !called {
		t.Error("Use() middleware was not called")
	}
}

func TestAPIGroup(t *testing.T) {
	api := New(nil)
	
	// Create a group with middleware
	groupCalled := false
	groupMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			groupCalled = true
			next.ServeHTTP(w, r)
		})
	}
	
	group := api.Group("/api", groupMiddleware)
	
	// Add route to group
	group.Get("/users", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("users"))
	})
	
	// Test group route
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/users", nil)
	
	api.ServeHTTP(w, r)
	
	if !groupCalled {
		t.Error("Group middleware was not called")
	}
	
	if body := w.Body.String(); body != "users" {
		t.Errorf("Group route response = %v, want users", body)
	}
}

func TestAPIMount(t *testing.T) {
	api := New(nil)
	
	// Create a sub-router
	subRouter := chi.NewRouter()
	subRouter.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("mounted"))
	})
	
	api.Mount("/sub", subRouter)
	
	// Test mounted route
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/sub/", nil)
	
	api.ServeHTTP(w, r)
	
	if w.Code != http.StatusOK {
		t.Errorf("Mount() status = %v, want %v", w.Code, http.StatusOK)
	}
	
	if body := w.Body.String(); body != "mounted" {
		t.Errorf("Mount() response = %v, want mounted", body)
	}
}

func TestRouterMethods(t *testing.T) {
	tests := []struct {
		method string
		setup  func(router *Router)
	}{
		{
			method: "GET",
			setup: func(router *Router) {
				router.Get("/test", func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte("GET"))
				})
			},
		},
		{
			method: "POST",
			setup: func(router *Router) {
				router.Post("/test", func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte("POST"))
				})
			},
		},
		{
			method: "PUT",
			setup: func(router *Router) {
				router.Put("/test", func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte("PUT"))
				})
			},
		},
		{
			method: "PATCH",
			setup: func(router *Router) {
				router.Patch("/test", func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte("PATCH"))
				})
			},
		},
		{
			method: "DELETE",
			setup: func(router *Router) {
				router.Delete("/test", func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte("DELETE"))
				})
			},
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			// Create fresh API for each test
			api := New(nil)
			router := api.Group("/api")
			
			tt.setup(router)
			
			w := httptest.NewRecorder()
			r := httptest.NewRequest(tt.method, "/api/test", nil)
			if tt.method == "POST" || tt.method == "PUT" || tt.method == "PATCH" {
				r.Header.Set("Content-Type", "application/json")
			}
			
			api.ServeHTTP(w, r)
			
			if w.Code != http.StatusOK {
				t.Errorf("%s status = %v, want %v", tt.method, w.Code, http.StatusOK)
			}
			
			if body := w.Body.String(); body != tt.method {
				t.Errorf("%s response = %v, want %v", tt.method, body, tt.method)
			}
		})
	}
}

type testResourceController struct{}

func (c *testResourceController) List(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("list"))
}

func (c *testResourceController) Create(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("create"))
}

func (c *testResourceController) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	w.Write([]byte("get:" + id))
}

func (c *testResourceController) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	w.Write([]byte("update:" + id))
}

func (c *testResourceController) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	w.Write([]byte("delete:" + id))
}

func TestRouterResource(t *testing.T) {
	api := New(nil)
	router := api.Group("/api")
	
	controller := &testResourceController{}
	router.Resource("/users", controller)
	
	tests := []struct {
		method string
		path   string
		want   string
	}{
		{"GET", "/api/users", "list"},
		{"POST", "/api/users", "create"},
		{"GET", "/api/users/123", "get:123"},
		{"PUT", "/api/users/123", "update:123"},
		{"DELETE", "/api/users/123", "delete:123"},
	}
	
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(tt.method, tt.path, nil)
			if tt.method == "POST" || tt.method == "PUT" {
				r.Header.Set("Content-Type", "application/json")
			}
			
			api.ServeHTTP(w, r)
			
			if w.Code != http.StatusOK {
				t.Errorf("Resource %s %s status = %v, want %v", tt.method, tt.path, w.Code, http.StatusOK)
			}
			
			if body := w.Body.String(); body != tt.want {
				t.Errorf("Resource %s %s response = %v, want %v", tt.method, tt.path, body, tt.want)
			}
		})
	}
}

func TestHealth(t *testing.T) {
	handler := Health()
	
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/health", nil)
	
	handler(w, r)
	
	if w.Code != http.StatusOK {
		t.Errorf("Health() status = %v, want %v", w.Code, http.StatusOK)
	}
	
	body := w.Body.String()
	if !strings.Contains(body, `"status":"healthy"`) {
		t.Error("Health() should return healthy status")
	}
	if !strings.Contains(body, `"timestamp":`) {
		t.Error("Health() should return timestamp")
	}
}

func TestNotFoundHandler(t *testing.T) {
	handler := NotFoundHandler()
	
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/not-found", nil)
	
	handler(w, r)
	
	if w.Code != http.StatusNotFound {
		t.Errorf("NotFoundHandler() status = %v, want %v", w.Code, http.StatusNotFound)
	}
	
	body := w.Body.String()
	if !strings.Contains(body, "NOT_FOUND") {
		t.Error("NotFoundHandler() should return NOT_FOUND error code")
	}
}

func TestMethodNotAllowedHandler(t *testing.T) {
	handler := MethodNotAllowedHandler()
	
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/", nil)
	
	handler(w, r)
	
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("MethodNotAllowedHandler() status = %v, want %v", w.Code, http.StatusMethodNotAllowed)
	}
	
	body := w.Body.String()
	if !strings.Contains(body, "METHOD_NOT_ALLOWED") {
		t.Error("MethodNotAllowedHandler() should return METHOD_NOT_ALLOWED error code")
	}
}

func TestSetupRoutes(t *testing.T) {
	api := New(&APIConfig{Version: "v2"})
	api.SetupRoutes()
	
	tests := []struct {
		path       string
		wantStatus int
		contains   string
	}{
		{
			path:       "/health",
			wantStatus: http.StatusOK,
			contains:   "healthy",
		},
		{
			path:       "/api/health",
			wantStatus: http.StatusOK,
			contains:   "healthy",
		},
		{
			path:       "/api",
			wantStatus: http.StatusOK,
			contains:   "Tjo API",
		},
		{
			path:       "/not-exists",
			wantStatus: http.StatusNotFound,
			contains:   "NOT_FOUND",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", tt.path, nil)
			
			api.ServeHTTP(w, r)
			
			if w.Code != tt.wantStatus {
				t.Errorf("SetupRoutes() %s status = %v, want %v", tt.path, w.Code, tt.wantStatus)
			}
			
			if body := w.Body.String(); !strings.Contains(body, tt.contains) {
				t.Errorf("SetupRoutes() %s should contain %v", tt.path, tt.contains)
			}
		})
	}
	
	// Test API info has correct version
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api", nil)
	api.ServeHTTP(w, r)
	
	body := w.Body.String()
	if !strings.Contains(body, `"version":"v2"`) {
		t.Error("SetupRoutes() /api should return configured version")
	}
}

func TestParam(t *testing.T) {
	api := New(nil)
	
	api.Router.Get("/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := Param(r, "id")
		w.Write([]byte(id))
	})
	
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/users/123", nil)
	
	api.ServeHTTP(w, r)
	
	if body := w.Body.String(); body != "123" {
		t.Errorf("Param() = %v, want 123", body)
	}
}

func TestQuery(t *testing.T) {
	r := httptest.NewRequest("GET", "/?key=value&other=test", nil)
	
	if got := Query(r, "key"); got != "value" {
		t.Errorf("Query(key) = %v, want value", got)
	}
	
	if got := Query(r, "other"); got != "test" {
		t.Errorf("Query(other) = %v, want test", got)
	}
	
	if got := Query(r, "missing"); got != "" {
		t.Errorf("Query(missing) = %v, want empty", got)
	}
}

func TestQueryInt(t *testing.T) {
	r := httptest.NewRequest("GET", "/?num=42&invalid=abc", nil)
	
	if got := QueryInt(r, "num", 0); got != 42 {
		t.Errorf("QueryInt(num) = %v, want 42", got)
	}
	
	if got := QueryInt(r, "invalid", 10); got != 10 {
		t.Errorf("QueryInt(invalid) = %v, want default 10", got)
	}
	
	if got := QueryInt(r, "missing", 5); got != 5 {
		t.Errorf("QueryInt(missing) = %v, want default 5", got)
	}
}

func TestQueryBool(t *testing.T) {
	r := httptest.NewRequest("GET", "/?t1=true&t2=1&t3=yes&f1=false&f2=0&f3=no", nil)
	
	tests := []struct {
		key          string
		defaultValue bool
		want         bool
	}{
		{"t1", false, true},
		{"t2", false, true},
		{"t3", false, true},
		{"f1", true, false},
		{"f2", true, false},
		{"f3", true, false},
		{"missing", true, true},
		{"missing", false, false},
	}
	
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			if got := QueryBool(r, tt.key, tt.defaultValue); got != tt.want {
				t.Errorf("QueryBool(%v, %v) = %v, want %v", tt.key, tt.defaultValue, got, tt.want)
			}
		})
	}
}

func TestRouterRoute(t *testing.T) {
	api := New(nil)
	router := api.Group("/api")
	
	router.Route("/v1", func(r chi.Router) {
		r.Get("/info", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("v1 info"))
		})
		r.Post("/data", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("v1 data"))
		})
	})
	
	tests := []struct {
		method string
		path   string
		want   string
	}{
		{"GET", "/api/v1/info", "v1 info"},
		{"POST", "/api/v1/data", "v1 data"},
	}
	
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(tt.method, tt.path, nil)
			if tt.method == "POST" {
				r.Header.Set("Content-Type", "application/json")
			}
			
			api.ServeHTTP(w, r)
			
			if w.Code != http.StatusOK {
				t.Errorf("Route() %s %s status = %v, want %v", tt.method, tt.path, w.Code, http.StatusOK)
			}
			
			if body := w.Body.String(); body != tt.want {
				t.Errorf("Route() %s %s response = %v, want %v", tt.method, tt.path, body, tt.want)
			}
		})
	}
}