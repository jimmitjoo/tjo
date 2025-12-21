package sms

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/twilio/twilio-go"
	twilioApi "github.com/twilio/twilio-go/rest/api/v2010"
	"github.com/vonage/vonage-go-sdk"
)

// ErrNoProvider is returned when attempting to send SMS without a configured provider
var ErrNoProvider = errors.New("no SMS provider configured")

// SMSProvider defines the interface for SMS providers
type SMSProvider interface {
	Send(to string, message string, unicode bool) error
}

// HTTPClient interface for dependency injection in tests
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Vonage provider implementation
type Vonage struct {
	APIKey     string
	APISecret  string
	FromNumber string
	httpClient HTTPClient // For testing
}

// Send sends an SMS via Vonage
func (v *Vonage) Send(to string, msg string, unicode bool) error {
	// Use injected client for testing if available
	if v.httpClient != nil {
		return v.sendWithHTTPClient(to, msg, unicode)
	}
	
	// Production implementation using Vonage SDK
	auth := vonage.CreateAuthFromKeySecret(v.APIKey, v.APISecret)
	client := vonage.NewSMSClient(auth)

	smsOpts := vonage.SMSOpts{}
	if unicode {
		smsOpts.Type = "unicode"
	}

	response, _, err := client.Send(v.FromNumber, to, msg, smsOpts)
	if err != nil {
		return err
	}
	
	if len(response.Messages) > 0 && response.Messages[0].Status != "0" {
		return fmt.Errorf("SMS send failed with status: %s", response.Messages[0].Status)
	}

	return nil
}

// sendWithHTTPClient is used for testing with mocked HTTP client
func (v *Vonage) sendWithHTTPClient(to string, msg string, unicode bool) error {
	data := url.Values{}
	data.Set("api_key", v.APIKey)
	data.Set("api_secret", v.APISecret)
	data.Set("from", v.FromNumber)
	data.Set("to", to)
	data.Set("text", msg)
	
	if unicode {
		data.Set("type", "unicode")
	}
	
	req, err := http.NewRequest("POST", "https://rest.nexmo.com/sms/json", strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	
	resp, err := v.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	
	var response struct {
		Messages []struct {
			Status    string `json:"status"`
			ErrorText string `json:"error-text"`
		} `json:"messages"`
	}
	
	if err := json.Unmarshal(body, &response); err != nil {
		return err
	}
	
	if len(response.Messages) > 0 && response.Messages[0].Status != "0" {
		if response.Messages[0].ErrorText != "" {
			return errors.New(response.Messages[0].ErrorText)
		}
		return fmt.Errorf("SMS send failed with status: %s", response.Messages[0].Status)
	}
	
	return nil
}

// Twilio provider implementation
type Twilio struct {
	AccountSid string
	APIKey     string
	APISecret  string
	FromNumber string
	httpClient HTTPClient // For testing
}

// Send sends an SMS via Twilio
func (t *Twilio) Send(to string, msg string, unicode bool) error {
	// Use injected client for testing if available
	if t.httpClient != nil {
		return t.sendWithHTTPClient(to, msg, unicode)
	}
	
	// Production implementation using Twilio SDK
	client := twilio.NewRestClientWithParams(twilio.ClientParams{
		Username:   t.APIKey,
		Password:   t.APISecret,
		AccountSid: t.AccountSid,
	})

	params := &twilioApi.CreateMessageParams{}
	params.SetTo(to)
	params.SetFrom(t.FromNumber)
	params.SetBody(msg)

	_, err := client.Api.CreateMessage(params)
	if err != nil {
		return fmt.Errorf("failed to send SMS: %w", err)
	}

	return nil
}

// sendWithHTTPClient is used for testing with mocked HTTP client
func (t *Twilio) sendWithHTTPClient(to string, msg string, unicode bool) error {
	data := url.Values{}
	data.Set("To", to)
	data.Set("From", t.FromNumber)
	data.Set("Body", msg)
	
	url := fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s/Messages.json", t.AccountSid)
	req, err := http.NewRequest("POST", url, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(t.APIKey, t.APISecret)
	
	resp, err := t.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("SMS send failed with status %d: %s", resp.StatusCode, string(body))
	}
	
	return nil
}

// CreateSMSProvider creates an SMS provider based on environment configuration
func CreateSMSProvider(provider string) SMSProvider {
	switch provider {
	case "vonage":
		return &Vonage{
			APIKey:     os.Getenv("VONAGE_API_KEY"),
			APISecret:  os.Getenv("VONAGE_API_SECRET"),
			FromNumber: os.Getenv("VONAGE_FROM_NUMBER"),
		}
	case "twilio":
		return &Twilio{
			AccountSid: os.Getenv("TWILIO_ACCOUNT_SID"),
			APIKey:     os.Getenv("TWILIO_API_KEY"),
			APISecret:  os.Getenv("TWILIO_API_SECRET"),
			FromNumber: os.Getenv("TWILIO_FROM_NUMBER"),
		}
	default:
		return nil
	}
}

// mockHTTPClient is a helper for testing
type mockHTTPClient struct {
	DoFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	if m.DoFunc != nil {
		return m.DoFunc(req)
	}
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(bytes.NewBufferString(`{"status":"success"}`)),
	}, nil
}

// newMockHTTPClient creates a new mock HTTP client for testing
func newMockHTTPClient(doFunc func(req *http.Request) (*http.Response, error)) *mockHTTPClient {
	return &mockHTTPClient{DoFunc: doFunc}
}

// defaultHTTPClient returns a default HTTP client with timeout
func defaultHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
	}
}