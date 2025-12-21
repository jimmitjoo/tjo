// Package otel provides OpenTelemetry integration for distributed tracing,
// metrics, and logging correlation in Tjo applications.
//
// Usage:
//
//	provider, err := otel.New(otel.Config{
//	    ServiceName: "my-app",
//	    Endpoint:    "localhost:4317",
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer provider.Shutdown(context.Background())
//
//	// Use middleware
//	router.Use(provider.Middleware())
package otel

import (
	"errors"
	"strings"
)

// ExporterType defines the telemetry exporter to use.
type ExporterType string

const (
	// ExporterOTLP exports to an OTLP-compatible collector (recommended).
	ExporterOTLP ExporterType = "otlp"
	// ExporterJaeger exports directly to Jaeger.
	ExporterJaeger ExporterType = "jaeger"
	// ExporterZipkin exports directly to Zipkin.
	ExporterZipkin ExporterType = "zipkin"
	// ExporterNone disables exporting (useful for testing).
	ExporterNone ExporterType = "none"
)

// SamplerType defines the sampling strategy.
type SamplerType string

const (
	// SamplerAlways samples all traces.
	SamplerAlways SamplerType = "always"
	// SamplerNever samples no traces.
	SamplerNever SamplerType = "never"
	// SamplerRatio samples a percentage of traces.
	SamplerRatio SamplerType = "ratio"
	// SamplerParentBased respects the parent span's sampling decision.
	SamplerParentBased SamplerType = "parent"
)

// Config holds OpenTelemetry configuration.
type Config struct {
	// ServiceName identifies this service in traces. Required.
	ServiceName string

	// ServiceVersion is the version of this service (optional).
	ServiceVersion string

	// Environment identifies the deployment environment (e.g., "production", "staging").
	Environment string

	// Endpoint is the collector endpoint (e.g., "localhost:4317" for OTLP).
	// Required unless Exporter is ExporterNone.
	Endpoint string

	// Exporter selects the telemetry exporter. Defaults to OTLP.
	Exporter ExporterType

	// Insecure disables TLS for the exporter connection.
	// Set to true for local development.
	Insecure bool

	// Sampler selects the sampling strategy. Defaults to SamplerAlways.
	Sampler SamplerType

	// SampleRatio is the sampling ratio when Sampler is SamplerRatio.
	// Value between 0.0 and 1.0. Defaults to 1.0.
	SampleRatio float64

	// EnableMetrics enables OpenTelemetry metrics collection.
	// Metrics are exported alongside traces.
	EnableMetrics bool

	// EnableTracing enables distributed tracing. Defaults to true.
	EnableTracing bool

	// Headers are additional headers to send with exports (e.g., authentication).
	Headers map[string]string

	// ResourceAttributes are additional attributes to add to all spans.
	ResourceAttributes map[string]string

	// PropagateB3 enables B3 propagation format (used by Zipkin).
	// By default, W3C TraceContext is used.
	PropagateB3 bool
}

// Validate checks that the configuration is valid.
func (c *Config) Validate() error {
	if c.ServiceName == "" {
		return errors.New("otel: ServiceName is required")
	}

	// Default exporter to OTLP
	if c.Exporter == "" {
		c.Exporter = ExporterOTLP
	}

	// Validate exporter type
	switch c.Exporter {
	case ExporterOTLP, ExporterJaeger, ExporterZipkin, ExporterNone:
		// Valid
	default:
		return errors.New("otel: invalid Exporter type: " + string(c.Exporter))
	}

	// Endpoint required unless exporter is none
	if c.Exporter != ExporterNone && c.Endpoint == "" {
		return errors.New("otel: Endpoint is required when exporter is enabled")
	}

	// Default sampler to always
	if c.Sampler == "" {
		c.Sampler = SamplerAlways
	}

	// Validate sampler type
	switch c.Sampler {
	case SamplerAlways, SamplerNever, SamplerRatio, SamplerParentBased:
		// Valid
	default:
		return errors.New("otel: invalid Sampler type: " + string(c.Sampler))
	}

	// Default sample ratio to 1.0
	if c.SampleRatio == 0 {
		c.SampleRatio = 1.0
	}

	// Validate sample ratio
	if c.SampleRatio < 0 || c.SampleRatio > 1 {
		return errors.New("otel: SampleRatio must be between 0.0 and 1.0")
	}

	// Default EnableTracing to true
	if !c.EnableMetrics && !c.EnableTracing {
		c.EnableTracing = true
	}

	return nil
}

// String returns a human-readable representation of the config.
func (c *Config) String() string {
	var b strings.Builder
	b.WriteString("otel.Config{")
	b.WriteString("ServiceName: " + c.ServiceName)
	if c.ServiceVersion != "" {
		b.WriteString(", Version: " + c.ServiceVersion)
	}
	if c.Environment != "" {
		b.WriteString(", Env: " + c.Environment)
	}
	b.WriteString(", Exporter: " + string(c.Exporter))
	if c.Endpoint != "" {
		b.WriteString(", Endpoint: " + c.Endpoint)
	}
	b.WriteString(", Sampler: " + string(c.Sampler))
	b.WriteString("}")
	return b.String()
}
