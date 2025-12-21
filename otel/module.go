package otel

import (
	"context"
	"os"
	"strconv"
)

// Module implements the tjo.Module interface for OpenTelemetry functionality.
// Use this to opt-in to distributed tracing in your application.
//
// Example:
//
//	app := tjo.Tjo{}
//	app.New(rootPath, otel.NewModule(
//	    otel.WithServiceName("my-app"),
//	    otel.WithOTLPExporter("localhost:4317"),
//	))
//
//	// Later, use the provider:
//	if otelModule := app.Modules.Get("otel"); otelModule != nil {
//	    provider := otelModule.(*otel.Module).Provider
//	    ctx, span := provider.Tracer().Start(ctx, "my-operation")
//	    defer span.End()
//	}
type Module struct {
	Provider *Provider
	config   Config
}

// ModuleOption is a function that configures the OTel module
type ModuleOption func(*Module)

// NewModule creates a new OpenTelemetry module with default configuration from environment.
func NewModule(opts ...ModuleOption) *Module {
	// Parse sample ratio
	sampleRatio := 1.0
	if ratio := os.Getenv("OTEL_SAMPLE_RATIO"); ratio != "" {
		if parsed, err := strconv.ParseFloat(ratio, 64); err == nil {
			sampleRatio = parsed
		}
	}

	m := &Module{
		config: Config{
			ServiceName:    os.Getenv("OTEL_SERVICE_NAME"),
			ServiceVersion: os.Getenv("OTEL_SERVICE_VERSION"),
			Environment:    os.Getenv("OTEL_ENVIRONMENT"),
			Endpoint:       os.Getenv("OTEL_ENDPOINT"),
			Exporter:       ExporterType(os.Getenv("OTEL_EXPORTER")),
			Insecure:       os.Getenv("OTEL_INSECURE") == "true",
			EnableTracing:  os.Getenv("OTEL_ENABLED") == "true",
			EnableMetrics:  os.Getenv("OTEL_METRICS_ENABLED") == "true",
			SampleRatio:    sampleRatio,
		},
	}

	// Set sampler from environment
	switch os.Getenv("OTEL_SAMPLER") {
	case "always":
		m.config.Sampler = SamplerAlways
	case "never":
		m.config.Sampler = SamplerNever
	case "ratio":
		m.config.Sampler = SamplerRatio
	case "parent":
		m.config.Sampler = SamplerParentBased
	default:
		m.config.Sampler = SamplerAlways
	}

	for _, opt := range opts {
		opt(m)
	}

	return m
}

// WithConfig sets the full configuration
func WithConfig(cfg Config) ModuleOption {
	return func(m *Module) {
		m.config = cfg
	}
}

// WithServiceName sets the service name for traces
func WithServiceName(name string) ModuleOption {
	return func(m *Module) {
		m.config.ServiceName = name
	}
}

// WithServiceVersion sets the service version for traces
func WithServiceVersion(version string) ModuleOption {
	return func(m *Module) {
		m.config.ServiceVersion = version
	}
}

// WithEnvironment sets the deployment environment
func WithEnvironment(env string) ModuleOption {
	return func(m *Module) {
		m.config.Environment = env
	}
}

// WithOTLPExporter configures OTLP export to the given endpoint
func WithOTLPExporter(endpoint string, insecure bool) ModuleOption {
	return func(m *Module) {
		m.config.Exporter = ExporterOTLP
		m.config.Endpoint = endpoint
		m.config.Insecure = insecure
		m.config.EnableTracing = true
	}
}

// WithZipkinExporter configures Zipkin export to the given endpoint
func WithZipkinExporter(endpoint string) ModuleOption {
	return func(m *Module) {
		m.config.Exporter = ExporterZipkin
		m.config.Endpoint = endpoint
		m.config.EnableTracing = true
	}
}

// WithAlwaysSample configures the always-sample strategy
func WithAlwaysSample() ModuleOption {
	return func(m *Module) {
		m.config.Sampler = SamplerAlways
	}
}

// WithRatioSample configures ratio-based sampling
func WithRatioSample(ratio float64) ModuleOption {
	return func(m *Module) {
		m.config.Sampler = SamplerRatio
		m.config.SampleRatio = ratio
	}
}

// Name returns the module identifier
func (m *Module) Name() string {
	return "otel"
}

// Initialize creates the OpenTelemetry provider.
// This is called automatically during app.New().
func (m *Module) Initialize(g interface{}) error {
	// Only initialize if service name is configured
	if m.config.ServiceName == "" {
		return nil
	}

	provider, err := New(m.config)
	if err != nil {
		return err
	}

	m.Provider = provider
	return nil
}

// Shutdown gracefully stops the OpenTelemetry provider, flushing pending spans.
func (m *Module) Shutdown(ctx context.Context) error {
	if m.Provider != nil {
		return m.Provider.Shutdown(ctx)
	}
	return nil
}

// IsEnabled returns true if tracing is enabled and configured
func (m *Module) IsEnabled() bool {
	return m.Provider != nil && m.Provider.IsEnabled()
}
