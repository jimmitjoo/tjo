package otel

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/zipkin"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
)

// Provider manages OpenTelemetry tracing and metrics.
type Provider struct {
	config         Config
	tracerProvider *sdktrace.TracerProvider
	tracer         trace.Tracer
	propagator     propagation.TextMapPropagator
	shutdownOnce   sync.Once
	shutdown       bool
	mu             sync.RWMutex
}

// New creates a new OpenTelemetry provider with the given configuration.
func New(cfg Config) (*Provider, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	p := &Provider{
		config: cfg,
	}

	if cfg.EnableTracing {
		if err := p.initTracing(); err != nil {
			return nil, fmt.Errorf("otel: failed to initialize tracing: %w", err)
		}
	}

	return p, nil
}

// initTracing initializes the tracer provider.
func (p *Provider) initTracing() error {
	ctx := context.Background()

	// Create exporter
	exporter, err := p.createExporter(ctx)
	if err != nil {
		return err
	}

	// Create resource with service information
	res, err := p.createResource(ctx)
	if err != nil {
		return err
	}

	// Create sampler
	sampler := p.createSampler()

	// Create tracer provider
	opts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	}

	if exporter != nil {
		opts = append(opts, sdktrace.WithBatcher(exporter))
	}

	p.tracerProvider = sdktrace.NewTracerProvider(opts...)

	// Register as global provider
	otel.SetTracerProvider(p.tracerProvider)

	// Set up propagation
	p.setupPropagation()

	// Create tracer for this service
	p.tracer = p.tracerProvider.Tracer(
		p.config.ServiceName,
		trace.WithInstrumentationVersion(p.config.ServiceVersion),
	)

	return nil
}

// createExporter creates the appropriate span exporter.
func (p *Provider) createExporter(ctx context.Context) (sdktrace.SpanExporter, error) {
	switch p.config.Exporter {
	case ExporterOTLP:
		return p.createOTLPExporter(ctx)
	case ExporterZipkin:
		return p.createZipkinExporter()
	case ExporterJaeger:
		// Jaeger now recommends using OTLP
		// The native Jaeger exporter is deprecated
		return p.createOTLPExporter(ctx)
	case ExporterNone:
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown exporter: %s", p.config.Exporter)
	}
}

// createOTLPExporter creates an OTLP gRPC exporter.
func (p *Provider) createOTLPExporter(ctx context.Context) (sdktrace.SpanExporter, error) {
	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(p.config.Endpoint),
	}

	if p.config.Insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}

	if len(p.config.Headers) > 0 {
		opts = append(opts, otlptracegrpc.WithHeaders(p.config.Headers))
	}

	client := otlptracegrpc.NewClient(opts...)
	return otlptrace.New(ctx, client)
}

// createZipkinExporter creates a Zipkin exporter.
func (p *Provider) createZipkinExporter() (sdktrace.SpanExporter, error) {
	// Zipkin expects HTTP endpoint like http://localhost:9411/api/v2/spans
	endpoint := p.config.Endpoint
	return zipkin.New(endpoint)
}

// createResource creates the resource describing this service.
func (p *Provider) createResource(ctx context.Context) (*resource.Resource, error) {
	attrs := []attribute.KeyValue{
		semconv.ServiceName(p.config.ServiceName),
	}

	if p.config.ServiceVersion != "" {
		attrs = append(attrs, semconv.ServiceVersion(p.config.ServiceVersion))
	}

	if p.config.Environment != "" {
		attrs = append(attrs, semconv.DeploymentEnvironment(p.config.Environment))
	}

	// Add custom resource attributes
	for k, v := range p.config.ResourceAttributes {
		attrs = append(attrs, attribute.String(k, v))
	}

	return resource.NewWithAttributes(
		semconv.SchemaURL,
		attrs...,
	), nil
}

// createSampler creates the appropriate sampler.
func (p *Provider) createSampler() sdktrace.Sampler {
	switch p.config.Sampler {
	case SamplerAlways:
		return sdktrace.AlwaysSample()
	case SamplerNever:
		return sdktrace.NeverSample()
	case SamplerRatio:
		return sdktrace.TraceIDRatioBased(p.config.SampleRatio)
	case SamplerParentBased:
		return sdktrace.ParentBased(sdktrace.AlwaysSample())
	default:
		return sdktrace.AlwaysSample()
	}
}

// setupPropagation configures context propagation.
func (p *Provider) setupPropagation() {
	var propagators []propagation.TextMapPropagator

	// Always include W3C TraceContext
	propagators = append(propagators, propagation.TraceContext{})

	// Optionally include Baggage
	propagators = append(propagators, propagation.Baggage{})

	p.propagator = propagation.NewCompositeTextMapPropagator(propagators...)
	otel.SetTextMapPropagator(p.propagator)
}

// Tracer returns the tracer for creating spans.
func (p *Provider) Tracer() trace.Tracer {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.tracer
}

// TracerProvider returns the underlying tracer provider.
func (p *Provider) TracerProvider() *sdktrace.TracerProvider {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.tracerProvider
}

// Propagator returns the text map propagator for context injection/extraction.
func (p *Provider) Propagator() propagation.TextMapPropagator {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.propagator
}

// Config returns the provider configuration.
func (p *Provider) Config() Config {
	return p.config
}

// IsEnabled returns true if tracing is enabled and the provider is active.
func (p *Provider) IsEnabled() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.config.EnableTracing && !p.shutdown && p.tracer != nil
}

// Shutdown gracefully shuts down the provider, flushing any pending spans.
// It should be called when the application exits.
func (p *Provider) Shutdown(ctx context.Context) error {
	var err error
	p.shutdownOnce.Do(func() {
		p.mu.Lock()
		p.shutdown = true
		p.mu.Unlock()

		if p.tracerProvider != nil {
			// Give pending spans time to export
			shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			err = p.tracerProvider.Shutdown(shutdownCtx)
		}
	})
	return err
}

// ForceFlush immediately exports all pending spans.
func (p *Provider) ForceFlush(ctx context.Context) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.tracerProvider == nil {
		return nil
	}
	return p.tracerProvider.ForceFlush(ctx)
}
