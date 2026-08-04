package main

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path"
	"regexp"
	"strings"
	"testing"
)

// Template .go.txt files are never compiled: they are embedded data, so neither
// `go build` nor `go vet` nor CI ever looks at them. Both defects in issue #14
// -- a middleware that never called next.ServeHTTP, and an unguarded
// strings.Split index that panicked on any malformed cookie -- shipped that way.
// These tests are the only thing standing between a broken template and a
// scaffolded project that does not work.

func goTemplateFiles(t *testing.T) []string {
	t.Helper()

	var files []string
	err := fs.WalkDir(templateFS, "templates", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, ".go.txt") {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no .go.txt templates found; this test is not checking anything")
	}
	return files
}

// placeholderPattern matches the $FOO$ markers the scaffolder substitutes.
var placeholderPattern = regexp.MustCompile(`\$[A-Z_]+\$`)

// substitutePlaceholders fills in every marker so the result is parseable Go.
// $APPURL$ appears in import paths and needs a path; the rest stand in for
// identifiers, and an identifier is also valid inside the string literals and
// comments some of them land in.
func substitutePlaceholders(src string) string {
	src = strings.ReplaceAll(src, "$APPURL$", "example.com/app")
	return placeholderPattern.ReplaceAllString(src, "Placeholder")
}

func TestGoTemplatesParse(t *testing.T) {
	for _, file := range goTemplateFiles(t) {
		t.Run(path.Base(file), func(t *testing.T) {
			data, err := templateFS.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}

			src := substitutePlaceholders(string(data))
			if _, err := parser.ParseFile(token.NewFileSet(), file, src, parser.AllErrors); err != nil {
				t.Errorf("template does not parse as Go: %v", err)
			}
		})
	}
}

func TestMiddlewareTemplatesCallNext(t *testing.T) {
	for _, file := range goTemplateFiles(t) {
		if !strings.Contains(file, "/middleware/") {
			continue
		}

		t.Run(path.Base(file), func(t *testing.T) {
			data, err := templateFS.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}

			src := string(data)
			if !strings.Contains(src, "next http.Handler") {
				t.Skip("not a wrapping middleware")
			}

			if !strings.Contains(src, "next.ServeHTTP") {
				t.Error("wrapping middleware never calls next.ServeHTTP; every route behind it returns an empty response")
			}
		})
	}
}
