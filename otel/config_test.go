package otel

import (
	"testing"
)

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid minimal config",
			config: Config{
				ServiceName: "test-service",
				Endpoint:    "localhost:4317",
			},
			wantErr: false,
		},
		{
			name: "valid full config",
			config: Config{
				ServiceName:    "test-service",
				ServiceVersion: "1.0.0",
				Environment:    "test",
				Endpoint:       "localhost:4317",
				Exporter:       ExporterOTLP,
				Insecure:       true,
				Sampler:        SamplerRatio,
				SampleRatio:    0.5,
				EnableTracing:  true,
				EnableMetrics:  true,
			},
			wantErr: false,
		},
		{
			name:    "missing service name",
			config:  Config{},
			wantErr: true,
			errMsg:  "ServiceName is required",
		},
		{
			name: "missing endpoint with exporter enabled",
			config: Config{
				ServiceName: "test-service",
				Exporter:    ExporterOTLP,
			},
			wantErr: true,
			errMsg:  "Endpoint is required",
		},
		{
			name: "no endpoint required for none exporter",
			config: Config{
				ServiceName: "test-service",
				Exporter:    ExporterNone,
			},
			wantErr: false,
		},
		{
			name: "invalid exporter type",
			config: Config{
				ServiceName: "test-service",
				Endpoint:    "localhost:4317",
				Exporter:    "invalid",
			},
			wantErr: true,
			errMsg:  "invalid Exporter type",
		},
		{
			name: "invalid sampler type",
			config: Config{
				ServiceName: "test-service",
				Endpoint:    "localhost:4317",
				Sampler:     "invalid",
			},
			wantErr: true,
			errMsg:  "invalid Sampler type",
		},
		{
			name: "sample ratio too low",
			config: Config{
				ServiceName: "test-service",
				Endpoint:    "localhost:4317",
				SampleRatio: -0.1,
			},
			wantErr: true,
			errMsg:  "SampleRatio must be between",
		},
		{
			name: "sample ratio too high",
			config: Config{
				ServiceName: "test-service",
				Endpoint:    "localhost:4317",
				SampleRatio: 1.5,
			},
			wantErr: true,
			errMsg:  "SampleRatio must be between",
		},
		{
			name: "valid zipkin exporter",
			config: Config{
				ServiceName: "test-service",
				Endpoint:    "http://localhost:9411/api/v2/spans",
				Exporter:    ExporterZipkin,
			},
			wantErr: false,
		},
		{
			name: "valid jaeger exporter (uses OTLP)",
			config: Config{
				ServiceName: "test-service",
				Endpoint:    "localhost:4317",
				Exporter:    ExporterJaeger,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Config.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && tt.errMsg != "" {
				if !containsString(err.Error(), tt.errMsg) {
					t.Errorf("Config.Validate() error = %v, want to contain %v", err, tt.errMsg)
				}
			}
		})
	}
}

func TestConfig_DefaultValues(t *testing.T) {
	cfg := Config{
		ServiceName: "test-service",
		Endpoint:    "localhost:4317",
	}

	err := cfg.Validate()
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	// Check defaults are set
	if cfg.Exporter != ExporterOTLP {
		t.Errorf("Default exporter = %v, want %v", cfg.Exporter, ExporterOTLP)
	}

	if cfg.Sampler != SamplerAlways {
		t.Errorf("Default sampler = %v, want %v", cfg.Sampler, SamplerAlways)
	}

	if cfg.SampleRatio != 1.0 {
		t.Errorf("Default sample ratio = %v, want 1.0", cfg.SampleRatio)
	}

	if !cfg.EnableTracing {
		t.Error("EnableTracing should default to true")
	}
}

func TestConfig_String(t *testing.T) {
	cfg := Config{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		Endpoint:       "localhost:4317",
		Exporter:       ExporterOTLP,
		Sampler:        SamplerAlways,
	}

	s := cfg.String()

	// Check that string contains key information
	if !containsString(s, "test-service") {
		t.Error("String() should contain service name")
	}
	if !containsString(s, "1.0.0") {
		t.Error("String() should contain version")
	}
	if !containsString(s, "test") {
		t.Error("String() should contain environment")
	}
	if !containsString(s, "otlp") {
		t.Error("String() should contain exporter")
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
