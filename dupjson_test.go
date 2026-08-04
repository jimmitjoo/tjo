package tjo

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestReadJsonRejectsDuplicateKeys pins the strictness encoding/json does not
// provide and is not going to.
//
// json.Unmarshal silently takes the last occurrence of a repeated member, so
// {"role":"user","role":"admin"} decodes to "admin" with no error. Whenever
// anything else in the request path parses the same body -- a proxy, a WAF, an
// audit log -- and resolves the conflict the other way, the two disagree about
// what the request said.
//
// encoding/json/v2 rejects duplicates, but Go 1.27 keeps v1's semantics behind
// the v1 API deliberately, so this does not arrive on its own. Measured under
// GOEXPERIMENT=jsonv2 before writing this: the v1 API still accepted it.
func TestReadJsonRejectsDuplicateKeys(t *testing.T) {
	g := &Tjo{}

	t.Run("top level", func(t *testing.T) {
		var got struct {
			Role string `json:"role"`
		}
		r := httptest.NewRequest("POST", "/", strings.NewReader(`{"role":"user","role":"admin"}`))

		err := g.ReadJson(httptest.NewRecorder(), r, &got)
		if !errors.Is(err, ErrDuplicateJSONKey) {
			t.Fatalf("err = %v, want ErrDuplicateJSONKey (decoded role=%q)", err, got.Role)
		}
	})

	t.Run("nested object", func(t *testing.T) {
		var got map[string]any
		r := httptest.NewRequest("POST", "/", strings.NewReader(`{"user":{"id":1,"id":2}}`))

		if err := g.ReadJson(httptest.NewRecorder(), r, &got); !errors.Is(err, ErrDuplicateJSONKey) {
			t.Fatalf("err = %v, want ErrDuplicateJSONKey", err)
		}
	})

	// The same name in sibling objects is not a duplicate, and rejecting it
	// would break every ordinary payload.
	t.Run("same name in sibling objects is fine", func(t *testing.T) {
		var got map[string]any
		r := httptest.NewRequest("POST", "/", strings.NewReader(`{"a":{"id":1},"b":{"id":2}}`))

		if err := g.ReadJson(httptest.NewRecorder(), r, &got); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	// A repeated name inside an array element must still be caught.
	t.Run("inside an array", func(t *testing.T) {
		var got map[string]any
		r := httptest.NewRequest("POST", "/", strings.NewReader(`{"items":[{"k":1,"k":2}]}`))

		if err := g.ReadJson(httptest.NewRecorder(), r, &got); !errors.Is(err, ErrDuplicateJSONKey) {
			t.Fatalf("err = %v, want ErrDuplicateJSONKey", err)
		}
	})

	// A string that happens to equal an earlier key, used as a value, is not a key.
	t.Run("values are not mistaken for keys", func(t *testing.T) {
		var got map[string]any
		r := httptest.NewRequest("POST", "/", strings.NewReader(`{"role":"role","other":"role"}`))

		if err := g.ReadJson(httptest.NewRecorder(), r, &got); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("ordinary payloads still decode", func(t *testing.T) {
		var got struct {
			Name string   `json:"name"`
			Tags []string `json:"tags"`
		}
		r := httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"a","tags":["x","y"]}`))

		if err := g.ReadJson(httptest.NewRecorder(), r, &got); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Name != "a" || len(got.Tags) != 2 {
			t.Fatalf("decoded wrong: %+v", got)
		}
	})

	t.Run("trailing content is still rejected", func(t *testing.T) {
		var got map[string]any
		r := httptest.NewRequest("POST", "/", strings.NewReader(`{"a":1}{"b":2}`))

		if err := g.ReadJson(httptest.NewRecorder(), r, &got); err == nil {
			t.Fatal("expected an error for a second object")
		}
	})
}
