package gemquick

import (
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/asaskevich/govalidator"
	"github.com/microcosm-cc/bluemonday"
)

type Validation struct {
	Data   url.Values
	Errors map[string]string
}

func (g *Gemquick) Validator(data url.Values) *Validation {
	return &Validation{Data: data, Errors: make(map[string]string)}
}

func (v *Validation) Valid() bool {
	return len(v.Errors) == 0
}

func (v *Validation) AddError(key, message string) {
	if _, exists := v.Errors[key]; !exists {
		v.Errors[key] = message
	}
}

func (v *Validation) Has(field string, r *http.Request) bool {
	x := r.Form.Get(field)
	return strings.TrimSpace(x) != ""
}

func (v *Validation) Required(r *http.Request, fields ...string) {
	for _, field := range fields {
		if !v.Has(field, r) {
			v.AddError(field, "This field cannot be blank")
		}
	}
}

func (v *Validation) Check(ok bool, key, message string) {
	if !ok {
		v.AddError(key, message)
	}
}

func (v *Validation) IsEmail(field, value string) {
	// Validate email length to prevent DoS attacks
	if len(value) > 254 { // RFC 5321 maximum email length
		v.AddError(field, "Email address too long")
		return
	}
	if !govalidator.IsEmail(value) {
		v.AddError(field, "Invalid email address")
	}
}

func (v *Validation) Equals(eq bool, field, verified string) {
	if !eq {
		v.AddError(field, "This field must equal: "+verified)
	}
}

func (v *Validation) IsInt(field, value string) {
	_, err := strconv.Atoi(value)
	if err != nil {
		v.AddError(field, "This field must be an integer")
	}
}

func (v *Validation) IsFloat(field, value string) {
	_, err := strconv.ParseFloat(value, 64)
	if err != nil {
		v.AddError(field, "This field must be a floating point number")
	}
}

func (v *Validation) IsString(field, value string) {
	if !govalidator.IsPrintableASCII(value) {
		v.AddError(field, "This field must contain only printable characters")
	}
}

func (v *Validation) IsDateISO(field, value string) {
	_, err := time.Parse("2006-01-02", value)
	if err != nil {
		v.AddError(field, "This field must be a date in the form of YYYY-MM-DD")
	}
}

func (v *Validation) NoSpaces(field, value string) {
	if strings.Contains(value, " ") {
		v.AddError(field, "This field cannot contain spaces")
	}
}

// MaxLength validates that a field doesn't exceed a maximum length
func (v *Validation) MaxLength(field, value string, maxLength int) {
	if len(value) > maxLength {
		v.AddError(field, fmt.Sprintf("This field must not exceed %d characters", maxLength))
	}
}

// MinLength validates that a field meets a minimum length requirement
func (v *Validation) MinLength(field, value string, minLength int) {
	if len(value) < minLength {
		v.AddError(field, fmt.Sprintf("This field must be at least %d characters", minLength))
	}
}

// IsAlphanumeric validates that a field contains only letters and numbers
func (v *Validation) IsAlphanumeric(field, value string) {
	if !govalidator.IsAlphanumeric(value) {
		v.AddError(field, "This field must contain only letters and numbers")
	}
}

// IsURL validates that a field contains a valid URL
func (v *Validation) IsURL(field, value string) {
	if !govalidator.IsURL(value) {
		v.AddError(field, "This field must be a valid URL")
	}
}

// SanitizeHTML removes ALL HTML tags from input using bluemonday's strict policy.
// Use this for input that should be displayed as plain text.
func (v *Validation) SanitizeHTML(value string) string {
	p := bluemonday.StrictPolicy()
	return p.Sanitize(value)
}

// SanitizeRichText allows safe HTML formatting (bold, italic, links, etc.) while
// removing dangerous elements like scripts, iframes, and event handlers.
// Use this for user-generated content like blog posts, comments, or rich text editors.
func (v *Validation) SanitizeRichText(value string) string {
	p := bluemonday.UGCPolicy()
	return p.Sanitize(value)
}

// EscapeHTML completely escapes all HTML special characters.
// Use this when you want to display user input as literal text, preserving
// characters like < > & as visible text rather than HTML entities.
func (v *Validation) EscapeHTML(value string) string {
	return html.EscapeString(value)
}
