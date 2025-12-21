package tjo

import (
	"net/http"

	"github.com/justinas/nosurf"
)

func (g *Tjo) SessionLoad(next http.Handler) http.Handler {
	if g.Logging != nil && g.Logging.Info != nil {
		g.Logging.Info.Println("SessionLoad called")
	}
	return g.HTTP.Session.LoadAndSave(next)
}

func (g *Tjo) NoSurf(next http.Handler) http.Handler {
	csrfHandler := nosurf.New(next)

	// Exempt API from CSRF protection:
	csrfHandler.ExemptGlob("/api/*")

	csrfHandler.SetBaseCookie(http.Cookie{
		HttpOnly: true,
		Path:     "/",
		Secure:   g.Config.Cookie.Secure,
		SameSite: http.SameSiteStrictMode,
		Domain:   g.Config.Cookie.Domain,
	})

	return csrfHandler
}
