package api

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
)

// Go types to JSON Schema, and back again to check an instance against one.
//
// OpenAPI 3.1's schemas *are* JSON Schema 2020-12 -- that is the headline
// change from 3.0, and it is why nothing here emits `nullable: true`, which was
// a 3.0-ism with no equivalent. A nullable type in 3.1 is a type array:
// `"type": ["string", "null"]`.
//
// The emitted subset is deliberately small: objects, arrays, primitives,
// $refs, a few formats. Small enough that checkInstance below can check a
// response against it in a hundred lines rather than by pulling in a JSON
// Schema validator, and small enough that every generator branch has a test.

// Schema is a JSON Schema 2020-12 schema, in the subset this package emits.
type Schema struct {
	Ref string `json:"$ref,omitempty"`

	// Type is a string, or a two-element array for a nullable type.
	Type any `json:"type,omitempty"`

	Format string `json:"format,omitempty"`

	// ContentEncoding is how 3.1 says "these bytes are base64", replacing
	// 3.0's `format: byte`.
	ContentEncoding string `json:"contentEncoding,omitempty"`

	Description string `json:"description,omitempty"`

	Items      *Schema            `json:"items,omitempty"`
	Properties map[string]*Schema `json:"properties,omitempty"`
	Required   []string           `json:"required,omitempty"`

	AdditionalProperties *Schema `json:"additionalProperties,omitempty"`

	Enum    []any `json:"enum,omitempty"`
	Example any   `json:"example,omitempty"`
}

// schemaSet collects the named schemas an operation refers to.
//
// Named types become components and are referred to by $ref; anonymous ones are
// inlined. That is not only tidiness: a self-referential type -- a comment with
// replies -- has no finite inline expansion, and the $ref is what terminates
// the recursion.
type schemaSet struct {
	byName map[string]*Schema
	byType map[reflect.Type]string
}

func newSchemaSet() *schemaSet {
	return &schemaSet{
		byName: map[string]*Schema{},
		byType: map[reflect.Type]string{},
	}
}

var timeType = reflect.TypeOf(time.Time{})

// schemaFor returns the schema for a value's type, registering named structs as
// components along the way.
func (s *schemaSet) schemaFor(v any) *Schema {
	if v == nil {
		return &Schema{}
	}
	return s.fromType(reflect.TypeOf(v))
}

func (s *schemaSet) fromType(t reflect.Type) *Schema {
	// A pointer is the same schema, and also null. Unwrapped first so that
	// *time.Time and **T behave.
	if t.Kind() == reflect.Ptr {
		return nullable(s.fromType(t.Elem()))
	}

	if t == timeType {
		return &Schema{Type: "string", Format: "date-time"}
	}

	switch t.Kind() {
	case reflect.Bool:
		return &Schema{Type: "boolean"}

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		// int64 is annotated because JSON numbers are doubles, and a consumer
		// generating a JavaScript client needs to know which of these will not
		// survive the trip.
		if t.Kind() == reflect.Int64 || t.Kind() == reflect.Uint64 {
			return &Schema{Type: "integer", Format: "int64"}
		}
		return &Schema{Type: "integer", Format: "int32"}

	case reflect.Float32, reflect.Float64:
		return &Schema{Type: "number"}

	case reflect.String:
		return &Schema{Type: "string"}

	case reflect.Slice, reflect.Array:
		// encoding/json writes []byte as a base64 string, not as an array of
		// numbers, and a document that said otherwise would be wrong about
		// every request body carrying one.
		if t.Elem().Kind() == reflect.Uint8 && t.Kind() == reflect.Slice {
			return &Schema{Type: "string", ContentEncoding: "base64"}
		}
		// A nil slice marshals to null, so an array is nullable unless the
		// field says omitempty -- which objectSchema handles.
		return &Schema{Type: "array", Items: s.fromType(t.Elem())}

	case reflect.Map:
		if t.Key().Kind() != reflect.String {
			// encoding/json can marshal integer keys, but the schema for it is
			// a lie either way: describe it as an open object.
			return &Schema{Type: "object"}
		}
		return &Schema{Type: "object", AdditionalProperties: s.fromType(t.Elem())}

	case reflect.Interface:
		// The empty schema, which in JSON Schema means "anything". Emitted as
		// {} rather than omitted, because an absent schema means something
		// different to a generator than an unconstrained one.
		return &Schema{}

	case reflect.Struct:
		return s.structSchema(t)

	default:
		// Channels, functions, complex numbers: encoding/json refuses these, so
		// a field holding one is a bug rather than a documentation problem.
		return &Schema{}
	}
}

// structSchema registers a named struct as a component and returns a $ref to
// it. Anonymous structs are inlined.
func (s *schemaSet) structSchema(t reflect.Type) *Schema {
	if t.Name() == "" {
		return s.objectSchema(t)
	}

	if name, ok := s.byType[t]; ok {
		return &Schema{Ref: "#/components/schemas/" + name}
	}

	name := s.uniqueName(t)

	// Registered before the fields are walked, so a type that refers to itself
	// finds the name rather than recursing forever.
	s.byType[t] = name
	s.byName[name] = nil
	s.byName[name] = s.objectSchema(t)

	return &Schema{Ref: "#/components/schemas/" + name}
}

// uniqueName picks a component name, disambiguating by package when two
// packages export the same type name.
func (s *schemaSet) uniqueName(t reflect.Type) string {
	name := componentName(t.Name())

	if _, taken := s.byName[name]; !taken {
		return name
	}

	if pkg := t.PkgPath(); pkg != "" {
		qualified := componentName(pkg[strings.LastIndex(pkg, "/")+1:]) + name
		if _, taken := s.byName[qualified]; !taken {
			return qualified
		}
	}

	for n := 2; ; n++ {
		numbered := fmt.Sprintf("%s%d", name, n)
		if _, taken := s.byName[numbered]; !taken {
			return numbered
		}
	}
}

// objectSchema walks a struct's exported fields the way encoding/json does.
func (s *schemaSet) objectSchema(t reflect.Type) *Schema {
	schema := &Schema{Type: "object", Properties: map[string]*Schema{}}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		if field.PkgPath != "" && !field.Anonymous {
			continue // unexported
		}

		tag := field.Tag.Get("json")
		name, opts, _ := strings.Cut(tag, ",")

		if name == "-" && opts == "" {
			continue
		}

		// An embedded struct with no name of its own has its fields promoted
		// into the parent, which is what encoding/json does and what a reader
		// of the JSON sees.
		if field.Anonymous && name == "" {
			embedded := field.Type
			for embedded.Kind() == reflect.Ptr {
				embedded = embedded.Elem()
			}
			if embedded.Kind() == reflect.Struct && embedded != timeType {
				promoted := s.objectSchema(embedded)
				for property, sub := range promoted.Properties {
					schema.Properties[property] = sub
				}
				schema.Required = append(schema.Required, promoted.Required...)
				continue
			}
		}

		if name == "" {
			name = field.Name
		}

		property := s.fromType(field.Type)
		if description := field.Tag.Get("doc"); description != "" {
			property = withDescription(property, description)
		}
		schema.Properties[name] = property

		// omitempty means the field may be absent; a pointer means it may be
		// null. Neither is required, and everything else is: encoding/json
		// writes it unconditionally.
		if !strings.Contains(opts, "omitempty") && field.Type.Kind() != reflect.Ptr {
			schema.Required = append(schema.Required, name)
		}
	}

	sort.Strings(schema.Required)
	if len(schema.Properties) == 0 {
		schema.Properties = nil
	}

	return schema
}

// withDescription attaches a description without mutating a shared $ref schema.
func withDescription(s *Schema, description string) *Schema {
	copied := *s
	copied.Description = description
	return &copied
}

// nullable turns a schema into one that also accepts null.
//
// A $ref cannot carry a sibling type in JSON Schema 2020-12 in the way a plain
// schema can, so a nullable reference is left as the reference: saying "this or
// null" would mean emitting a oneOf, and a generator reading a bare $ref
// already treats the value as possibly absent.
func nullable(s *Schema) *Schema {
	if s.Ref != "" {
		return s
	}

	copied := *s
	switch existing := copied.Type.(type) {
	case string:
		copied.Type = []string{existing, "null"}
	case nil:
		// The empty schema already accepts null.
	}
	return &copied
}

// componentName strips a Go type name down to something a $ref can hold, which
// matters for generics: reflect names them
// "Envelope[github.com/jimmitjoo/tjo/api.Invoice]", and neither the slashes nor
// the brackets belong in a component key.
func componentName(name string) string {
	base, args, generic := strings.Cut(name, "[")
	if !generic {
		return sanitise(name)
	}

	out := sanitise(base) + "Of"

	for _, arg := range strings.Split(strings.TrimSuffix(args, "]"), ",") {
		arg = strings.TrimSpace(arg)

		list := strings.HasPrefix(arg, "[]")
		arg = strings.TrimPrefix(arg, "[]")
		arg = strings.TrimPrefix(arg, "*")

		if i := strings.LastIndexAny(arg, "./"); i >= 0 {
			arg = arg[i+1:]
		}

		out += sanitise(arg)
		if list {
			out += "List"
		}
	}

	return out
}

func sanitise(s string) string {
	var b strings.Builder
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_':
			b.WriteRune(c)
		}
	}
	return b.String()
}

// checkInstance reports whether a decoded JSON value satisfies a schema.
//
// It understands exactly the subset above and nothing else, which is the whole
// reason it can exist without a JSON Schema library: it is only ever pointed at
// schemas this file produced. CheckResponse is what uses it, to catch a handler
// whose response has drifted from what its operation declared.
func checkInstance(value any, schema *Schema, components map[string]*Schema, path string) error {
	if schema == nil {
		return nil
	}

	if schema.Ref != "" {
		name := strings.TrimPrefix(schema.Ref, "#/components/schemas/")
		resolved, ok := components[name]
		if !ok {
			return fmt.Errorf("%s: the schema refers to %s, which is not in the document", path, schema.Ref)
		}
		// A $ref stands in for a possibly-null value; see nullable.
		if value == nil {
			return nil
		}
		return checkInstance(value, resolved, components, path)
	}

	types := typeNames(schema.Type)
	if len(types) == 0 {
		return nil // the empty schema accepts anything
	}

	actual := jsonTypeOf(value)
	if !matchesAny(actual, types) {
		return fmt.Errorf("%s: declared %s, got %s", path, strings.Join(types, " or "), actual)
	}

	switch actual {
	case "object":
		object := value.(map[string]any)

		for _, required := range schema.Required {
			if _, present := object[required]; !present {
				return fmt.Errorf("%s: declares %q required and the response has no such field", path, required)
			}
		}

		for key, sub := range object {
			property, declared := schema.Properties[key]
			if !declared {
				if schema.AdditionalProperties != nil {
					property = schema.AdditionalProperties
				} else if len(schema.Properties) > 0 {
					return fmt.Errorf("%s: the response has %q, which the schema does not declare", path, key)
				} else {
					continue
				}
			}
			if err := checkInstance(sub, property, components, path+"."+key); err != nil {
				return err
			}
		}

	case "array":
		for i, item := range value.([]any) {
			if err := checkInstance(item, schema.Items, components, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	}

	return nil
}

func typeNames(t any) []string {
	switch v := t.(type) {
	case string:
		return []string{v}
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, each := range v {
			if name, ok := each.(string); ok {
				out = append(out, name)
			}
		}
		return out
	default:
		return nil
	}
}

func matchesAny(actual string, declared []string) bool {
	for _, want := range declared {
		if actual == want {
			return true
		}
		// Every JSON number decodes as a float64; an integer schema is
		// satisfied by one with no fractional part.
		if want == "integer" && actual == "number" {
			return true
		}
	}
	return false
}

// jsonTypeOf names a decoded JSON value's type.
func jsonTypeOf(v any) string {
	switch value := v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64:
		return "number"
	case json.Number:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return fmt.Sprintf("%T", value)
	}
}
