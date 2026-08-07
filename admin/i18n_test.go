package admin

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jimmitjoo/tjo/i18n"
	"golang.org/x/text/language"
)

// The panel in Swedish, from a header, with no application code involved.
func TestTheAdminPanelTranslates(t *testing.T) {
	eachDatabase(t, func(t *testing.T, db *sql.DB, driver string) {
		seed(t, db, driver)

		catalogue := i18n.NewWithFramework(language.English)
		catalogue.SetString(language.Swedish, "admin.new", "Ny")
		catalogue.SetString(language.Swedish, "admin.search", "Sök")
		catalogue.SetString(language.Swedish, "admin.data", "Data")
		catalogue.Set(language.Swedish, "admin.records", i18n.Message{
			One: "{count} post", Other: "{count} poster",
		})
		catalogue.SetString(language.Swedish, "admin.field.articles.title", "Rubrik")
		catalogue.SetString(language.Swedish, "admin.resource.articles", "Artiklar")

		h := catalogue.Middleware(testPanel(db, driver, AllowAll).Handler("/admin"))

		r := httptest.NewRequest("GET", "/r/articles", nil)
		r.Header.Set("Accept-Language", "sv")

		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)

		body := rec.Body.String()

		for _, want := range []string{
			`lang="sv"`,
			">Ny<",     // the New button
			"3 poster", // the plural form, from the Swedish catalogue
			"Rubrik",   // the column label, overriding humanise()
			"Artiklar", // the resource name
		} {
			if !strings.Contains(body, want) {
				t.Errorf("the Swedish panel does not contain %q", want)
			}
		}

		// An untranslated key falls back to English rather than to the key.
		if strings.Contains(body, "admin.") {
			t.Error("a raw message key reached the page")
		}
	})
}

// Right-to-left is not a translation, it is a layout. An Arabic operator gets
// a mirrored panel or an unusable one.
func TestTheAdminPanelMirrorsForRightToLeft(t *testing.T) {
	eachDatabase(t, func(t *testing.T, db *sql.DB, driver string) {
		seed(t, db, driver)

		catalogue := i18n.NewWithFramework(language.English)
		catalogue.SetString(language.Arabic, "admin.new", "جديد")

		h := catalogue.Middleware(testPanel(db, driver, AllowAll).Handler("/admin"))

		r := httptest.NewRequest("GET", "/r/articles", nil)
		r.Header.Set("Accept-Language", "ar")

		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)

		body := rec.Body.String()

		if !strings.Contains(body, `dir="rtl"`) {
			t.Error("the Arabic panel is not marked right-to-left")
		}
		if !strings.Contains(body, "جديد") {
			t.Error("the Arabic label is missing")
		}

		// The stylesheet must use logical properties, or `dir` changes the
		// text direction and leaves the chrome where it was.
		for _, physical := range []string{"border-right:", "border-left:", "text-align: left", "text-align: right"} {
			if strings.Contains(body, physical) {
				t.Errorf("the layout still uses the physical property %q, so it does not mirror", physical)
			}
		}
	})
}

// Without a middleware the panel still renders, in the fallback language.
// Translating is never a precondition for serving.
func TestTheAdminPanelRendersWithoutAnyLocaleConfigured(t *testing.T) {
	eachDatabase(t, func(t *testing.T, db *sql.DB, driver string) {
		seed(t, db, driver)

		rec := get(t, testPanel(db, driver, AllowAll).Handler("/admin"), "/r/articles")

		if rec.Code != http.StatusOK {
			t.Fatalf("%d", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, ">New<") || !strings.Contains(body, "3 records") {
			t.Errorf("the untranslated panel is not in English:\n%s", body[:min(len(body), 600)])
		}
	})
}
