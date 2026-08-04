package render

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

var pageData = []struct {
	name          string
	renderer      string
	template      string
	errorExpected bool
	errorMessage  string
}{
	{"go_page", "go", "home", false, "error rendering go template"},
	{"go_page_no_template", "go", "no-file", true, "no error while rendering non-existent template, when one is expected"},
	{"jet_page", "jet", "home", false, "error rendering jet template"},
	{"jet_page_no_template", "jet", "no-file", true, "no error while rendering non-existent template, when one is expected"},
	{"invalid_renderer_engine", "foo", "homr", true, "no error rendering with non-existent template engine"},
}

func TestRender_Page(t *testing.T) {

	for _, e := range pageData {
		r, err := http.NewRequest("GET", "/some-url", nil)

		if err != nil {
			t.Error(err)
		}

		w := httptest.NewRecorder()

		testRenderer.Renderer = e.renderer
		testRenderer.RootPath = "./testdata"

		err = testRenderer.Page(w, r, e.template, nil, nil)

		if e.errorExpected {
			if err == nil {
				t.Errorf("%s: %s", e.name, e.errorMessage)
			}
		} else {
			if err != nil {
				t.Errorf("%s: %s: %s", e.name, e.errorMessage, err.Error())
			}
		}
	}
}

func TestRender_GoPage(t *testing.T) {
	w := httptest.NewRecorder()
	r, err := http.NewRequest("GET", "/url", nil)

	if err != nil {
		t.Error(err)
	}

	testRenderer.Renderer = "go"
	testRenderer.RootPath = "./testdata"

	err = testRenderer.Page(w, r, "home", nil, nil)

	if err != nil {
		t.Error("Error rendering page", err)
	}
}

func TestRender_JetPage(t *testing.T) {
	w := httptest.NewRecorder()
	r, err := http.NewRequest("GET", "/url", nil)

	if err != nil {
		t.Error(err)
	}

	testRenderer.Renderer = "jet"
	testRenderer.RootPath = "./testdata"

	err = testRenderer.Page(w, r, "home", nil, nil)

	if err != nil {
		t.Error("Error rendering page", err)
	}
}

// TestRender_GoPageEscapesOutput guards against a regression to text/template,
// which performs no contextual escaping and made every interpolated value an
// XSS sink. See GHSA-2w6x-c7q3-qcgr.
func TestRender_GoPageEscapesOutput(t *testing.T) {
	const payload = `<script>alert(1)</script>`

	r, err := http.NewRequest("GET", "/some-url", nil)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()

	testRenderer.Renderer = "go"
	testRenderer.RootPath = "./testdata"

	td := &TemplateData{StringMap: map[string]string{"title": payload}}
	if err := testRenderer.Page(w, r, "home", nil, td); err != nil {
		t.Fatal(err)
	}

	body := w.Body.String()
	if strings.Contains(body, payload) {
		t.Errorf("unescaped payload in output: %s", body)
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Errorf("expected HTML-escaped payload, got: %s", body)
	}
}

// TestRender_RejectsWrongDataType covers issue #15. Both renderers did an
// unchecked type assertion on data, so passing anything other than
// *TemplateData panicked the request instead of returning an error.
func TestRender_RejectsWrongDataType(t *testing.T) {
	for _, renderer := range []string{"go", "jet"} {
		t.Run(renderer, func(t *testing.T) {
			r, err := http.NewRequest("GET", "/some-url", nil)
			if err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()

			testRenderer.Renderer = renderer
			testRenderer.RootPath = "./testdata"

			assert.NotPanics(t, func() {
				err = testRenderer.Page(w, r, "home", nil, map[string]string{"nope": "x"})
			})
			assert.Error(t, err, "wrong data type should be an error, not a panic")
		})
	}
}

// TestRender_GoPagePopulatesDefaultData covers the other half of #15: GoPage
// never called defaultData, so .CSRFToken was empty in every Go template and
// the documented hidden form input rendered blank.
func TestRender_GoPagePopulatesDefaultData(t *testing.T) {
	r, err := http.NewRequest("GET", "/some-url", nil)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()

	testRenderer.Renderer = "go"
	testRenderer.RootPath = "./testdata"
	testRenderer.ServerName = "probe-server"

	td := &TemplateData{}
	if err := testRenderer.Page(w, r, "home", nil, td); err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, "probe-server", td.ServerName,
		"GoPage did not run defaultData over the template data")
}
