package tjo

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jimmitjoo/tjo/logging"
)

func (g *Tjo) routes() http.Handler {
	mux := chi.NewRouter()
	mux.Use(middleware.RequestID)
	mux.Use(middleware.RealIP)

	// Add OpenTelemetry tracing middleware if enabled
	if g.Logging != nil && g.Logging.OTel != nil && g.Logging.OTel.IsEnabled() {
		mux.Use(g.Logging.OTel.Middleware())
	}

	// Add structured logging middleware if available
	if g.Logging != nil && g.Logging.Logger != nil {
		mux.Use(logging.StructuredLoggingMiddleware(g.Logging.Logger))
		mux.Use(logging.RecoveryMiddleware(g.Logging.Logger))

		// Add metrics middleware if metrics are available
		if g.Logging.App != nil {
			mux.Use(logging.MetricsMiddleware(g.Logging.App, g.Logging.Logger))
		}
	}

	if g.Debug {
		mux.Use(middleware.Logger)
	}

	mux.Use(middleware.Recoverer)

	// Only add session and CSRF middleware if HTTP service is configured
	if g.HTTP != nil && g.HTTP.Session != nil {
		mux.Use(g.SessionLoad)
		mux.Use(g.NoSurf)
	}

	return mux
}

// AddMonitoringRoutes adds health and metrics endpoints.
// Call this in your routes() function AFTER adding your middleware.
func (g *Tjo) AddMonitoringRoutes(mux *chi.Mux) {
	if g.Logging == nil || g.Logging.Metrics == nil || g.Logging.Health == nil {
		return
	}

	// Health endpoints
	mux.Get("/health", logging.HealthHandler(g.Logging.Health))
	mux.Get("/health/ready", logging.ReadinessHandler())
	mux.Get("/health/live", logging.LivenessHandler())

	// Metrics endpoint
	mux.Get("/metrics", logging.MetricsHandler(g.Logging.Metrics))
}
