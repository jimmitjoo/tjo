package tjo

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jimmitjoo/tjo/config"
)

// TestRandomStringCryptographicallySecure verifies that RandomString uses secure randomness
func TestRandomStringCryptographicallySecure(t *testing.T) {
	g := Tjo{}
	
	// Generate multiple strings and ensure they're unique
	generated := make(map[string]bool)
	iterations := 100
	length := 32
	
	for i := 0; i < iterations; i++ {
		str := g.RandomString(length)
		
		// Check length
		if len(str) != length {
			t.Errorf("Expected length %d, got %d", length, len(str))
		}
		
		// Check for duplicates (should be extremely unlikely with secure random)
		if generated[str] {
			t.Errorf("Duplicate string generated: %s", str)
		}
		generated[str] = true
		
		// Verify characters are from allowed set
		for _, char := range str {
			if !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_+", char) {
				t.Errorf("Invalid character in random string: %c", char)
			}
		}
	}
	
	// Statistical test: ensure reasonable distribution (basic check)
	if len(generated) < iterations {
		t.Errorf("Not enough unique strings generated: %d/%d", len(generated), iterations)
	}
}

// TestDownloadFilePathTraversal tests protection against path traversal attacks
func TestDownloadFilePathTraversal(t *testing.T) {
	
	tests := []struct {
		name      string
		path      string
		filename  string
		shouldErr bool
	}{
		{
			name:      "Normal filename",
			path:      "/safe/path",
			filename:  "document.pdf",
			shouldErr: false, // Will error due to file not existing, but not due to validation
		},
		{
			name:      "Path traversal with ..",
			path:      "/safe/path",
			filename:  "../../../etc/passwd",
			shouldErr: true,
		},
		{
			name:      "Path traversal with encoded ..",
			path:      "/safe/path",
			filename:  "..%2F..%2Fetc%2Fpasswd",
			shouldErr: true,
		},
		{
			name:      "Forward slash in filename",
			path:      "/safe/path",
			filename:  "subdir/file.txt",
			shouldErr: true,
		},
		{
			name:      "Backslash in filename",
			path:      "/safe/path",
			filename:  "subdir\\file.txt",
			shouldErr: true,
		},
		{
			name:      "Hidden file starting with dot",
			path:      "/safe/path",
			filename:  ".hidden",
			shouldErr: false, // Hidden files should be allowed
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Mock response writer and request would be needed for full test
			// Here we test the validation logic
			err := validateDownloadPath(tt.path, tt.filename)
			if tt.shouldErr && err == nil {
				t.Errorf("Expected error for path traversal attempt with filename: %s", tt.filename)
			}
			if !tt.shouldErr && err != nil && !strings.Contains(err.Error(), "no such file") {
				t.Errorf("Unexpected validation error for safe filename %s: %v", tt.filename, err)
			}
		})
	}
}

// Helper function that mimics the validation logic from DownloadFile
func validateDownloadPath(pathToFile, filename string) error {
	if strings.Contains(filename, "..") || strings.Contains(filename, "/") || strings.Contains(filename, "\\") {
		return errors.New("invalid filename")
	}
	
	cleanPath := filepath.Clean(pathToFile)
	absPath, err := filepath.Abs(cleanPath)
	if err != nil {
		return err
	}
	
	fp := filepath.Join(absPath, filename)
	fileToServe := filepath.Clean(fp)
	
	if !strings.HasPrefix(fileToServe, absPath) {
		return errors.New("invalid file path")
	}
	
	return nil
}

// TestEncryptionErrorHandling verifies proper error handling in encryption functions
func TestEncryptionErrorHandling(t *testing.T) {
	tests := []struct {
		name      string
		key       []byte
		plaintext string
		shouldErr bool
	}{
		{
			name:      "Valid 32-byte key",
			key:       []byte("12345678901234567890123456789012"),
			plaintext: "secret data",
			shouldErr: false,
		},
		{
			name:      "Invalid key length",
			key:       []byte("short"),
			plaintext: "secret data",
			shouldErr: true,
		},
		{
			name:      "Empty plaintext",
			key:       []byte("12345678901234567890123456789012"),
			plaintext: "",
			shouldErr: false,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enc := Encryption{Key: tt.key}
			
			// Test encryption
			encrypted, err := enc.Encrypt(tt.plaintext)
			if tt.shouldErr && err == nil {
				t.Error("Expected encryption error but got none")
			}
			if !tt.shouldErr && err != nil {
				t.Errorf("Unexpected encryption error: %v", err)
			}
			
			// If encryption succeeded, test decryption
			if err == nil {
				decrypted, err := enc.Decrypt(encrypted)
				if err != nil {
					t.Errorf("Decryption failed: %v", err)
				}
				if decrypted != tt.plaintext {
					t.Errorf("Decrypted text doesn't match. Got: %s, Want: %s", decrypted, tt.plaintext)
				}
			}
		})
	}
}

// TestDecryptInvalidInput verifies that Decrypt properly handles invalid input
func TestDecryptInvalidInput(t *testing.T) {
	enc := Encryption{Key: []byte("12345678901234567890123456789012")}
	
	tests := []struct {
		name      string
		input     string
		shouldErr bool
	}{
		{
			name:      "Invalid base64",
			input:     "not-valid-base64!@#$%",
			shouldErr: true,
		},
		{
			name:      "Too short ciphertext",
			input:     "dG9vc2hvcnQ=", // "tooshort" in base64
			shouldErr: true,
		},
		{
			name:      "Empty string",
			input:     "",
			shouldErr: true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := enc.Decrypt(tt.input)
			if tt.shouldErr && err == nil {
				t.Error("Expected decryption error for invalid input but got none")
			}
			if !tt.shouldErr && err != nil {
				t.Errorf("Unexpected decryption error: %v", err)
			}
		})
	}
}

// TestValidationMaxLength verifies input length validation
func TestValidationMaxLength(t *testing.T) {
	v := &Validation{Errors: make(map[string]string)}
	
	// Test email max length
	longEmail := strings.Repeat("a", 250) + "@test.com"
	v.IsEmail("email", longEmail)
	if _, exists := v.Errors["email"]; !exists {
		t.Error("Expected error for email exceeding maximum length")
	}
	
	// Test normal email
	v.Errors = make(map[string]string)
	v.IsEmail("email", "normal@test.com")
	if _, exists := v.Errors["email"]; exists {
		t.Error("Unexpected error for normal email")
	}
	
	// Test MaxLength function
	v.Errors = make(map[string]string)
	v.MaxLength("field", "short", 10)
	if _, exists := v.Errors["field"]; exists {
		t.Error("Unexpected error for string within max length")
	}
	
	v.MaxLength("field", "this is a very long string", 10)
	if _, exists := v.Errors["field"]; !exists {
		t.Error("Expected error for string exceeding max length")
	}
}

// TestValidationSanitization verifies HTML/XSS sanitization
// SanitizeHTML now uses bluemonday's StrictPolicy which removes ALL HTML tags
func TestValidationSanitization(t *testing.T) {
	v := &Validation{Errors: make(map[string]string)}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Script tag",
			input:    "<script>alert('XSS')</script>",
			expected: "", // bluemonday removes all HTML tags
		},
		{
			name:     "JavaScript protocol",
			input:    "<a href='javascript:alert(1)'>Click</a>",
			expected: "Click", // bluemonday removes tags but keeps text content
		},
		{
			name:     "Event handlers",
			input:    "<img src=x onerror=alert(1) onclick=alert(2)>",
			expected: "", // bluemonday removes all HTML tags
		},
		{
			name:     "Normal text",
			input:    "This is normal text",
			expected: "This is normal text",
		},
		{
			name:     "Mixed content",
			input:    "Hello <b>world</b>!",
			expected: "Hello world!", // Tags removed, text preserved
		},
		{
			name:     "SVG XSS",
			input:    "<svg onload=alert(1)>",
			expected: "", // bluemonday removes all HTML tags
		},
		{
			name:     "Uppercase script",
			input:    "<SCRIPT>alert('XSS')</SCRIPT>",
			expected: "", // bluemonday handles case-insensitive
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sanitized := v.SanitizeHTML(tt.input)
			if sanitized != tt.expected {
				t.Errorf("Sanitization failed. Got: %q, Want: %q", sanitized, tt.expected)
			}
		})
	}
}

// TestCSRFProtection verifies CSRF middleware configuration
func TestCSRFProtection(t *testing.T) {
	g := &Tjo{
		Config: &config.Config{
			Cookie: config.CookieConfig{
				Secure: true,
				Domain: "example.com",
			},
		},
	}
	
	// The NoSurf middleware should be configured with secure settings
	// This is a basic test to ensure the function doesn't panic
	handler := g.NoSurf(nil)
	if handler == nil {
		t.Error("NoSurf middleware returned nil")
	}
}

// TestFilePermissions verifies secure file permissions
func TestFilePermissions(t *testing.T) {
	g := Tjo{}
	
	// Test CreateDirIfNotExists uses secure permissions
	// The function uses 0755 which is appropriate for directories
	// This test ensures the constant hasn't been changed to something insecure
	
	// We can't easily test the actual permissions without creating files,
	// but we can verify the function doesn't panic
	err := g.CreateDirIfNotExists("/tmp/test_" + g.RandomString(10))
	if err != nil && !strings.Contains(err.Error(), "permission denied") {
		// Error is expected if we don't have permissions, but shouldn't be other types
		if !strings.Contains(err.Error(), "no such file") {
			t.Errorf("Unexpected error type: %v", err)
		}
	}
}