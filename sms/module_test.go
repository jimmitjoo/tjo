package sms

import (
	"context"
	"testing"
)

func TestModule_Name(t *testing.T) {
	m := NewModule()
	if got := m.Name(); got != "sms" {
		t.Errorf("Name() = %q, want %q", got, "sms")
	}
}

func TestModule_Initialize_NoProvider(t *testing.T) {
	m := NewModule()
	m.config.Provider = ""

	if err := m.Initialize(nil); err != nil {
		t.Errorf("Initialize() = %v, want nil", err)
	}

	if m.IsConfigured() {
		t.Error("IsConfigured() = true, want false when no provider")
	}
}

func TestModule_Initialize_Vonage(t *testing.T) {
	m := NewModule(WithVonage("key", "secret", "+1234567890"))

	if err := m.Initialize(nil); err != nil {
		t.Errorf("Initialize() = %v, want nil", err)
	}

	if !m.IsConfigured() {
		t.Error("IsConfigured() = false, want true for Vonage")
	}

	vonage, ok := m.Provider.(*Vonage)
	if !ok {
		t.Fatal("Provider is not *Vonage")
	}

	if vonage.APIKey != "key" {
		t.Errorf("APIKey = %q, want %q", vonage.APIKey, "key")
	}
	if vonage.APISecret != "secret" {
		t.Errorf("APISecret = %q, want %q", vonage.APISecret, "secret")
	}
	if vonage.FromNumber != "+1234567890" {
		t.Errorf("FromNumber = %q, want %q", vonage.FromNumber, "+1234567890")
	}
}

func TestModule_Initialize_Twilio(t *testing.T) {
	m := NewModule(WithTwilio("account", "key", "secret", "+1234567890"))

	if err := m.Initialize(nil); err != nil {
		t.Errorf("Initialize() = %v, want nil", err)
	}

	if !m.IsConfigured() {
		t.Error("IsConfigured() = false, want true for Twilio")
	}

	twilio, ok := m.Provider.(*Twilio)
	if !ok {
		t.Fatal("Provider is not *Twilio")
	}

	if twilio.AccountSid != "account" {
		t.Errorf("AccountSid = %q, want %q", twilio.AccountSid, "account")
	}
	if twilio.APIKey != "key" {
		t.Errorf("APIKey = %q, want %q", twilio.APIKey, "key")
	}
}

func TestModule_Shutdown(t *testing.T) {
	m := NewModule()
	ctx := context.Background()

	if err := m.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown() = %v, want nil", err)
	}
}

func TestModule_Send_NoProvider(t *testing.T) {
	m := NewModule()
	_ = m.Initialize(nil)

	err := m.Send("+1234567890", "test", false)
	if err != ErrNoProvider {
		t.Errorf("Send() = %v, want ErrNoProvider", err)
	}
}

func TestWithConfig(t *testing.T) {
	cfg := &Config{
		Provider: "vonage",
		Vonage: VonageConfig{
			APIKey:     "custom-key",
			APISecret:  "custom-secret",
			FromNumber: "+9999999999",
		},
	}

	m := NewModule(WithConfig(cfg))
	_ = m.Initialize(nil)

	vonage, ok := m.Provider.(*Vonage)
	if !ok {
		t.Fatal("Provider is not *Vonage")
	}

	if vonage.APIKey != "custom-key" {
		t.Errorf("APIKey = %q, want %q", vonage.APIKey, "custom-key")
	}
}

func TestWithProvider(t *testing.T) {
	m := NewModule(
		WithProvider("twilio"),
		WithTwilio("acc", "key", "secret", "+1111111111"),
	)

	if m.config.Provider != "twilio" {
		t.Errorf("Provider = %q, want %q", m.config.Provider, "twilio")
	}
}
