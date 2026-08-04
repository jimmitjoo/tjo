package tjo

import (
	"errors"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jimmitjoo/tjo/logging"
)

// routes builds the application router.
//
// It returns an error rather than quietly producing a router without the
// session and CSRF middleware. That silent path is what GHSA-9m5v-pvgv-cv8j
// was: New() called this before assigning g.HTTP.Session, the guard below
// evaluated false, and every application served every request unprotected
// with nothing failing and nothing logged.
//
// "Misconfigured" and "unprotected" are not the same outcome, and a security
// control that removes itself when a dependency is missing has chosen the
// wrong one.
func (g *Tjo) routes() (*chi.Mux, error) {
	if g.HTTP == nil {
		return nil, errors.New("routes: HTTP service is not initialised")
	}
	if g.HTTP.Session == nil {
		return nil, errors.New("routes: session manager is not initialised, so SessionLoad and CSRF cannot be installed")
	}

	mux := chi.NewRouter()
	mux.Use(middleware.RequestID)

	// chi's middleware.RealIP is deliberately NOT installed.
	//
	// It overwrites r.RemoteAddr from X-Forwarded-For or X-Real-IP without
	// establishing that the peer is a proxy entitled to set them. Every
	// downstream consumer of RemoteAddr then reads an attacker-chosen value.
	//
	// That defeats the fix published as GHSA-hm83-wmj9-52fm. IPThrottler.getRealIP
	// consults proxy headers only when the peer is a configured trusted proxy --
	// but with RealIP mounted, the "peer" it inspects is already the forged
	// header, so it returns it unchallenged. The same applies to
	// IPBlacklistMiddleware and to anything a user writes against RemoteAddr.
	//
	// The framework has its own trusted-proxy-aware resolution. A second,
	// naive implementation mounted above it does not add a fallback; it removes
	// the guarantee.

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

	mux.Use(g.SessionLoad)
	mux.Use(g.CrossOriginProtection)
	mux.Use(g.CSRF)

	return mux, nil
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
