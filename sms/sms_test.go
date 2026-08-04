package sms

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVonage_Send(t *testing.T) {
	tests := []struct {
		name          string
		mockResponse  func(req *http.Request) (*http.Response, error)
		to            string
		message       string
		unicode       bool
		expectError   bool
		errorContains string
	}{
		{
			name: "Successful SMS send",
			mockResponse: func(req *http.Request) (*http.Response, error) {
				// Verify request
				body, _ := io.ReadAll(req.Body)
				values, _ := url.ParseQuery(string(body))
				
				assert.Equal(t, "test-key", values.Get("api_key"))
				assert.Equal(t, "test-secret", values.Get("api_secret"))
				assert.Equal(t, "+1234567890", values.Get("from"))
				assert.Equal(t, "+0987654321", values.Get("to"))
				assert.Equal(t, "Test message", values.Get("text"))
				
				response := `{
					"message-count": "1",
					"messages": [{
						"to": "+0987654321",
						"message-id": "test-msg-id",
						"status": "0",
						"remaining-balance": "15.50"
					}]
				}`
				
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewBufferString(response)),
				}, nil
			},
			to:          "+0987654321",
			message:     "Test message",
			unicode:     false,
			expectError: false,
		},
		{
			name: "Unicode SMS send",
			mockResponse: func(req *http.Request) (*http.Response, error) {
				body, _ := io.ReadAll(req.Body)
				values, _ := url.ParseQuery(string(body))
				
				assert.Equal(t, "unicode", values.Get("type"))
				
				response := `{
					"message-count": "1",
					"messages": [{"status": "0"}]
				}`
				
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewBufferString(response)),
				}, nil
			},
			to:          "+0987654321",
			message:     "Test message 😀",
			unicode:     true,
			expectError: false,
		},
		{
			name: "Invalid credentials error",
			mockResponse: func(req *http.Request) (*http.Response, error) {
				response := `{
					"message-count": "1",
					"messages": [{
						"status": "4",
						"error-text": "Invalid credentials"
					}]
				}`
				
				return &http.Response{
					StatusCode: 401,
					Body:       io.NopCloser(bytes.NewBufferString(response)),
				}, nil
			},
			to:            "+0987654321",
			message:       "Test message",
			unicode:       false,
			expectError:   true,
			errorContains: "Invalid credentials",
		},
		{
			name: "Network error",
			mockResponse: func(req *http.Request) (*http.Response, error) {
				return nil, errors.New("network error")
			},
			to:            "+0987654321",
			message:       "Test message",
			unicode:       false,
			expectError:   true,
			errorContains: "network error",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &Vonage{
				APIKey:     "test-key",
				APISecret:  "test-secret",
				FromNumber: "+1234567890",
				httpClient: newMockHTTPClient(tt.mockResponse),
			}
			
			err := client.Send(tt.to, tt.message, tt.unicode)
			
			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestTwilio_Send(t *testing.T) {
	tests := []struct {
		name          string
		mockResponse  func(req *http.Request) (*http.Response, error)
		to            string
		message       string
		unicode       bool
		expectError   bool
		errorContains string
	}{
		{
			name: "Successful SMS send",
			mockResponse: func(req *http.Request) (*http.Response, error) {
				// Verify request
				assert.Equal(t, "/2010-04-01/Accounts/AC123456789/Messages.json", req.URL.Path)
				
				// Check basic auth
				username, password, ok := req.BasicAuth()
				assert.True(t, ok)
				assert.Equal(t, "test-key", username)
				assert.Equal(t, "test-secret", password)
				
				// Check body
				body, _ := io.ReadAll(req.Body)
				values, _ := url.ParseQuery(string(body))
				assert.Equal(t, "+0987654321", values.Get("To"))
				assert.Equal(t, "+1234567890", values.Get("From"))
				assert.Equal(t, "Test message", values.Get("Body"))
				
				response := `{
					"sid": "SM123456789",
					"status": "queued",
					"to": "+0987654321",
					"from": "+1234567890",
					"body": "Test message"
				}`
				
				return &http.Response{
					StatusCode: 201,
					Body:       io.NopCloser(bytes.NewBufferString(response)),
				}, nil
			},
			to:          "+0987654321",
			message:     "Test message",
			unicode:     false,
			expectError: false,
		},
		{
			name: "Authentication error",
			mockResponse: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: 401,
					Body:       io.NopCloser(strings.NewReader("Authentication failed")),
				}, nil
			},
			to:            "+0987654321",
			message:       "Test message",
			unicode:       false,
			expectError:   true,
			errorContains: "status 401",
		},
		{
			name: "Network error",
			mockResponse: func(req *http.Request) (*http.Response, error) {
				return nil, errors.New("connection refused")
			},
			to:            "+0987654321",
			message:       "Test message",
			unicode:       false,
			expectError:   true,
			errorContains: "connection refused",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &Twilio{
				AccountSid: "AC123456789",
				APIKey:     "test-key",
				APISecret:  "test-secret",
				FromNumber: "+1234567890",
				httpClient: newMockHTTPClient(tt.mockResponse),
			}
			
			err := client.Send(tt.to, tt.message, tt.unicode)
			
			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCreateSMSProvider(t *testing.T) {
	tests := []struct {
		name      string
		provider  string
		env       map[string]string
		expectNil bool
		verify    func(t *testing.T, provider SMSProvider)
	}{
		{
			name:     "Vonage provider",
			provider: "vonage",
			env: map[string]string{
				"VONAGE_API_KEY":     "key",
				"VONAGE_API_SECRET":  "secret",
				"VONAGE_FROM_NUMBER": "+123",
			},
			expectNil: false,
			verify: func(t *testing.T, provider SMSProvider) {
				v, ok := provider.(*Vonage)
				require.True(t, ok)
				assert.Equal(t, "key", v.APIKey)
				assert.Equal(t, "secret", v.APISecret)
				assert.Equal(t, "+123", v.FromNumber)
			},
		},
		{
			name:     "Twilio provider",
			provider: "twilio",
			env: map[string]string{
				"TWILIO_ACCOUNT_SID": "sid",
				"TWILIO_API_KEY":     "key",
				"TWILIO_API_SECRET":  "secret",
				"TWILIO_FROM_NUMBER": "+456",
			},
			expectNil: false,
			verify: func(t *testing.T, provider SMSProvider) {
				tw, ok := provider.(*Twilio)
				require.True(t, ok)
				assert.Equal(t, "sid", tw.AccountSid)
				assert.Equal(t, "key", tw.APIKey)
				assert.Equal(t, "secret", tw.APISecret)
				assert.Equal(t, "+456", tw.FromNumber)
			},
		},
		{
			name:      "Unknown provider",
			provider:  "unknown",
			expectNil: true,
		},
		{
			name:      "Empty provider",
			provider:  "",
			expectNil: true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save and restore environment
			if tt.env != nil {
				oldEnv := make(map[string]string)
				for k := range tt.env {
					oldEnv[k] = os.Getenv(k)
				}
				defer func() {
					for k, v := range oldEnv {
						if v == "" {
							os.Unsetenv(k)
						} else {
							os.Setenv(k, v)
						}
					}
				}()
				
				// Set test environment
				for k, v := range tt.env {
					os.Setenv(k, v)
				}
			}
			
			provider := CreateSMSProvider(tt.provider)
			
			if tt.expectNil {
				assert.Nil(t, provider)
			} else {
				require.NotNil(t, provider)
				if tt.verify != nil {
					tt.verify(t, provider)
				}
			}
		})
	}
}

func TestSMSProviderInterface(t *testing.T) {
	// Ensure both Vonage and Twilio implement SMSProvider
	var _ SMSProvider = (*Vonage)(nil)
	var _ SMSProvider = (*Twilio)(nil)
}

func TestMockHTTPClient(t *testing.T) {
	t.Run("Custom response", func(t *testing.T) {
		client := newMockHTTPClient(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 418,
				Body:       io.NopCloser(strings.NewReader("I'm a teapot")),
			}, nil
		})
		
		req, _ := http.NewRequest("GET", "http://example.com", nil)
		resp, err := client.Do(req)
		
		assert.NoError(t, err)
		assert.Equal(t, 418, resp.StatusCode)
		
		body, _ := io.ReadAll(resp.Body)
		assert.Equal(t, "I'm a teapot", string(body))
	})
	
	t.Run("Default response", func(t *testing.T) {
		client := newMockHTTPClient(nil)
		
		req, _ := http.NewRequest("GET", "http://example.com", nil)
		resp, err := client.Do(req)
		
		assert.NoError(t, err)
		assert.Equal(t, 200, resp.StatusCode)
		
		body, _ := io.ReadAll(resp.Body)
		assert.Equal(t, `{"status":"success"}`, string(body))
	})
}

func TestVonage_ProductionPath(t *testing.T) {
	// Reaches rest.nexmo.com for real. It asserts only that *something* went
	// wrong with deliberately invalid credentials, which is a weak claim that
	// also fails with no network at all -- so it is skipped under -short, which
	// is what CI runs.
	if testing.Short() {
		t.Skip("makes a live request to the Vonage API")
	}

	v := &Vonage{
		APIKey:     "test",
		APISecret:  "test",
		FromNumber: "+123",
		httpClient: nil,
	}

	// A nil httpClient must fall back to defaultHTTPClient rather than
	// panicking. Before v0.8.0 this branch called vonage-go-sdk instead, so the
	// code that shipped to users was the one path nothing else in this file
	// covered.
	err := v.Send("+456", "test", false)
	assert.Error(t, err)
}

func TestTwilio_ProductionPath(t *testing.T) {
	// Same as TestVonage_ProductionPath: a live call asserting only that it
	// failed. Twilio still routes production through its SDK, so this remains
	// the one test touching that branch -- see the note on Twilio.Send.
	if testing.Short() {
		t.Skip("makes a live request to the Twilio API")
	}

	tw := &Twilio{
		AccountSid: "AC123",
		APIKey:     "test",
		APISecret:  "test",
		FromNumber: "+123",
		httpClient: nil,
	}
	
	// This will use the production SDK path which will fail with test credentials
	// We're just testing that it attempts to use the SDK
	err := tw.Send("+456", "test", false)
	assert.Error(t, err) // Expected to fail with test credentials
}

// Benchmark tests
func BenchmarkVonage_Send(b *testing.B) {
	client := &Vonage{
		APIKey:     "test",
		APISecret:  "test",
		FromNumber: "+123",
		httpClient: newMockHTTPClient(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(`{"messages":[{"status":"0"}]}`)),
			}, nil
		}),
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = client.Send("+123", "test", false)
	}
}

func BenchmarkTwilio_Send(b *testing.B) {
	client := &Twilio{
		AccountSid: "sid",
		APIKey:     "test",
		APISecret:  "test",
		FromNumber: "+123",
		httpClient: newMockHTTPClient(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 201,
				Body:       io.NopCloser(strings.NewReader(`{"sid":"SM123","status":"queued"}`)),
			}, nil
		}),
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = client.Send("+123", "test", false)
	}
}

func BenchmarkCreateSMSProvider(b *testing.B) {
	os.Setenv("VONAGE_API_KEY", "test")
	os.Setenv("VONAGE_API_SECRET", "test")
	os.Setenv("VONAGE_FROM_NUMBER", "test")
	defer func() {
		os.Unsetenv("VONAGE_API_KEY")
		os.Unsetenv("VONAGE_API_SECRET")
		os.Unsetenv("VONAGE_FROM_NUMBER")
	}()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = CreateSMSProvider("vonage")
	}
}