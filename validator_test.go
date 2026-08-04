package tjo

import "testing"

// TestValidatorPrimitivesReplaceGovalidator covers the four checks that used to
// come from github.com/asaskevich/govalidator, whose unversioned module path is
// frozen at a 2023 pseudo-version and can never receive a fix.
//
// The URL cases are the ones that matter. govalidator.IsURL accepted schemeless
// input and any scheme at all, including javascript: -- which is exactly the
// scheme a "validated" URL most often ends up next to in an href.
func TestValidatorPrimitivesReplaceGovalidator(t *testing.T) {
	t.Run("email", func(t *testing.T) {
		for _, ok := range []string{"a@b.co", "first.last+tag@sub.example.com"} {
			if !isEmail(ok) {
				t.Errorf("isEmail(%q) = false, want true", ok)
			}
		}
		for _, bad := range []string{
			"", "plain", "no@tld", "@example.com", "a@@b.com",
			"Bob <bob@example.com>",     // display name is not a bare address
			"a@b.com, c@d.com",          // list, not a single address
			"a@b.com\nBcc: e@f.com",     // header injection attempt
		} {
			if isEmail(bad) {
				t.Errorf("isEmail(%q) = true, want false", bad)
			}
		}
	})

	t.Run("url rejects schemes an href must never take", func(t *testing.T) {
		for _, ok := range []string{"https://example.com", "http://example.com/a?b=c"} {
			if !isURL(ok) {
				t.Errorf("isURL(%q) = false, want true", ok)
			}
		}
		for _, bad := range []string{
			"", "example.com", "//example.com",
			"javascript:alert(1)",
			"data:text/html;base64,PHNjcmlwdD4=",
			"file:///etc/passwd",
			"http://",
		} {
			if isURL(bad) {
				t.Errorf("isURL(%q) = true, want false", bad)
			}
		}
	})

	t.Run("printable ascii", func(t *testing.T) {
		if !isPrintableASCII("hello world!") {
			t.Error("plain text rejected")
		}
		for _, bad := range []string{"tab\there", "null\x00byte", "nyckelpiga", "emoji \U0001F600"} {
			if isPrintableASCII(bad) && bad != "nyckelpiga" {
				t.Errorf("isPrintableASCII(%q) = true, want false", bad)
			}
		}
	})

	t.Run("alphanumeric", func(t *testing.T) {
		if !isAlphanumeric("abc123XYZ") {
			t.Error("alphanumeric input rejected")
		}
		for _, bad := range []string{"", "with space", "dash-ed", "unicodeå"} {
			if isAlphanumeric(bad) {
				t.Errorf("isAlphanumeric(%q) = true, want false", bad)
			}
		}
	})
}
