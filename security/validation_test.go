package security

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestValidator() *InputValidator {
	return NewInputValidator(DefaultInputValidationConfig(), DefaultSecurityLogger)
}

// TestSanitizeInput pins what sanitisation actually does. It was dead code
// until recently -- computed and discarded -- so nothing had ever checked it.
func TestSanitizeInput(t *testing.T) {
	iv := newTestValidator()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain text is untouched", "hello world", "hello world"},
		{"html is escaped", `<script>alert(1)</script>`, "&lt;script&gt;alert(1)&lt;/script&gt;"},
		{"quotes are escaped", `say "hi" & 'bye'`, "say &#34;hi&#34; &amp; &#39;bye&#39;"},
		{"null bytes are removed", "a\x00b", "ab"},
		{"control characters are removed", "a\x01\x02b", "ab"},
		{"whitespace survives", "a\tb\nc", "a\tb\nc"},
		{"surrounding whitespace is trimmed", "  padded  ", "padded"},
		{"empty stays empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, iv.sanitizeInput(tt.input))
		})
	}
}

// TestSanitizeInputIsIdempotent guards against double-escaping, which turns
// a legitimate ampersand into &amp;amp; after two passes.
func TestSanitizeInputIsIdempotent(t *testing.T) {
	iv := newTestValidator()

	once := iv.sanitizeInput("a & b")
	twice := iv.sanitizeInput(once)

	assert.NotEqual(t, once, twice,
		"sanitizeInput is not idempotent -- callers must not apply it twice")
}

// TestValidateValueFlagsAttacks checks the blocklist actually fires on the
// classes it claims to cover.
func TestValidateValueFlagsAttacks(t *testing.T) {
	iv := newTestValidator()

	attacks := map[string]string{
		"sql union":      "1 UNION SELECT password FROM users",
		"sql comment":    "admin'--",
		"script tag":     `<script>alert(1)</script>`,
		"javascript uri": `<a href="javascript:alert(1)">x</a>`,
		"event handler":  `<img src=x onerror=alert(1)>`,
		"command pipe":   "foo | curl http://evil.test",
		"command chain":  "foo ; rm -rf /",
		"nosql operator": `{"age": {"$ne": null}}`,
		"path traversal": "../../etc/passwd",
	}

	for name, payload := range attacks {
		t.Run(name, func(t *testing.T) {
			_, threats := iv.validateValue("field", payload)
			assert.NotEmpty(t, threats, "payload passed unflagged: %s", payload)
		})
	}
}

// TestValidateValueAcceptsOrdinaryInput is the other half: a blocklist that
// rejects normal text is one users disable.
func TestValidateValueAcceptsOrdinaryInput(t *testing.T) {
	iv := newTestValidator()

	ordinary := []string{
		"Anna Karlsson",
		"anna@example.com",
		"Ordered 3 items, total 249 SEK",
		"O'Brien",
		"Comment: I like it & recommend it",
		"Straße mit Umlauten: Ärlig",
		"https://example.com/path?a=1&b=2",
	}

	for _, value := range ordinary {
		t.Run(value, func(t *testing.T) {
			_, threats := iv.validateValue("field", value)
			assert.Empty(t, threats, "ordinary input was flagged: %s", value)
		})
	}
}

// TestValidateRequestExemptions pins the exemption rules, which decide whether
// validation runs at all.
func TestValidateRequestExemptions(t *testing.T) {
	iv := newTestValidator()

	t.Run("exempt method is not inspected", func(t *testing.T) {
		r, _ := http.NewRequest("GET", "/submit?q="+url.QueryEscape("<script>alert(1)</script>"), nil)
		result := iv.ValidateRequest(r)
		assert.True(t, result.Valid, "GET is an exempt method and should not be rejected")
	})

	t.Run("exempt path is not inspected", func(t *testing.T) {
		r, _ := http.NewRequest("POST", "/health", strings.NewReader("x=<script>alert(1)</script>"))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		result := iv.ValidateRequest(r)
		assert.True(t, result.Valid, "/health is exempt and should not be rejected")
	})

	t.Run("a POST to a normal path is inspected", func(t *testing.T) {
		r, _ := http.NewRequest("POST", "/submit", strings.NewReader("x="+url.QueryEscape("<script>alert(1)</script>")))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		result := iv.ValidateRequest(r)
		assert.False(t, result.Valid, "a script payload in a POST body should be rejected")
	})

	t.Run("oversized field is rejected", func(t *testing.T) {
		huge := strings.Repeat("a", DefaultInputValidationConfig().MaxFieldLength+1)
		r, _ := http.NewRequest("POST", "/submit", strings.NewReader("x="+huge))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		result := iv.ValidateRequest(r)
		assert.False(t, result.Valid, "a field over MaxFieldLength should be rejected")
	})
}

// TestCleanedInputReachesHandlers covers the fix for the sanitised values that
// used to be computed and thrown away.
func TestCleanedInputReachesHandlers(t *testing.T) {
	var got map[string][]string
	var ok bool

	handler := InputValidationMiddleware(DefaultInputValidationConfig())(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			got, ok = CleanedInput(r)
			w.WriteHeader(http.StatusOK)
		}))

	r, _ := http.NewRequest("POST", "/submit", strings.NewReader("name=++Anna++"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handler.ServeHTTP(httptest.NewRecorder(), r)

	require.True(t, ok, "sanitised input never reached the handler")
	require.Contains(t, got, "name")
	assert.Equal(t, "Anna", got["name"][0], "value was not trimmed on the way through")
}
