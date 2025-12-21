package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// API represents the main API structure
type API struct {
	Router         *chi.Mux
	Config         *APIConfig
	VersionRouter  *VersionRouter
	RateLimiter    RateLimiter
	middlewares    []func(http.Handler) http.Handler
}

// New creates a new API instance
func New(config *APIConfig) *API {
	if config == nil {
		config = &APIConfig{
			Version:         "v1",
			RateLimitPerMin: 60,
			EnableCORS:      false, // Disabled by default for security
			AllowedOrigins:  nil,   // Must be explicitly configured
			EnableMetrics:   true,
			Debug:           false,
		}
	}
	
	api := &API{
		Router: chi.NewRouter(),
		Config: config,
	}
	
	// Setup version router
	versionConfig := &VersionConfig{
		DefaultVersion:    config.Version,
		SupportedVersions: []string{config.Version},
		VersionHeader:     "X-API-Version",
		VersionInPath:     true,
	}
	api.VersionRouter = NewVersionRouter(versionConfig)
	
	// Setup rate limiter
	if config.RateLimitPerMin > 0 {
		api.RateLimiter = NewTokenBucket(
			config.RateLimitPerMin,
			config.RateLimitPerMin,
			time.Minute,
		)
	}
	
	// Setup default middleware
	api.setupMiddleware()
	
	return api
}

// setupMiddleware configures default middleware
func (api *API) setupMiddleware() {
	// Request ID
	api.Router.Use(middleware.RequestID)
	
	// Real IP
	api.Router.Use(middleware.RealIP)
	
	// Logger
	if api.Config.Debug {
		api.Router.Use(middleware.Logger)
	}
	
	// Recoverer
	api.Router.Use(middleware.Recoverer)
	api.Router.Use(ErrorHandler(api.Config.Debug))
	
	// Security headers
	api.Router.Use(SecureHeaders)
	
	// CORS
	if api.Config.EnableCORS {
		api.Router.Use(CORS(api.Config.AllowedOrigins))
	}
	
	// Request timer
	if api.Config.EnableMetrics {
		api.Router.Use(RequestTimer)
	}
	
	// API Version
	api.Router.Use(APIVersion(api.Config.Version))
	
	// Rate limiting
	if api.RateLimiter != nil {
		api.Router.Use(RateLimitMiddleware(api.RateLimiter, IPKeyFunc))
	}
	
	// Content type checking
	api.Router.Use(ContentTypeJSON)
}

// Use adds middleware to the API
func (api *API) Use(middlewares ...func(http.Handler) http.Handler) {
	api.middlewares = append(api.middlewares, middlewares...)
	api.Router.Use(middlewares...)
}

// Group creates a route group with optional middleware
func (api *API) Group(pattern string, middlewares ...func(http.Handler) http.Handler) *Router {
	return &Router{
		mux:         api.Router,
		pattern:     pattern,
		middlewares: middlewares,
		api:         api,
	}
}

// Mount mounts a handler at the specified pattern
func (api *API) Mount(pattern string, handler http.Handler) {
	api.Router.Mount(pattern, handler)
}

// ServeHTTP implements http.Handler
func (api *API) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	api.Router.ServeHTTP(w, r)
}

// Router represents an API route group
type Router struct {
	mux         *chi.Mux
	pattern     string
	middlewares []func(http.Handler) http.Handler
	api         *API
}

// Route creates a new sub-router
func (r *Router) Route(pattern string, fn func(r chi.Router)) {
	r.mux.Route(r.pattern+pattern, fn)
}

// Get registers a GET route
func (r *Router) Get(pattern string, handler http.HandlerFunc) {
	r.handle("GET", pattern, handler)
}

// Post registers a POST route
func (r *Router) Post(pattern string, handler http.HandlerFunc) {
	r.handle("POST", pattern, handler)
}

// Put registers a PUT route
func (r *Router) Put(pattern string, handler http.HandlerFunc) {
	r.handle("PUT", pattern, handler)
}

// Patch registers a PATCH route
func (r *Router) Patch(pattern string, handler http.HandlerFunc) {
	r.handle("PATCH", pattern, handler)
}

// Delete registers a DELETE route
func (r *Router) Delete(pattern string, handler http.HandlerFunc) {
	r.handle("DELETE", pattern, handler)
}

// handle registers a route with middleware
func (r *Router) handle(method, pattern string, handler http.HandlerFunc) {
	fullPattern := r.pattern + pattern
	
	// Apply group middleware
	h := http.Handler(handler)
	for i := len(r.middlewares) - 1; i >= 0; i-- {
		h = r.middlewares[i](h)
	}
	
	r.mux.Method(method, fullPattern, h)
}

// Resource creates RESTful routes for a resource
func (r *Router) Resource(pattern string, controller ResourceController) {
	r.Route(pattern, func(router chi.Router) {
		router.Get("/", controller.List)       // GET /resources
		router.Post("/", controller.Create)    // POST /resources
		router.Get("/{id}", controller.Get)    // GET /resources/{id}
		router.Put("/{id}", controller.Update) // PUT /resources/{id}
		router.Delete("/{id}", controller.Delete) // DELETE /resources/{id}
	})
}

// ResourceController defines methods for a RESTful resource
type ResourceController interface {
	List(w http.ResponseWriter, r *http.Request)
	Create(w http.ResponseWriter, r *http.Request)
	Get(w http.ResponseWriter, r *http.Request)
	Update(w http.ResponseWriter, r *http.Request)
	Delete(w http.ResponseWriter, r *http.Request)
}

// Health returns a health check handler
func Health() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		JSON(w, http.StatusOK, map[string]interface{}{
			"status": "healthy",
			"timestamp": time.Now().Unix(),
		})
	}
}

// NotFoundHandler returns a 404 handler
func NotFoundHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		NotFound(w, "Endpoint not found")
	}
}

// MethodNotAllowedHandler returns a 405 handler
func MethodNotAllowedHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED",
			"Method not allowed for this endpoint", nil)
	}
}

// SetupRoutes sets up default API routes
func (api *API) SetupRoutes() {
	// Health check
	api.Router.Get("/health", Health())
	api.Router.Get("/api/health", Health())
	
	// API info
	api.Router.Get("/api", func(w http.ResponseWriter, r *http.Request) {
		JSON(w, http.StatusOK, map[string]interface{}{
			"name":    "Tjo API",
			"version": api.Config.Version,
			"docs":    "/api/docs",
		})
	})
	
	// Not found handler
	api.Router.NotFound(NotFoundHandler())
	
	// Method not allowed handler
	api.Router.MethodNotAllowed(MethodNotAllowedHandler())
}

// Param gets a URL parameter value
func Param(r *http.Request, key string) string {
	return chi.URLParam(r, key)
}

// Query gets a query parameter value
func Query(r *http.Request, key string) string {
	return r.URL.Query().Get(key)
}

// QueryInt gets a query parameter as int
func QueryInt(r *http.Request, key string, defaultValue int) int {
	value := Query(r, key)
	if value == "" {
		return defaultValue
	}
	
	if intValue, err := strconv.Atoi(value); err == nil {
		return intValue
	}
	return defaultValue
}

// QueryBool gets a query parameter as bool
func QueryBool(r *http.Request, key string, defaultValue bool) bool {
	value := Query(r, key)
	if value == "" {
		return defaultValue
	}
	
	return value == "true" || value == "1" || value == "yes"
}