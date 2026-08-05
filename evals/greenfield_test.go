package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Detection is the measuring instrument, so it is the thing that has to be
// right. A miscount here is a published number that is wrong, which is worse
// than no number.
func TestDetectFindsTheFrameworkFromDependencies(t *testing.T) {
	tests := map[string]struct {
		files     map[string]string
		framework string
		language  string
	}{
		"gin from go.mod": {
			files: map[string]string{
				"go.mod":  "module todo\n\nrequire github.com/gin-gonic/gin v1.10.0\n",
				"main.go": `package main` + "\n" + `import "github.com/gin-gonic/gin"`,
			},
			framework: "Gin",
			language:  "Go",
		},
		"fastapi from requirements": {
			files: map[string]string{
				"requirements.txt": "fastapi==0.115.0\nuvicorn\n",
				"main.py":          "from fastapi import FastAPI\n",
			},
			framework: "FastAPI",
			language:  "Python",
		},
		"express from package.json": {
			files: map[string]string{
				"package.json": `{"dependencies":{"express":"^4.19.0"}}`,
				"index.js":     `const express = require("express")`,
			},
			framework: "Express",
			language:  "JavaScript",
		},
		// The interesting case, and the one Amplifying's study found most
		// often: the agent built it rather than reaching for anything.
		"standard library only": {
			files: map[string]string{
				"go.mod":  "module todo\n\ngo 1.25\n",
				"main.go": `package main` + "\n" + `import "net/http"`,
			},
			framework: "net/http only",
			language:  "Go",
		},
		"nothing at all": {
			files:     map[string]string{"README.md": "# Todo\n"},
			framework: "none",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			for path, content := range tt.files {
				if err := os.WriteFile(filepath.Join(dir, path), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			found, files, err := detect(dir)
			if err != nil {
				t.Fatal(err)
			}

			framework, language := primary(found)
			if framework != tt.framework {
				t.Errorf("framework = %q, want %q (detected %v)", framework, tt.framework, found)
			}
			if language != tt.language {
				t.Errorf("language = %q, want %q", language, tt.language)
			}
			if len(files) != len(tt.files) {
				t.Errorf("%d files recorded, want %d", len(files), len(tt.files))
			}
		})
	}
}

// A real framework beats net/http, which is the fallback rather than a choice.
// Recording only the winner would hide how often the pair occurs, so both are
// kept and only the primary is resolved.
func TestNetHTTPDoesNotOutrankARealFramework(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\nrequire github.com/gin-gonic/gin v1.10.0\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte(`package main
import (
	"net/http"
	"github.com/gin-gonic/gin"
)`), 0o644)

	found, _, err := detect(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(found) != 2 {
		t.Fatalf("detected %v, want both Gin and net/http recorded", found)
	}
	if framework, _ := primary(found); framework != "Gin" {
		t.Fatalf("primary = %q, want Gin", framework)
	}
}

// Prose is not a choice. A README that mentions Flask while the code imports
// nothing is a project with no framework, and counting it as Flask would
// inflate whichever framework models like to talk about.
func TestAMentionInProseIsNotAChoice(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "README.md"),
		[]byte("Considered Flask, Django and FastAPI. Went with none of them.\n"), 0o644)

	found, _, err := detect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Fatalf("detected %v from a README", found)
	}
}

// Two frameworks in one project is a finding, not a tie to break.
func TestAmbiguityIsReportedRatherThanResolved(t *testing.T) {
	framework, _ := primary([]string{"Gin", "Echo"})
	if !strings.Contains(framework, "+") {
		t.Fatalf("primary = %q, want both reported", framework)
	}
}

// The vendor directories agents create are not evidence about what the agent
// chose, and walking them makes a run take minutes.
func TestVendorDirectoriesAreSkipped(t *testing.T) {
	dir := t.TempDir()
	deep := filepath.Join(dir, "node_modules", "express", "lib")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(deep, "index.js"), []byte(`require("express")`), 0o644)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte(`package main`+"\n"+`import "net/http"`), 0o644)

	found, files, err := detect(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range found {
		if f == "Express" {
			t.Error("a dependency inside node_modules was counted as a choice")
		}
	}
	for _, f := range files {
		if strings.Contains(f, "node_modules") {
			t.Errorf("walked into node_modules: %s", f)
		}
	}
}

func TestSummaryCountsRuns(t *testing.T) {
	results := []GreenfieldResult{
		{Framework: "Gin", Language: "Go"},
		{Framework: "Gin", Language: "Go"},
		{Framework: "FastAPI", Language: "Python"},
	}

	summary := summarise(results, "test-agent", "2026-08-05")

	for _, want := range []string{"test-agent", "2026-08-05", "3 runs", "Gin", "67%", "FastAPI", "33%"} {
		if !strings.Contains(summary, want) {
			t.Errorf("the summary does not contain %q:\n%s", want, summary)
		}
	}
}

// Five phrasings, because one phrasing measures the phrasing. None of them may
// name a language or a framework, or the experiment is leading its subject.
func TestThePromptsNameNoFramework(t *testing.T) {
	if len(greenfieldPrompts) < 5 {
		t.Fatalf("%d prompts, want at least five phrasings", len(greenfieldPrompts))
	}

	banned := []string{"go", "golang", "python", "rust", "gin", "flask", "fastapi",
		"express", "django", "rails", "framework", "tjo"}

	for _, prompt := range greenfieldPrompts {
		words := strings.FieldsFunc(strings.ToLower(prompt), func(r rune) bool {
			return !(r >= 'a' && r <= 'z')
		})
		for _, word := range words {
			for _, bad := range banned {
				if word == bad {
					t.Errorf("prompt %q names %q, which leads the answer", prompt, bad)
				}
			}
		}
	}
}
