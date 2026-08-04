package security

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strings"
	"unicode"
)

// InputValidationConfig holds configuration for input validation
type InputValidationConfig struct {
	MaxFieldLength  int
	MaxTotalLength  int
	AllowHTML       bool
	StrictMode      bool
	CustomPatterns  map[string]*regexp.Regexp
	BlockedPatterns []*regexp.Regexp
	ExemptPaths     []string
	ExemptMethods   []string
}

// DefaultInputValidationConfig returns a secure default configuration
func DefaultInputValidationConfig() InputValidationConfig {
	// Common attack patterns to block
	blockPatterns := []*regexp.Regexp{
		// SQL injection patterns
		regexp.MustCompile(`(?i)(union\s+(all\s+)?select|insert\s+into|delete\s+from|drop\s+table|alter\s+table)`),
		regexp.MustCompile(`(?i)(\bor\b\s+\d+\s*=\s*\d+|\band\b\s+\d+\s*=\s*\d+)`),
		regexp.MustCompile(`(?i)(exec\s*\(|execute\s*\(|sp_executesql)`),

		// XSS patterns
		regexp.MustCompile(`(?i)(<script[^>]*>|</script>|javascript:|vbscript:|onload=|onerror=)`),
		regexp.MustCompile(`(?i)(eval\s*\(|expression\s*\(|setTimeout\s*\(|setInterval\s*\()`),

		// Path traversal patterns
		regexp.MustCompile(`\.\./|\.\.\\|\.\./\.\./|\.\.\\/\.\.\\/`),

		// Command injection patterns
		regexp.MustCompile(`(?i)(\|\s*curl\s+|\|\s*wget\s+|\|\s*nc\s+|\|\s*netcat\s+)`),
		regexp.MustCompile(`(?i)(;\s*rm\s+|;\s*del\s+|;\s*format\s+|;\s*shutdown\s+)`),

		// LDAP injection patterns
		regexp.MustCompile(`(?i)(\*\s*\)\s*\(|\*\s*\)\s*\(&)`),

		// NoSQL injection patterns
		regexp.MustCompile(`(?i)(\$where|\$regex|\$ne|\$gt|\$lt)`),
	}

	return InputValidationConfig{
		MaxFieldLength:  1000,
		MaxTotalLength:  10000,
		AllowHTML:       false,
		StrictMode:      true,
		CustomPatterns:  make(map[string]*regexp.Regexp),
		BlockedPatterns: blockPatterns,
		ExemptPaths:     []string{"/health", "/metrics"},
		ExemptMethods:   []string{"GET", "HEAD", "OPTIONS"},
	}
}

// InputValidator handles centralized input validation
type InputValidator struct {
	config InputValidationConfig
	logger *SecurityLogger
}

// NewInputValidator creates a new input validator
func NewInputValidator(config InputValidationConfig, logger *SecurityLogger) *InputValidator {
	if logger == nil {
		logger = DefaultSecurityLogger
	}
	return &InputValidator{
		config: config,
		logger: logger,
	}
}

// ValidationResult holds the result of input validation
type ValidationResult struct {
	Valid        bool
	Errors       []string
	CleanedInput map[string][]string
	Threats      []ThreatDetails
}

// ThreatDetails contains information about detected threats
type ThreatDetails struct {
	Field      string
	ThreatType string
	Pattern    string
	Severity   string
}

// ValidateRequest validates all input data in a request
func (iv *InputValidator) ValidateRequest(r *http.Request) *ValidationResult {
	result := &ValidationResult{
		Valid:        true,
		Errors:       []string{},
		CleanedInput: make(map[string][]string),
		Threats:      []ThreatDetails{},
	}

	// Check if path is exempt
	if iv.isExemptPath(r.URL.Path) || iv.isExemptMethod(r.Method) {
		return result
	}

	// Parse form data
	if err := r.ParseForm(); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, "Failed to parse form data")
		return result
	}

	totalLength := 0

	// Validate form values
	for field, values := range r.Form {
		cleanedValues := []string{}

		for _, value := range values {
			// Check field length
			if len(value) > iv.config.MaxFieldLength {
				result.Valid = false
				result.Errors = append(result.Errors, fmt.Sprintf("Field '%s' exceeds maximum length", field))
				continue
			}

			totalLength += len(value)

			// Validate individual value
			cleanedValue, threats := iv.validateValue(field, value)

			if len(threats) > 0 {
				result.Threats = append(result.Threats, threats...)
				if iv.config.StrictMode {
					result.Valid = false
					for _, threat := range threats {
						result.Errors = append(result.Errors,
							fmt.Sprintf("Security threat detected in field '%s': %s", field, threat.ThreatType))
					}
				}
			}

			cleanedValues = append(cleanedValues, cleanedValue)
		}

		result.CleanedInput[field] = cleanedValues
	}

	// Check total length
	if totalLength > iv.config.MaxTotalLength {
		result.Valid = false
		result.Errors = append(result.Errors, "Total input size exceeds maximum allowed")
	}

	// Log security threats
	if len(result.Threats) > 0 {
		iv.logThreats(r, result.Threats)
	}

	return result
}

// validateValue validates a single input value
func (iv *InputValidator) validateValue(field, value string) (string, []ThreatDetails) {
	var threats []ThreatDetails
	cleanedValue := value

	// Check blocked patterns
	for _, pattern := range iv.config.BlockedPatterns {
		if pattern.MatchString(value) {
			threats = append(threats, ThreatDetails{
				Field:      field,
				ThreatType: iv.identifyThreatType(pattern),
				Pattern:    pattern.String(),
				Severity:   "high",
			})
		}
	}

	// Check custom patterns
	for name, pattern := range iv.config.CustomPatterns {
		if pattern.MatchString(value) {
			threats = append(threats, ThreatDetails{
				Field:      field,
				ThreatType: name,
				Pattern:    pattern.String(),
				Severity:   "medium",
			})
		}
	}

	// Basic sanitization
	cleanedValue = iv.sanitizeInput(cleanedValue)

	// Additional checks for suspicious content
	if iv.containsSuspiciousContent(value) {
		threats = append(threats, ThreatDetails{
			Field:      field,
			ThreatType: "suspicious_content",
			Pattern:    "general_suspicious_patterns",
			Severity:   "low",
		})
	}

	return cleanedValue, threats
}

// sanitizeInput performs basic input sanitization
func (iv *InputValidator) sanitizeInput(input string) string {
	// Remove null bytes
	input = strings.ReplaceAll(input, "\x00", "")

	// Remove control characters except whitespace
	var result strings.Builder
	for _, r := range input {
		if unicode.IsControl(r) && !unicode.IsSpace(r) {
			continue
		}
		result.WriteRune(r)
	}
	input = result.String()

	// HTML escape if not allowing HTML
	if !iv.config.AllowHTML {
		input = html.EscapeString(input)
	}

	// Trim excessive whitespace
	input = strings.TrimSpace(input)

	return input
}

// containsSuspiciousContent checks for suspicious content patterns
func (iv *InputValidator) containsSuspiciousContent(input string) bool {
	suspiciousIndicators := []string{
		"<script", "</script", "javascript:", "vbscript:",
		"eval(", "setTimeout(", "setInterval(",
		"../", ".\\", "cmd.exe", "/bin/sh",
		"union select", "drop table", "delete from",
	}

	lowerInput := strings.ToLower(input)
	for _, indicator := range suspiciousIndicators {
		if strings.Contains(lowerInput, indicator) {
			return true
		}
	}

	return false
}

// identifyThreatType identifies the type of threat based on regex pattern
func (iv *InputValidator) identifyThreatType(pattern *regexp.Regexp) string {
	patternStr := strings.ToLower(pattern.String())

	if strings.Contains(patternStr, "union") || strings.Contains(patternStr, "select") {
		return "sql_injection"
	}
	if strings.Contains(patternStr, "script") || strings.Contains(patternStr, "javascript") {
		return "xss_attempt"
	}
	if strings.Contains(patternStr, "\\.\\.") {
		return "path_traversal"
	}
	if strings.Contains(patternStr, "exec") || strings.Contains(patternStr, "cmd") {
		return "command_injection"
	}
	if strings.Contains(patternStr, "\\$") {
		return "nosql_injection"
	}

	return "unknown_threat"
}

// isExemptPath checks if a path is exempt from validation
func (iv *InputValidator) isExemptPath(path string) bool {
	for _, exemptPath := range iv.config.ExemptPaths {
		if path == exemptPath || strings.HasPrefix(path, exemptPath+"/") {
			return true
		}
	}
	return false
}

// isExemptMethod checks if a method is exempt from validation
func (iv *InputValidator) isExemptMethod(method string) bool {
	for _, exemptMethod := range iv.config.ExemptMethods {
		if method == exemptMethod {
			return true
		}
	}
	return false
}

// logThreats logs detected security threats
func (iv *InputValidator) logThreats(r *http.Request, threats []ThreatDetails) {
	for _, threat := range threats {
		switch threat.ThreatType {
		case "sql_injection":
			iv.logger.LogSQLInjectionAttempt(r, threat.Pattern)
		case "xss_attempt":
			iv.logger.LogXSSAttempt(r, threat.Pattern)
		case "path_traversal":
			iv.logger.LogPathTraversal(r, threat.Field)
		default:
			iv.logger.LogSuspiciousRequest(r,
				fmt.Sprintf("%s detected in field %s", threat.ThreatType, threat.Field),
				threat.Severity)
		}
	}
}

// InputValidationMiddleware creates middleware for input validation
func InputValidationMiddleware(config InputValidationConfig) func(next http.Handler) http.Handler {
	validator := NewInputValidator(config, DefaultSecurityLogger)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			result := validator.ValidateRequest(r)

			if !result.Valid {
				// Log validation failure
				DefaultSecurityLogger.LogSuspiciousRequest(r,
					fmt.Sprintf("Input validation failed: %s", strings.Join(result.Errors, ", ")),
					"high")

				// Return error response
				http.Error(w, "Invalid input data", http.StatusBadRequest)
				return
			}

			// Make the sanitised values reachable. They used to be computed
			// and thrown away with a comment saying a real implementation
			// would do this, so the sanitising half of the middleware had no
			// effect at all.
			ctx := context.WithValue(r.Context(), cleanedInputKey, result.CleanedInput)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// contextKey is unexported so no other package can collide with it.
type contextKey int

const cleanedInputKey contextKey = iota

// CleanedInput returns the sanitised values InputValidationMiddleware produced
// for this request, if it ran.
func CleanedInput(r *http.Request) (map[string][]string, bool) {
	cleaned, ok := r.Context().Value(cleanedInputKey).(map[string][]string)
	return cleaned, ok
}

// GetCleanedFormValue safely retrieves a cleaned form value, preferring the
// value InputValidationMiddleware sanitised for this request.
func GetCleanedFormValue(r *http.Request, key string) string {
	if cleaned, ok := CleanedInput(r); ok {
		if values, present := cleaned[key]; present && len(values) > 0 {
			return values[0]
		}
	}

	value := r.FormValue(key)
	if value == "" {
		return ""
	}

	// Basic sanitization
	value = html.EscapeString(value)
	value = strings.TrimSpace(value)

	return value
}

// Global input validator instance
var DefaultInputValidator = NewInputValidator(DefaultInputValidationConfig(), DefaultSecurityLogger)
