package otel

import (
	"context"
	"testing"
	"time"
)

func TestNew_ValidConfig(t *testing.T) {
	cfg := Config{
		ServiceName:   "test-service",
		Exporter:      ExporterNone, // No exporter to avoid connection issues
		EnableTracing: true,
	}

	provider, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer provider.Shutdown(context.Background())

	if provider == nil {
		t.Fatal("New() returned nil provider")
	}

	if !provider.IsEnabled() {
		t.Error("Provider should be enabled")
	}

	if provider.Tracer() == nil {
		t.Error("Tracer() should not return nil")
	}

	if provider.TracerProvider() == nil {
		t.Error("TracerProvider() should not return nil")
	}

	if provider.Propagator() == nil {
		t.Error("Propagator() should not return nil")
	}
}

func TestNew_InvalidConfig(t *testing.T) {
	cfg := Config{} // Missing required fields

	_, err := New(cfg)
	if err == nil {
		t.Error("New() should return error for invalid config")
	}
}

func TestProvider_Shutdown(t *testing.T) {
	cfg := Config{
		ServiceName:   "test-service",
		Exporter:      ExporterNone,
		EnableTracing: true,
	}

	provider, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Shutdown should not error
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = provider.Shutdown(ctx)
	if err != nil {
		t.Errorf("Shutdown() error = %v", err)
	}

	// After shutdown, provider should be disabled
	if provider.IsEnabled() {
		t.Error("Provider should be disabled after shutdown")
	}

	// Multiple shutdowns should be safe
	err = provider.Shutdown(ctx)
	if err != nil {
		t.Errorf("Second Shutdown() error = %v", err)
	}
}

func TestProvider_ForceFlush(t *testing.T) {
	cfg := Config{
		ServiceName:   "test-service",
		Exporter:      ExporterNone,
		EnableTracing: true,
	}

	provider, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer provider.Shutdown(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = provider.ForceFlush(ctx)
	if err != nil {
		t.Errorf("ForceFlush() error = %v", err)
	}
}

func TestProvider_Config(t *testing.T) {
	cfg := Config{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		Exporter:       ExporterNone,
		EnableTracing:  true,
	}

	provider, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer provider.Shutdown(context.Background())

	returnedCfg := provider.Config()

	if returnedCfg.ServiceName != cfg.ServiceName {
		t.Errorf("Config().ServiceName = %v, want %v", returnedCfg.ServiceName, cfg.ServiceName)
	}

	if returnedCfg.ServiceVersion != cfg.ServiceVersion {
		t.Errorf("Config().ServiceVersion = %v, want %v", returnedCfg.ServiceVersion, cfg.ServiceVersion)
	}

	if returnedCfg.Environment != cfg.Environment {
		t.Errorf("Config().Environment = %v, want %v", returnedCfg.Environment, cfg.Environment)
	}
}

func TestProvider_Samplers(t *testing.T) {
	tests := []struct {
		name        string
		sampler     SamplerType
		sampleRatio float64
	}{
		{"always", SamplerAlways, 1.0},
		{"never", SamplerNever, 0.0},
		{"ratio", SamplerRatio, 0.5},
		{"parent", SamplerParentBased, 1.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				ServiceName:   "test-service",
				Exporter:      ExporterNone,
				Sampler:       tt.sampler,
				SampleRatio:   tt.sampleRatio,
				EnableTracing: true,
			}

			provider, err := New(cfg)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			defer provider.Shutdown(context.Background())

			if !provider.IsEnabled() {
				t.Error("Provider should be enabled")
			}
		})
	}
}

func TestProvider_ResourceAttributes(t *testing.T) {
	cfg := Config{
		ServiceName:   "test-service",
		Exporter:      ExporterNone,
		EnableTracing: true,
		ResourceAttributes: map[string]string{
			"custom.attr": "value",
			"another":     "test",
		},
	}

	provider, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer provider.Shutdown(context.Background())

	if !provider.IsEnabled() {
		t.Error("Provider should be enabled")
	}
}

func TestProvider_TracingDisabled(t *testing.T) {
	cfg := Config{
		ServiceName:   "test-service",
		Exporter:      ExporterNone,
		EnableTracing: false,
		EnableMetrics: false,
	}

	// When both tracing and metrics are disabled, Validate sets EnableTracing = true
	err := cfg.Validate()
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	if !cfg.EnableTracing {
		t.Error("EnableTracing should be set to true when both are disabled")
	}
}
