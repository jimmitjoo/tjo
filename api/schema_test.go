package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// schemaJSON renders the schema for a value, with its components, so a test can
// assert on the whole thing rather than on a tree of pointers.
func schemaJSON(t *testing.T, v any) (string, map[string]*Schema) {
	t.Helper()

	set := newSchemaSet()
	schema := set.schemaFor(v)

	body, err := json.Marshal(struct {
		Schema     *Schema            `json:"schema"`
		Components map[string]*Schema `json:"components,omitempty"`
	}{schema, set.byName})
	if err != nil {
		t.Fatal(err)
	}

	return string(body), set.byName
}

// A type that refers to itself has no finite inline expansion. The $ref is what
// stops the generator recursing until the stack runs out.
func TestASelfReferentialTypeTerminates(t *testing.T) {
	type Comment struct {
		Body    string     `json:"body"`
		Replies []*Comment `json:"replies,omitempty"`
	}

	done := make(chan string, 1)
	go func() {
		body, _ := schemaJSON(t, Comment{})
		done <- body
	}()

	select {
	case body := <-done:
		if !strings.Contains(body, `"$ref":"#/components/schemas/Comment"`) {
			t.Fatalf("the recursion is not broken by a reference:\n%s", body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("generating a schema for a self-referential type did not finish")
	}
}

// The generator has to see the JSON encoding/json produces, not the Go struct.
func TestTheSchemaFollowsTheJSONTags(t *testing.T) {
	type Inner struct {
		Shared string `json:"shared"`
	}
	type Outer struct {
		Inner                   // embedded: encoding/json promotes its fields
		Renamed  string         `json:"renamed"`
		Optional string         `json:"optional,omitempty"`
		Skipped  string         `json:"-"`
		Untagged string         // keeps its Go name
		Pointer  *string        `json:"pointer"`
		Raw      []byte         `json:"raw"`
		Numbers  []int          `json:"numbers"`
		Lookup   map[string]int `json:"lookup"`
		Anything any            `json:"anything"`
		When     time.Time      `json:"when"`
		hidden   string
	}

	body, _ := schemaJSON(t, Outer{})

	var doc struct {
		Components map[string]*Schema `json:"components"`
	}
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatal(err)
	}

	schema := doc.Components["Outer"]
	if schema == nil {
		t.Fatalf("no Outer component in %s", body)
	}

	for _, absent := range []string{"Skipped", "hidden", "Inner"} {
		if _, present := schema.Properties[absent]; present {
			t.Errorf("%q is in the schema and not in the JSON", absent)
		}
	}
	for _, present := range []string{"shared", "renamed", "optional", "Untagged", "pointer", "raw", "numbers", "lookup", "anything", "when"} {
		if _, ok := schema.Properties[present]; !ok {
			t.Errorf("%q is in the JSON and not in the schema", present)
		}
	}

	// Required is everything encoding/json writes unconditionally: not
	// omitempty, not a pointer.
	required := strings.Join(schema.Required, ",")
	for _, want := range []string{"shared", "renamed", "Untagged", "raw", "when"} {
		if !strings.Contains(required, want) {
			t.Errorf("%q is written unconditionally and is not required", want)
		}
	}
	for _, notRequired := range []string{"optional", "pointer"} {
		if strings.Contains(required, notRequired) {
			t.Errorf("%q may be absent and is required", notRequired)
		}
	}

	// The types that are not what the Go kind suggests.
	if s := schema.Properties["when"]; s.Type != "string" || s.Format != "date-time" {
		t.Errorf("time.Time is %+v", s)
	}
	if s := schema.Properties["raw"]; s.Type != "string" || s.ContentEncoding != "base64" {
		t.Errorf("[]byte is %+v, and encoding/json writes it as a base64 string", s)
	}
	if s := schema.Properties["lookup"]; s.Type != "object" || s.AdditionalProperties == nil {
		t.Errorf("a map is %+v", s)
	}
	if s := schema.Properties["anything"]; s.Type != nil {
		t.Errorf("any is %+v, and should be the empty schema", s)
	}
}

// 3.1 is JSON Schema 2020-12, where a nullable type is a type array. `nullable`
// was 3.0 and has no meaning here at all.
func TestNullableIsATypeArrayAndNeverTheKeyword(t *testing.T) {
	type Thing struct {
		Name *string `json:"name"`
	}

	body, _ := schemaJSON(t, Thing{})

	if strings.Contains(body, "nullable") {
		t.Errorf("the schema uses 3.0's nullable:\n%s", body)
	}
	if !strings.Contains(body, `"type":["string","null"]`) {
		t.Errorf("a pointer is not described as nullable:\n%s", body)
	}
}

// Two packages exporting the same type name must not collide into one
// component, which would describe one of them as the other.
//
// Two same-named types cannot both exist in one test package, so the collision
// is staged directly: the name is taken before the type that wants it arrives,
// which is exactly the state uniqueName exists to resolve.
func TestASameNamedTypeGetsItsOwnComponent(t *testing.T) {
	set := newSchemaSet()
	set.byName["Invoice"] = &Schema{Type: "object"} // some other package's

	schema := set.schemaFor(Invoice{})

	if schema.Ref == "#/components/schemas/Invoice" {
		t.Fatal("the second Invoice overwrote the first")
	}
	if schema.Ref != "#/components/schemas/apiInvoice" {
		t.Fatalf("the second Invoice became %q, and a reader cannot tell which package it is", schema.Ref)
	}
	if len(set.byName) != 2 {
		t.Fatalf("components are %v", keysOf(set.byName))
	}
}

// checkInstance is only ever pointed at schemas this file produced, so its job
// is to agree with them exactly.
func TestCheckInstanceAgreesWithTheGenerator(t *testing.T) {
	set := newSchemaSet()
	schema := set.schemaFor(Envelope[[]Invoice]{})

	good := `{"success":true,"data":[{"id":1,"number":"INV-1","total":1.5,"paid":false,"due_at":"1970-01-01T00:00:00Z"}],"timestamp":1}`

	var value any
	if err := json.Unmarshal([]byte(good), &value); err != nil {
		t.Fatal(err)
	}
	if err := checkInstance(value, schema, set.byName, "body"); err != nil {
		t.Fatalf("rejected what the generator describes: %v", err)
	}

	bad := map[string]string{
		"a null where a string was declared":   `{"success":true,"data":[{"id":1,"number":null,"total":1,"paid":false,"due_at":"x"}],"timestamp":1}`,
		"a missing required field":             `{"success":true,"data":[{"id":1,"total":1,"paid":false,"due_at":"x"}],"timestamp":1}`,
		"an object where a list was declared":  `{"success":true,"data":{"id":1},"timestamp":1}`,
		"a string where a number was declared": `{"success":true,"data":[{"id":"one","number":"n","total":1,"paid":false,"due_at":"x"}],"timestamp":1}`,
	}

	for name, body := range bad {
		t.Run(name, func(t *testing.T) {
			var value any
			if err := json.Unmarshal([]byte(body), &value); err != nil {
				t.Fatal(err)
			}
			if err := checkInstance(value, schema, set.byName, "body"); err == nil {
				t.Error("accepted")
			}
		})
	}
}

// Generic type names arrive from reflect full of characters a $ref cannot hold.
func TestGenericTypeNamesBecomeUsableComponentNames(t *testing.T) {
	cases := map[string]string{
		"Invoice": "Invoice",
		"Envelope[github.com/jimmitjoo/tjo/api.Invoice]":   "EnvelopeOfInvoice",
		"Envelope[[]github.com/jimmitjoo/tjo/api.Invoice]": "EnvelopeOfInvoiceList",
		"Envelope[*github.com/jimmitjoo/tjo/api.Invoice]":  "EnvelopeOfInvoice",
		"Pair[string,int]": "PairOfstringint",
	}

	for name, want := range cases {
		if got := componentName(name); got != want {
			t.Errorf("%s -> %s, want %s", name, got, want)
		}
	}
}
