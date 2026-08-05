package i18n

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

	"golang.org/x/text/language"
)

// The definition of done: one plural key, three languages with one, four and
// six forms, all correct.
//
// English has two forms and `if count == 1` is right in English and wrong in
// most of the world. A catalogue design that stored {singular, plural} would be
// broken for Polish before it shipped, and would be found by a Polish user
// rather than by this test.
func TestPluralsAcrossOneFourAndSixForms(t *testing.T) {
	catalogue := New(language.English)

	if err := catalogue.Load(fstest.MapFS{
		"locales/en.json": &fstest.MapFile{Data: []byte(`{
			"cart.items": {"one": "{count} item", "other": "{count} items"}
		}`)},
		"locales/pl.json": &fstest.MapFile{Data: []byte(`{
			"cart.items": {
				"one":   "{count} produkt",
				"few":   "{count} produkty",
				"many":  "{count} produktów",
				"other": "{count} produktu"
			}
		}`)},
		"locales/ar.json": &fstest.MapFile{Data: []byte(`{
			"cart.items": {
				"zero":  "لا منتجات",
				"one":   "منتج واحد",
				"two":   "منتجان",
				"few":   "{count} منتجات",
				"many":  "{count} منتجًا",
				"other": "{count} منتج"
			}
		}`)},
		"locales/ja.json": &fstest.MapFile{Data: []byte(`{
			"cart.items": {"other": "{count}点"}
		}`)},
	}, "locales/*.json"); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		locale string
		count  int
		want   string
	}{
		// English: one, other.
		{"en", 1, "1 item"},
		{"en", 0, "0 items"},
		{"en", 5, "5 items"},

		// Polish: one, few, many. 2-4 take few; 5+ and 0 take many.
		{"pl", 1, "1 produkt"},
		{"pl", 2, "2 produkty"},
		{"pl", 3, "3 produkty"},
		{"pl", 5, "5 produktów"},
		{"pl", 0, "0 produktów"},

		// Arabic: all six.
		{"ar", 0, "لا منتجات"},
		{"ar", 1, "منتج واحد"},
		{"ar", 2, "منتجان"},
		{"ar", 3, "3 منتجات"},
		{"ar", 11, "11 منتجًا"},
		{"ar", 100, "100 منتج"},

		// Japanese: one form for everything.
		{"ja", 1, "1点"},
		{"ja", 5, "5点"},
	}

	for _, tt := range tests {
		printer := catalogue.Printer(language.MustParse(tt.locale))
		if got := printer.N("cart.items", tt.count); got != tt.want {
			t.Errorf("%s N(cart.items, %d) = %q, want %q", tt.locale, tt.count, got, tt.want)
		}
	}
}

// A catalogue that fills in only the categories its language uses must still
// answer for every count. Everything falls back to "other", which is the one
// form every language has.
func TestMissingCategoriesFallBackToOther(t *testing.T) {
	catalogue := New(language.English)
	catalogue.Set(language.Polish, "x", Message{One: "one", Other: "other", plural: true})

	printer := catalogue.Printer(language.Polish)

	// 2 is "few" in Polish and the catalogue has no few.
	if got := printer.N("x", 2); got != "other" {
		t.Errorf("N(x, 2) = %q, want the other form", got)
	}
	if got := printer.N("x", 1); got != "one" {
		t.Errorf("N(x, 1) = %q", got)
	}
}

// A plural message with no "other" is a hole that only shows up for the counts
// nobody tested, so it is refused at load time.
func TestAPluralMessageWithoutOtherIsRefused(t *testing.T) {
	catalogue := New(language.English)

	err := catalogue.Load(fstest.MapFS{
		"locales/en.json": &fstest.MapFile{Data: []byte(`{"x": {"one": "just one"}}`)},
	}, "locales/*.json")

	if err == nil {
		t.Fatal("a plural message with no other form was accepted")
	}
}

// Matching, not string comparison. This is the whole reason to use
// language.Matcher: sv-FI is Swedish, and pt-BR should reach pt before English.
func TestNegotiationMatchesRatherThanCompares(t *testing.T) {
	catalogue := New(language.English)
	catalogue.SetString(language.Swedish, "k", "svenska")
	catalogue.SetString(language.Portuguese, "k", "português")

	tests := map[string]string{
		"sv-FI":                   "sv",
		"sv":                      "sv",
		"pt-BR":                   "pt",
		"da":                      "en", // not loaded, falls back
		"":                        "en",
		"sv-FI,sv;q=0.9,en;q=0.8": "sv",
		"fr;q=0.9, sv;q=0.8":      "sv",
		"zz-not-a-language":       "en",
	}

	for header, want := range tests {
		got := catalogue.Match(header)
		base, _ := got.Base()
		if base.String() != want {
			t.Errorf("Match(%q) = %s, want %s", header, got, want)
		}
	}
}

// A missing key renders as the key. Not empty, not a panic: a screen showing
// "cart.checkout" is obviously broken and still usable, which is what a
// half-translated deployment needs.
func TestAMissingKeyRendersAsTheKey(t *testing.T) {
	catalogue := New(language.English)
	printer := catalogue.Printer(language.English)

	if got := printer.T("nothing.here"); got != "nothing.here" {
		t.Errorf("T = %q, want the key", got)
	}
	if printer.Has("nothing.here") {
		t.Error("Has said a missing key exists")
	}
}

// A locale that has some keys but not this one falls back to the source
// language rather than to the key.
func TestAPartialTranslationFallsBackToTheSourceLanguage(t *testing.T) {
	catalogue := New(language.English)
	catalogue.SetString(language.English, "a", "Apple")
	catalogue.SetString(language.English, "b", "Banana")
	catalogue.SetString(language.Swedish, "a", "Äpple")

	printer := catalogue.Printer(language.Swedish)

	if got := printer.T("a"); got != "Äpple" {
		t.Errorf("translated key = %q", got)
	}
	if got := printer.T("b"); got != "Banana" {
		t.Errorf("untranslated key = %q, want the English source", got)
	}
}

func TestPlaceholders(t *testing.T) {
	catalogue := New(language.English)
	catalogue.SetString(language.English, "greeting", "Hello {name}, you have {n} messages")

	printer := catalogue.Printer(language.English)

	got := printer.T("greeting", "name", "Alex", "n", 3)
	if got != "Hello Alex, you have 3 messages" {
		t.Errorf("got %q", got)
	}
}

// A translation is data, not a template. If it could execute, a translator --
// or whoever compromised the translation tool -- could do more than translate.
func TestATranslationCannotExecute(t *testing.T) {
	catalogue := New(language.English)
	catalogue.SetString(language.English, "evil", "{{.Secret}} and {{range}} and {name}")

	got := catalogue.Printer(language.English).T("evil", "name", "value")

	if got != "{{.Secret}} and {{range}} and value" {
		t.Fatalf("got %q -- template syntax should be literal text", got)
	}
}

func TestWritingDirection(t *testing.T) {
	catalogue := New(language.English)

	rtl := []string{"ar", "he", "fa", "ur"}
	for _, tag := range rtl {
		if got := catalogue.Printer(language.MustParse(tag)).Dir(); got != RightToLeft {
			t.Errorf("%s is %s, want rtl", tag, got)
		}
	}
	for _, tag := range []string{"en", "sv", "ja", "pl"} {
		if got := catalogue.Printer(language.MustParse(tag)).Dir(); got != LeftToRight {
			t.Errorf("%s is %s, want ltr", tag, got)
		}
	}
}

func TestLocaleAwareFormatting(t *testing.T) {
	catalogue := New(language.English)

	when := time.Date(2026, 8, 5, 14, 30, 0, 0, time.UTC)

	dates := map[string]string{
		"sv":    "2026-08-05",
		"en-US": "08/05/2026",
		"en-GB": "05/08/2026",
		"de":    "05.08.2026",
	}
	for tag, want := range dates {
		if got := catalogue.Printer(language.MustParse(tag)).Date(when); got != want {
			t.Errorf("%s date = %q, want %q", tag, got, want)
		}
	}

	// Numbers differ by more than the decimal separator: grouping differs too.
	if english, swedish := catalogue.Printer(language.English).Number(1234.5),
		catalogue.Printer(language.Swedish).Number(1234.5); english == swedish {
		t.Errorf("English and Swedish formatted 1234.5 identically as %q", english)
	}

	// Yen has no minor unit. Formatting it with two decimals is a rounding
	// error in somebody's invoice.
	if got := catalogue.Printer(language.Japanese).Money("JPY", 1500); got == "" {
		t.Error("no output for JPY")
	}
}

// The negotiated language has to reach the response, or a cache serves one
// visitor's language to everyone.
func TestMiddlewareNegotiatesAndSetsHeaders(t *testing.T) {
	catalogue := New(language.English)
	catalogue.SetString(language.English, "hi", "Hello")
	catalogue.SetString(language.Swedish, "hi", "Hej")

	handler := catalogue.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(From(r.Context()).T("hi")))
	}))

	tests := []struct {
		name   string
		header string
		query  string
		cookie string
		want   string
	}{
		{"header", "sv", "", "", "Hej"},
		{"header with quality", "sv-FI,sv;q=0.9,en;q=0.8", "", "", "Hej"},
		{"no header", "", "", "", "Hello"},
		{"query beats header", "en", "sv", "", "Hej"},
		{"cookie beats header", "en", "", "sv", "Hej"},
		{"query beats cookie", "en", "sv", "en", "Hej"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/?lang="+tt.query, nil)
			if tt.header != "" {
				r.Header.Set("Accept-Language", tt.header)
			}
			if tt.cookie != "" {
				r.AddCookie(&http.Cookie{Name: "lang", Value: tt.cookie})
			}

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, r)

			if got := rec.Body.String(); got != tt.want {
				t.Errorf("body = %q, want %q", got, tt.want)
			}
			if rec.Header().Get("Content-Language") == "" {
				t.Error("no Content-Language header")
			}
			if rec.Header().Get("Vary") != "Accept-Language" {
				t.Errorf("Vary = %q -- without it a shared cache serves one visitor's language to everyone",
					rec.Header().Get("Vary"))
			}
		})
	}
}

// Choosing a language from a link should stick without a session.
func TestChoosingALanguageSetsACookie(t *testing.T) {
	catalogue := New(language.English)
	catalogue.SetString(language.Swedish, "hi", "Hej")

	handler := catalogue.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/?lang=sv", nil))

	cookies := rec.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Name != "lang" {
		t.Fatalf("no language cookie was set: %v", cookies)
	}
	if !cookies[0].HttpOnly {
		t.Error("the language cookie is readable by scripts for no reason")
	}
}

// A context with no printer -- a job, a test, a handler outside the middleware
// -- still translates, so translating is never a nil check.
func TestFromNeverReturnsNil(t *testing.T) {
	if p := From(t.Context()); p == nil {
		t.Fatal("From returned nil")
	}
	if got := From(t.Context()).T("anything"); got != "anything" {
		t.Errorf("got %q", got)
	}
}
