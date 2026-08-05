package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// The experiment nobody has published: what does a 2026-generation agent reach
// for when the directory is empty and the prompt names no framework?
//
// Two studies get cited for this and neither answers it. "LLMs Love Python"
// (ACL Findings 2026) measured greenfield choice -- Flask 88% against FastAPI
// 9% for "build a web server" -- on the 2024-25 model generation, and nobody
// has re-run it. "What Claude Code Actually Chooses" is methodologically
// excellent and every one of its four test repositories already contained a
// framework, so it measures library selection inside an existing stack.
//
// Greenfield framework selection by a current agent is unmeasured. The harness
// to measure it is a small delta on what this suite already does.
//
// # What this measures, and what it does not
//
// It records what the agent reached for. Not whether it compiled, not whether
// it was any good: this is the prior, not the quality. A run where every agent
// picks Gin is a successful experiment.
//
// # The expected result
//
// Tjo will almost certainly not appear. Publishing an experiment whose finding
// is "our framework is invisible" is the point -- it is the first public number
// on the question, it is checkable, and it makes "does shipping skills move it"
// answerable rather than arguable. If this data is ever used to argue that Tjo
// should be chosen, the experiment is worthless and so is the credibility.

// greenfieldPrompts are five phrasings of the same request, none naming a
// language or a framework.
//
// Five because a single phrasing measures the phrasing. They vary the domain,
// the formality and whether persistence is spelled out, and none of them says
// "web framework" -- an agent told to pick a framework is being led.
var greenfieldPrompts = []string{
	"Build me a JSON API for managing todos, with persistence.",
	"I need a small backend service that stores customer records and exposes them over HTTP.",
	"Write a web service with a couple of endpoints and a database behind it.",
	"Set up a new project: REST API, some CRUD, data has to survive a restart.",
	"Create an HTTP API for a bookmarking app. It should save bookmarks somewhere durable.",
}

// GreenfieldRunsPerPrompt follows Amplifying's method -- three runs per
// phrasing, with the directory reset between them -- so the numbers are
// comparable to theirs rather than merely adjacent.
const GreenfieldRunsPerPrompt = 3

// frameworkSignature identifies a framework from what the agent wrote.
//
// Matched against go.mod, package.json, requirements.txt, pyproject.toml,
// Cargo.toml and the source, because the agent chooses the language too and
// "which language" is half the finding.
type frameworkSignature struct {
	Name     string
	Language string

	// Patterns are matched against file contents. An import path is stronger
	// evidence than a mention in prose, which is why these are import paths and
	// dependency names rather than words.
	Patterns []*regexp.Regexp
}

var signatures = buildSignatures()

func buildSignatures() []frameworkSignature {
	raw := []struct {
		name, language string
		patterns       []string
	}{
		// Go
		{"Gin", "Go", []string{`github\.com/gin-gonic/gin`}},
		{"Echo", "Go", []string{`github\.com/labstack/echo`}},
		{"Fiber", "Go", []string{`github\.com/gofiber/fiber`}},
		{"Chi", "Go", []string{`github\.com/go-chi/chi`}},
		{"Gorilla", "Go", []string{`github\.com/gorilla/mux`}},
		{"net/http only", "Go", []string{`"net/http"`}},
		{"Tjo", "Go", []string{`github\.com/jimmitjoo/tjo`}},
		{"Buffalo", "Go", []string{`github\.com/gobuffalo/buffalo`}},
		{"Beego", "Go", []string{`github\.com/beego/beego`}},

		// Python
		{"FastAPI", "Python", []string{`\bfastapi\b`}},
		{"Flask", "Python", []string{`\bflask\b`}},
		{"Django", "Python", []string{`\bdjango\b`}},

		// JavaScript and TypeScript
		{"Express", "JavaScript", []string{`"express"`}},
		{"Hono", "JavaScript", []string{`"hono"`}},
		{"Fastify", "JavaScript", []string{`"fastify"`}},
		{"NestJS", "JavaScript", []string{`"@nestjs/core"`}},
		{"Next.js", "JavaScript", []string{`"next"`}},

		// Others
		{"Axum", "Rust", []string{`\baxum\b`}},
		{"Actix", "Rust", []string{`\bactix-web\b`}},
		{"Rails", "Ruby", []string{`\brails\b`}},
		{"Laravel", "PHP", []string{`laravel/framework`}},
		{"Spring Boot", "Java", []string{`spring-boot`}},
	}

	out := make([]frameworkSignature, 0, len(raw))
	for _, s := range raw {
		sig := frameworkSignature{Name: s.name, Language: s.language}
		for _, p := range s.patterns {
			sig.Patterns = append(sig.Patterns, regexp.MustCompile(`(?i)`+p))
		}
		out = append(out, sig)
	}
	return out
}

// GreenfieldResult is one run.
type GreenfieldResult struct {
	Prompt    string   `json:"prompt"`
	Run       int      `json:"run"`
	Agent     string   `json:"agent"`
	Detected  []string `json:"detected"`
	Language  string   `json:"language"`
	Framework string   `json:"framework"`
	Files     []string `json:"files"`
	Err       string   `json:"error,omitempty"`
}

// detect reports which frameworks appear in a directory tree.
//
// Everything found is reported, and the primary pick is decided afterwards --
// a project with both `net/http` and Gin has picked Gin, and recording only the
// winner would hide how often that pair occurs.
func detect(root string) (found []string, files []string, err error) {
	seen := map[string]bool{}

	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", "target", "__pycache__", ".venv":
				return filepath.SkipDir
			}
			return nil
		}

		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			relative = path
		}
		files = append(files, relative)

		// Only the files that declare dependencies or import them. A README
		// mentioning Flask is not a choice of Flask.
		switch {
		case d.Name() == "go.mod", d.Name() == "package.json", d.Name() == "requirements.txt",
			d.Name() == "pyproject.toml", d.Name() == "Cargo.toml", d.Name() == "Gemfile",
			d.Name() == "composer.json", d.Name() == "pom.xml", d.Name() == "build.gradle":
		default:
			ext := filepath.Ext(d.Name())
			if ext != ".go" && ext != ".py" && ext != ".js" && ext != ".ts" && ext != ".rs" && ext != ".rb" && ext != ".php" && ext != ".java" {
				return nil
			}
		}

		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}

		for _, sig := range signatures {
			if seen[sig.Name] {
				continue
			}
			for _, pattern := range sig.Patterns {
				if pattern.Match(content) {
					seen[sig.Name] = true
					found = append(found, sig.Name)
					break
				}
			}
		}
		return nil
	})

	sort.Strings(found)
	sort.Strings(files)
	return found, files, err
}

// primary picks the framework of record from everything detected.
//
// A real framework beats "net/http only", which is the fallback rather than a
// choice. Beyond that, ambiguity is reported rather than resolved: two
// frameworks in one project is a finding, not a tie to break.
func primary(found []string) (framework, language string) {
	if len(found) == 0 {
		return "none", ""
	}

	byName := map[string]frameworkSignature{}
	for _, sig := range signatures {
		byName[sig.Name] = sig
	}

	var real []string
	for _, name := range found {
		if name != "net/http only" {
			real = append(real, name)
		}
	}

	switch {
	case len(real) == 0:
		return "net/http only", "Go"
	case len(real) == 1:
		return real[0], byName[real[0]].Language
	default:
		return strings.Join(real, "+"), byName[real[0]].Language
	}
}

// runGreenfield runs the whole experiment and writes the raw data.
//
// Raw responses are published alongside the summary, which is the difference
// between a number and a claim: someone who disagrees can recount.
func runGreenfield(agent, agentLabel, outDir string, budget time.Duration, keep bool) error {
	if agent == "" {
		return fmt.Errorf("-agent is required for the greenfield experiment")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	var results []GreenfieldResult

	for _, prompt := range greenfieldPrompts {
		for run := 1; run <= GreenfieldRunsPerPrompt; run++ {
			result := GreenfieldResult{Prompt: prompt, Run: run, Agent: agentLabel}

			// A fresh empty directory per run. This is the whole design: an
			// agent shown an existing project is being asked a different
			// question, which is the one the published studies answered.
			dir, err := os.MkdirTemp("", "tjo-greenfield-")
			if err != nil {
				return err
			}
			if !keep {
				defer os.RemoveAll(dir)
			}

			e := &env{dir: dir, cli: "", log: &strings.Builder{}}

			if err := e.runAgent(agent, prompt, budget); err != nil {
				result.Err = err.Error()
			}

			found, files, err := detect(dir)
			if err != nil {
				result.Err = strings.TrimSpace(result.Err + " " + err.Error())
			}

			result.Detected = found
			result.Files = files
			result.Framework, result.Language = primary(found)

			results = append(results, result)

			fmt.Printf("  %-58s run %d  %s\n", truncatePrompt(prompt), run, result.Framework)
		}
	}

	stamp := time.Now().UTC().Format("2006-01-02")
	rawPath := filepath.Join(outDir, fmt.Sprintf("greenfield-%s-%s.json", sanitise(agentLabel), stamp))

	raw, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(rawPath, raw, 0o644); err != nil {
		return err
	}

	fmt.Printf("\n%s\n", summarise(results, agentLabel, stamp))
	fmt.Printf("raw data: %s\n", rawPath)

	return nil
}

// summarise counts the picks.
func summarise(results []GreenfieldResult, agent, date string) string {
	counts := map[string]int{}
	languages := map[string]int{}

	for _, r := range results {
		counts[r.Framework]++
		if r.Language != "" {
			languages[r.Language]++
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Greenfield framework selection — %s, %s, %d runs\n\n", agent, date, len(results))

	fmt.Fprintf(&b, "  Framework\n")
	for _, entry := range sorted(counts) {
		fmt.Fprintf(&b, "    %-24s %3d  %4.0f%%\n", entry.key, entry.count,
			100*float64(entry.count)/float64(len(results)))
	}

	fmt.Fprintf(&b, "\n  Language\n")
	for _, entry := range sorted(languages) {
		fmt.Fprintf(&b, "    %-24s %3d  %4.0f%%\n", entry.key, entry.count,
			100*float64(entry.count)/float64(len(results)))
	}

	return b.String()
}

type countEntry struct {
	key   string
	count int
}

func sorted(counts map[string]int) []countEntry {
	out := make([]countEntry, 0, len(counts))
	for key, count := range counts {
		out = append(out, countEntry{key, count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].count != out[j].count {
			return out[i].count > out[j].count
		}
		return out[i].key < out[j].key
	})
	return out
}

func truncatePrompt(p string) string {
	if len(p) <= 55 {
		return p
	}
	return p[:54] + "…"
}

func sanitise(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
