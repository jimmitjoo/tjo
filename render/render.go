package render

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"text/template"

	"github.com/CloudyKit/jet/v6"
	"github.com/alexedwards/scs/v2"
	"github.com/justinas/nosurf"
)

type Render struct {
	Renderer   string
	RootPath   string
	Secure     bool
	Port       string
	ServerName string
	JetViews   *jet.Set
	Session    *scs.SessionManager
}

// TemplateData holds data that is automatically passed to all templates.
// These variables are available in your Jet or Go templates:
//
// Automatically Populated (via defaultData):
//   - CSRFToken: CSRF protection token for forms. Use in forms like:
//     <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
//   - Flash: One-time flash message from session (auto-removed after display)
//   - Error: One-time error message from session (auto-removed after display)
//   - IsAuthenticated: True if "userID" exists in session
//   - Secure: Whether the connection is HTTPS
//   - ServerName: The server name from configuration
//   - Port: The port the server is running on
//
// User-Provided Data Maps:
//   - IntMap: For passing integer values to templates
//   - StringMap: For passing string values to templates
//   - FloatMap: For passing float values to templates
//   - Data: For passing any other data to templates
//
// Example usage in handler:
//
//	td := &render.TemplateData{
//	    StringMap: map[string]string{"title": "My Page"},
//	    Data: map[string]interface{}{"users": users},
//	}
//	h.render(w, r, "users", nil, td)
//
// Example usage in template (Jet):
//
//	<h1>{{ .StringMap.title }}</h1>
//	{{ if .Flash }}<div class="alert">{{ .Flash }}</div>{{ end }}
//	{{ if .Error }}<div class="error">{{ .Error }}</div>{{ end }}
//	<form>
//	    <input type="hidden" name="csrf_token" value="{{ .CSRFToken }}">
//	</form>
type TemplateData struct {
	IsAuthenticated bool                   // True if user is logged in (userID in session)
	IntMap          map[string]int         // Integer values to pass to template
	StringMap       map[string]string      // String values to pass to template
	FloatMap        map[string]float32     // Float values to pass to template
	Data            map[string]interface{} // Any other data to pass to template
	CSRFToken       string                 // CSRF protection token for forms
	Port            string                 // Server port
	ServerName      string                 // Server name
	Secure          bool                   // True if HTTPS
	Error           string                 // One-time error message (from session)
	Flash           string                 // One-time flash message (from session)
}

func (g *Render) defaultData(td *TemplateData, r *http.Request) *TemplateData {

	td.Secure = g.Secure
	td.ServerName = g.ServerName
	td.Port = g.Port
	td.CSRFToken = nosurf.Token(r)

	if g.Session != nil {
		if g.Session.Exists(r.Context(), "userID") {
			td.IsAuthenticated = true
		}

		td.Error = g.Session.PopString(r.Context(), "error")
		td.Flash = g.Session.PopString(r.Context(), "flash")
	}

	return td
}

func (g *Render) Page(w http.ResponseWriter, r *http.Request, view string, variables, data interface{}) error {

	switch strings.ToLower(g.Renderer) {
	case "go":
		return g.GoPage(w, r, view, data)
	case "jet":
		return g.JetPage(w, r, view, variables, data)
	default:

	}

	return errors.New("no rendering engine specified")
}

// GoPage renders a standard Go template
func (g *Render) GoPage(w http.ResponseWriter, r *http.Request, view string, data interface{}) error {

	tmpl, err := template.ParseFiles(fmt.Sprintf("%s/views/%s.page.tmpl", g.RootPath, view))

	if err != nil {
		return err
	}

	td := &TemplateData{}
	if data != nil {
		td = data.(*TemplateData)
	}

	err = tmpl.Execute(w, &td)

	if err != nil {
		return err
	}

	return nil
}

// JetPage renders a template using the jet templating language
func (g *Render) JetPage(w http.ResponseWriter, r *http.Request, templateName string, variables, data interface{}) error {
	var vars jet.VarMap

	if variables == nil {
		vars = make(jet.VarMap)
	} else {
		vars = variables.(jet.VarMap)
	}

	td := &TemplateData{}
	if data != nil {
		td = data.(*TemplateData)
	}

	td = g.defaultData(td, r)

	t, err := g.JetViews.GetTemplate(fmt.Sprintf("%s.jet", templateName))
	if err != nil {
		log.Println(err)
		return err
	}

	if err = t.Execute(w, vars, td); err != nil {
		log.Println(err)
		return err
	}

	return nil
}
