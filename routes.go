package gemquick

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jimmitjoo/gemquick/logging"
)

func (g *Gemquick) routes() http.Handler {
	mux := chi.NewRouter()
	mux.Use(middleware.RequestID)
	mux.Use(middleware.RealIP)

	// Add structured logging middleware if available
	if g.Logger != nil {
		mux.Use(logging.StructuredLoggingMiddleware(g.Logger))
		mux.Use(logging.RecoveryMiddleware(g.Logger))
		
		// Add metrics middleware if metrics are available
		if g.AppMetrics != nil {
			mux.Use(logging.MetricsMiddleware(g.AppMetrics, g.Logger))
		}
	}

	if g.Debug {
		mux.Use(middleware.Logger)
	}

	mux.Use(middleware.Recoverer)
	mux.Use(g.SessionLoad)
	mux.Use(g.NoSurf)

	return mux
}

// addMonitoringRoutes adds health and metrics endpoints
func (g *Gemquick) addMonitoringRoutes(mux *chi.Mux) {
	if g.MetricRegistry == nil || g.HealthMonitor == nil {
		return
	}

	// Health endpoints
	mux.Get("/health", logging.HealthHandler(g.HealthMonitor))
	mux.Get("/health/ready", logging.ReadinessHandler())
	mux.Get("/health/live", logging.LivenessHandler())
	
	// Metrics endpoint
	mux.Get("/metrics", logging.MetricsHandler(g.MetricRegistry))
}
