package tjo

import (
	"context"
	"github.com/jimmitjoo/tjo/i18n"
	"golang.org/x/text/language"

	"html"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/microcosm-cc/bluemonday"
)

type Validation struct {
	Data   url.Values
	Errors map[string]string

	// Printer renders the framework's own messages in the request's language.
	// Nil means the fallback language, which is what a background job gets.
	Printer *i18n.Printer
}

func (g *Tjo) Validator(data url.Values) *Validation {
	return &Validation{Data: data, Errors: make(map[string]string)}
}

// ValidatorFor is Validator with the request's language attached, so the
// framework's own validation messages come back in it.
//
// Prefer it in a handler. Validator stays for the places with no request --
// a job, a CLI command -- where the fallback language is the only sensible
// answer anyway.
func (g *Tjo) ValidatorFor(ctx context.Context, data url.Values) *Validation {
	return &Validation{
		Data:    data,
		Errors:  make(map[string]string),
		Printer: i18n.From(ctx),
	}
}

func (v *Validation) Valid() bool {
	return len(v.Errors) == 0
}

func (v *Validation) AddError(key, message string) {
	if _, exists := v.Errors[key]; !exists {
		v.Errors[key] = message
	}
}

// addKey records a framework message by translation key.
//
// The framework's own messages are keys, not English. Which language they come
// out in depends on the request, so a Validation carries the printer that was
// negotiated for it -- see Tjo.Validator, which passes one in.
//
// A Validation built without one still works and answers in the fallback
// language: validating in a background job should not be a nil check.
func (v *Validation) addKey(field, key string, args ...any) {
	v.AddError(field, v.printer().T(key, args...))
}

func (v *Validation) printer() *i18n.Printer {
	if v.Printer != nil {
		return v.Printer
	}
	return i18n.Default().Printer(language.English)
}

func (v *Validation) Has(field string, r *http.Request) bool {
	x := r.Form.Get(field)
	return strings.TrimSpace(x) != ""
}

func (v *Validation) Required(r *http.Request, fields ...string) {
	for _, field := range fields {
		if !v.Has(field, r) {
			v.addKey(field, "validation.blank")
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
		v.addKey(field, "validation.email_too_long")
		return
	}
	if !isEmail(value) {
		v.addKey(field, "validation.email_invalid")
	}
}

func (v *Validation) Equals(eq bool, field, verified string) {
	if !eq {
		v.addKey(field, "validation.must_equal", "value", verified)
	}
}

func (v *Validation) IsInt(field, value string) {
	_, err := strconv.Atoi(value)
	if err != nil {
		v.addKey(field, "validation.integer")
	}
}

func (v *Validation) IsFloat(field, value string) {
	_, err := strconv.ParseFloat(value, 64)
	if err != nil {
		v.addKey(field, "validation.float")
	}
}

func (v *Validation) IsString(field, value string) {
	if !isPrintableASCII(value) {
		v.addKey(field, "validation.printable")
	}
}

func (v *Validation) IsDateISO(field, value string) {
	_, err := time.Parse("2006-01-02", value)
	if err != nil {
		v.addKey(field, "validation.date")
	}
}

func (v *Validation) NoSpaces(field, value string) {
	if strings.Contains(value, " ") {
		v.addKey(field, "validation.no_spaces")
	}
}

// MaxLength validates that a field doesn't exceed a maximum length
func (v *Validation) MaxLength(field, value string, maxLength int) {
	if len(value) > maxLength {
		v.addKey(field, "validation.max_length", "max", maxLength)
	}
}

// MinLength validates that a field meets a minimum length requirement
func (v *Validation) MinLength(field, value string, minLength int) {
	if len(value) < minLength {
		v.addKey(field, "validation.min_length", "min", minLength)
	}
}

// IsAlphanumeric validates that a field contains only letters and numbers
func (v *Validation) IsAlphanumeric(field, value string) {
	if !isAlphanumeric(value) {
		v.addKey(field, "validation.alphanumeric")
	}
}

// IsURL validates that a field contains a valid URL
func (v *Validation) IsURL(field, value string) {
	if !isURL(value) {
		v.addKey(field, "validation.url")
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

// The four checks below replace github.com/asaskevich/govalidator, which this
// package used for exactly these four calls and nothing else.
//
// The unversioned module path it was pinned to has been frozen at a 2023
// pseudo-version since development moved to /v12, so it could never receive a
// fix. Swapping it for go-playground/validator would have traded one
// third-party validation framework for a larger one; four small functions is
// less surface than either, and three of them are a stdlib call.

// isEmail reports whether value is a single, plain address.
//
// net/mail.ParseAddress accepts things that are valid RFC 5322 but wrong here:
// display names ("Bob <bob@x.com>"), and group syntax. Requiring the parsed
// address to equal the input rejects both.
func isEmail(value string) bool {
	addr, err := mail.ParseAddress(value)
	if err != nil {
		return false
	}
	if addr.Address != value || addr.Name != "" {
		return false
	}
	at := strings.LastIndex(value, "@")
	return at > 0 && strings.Contains(value[at+1:], ".")
}

// isURL reports whether value is an absolute http or https URL with a host.
//
// Deliberately narrower than govalidator.IsURL, which accepted schemeless
// input like "example.com" and any scheme at all -- including javascript:,
// which is the one a validated URL most often ends up in an href next to.
func isURL(value string) bool {
	u, err := url.Parse(value)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	return u.Host != ""
}

// isPrintableASCII reports whether value is entirely printable 7-bit ASCII.
// Space is printable; control characters and anything above 0x7e are not.
func isPrintableASCII(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] < 0x20 || value[i] > 0x7e {
			return false
		}
	}
	return true
}

// isAlphanumeric reports whether value is entirely ASCII letters and digits.
// Empty is not alphanumeric, matching what the previous implementation did.
func isAlphanumeric(value string) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		switch {
		case c >= '0' && c <= '9':
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		default:
			return false
		}
	}
	return true
}
