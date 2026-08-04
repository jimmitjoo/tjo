package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// A task is one thing an agent or the CLI has to get right.
//
// Every task is derived from a defect that actually shipped. Synthetic tasks
// measure whether a model can do something nobody needed; these measure whether
// the failures this project has already had would happen again.
type task struct {
	Name string

	// Origin is the issue the task comes from, so a failure can be read against
	// what it is guarding.
	Origin string

	// Prompt is what the agent is asked to do. Empty means deterministic: the
	// task runs Setup and grades it without a model in the loop.
	Prompt string

	// Setup prepares the project. It runs before the agent, and for
	// deterministic tasks it is the whole task.
	Setup func(e *env) error

	// Check runs after Setup and after any agent. Returning an error fails the
	// task with that message as the reason.
	Check func(e *env) error
}

func tasks() []task {
	return []task{
		// ---- Deterministic: the CLI's own output ------------------------------
		//
		// These should always pass. A failure is a framework bug, and they are
		// here so a generative number is never read against a broken baseline.

		{
			Name:   "scaffold-default",
			Origin: "#26, #27",
			Setup: func(e *env) error {
				return e.tjo("new", "app", "-d", "sqlite")
			},
			Check: func(e *env) error { return e.buildsAndVets("app") },
		},
		{
			Name:   "scaffold-blog",
			Origin: "#33",
			Setup: func(e *env) error {
				return e.tjo("new", "app", "-t", "blog", "-d", "sqlite")
			},
			Check: func(e *env) error { return e.buildsAndVets("app") },
		},
		{
			Name:   "scaffold-api",
			Origin: "#33",
			Setup: func(e *env) error {
				return e.tjo("new", "app", "-t", "api", "-d", "sqlite")
			},
			Check: func(e *env) error { return e.buildsAndVets("app") },
		},
		{
			Name:   "scaffold-saas",
			Origin: "#33",
			Setup: func(e *env) error {
				return e.tjo("new", "app", "-t", "saas", "-d", "sqlite")
			},
			Check: func(e *env) error { return e.buildsAndVets("app") },
		},
		{
			// The combination is what breaks. A helper defined in one generator's
			// output and called from another compiles alone and not together,
			// which is how #28 shipped 181 references to a struct that no longer
			// existed.
			Name:   "generators-together",
			Origin: "#28",
			Setup: func(e *env) error {
				if err := e.tjo("new", "app", "-d", "sqlite"); err != nil {
					return err
				}
				for _, args := range [][]string{
					{"make", "auth"},
					{"make", "controller", "Widget"},
					{"make", "handler", "Thing"},
				} {
					if err := e.tjoIn("app", args...); err != nil {
						return err
					}
				}
				return e.goIn("app", "mod", "tidy")
			},
			Check: func(e *env) error {
				if err := e.buildsAndVets("app"); err != nil {
					return err
				}
				return e.goIn("app", "test", "./...")
			},
		},

		// ---- Generative: a model writes the code -----------------------------

		{
			Name:   "add-handler",
			Origin: "#28",
			Prompt: "Add a handler that responds to GET /about with the text " +
				"\"About us\". Register it in routes.go. Use the framework's own " +
				"generator rather than writing the file by hand.",
			Setup: func(e *env) error {
				return e.tjo("new", "app", "-d", "sqlite")
			},
			Check: func(e *env) error {
				if err := e.buildsAndVets("app"); err != nil {
					return err
				}
				return e.grep("app/routes.go", "/about")
			},
		},
		{
			Name:   "add-model-and-migration",
			Origin: "#26",
			Prompt: "Add a Product model with a name and a price, and a migration " +
				"that creates the matching table. Use the framework's generators.",
			Setup: func(e *env) error {
				return e.tjo("new", "app", "-d", "sqlite")
			},
			Check: func(e *env) error {
				if err := e.buildsAndVets("app"); err != nil {
					return err
				}
				if err := e.exists("app/data/product.go"); err != nil {
					return err
				}
				return e.globExists("app/migrations/*product*")
			},
		},
		{
			// The workspace trap, which is the first thing AGENTS.md warns about.
			// An agent that runs `go test ./...` from the root and reports success
			// has tested one of five modules.
			Name:   "stream-endpoint",
			Origin: "#56",
			Prompt: "Add a GET /events endpoint that streams three Server-Sent " +
				"Events using the framework's sse package, then returns. Register " +
				"it in routes.go.",
			Setup: func(e *env) error {
				return e.tjo("new", "app", "-d", "sqlite")
			},
			Check: func(e *env) error {
				if err := e.buildsAndVets("app"); err != nil {
					return err
				}
				return e.grep("app/routes.go", "/events")
			},
		},
	}
}

// env is one task's working directory plus the tools it needs.
type env struct {
	dir string
	cli string
	log *strings.Builder
}

func (e *env) tjo(args ...string) error   { return e.run(e.dir, e.cli, args...) }
func (e *env) goCmd(args ...string) error { return e.run(e.dir, "go", args...) }

func (e *env) tjoIn(sub string, args ...string) error {
	return e.run(filepath.Join(e.dir, sub), e.cli, args...)
}

func (e *env) goIn(sub string, args ...string) error {
	return e.run(filepath.Join(e.dir, sub), "go", args...)
}

func (e *env) buildsAndVets(sub string) error {
	if err := e.goIn(sub, "build", "./..."); err != nil {
		return fmt.Errorf("does not build: %w", err)
	}
	if err := e.goIn(sub, "vet", "./..."); err != nil {
		return fmt.Errorf("does not vet: %w", err)
	}
	return nil
}

func (e *env) exists(rel string) error {
	if _, err := os.Stat(filepath.Join(e.dir, rel)); err != nil {
		return fmt.Errorf("%s was not created", rel)
	}
	return nil
}

func (e *env) globExists(pattern string) error {
	matches, err := filepath.Glob(filepath.Join(e.dir, pattern))
	if err != nil {
		return err
	}
	if len(matches) == 0 {
		return fmt.Errorf("nothing matched %s", pattern)
	}
	return nil
}

func (e *env) grep(rel, want string) error {
	data, err := os.ReadFile(filepath.Join(e.dir, rel))
	if err != nil {
		return err
	}
	if !strings.Contains(string(data), want) {
		return fmt.Errorf("%s does not mention %q", rel, want)
	}
	return nil
}
