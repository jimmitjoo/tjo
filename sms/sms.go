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
	apiBase    string     // For testing; defaults to Vonage's API
}

// baseURL returns the API root, overridable so tests can point at httptest
// rather than reaching the real API to assert that something failed.
func (v *Vonage) baseURL() string {
	if v.apiBase != "" {
		return v.apiBase
	}
	return "https://rest.nexmo.com"
}

// Send sends an SMS via Vonage.
//
// This used to call vonage-go-sdk in production and take the plain HTTP path
// below only when a client was injected for tests -- so the code that shipped
// was the code nothing exercised, and the code with coverage never ran for
// users.
//
// The SDK is gone. It pulled in golang-jwt/jwt v3, which carries CVE-2025-30204
// at CVSS 8.7 with no fix in the v3 line, and vonage-go-sdk v0.14.0 is the
// latest release, so there was nothing to upgrade to. We authenticate with
// key and secret rather than JWT, so the vulnerable parser was almost certainly
// unreachable at runtime -- but "we believe it is unreachable" is not something
// to encode as a permanent exception in the CI gate.
//
// The HTTP path is also strictly better: it surfaces Vonage's error-text field,
// which the SDK branch discarded in favour of a bare status code.
func (v *Vonage) Send(to string, msg string, unicode bool) error {
	return v.sendWithHTTPClient(to, msg, unicode)
}

// sendWithHTTPClient posts to Vonage's SMS endpoint. httpClient is the seam
// tests inject through; it falls back to http.DefaultClient.
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
	
	req, err := http.NewRequest("POST", v.baseURL()+"/sms/json", strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// defaultHTTPClient rather than http.DefaultClient: the latter has no
	// timeout, and an SMS send that hangs forever holds the caller with it.
	client := v.httpClient
	if client == nil {
		client = defaultHTTPClient()
	}

	resp, err := client.Do(req)
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
	apiBase    string     // For testing; defaults to Twilio's API
}

// baseURL returns the API root, overridable so tests can point at httptest
// rather than reaching the real API to assert that something failed.
func (t *Twilio) baseURL() string {
	if t.apiBase != "" {
		return t.apiBase
	}
	return "https://api.twilio.com"
}

// Send sends an SMS via Twilio.
//
// Plain HTTP rather than twilio-go, for the same reason Vonage above dropped
// its SDK: routing production through the SDK while the HTTP path ran only
// under an injected test client meant the code that shipped and the code that
// had coverage were different code. That split is what hid vonage-go-sdk's
// dependency on golang-jwt/jwt v3 from every test in this package until
// govulncheck found it.
func (t *Twilio) Send(to string, msg string, unicode bool) error {
	return t.sendWithHTTPClient(to, msg, unicode)
}

// twilioError is the error shape Twilio returns on a 4xx or 5xx.
// https://www.twilio.com/docs/usage/requests-to-twilio#error-responses
type twilioError struct {
	Code     int    `json:"code"`
	Message  string `json:"message"`
	MoreInfo string `json:"more_info"`
	Status   int    `json:"status"`
}

// sendWithHTTPClient posts to Twilio's Messages endpoint. httpClient is the
// seam tests inject through; it falls back to defaultHTTPClient, which has a
// timeout, unlike http.DefaultClient.
func (t *Twilio) sendWithHTTPClient(to string, msg string, unicode bool) error {
	data := url.Values{}
	data.Set("To", to)
	data.Set("From", t.FromNumber)
	data.Set("Body", msg)

	endpoint := fmt.Sprintf("%s/2010-04-01/Accounts/%s/Messages.json", t.baseURL(), t.AccountSid)
	req, err := http.NewRequest("POST", endpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(t.APIKey, t.APISecret)

	client := t.httpClient
	if client == nil {
		client = defaultHTTPClient()
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)

		// Twilio describes its failures in the body. Returning the raw bytes
		// made "SMS send failed with status 400: {...}" the whole diagnosis,
		// which is worse than what the SDK gave back.
		var e twilioError
		if json.Unmarshal(body, &e) == nil && e.Message != "" {
			if e.MoreInfo != "" {
				return fmt.Errorf("twilio %d: %s (%s)", e.Code, e.Message, e.MoreInfo)
			}
			return fmt.Errorf("twilio %d: %s", e.Code, e.Message)
		}

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