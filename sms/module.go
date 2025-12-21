package sms

import (
	"context"
	"os"
)

// Module implements the tjo.Module interface for SMS functionality.
// Use this to opt-in to SMS support in your application.
//
// Example:
//
//	app := tjo.Tjo{}
//	app.New(rootPath, sms.NewModule())
//
//	// Later, send SMS:
//	if smsModule := app.Modules.Get("sms"); smsModule != nil {
//	    provider := smsModule.(*sms.Module).Provider
//	    provider.Send("+1234567890", "Hello!", false)
//	}
type Module struct {
	Provider SMSProvider
	config   *Config
}

// Config holds SMS module configuration
type Config struct {
	Provider   string
	Vonage     VonageConfig
	Twilio     TwilioConfig
}

// VonageConfig holds Vonage-specific configuration
type VonageConfig struct {
	APIKey     string
	APISecret  string
	FromNumber string
}

// TwilioConfig holds Twilio-specific configuration
type TwilioConfig struct {
	AccountSid string
	APIKey     string
	APISecret  string
	FromNumber string
}

// Option is a function that configures the SMS module
type Option func(*Module)

// NewModule creates a new SMS module with default configuration from environment.
// By default, it reads configuration from environment variables.
// Use WithConfig to override.
func NewModule(opts ...Option) *Module {
	m := &Module{
		config: &Config{
			Provider: os.Getenv("SMS_PROVIDER"),
			Vonage: VonageConfig{
				APIKey:     os.Getenv("VONAGE_API_KEY"),
				APISecret:  os.Getenv("VONAGE_API_SECRET"),
				FromNumber: os.Getenv("VONAGE_FROM_NUMBER"),
			},
			Twilio: TwilioConfig{
				AccountSid: os.Getenv("TWILIO_ACCOUNT_SID"),
				APIKey:     os.Getenv("TWILIO_API_KEY"),
				APISecret:  os.Getenv("TWILIO_API_SECRET"),
				FromNumber: os.Getenv("TWILIO_FROM_NUMBER"),
			},
		},
	}

	for _, opt := range opts {
		opt(m)
	}

	return m
}

// WithConfig sets custom configuration for the SMS module
func WithConfig(cfg *Config) Option {
	return func(m *Module) {
		m.config = cfg
	}
}

// WithProvider sets a specific SMS provider
func WithProvider(provider string) Option {
	return func(m *Module) {
		m.config.Provider = provider
	}
}

// WithVonage configures Vonage as the SMS provider
func WithVonage(apiKey, apiSecret, fromNumber string) Option {
	return func(m *Module) {
		m.config.Provider = "vonage"
		m.config.Vonage = VonageConfig{
			APIKey:     apiKey,
			APISecret:  apiSecret,
			FromNumber: fromNumber,
		}
	}
}

// WithTwilio configures Twilio as the SMS provider
func WithTwilio(accountSid, apiKey, apiSecret, fromNumber string) Option {
	return func(m *Module) {
		m.config.Provider = "twilio"
		m.config.Twilio = TwilioConfig{
			AccountSid: accountSid,
			APIKey:     apiKey,
			APISecret:  apiSecret,
			FromNumber: fromNumber,
		}
	}
}

// Name returns the module identifier
func (m *Module) Name() string {
	return "sms"
}

// Initialize sets up the SMS provider based on configuration.
// This is called automatically during app.New().
func (m *Module) Initialize(g interface{}) error {
	switch m.config.Provider {
	case "vonage":
		m.Provider = &Vonage{
			APIKey:     m.config.Vonage.APIKey,
			APISecret:  m.config.Vonage.APISecret,
			FromNumber: m.config.Vonage.FromNumber,
		}
	case "twilio":
		m.Provider = &Twilio{
			AccountSid: m.config.Twilio.AccountSid,
			APIKey:     m.config.Twilio.APIKey,
			APISecret:  m.config.Twilio.APISecret,
			FromNumber: m.config.Twilio.FromNumber,
		}
	default:
		// No provider configured - that's OK, SMS is optional
		m.Provider = nil
	}

	return nil
}

// Shutdown gracefully stops the SMS module.
// SMS has no persistent connections, so this is a no-op.
func (m *Module) Shutdown(ctx context.Context) error {
	return nil
}

// Send is a convenience method that delegates to the configured provider.
// Returns an error if no provider is configured.
func (m *Module) Send(to, message string, unicode bool) error {
	if m.Provider == nil {
		return ErrNoProvider
	}
	return m.Provider.Send(to, message, unicode)
}

// IsConfigured returns true if an SMS provider is configured
func (m *Module) IsConfigured() bool {
	return m.Provider != nil
}
