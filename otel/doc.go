// Package otel provides OpenTelemetry integration for Tjo applications.
//
// # Overview
//
// This package enables distributed tracing, metrics collection, and log correlation
// using the OpenTelemetry standard. It integrates seamlessly with Tjo's existing
// logging and metrics infrastructure.
//
// # Quick Start
//
//	// Create provider
//	provider, err := otel.New(otel.Config{
//	    ServiceName: "my-app",
//	    Endpoint:    "localhost:4317",
//	    Insecure:    true, // For local development
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer provider.Shutdown(context.Background())
//
//	// Add middleware to your router
//	router.Use(provider.Middleware())
//
// # Tracing HTTP Requests
//
// The middleware automatically creates spans for all HTTP requests:
//
//	router.Use(provider.Middleware())
//	// All requests now have:
//	// - Automatic span creation
//	// - Request attributes (method, URL, status, etc.)
//	// - Context propagation
//	// - X-Trace-ID response header
//
// # Custom Spans
//
// Create custom spans for operations within your handlers:
//
//	func (h *Handler) ProcessOrder(w http.ResponseWriter, r *http.Request) {
//	    ctx, span := otel.Start(r.Context(), "process_order")
//	    defer span.End()
//
//	    // Add attributes
//	    otel.SetAttributes(ctx,
//	        otel.String("order.id", orderID),
//	        otel.Int("order.items", len(items)),
//	    )
//
//	    // Add events
//	    otel.AddEvent(ctx, "validation_complete")
//
//	    // Record errors
//	    if err != nil {
//	        otel.RecordError(ctx, err)
//	    }
//	}
//
// # Database Tracing
//
// Wrap your database connection for automatic query tracing:
//
//	tracedDB := otel.WrapDB(db, "postgres", "myapp")
//	rows, err := tracedDB.Query(ctx, "SELECT * FROM users WHERE id = ?", userID)
//
// # Log Correlation
//
// Add trace context to your logs:
//
//	logger := otel.LoggerWithTrace(ctx, g.Logging.Logger)
//	logger.Info("Processing request", nil)
//	// Logs include trace_id and span_id fields
//
// Or add trace fields manually:
//
//	g.Logging.Logger.Info("Processing", otel.TraceFields(ctx))
//
// # Exporters
//
// The package supports three exporters:
//
//   - OTLP (recommended): Works with any OpenTelemetry Collector, Jaeger, etc.
//   - Zipkin: Direct export to Zipkin
//   - None: Disables export (useful for testing)
//
// Example with OTLP:
//
//	provider, _ := otel.New(otel.Config{
//	    ServiceName: "my-app",
//	    Exporter:    otel.ExporterOTLP,
//	    Endpoint:    "localhost:4317",
//	})
//
// # Sampling
//
// Control trace sampling to reduce overhead:
//
//	// Sample all traces (default, good for development)
//	Sampler: otel.SamplerAlways
//
//	// Sample 10% of traces (good for high-traffic production)
//	Sampler: otel.SamplerRatio
//	SampleRatio: 0.1
//
//	// Respect parent span's sampling decision
//	Sampler: otel.SamplerParentBased
//
// # Environment Configuration
//
// Set these environment variables for configuration:
//
//	OTEL_ENABLED=true
//	OTEL_SERVICE_NAME=my-app
//	OTEL_SERVICE_VERSION=1.0.0
//	OTEL_EXPORTER=otlp
//	OTEL_ENDPOINT=localhost:4317
//	OTEL_INSECURE=true
//	OTEL_SAMPLER=ratio
//	OTEL_SAMPLE_RATIO=0.1
//
// # Local Development with Jaeger
//
// Run Jaeger locally with Docker:
//
//	docker run -d --name jaeger \
//	  -p 16686:16686 \
//	  -p 4317:4317 \
//	  jaegertracing/all-in-one:latest
//
// Then view traces at http://localhost:16686
package otel
