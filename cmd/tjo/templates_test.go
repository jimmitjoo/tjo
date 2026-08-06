package main

import (
	"bytes"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
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

// TestLoginTemplatesRotateTheSession guards against session fixation in
// generated code. Logout renewed the session token; login did not, so an
// attacker who fixed a session ID on the victim's browser beforehand still
// held a valid authenticated session afterwards. Both places where
// authentication completes -- the plain login and the 2FA verification --
// must rotate before writing userID.
func TestLoginTemplatesRotateTheSession(t *testing.T) {
	files := map[string]string{
		"auth-handlers.go.txt": "templates/handlers/auth-handlers.go.txt",
		"totp-handlers.go.txt": "templates/handlers/totp-handlers.go.txt",
	}

	for name, path := range files {
		t.Run(name, func(t *testing.T) {
			data, err := templateFS.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			src := string(data)

			idx := strings.Index(src, `Session.Put(r.Context(), "userID"`)
			if idx < 0 {
				t.Skip("this template does not establish a session")
			}

			// Look back a short way for the renewal that must precede it.
			start := idx - 600
			if start < 0 {
				start = 0
			}

			if !strings.Contains(src[start:idx], "RenewToken") {
				t.Error("userID is written without renewing the session token first; " +
					"a fixed session ID would survive login")
			}
		})
	}
}

// TestGeneratedGoModMatchesFramework guards the scaffolded go.mod against the
// drift that shipped two releases. The pin sat at v0.5.4 -- a tag published
// before the gemquick to tjo rename, so its go.mod declares a different module
// path and every generated project failed `go mod tidy` with:
//
//	module declares its path as: github.com/jimmitjoo/gemquick
//	but was required as: github.com/jimmitjoo/tjo
//
// It stayed there because `make release` used GNU sed syntax that fails on
// macOS, so the rewrite silently never ran. The real guard is the CI job that
// scaffolds and builds (#29); this catches the cheap half without a network.
func TestGeneratedGoModMatchesFramework(t *testing.T) {
	data, err := templateFS.ReadFile("templates/go.mod.txt")
	if err != nil {
		t.Fatal(err)
	}
	tmpl := string(data)

	t.Run("go directive matches the framework", func(t *testing.T) {
		root, err := os.ReadFile("../../go.mod")
		if err != nil {
			t.Fatal(err)
		}

		want := goDirective(string(root))
		got := goDirective(tmpl)

		if want == "" {
			t.Fatal("could not read the framework's go directive")
		}
		if got != want {
			t.Errorf("template targets go %s, framework requires go %s", got, want)
		}
	})

	t.Run("does not pin a version that cannot resolve", func(t *testing.T) {
		// v0.5.4 and earlier declare module github.com/jimmitjoo/gemquick.
		// v0.6.0 and v0.6.1 require their submodules at 000000000000
		// placeholders that consumers cannot resolve. See #25 and #26.
		unresolvable := []string{"v0.5.4", "v0.6.0", "v0.6.1"}

		for _, bad := range unresolvable {
			if strings.Contains(tmpl, "github.com/jimmitjoo/tjo "+bad) {
				t.Errorf("template pins %s, which no consumer can resolve", bad)
			}
		}
	})
}

// goDirective extracts the version from a go.mod's `go` line.
func goDirective(mod string) string {
	for _, line := range strings.Split(mod, "\n") {
		if strings.HasPrefix(line, "go ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "go "))
		}
	}
	return ""
}

// Every string a generated application shows a user is a key, not English.
//
// Grep is the test, as #83 asked for. A flash message written as a literal
// compiles perfectly and is invisible until somebody reads the application in
// a language it was not written in.
func TestGeneratedAuthHandlersContainNoEnglishMessages(t *testing.T) {
	// The literals that used to be here, and the ones most likely to come back.
	banned := []string{
		"These credentials do not match",
		"Your account is not active",
		"You have been registered",
		"has been activated",
		"a reset link is on its way",
		"Password reset. You can now log in",
		"Invalid verification code",
		"Invalid recovery code",
		"Setup session expired",
		"Two-factor authentication has been",
	}

	for _, name := range []string{
		"templates/handlers/auth-handlers.go.txt",
		"templates/handlers/totp-handlers.go.txt",
	} {
		content, err := templateFS.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, phrase := range banned {
			if bytes.Contains(content, []byte(phrase)) {
				t.Errorf("%s contains the literal %q -- it should be a translation key", name, phrase)
			}
		}
		if !bytes.Contains(content, []byte("i18n.From(")) {
			t.Errorf("%s does not translate anything", name)
		}
	}
}

// The login view is what #83's definition of done is measured on, so its
// labels are keys rather than words.
func TestTheLoginViewIsTranslatable(t *testing.T) {
	content, err := templateFS.ReadFile("templates/views/login.jet")
	if err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{"login.title", "login.email", "login.password", "login.submit"} {
		if !bytes.Contains(content, []byte(key)) {
			t.Errorf("login.jet does not use %q", key)
		}
	}
	// The English that used to be in it.
	for _, english := range []string{">Sign in<", ">Email address<", ">Password<"} {
		if bytes.Contains(content, []byte(english)) {
			t.Errorf("login.jet still contains the literal %q", english)
		}
	}
}
