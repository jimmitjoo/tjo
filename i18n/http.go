package i18n

import (
	"context"
	"net/http"

	"golang.org/x/text/language"
)

type contextKey struct{}

// Middleware negotiates a locale for each request and puts a printer in the
// context.
//
// The order of preference, most explicit first:
//
//  1. A query parameter, so a link can carry a language. `?lang=sv`.
//  2. A cookie, so the choice survives the next request.
//  3. The Accept-Language header.
//  4. The catalogue's fallback.
//
// A user's stated preference beats their browser's, because a browser
// configured once in a hotel in 2019 is not a preference. When the query
// parameter is used, the cookie is set, so choosing a language is sticky
// without a session.
func (c *Catalogue) Middleware(next http.Handler) http.Handler {
	return c.MiddlewareWithOptions(Options{})(next)
}

// Options configures negotiation.
type Options struct {
	// QueryParam is the query parameter carrying a language. Empty means
	// "lang". Set to "-" to disable.
	QueryParam string

	// CookieName is where a chosen language is remembered. Empty means
	// "lang". Set to "-" to disable.
	CookieName string

	// CookieMaxAge is how long the choice is remembered, in seconds. Zero
	// means one year.
	CookieMaxAge int

	// Secure marks the cookie Secure. Set it anywhere but local development.
	Secure bool

	// From overrides the whole negotiation. Return the zero tag to fall
	// through to the default order -- which is what an application does when
	// it reads a preference off the session for signed-in users and wants the
	// header for everyone else.
	From func(r *http.Request) language.Tag
}

// MiddlewareWithOptions is Middleware with the negotiation configured.
func (c *Catalogue) MiddlewareWithOptions(opts Options) func(http.Handler) http.Handler {
	query := opts.QueryParam
	if query == "" {
		query = "lang"
	}
	cookie := opts.CookieName
	if cookie == "" {
		cookie = "lang"
	}
	maxAge := opts.CookieMaxAge
	if maxAge == 0 {
		maxAge = 365 * 24 * 60 * 60
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var (
				tag     language.Tag
				chosen  bool
				fromURL bool
			)

			if opts.From != nil {
				if t := opts.From(r); t != language.Und {
					tag, chosen = c.Match(t.String()), true
				}
			}

			if !chosen && query != "-" {
				if requested := r.URL.Query().Get(query); requested != "" {
					tag, chosen, fromURL = c.Match(requested), true, true
				}
			}

			if !chosen && cookie != "-" {
				if stored, err := r.Cookie(cookie); err == nil && stored.Value != "" {
					tag, chosen = c.Match(stored.Value), true
				}
			}

			if !chosen {
				tag = c.Match(r.Header.Get("Accept-Language"))
			}

			if fromURL && cookie != "-" {
				http.SetCookie(w, &http.Cookie{
					Name:     cookie,
					Value:    tag.String(),
					Path:     "/",
					MaxAge:   maxAge,
					HttpOnly: true,
					Secure:   opts.Secure,
					SameSite: http.SameSiteLaxMode,
				})
			}

			printer := c.Printer(tag)

			// Content-Language tells caches and assistive technology which
			// language they are looking at, and it is the header that makes a
			// translated page correct rather than merely translated.
			w.Header().Set("Content-Language", tag.String())

			// Vary, or a shared cache serves one visitor's language to
			// everyone. This is the bug that makes a site randomly Swedish.
			w.Header().Add("Vary", "Accept-Language")

			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), contextKey{}, printer)))
		})
	}
}

// From returns the printer for a request.
//
// It never returns nil. A context with no printer -- a background job, a test,
// a handler mounted outside the middleware -- gets one for the fallback locale,
// so translating is never a nil check.
func From(ctx context.Context) *Printer {
	if printer, ok := ctx.Value(contextKey{}).(*Printer); ok && printer != nil {
		return printer
	}
	return defaultCatalogue.Printer(defaultCatalogue.fallback)
}

// WithPrinter puts a printer in a context, for jobs and tests.
func WithPrinter(ctx context.Context, p *Printer) context.Context {
	return context.WithValue(ctx, contextKey{}, p)
}

// defaultCatalogue backs From when nothing was configured, and is where the
// framework's own messages live.
//
// A package-level default is usually a smell. Here it is what lets `validator`
// and the admin panel emit keys without every one of them taking a catalogue
// argument, and an application that configures its own replaces it once at
// start-up.
var defaultCatalogue = New(language.English)

// Default returns the catalogue the framework's own messages use.
func Default() *Catalogue { return defaultCatalogue }

// SetDefault replaces it. Call once, at start-up, before serving.
func SetDefault(c *Catalogue) { defaultCatalogue = c }

// T translates using the default catalogue's fallback locale.
//
// For the places that have no request -- a job, a CLI command, a start-up
// error. Anything serving a user should use From(ctx).T.
func T(key string, args ...any) string {
	return defaultCatalogue.Printer(defaultCatalogue.fallback).T(key, args...)
}
