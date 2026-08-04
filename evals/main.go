// Command evals measures whether a coding agent produces a working Tjo
// application, using the compiler as the grader.
//
// See README.md for why this exists and how to read the number.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	var (
		cli    = flag.String("cli", "", "path to a tjo binary (required)")
		agent  = flag.String("agent", "", "command that takes a prompt on stdin and edits the working directory; without it only deterministic tasks run")
		only   = flag.String("task", "", "run one task by name")
		keep   = flag.Bool("keep", false, "keep working directories for inspection")
		budget = flag.Duration("timeout", 10*time.Minute, "per-task timeout")
	)
	flag.Parse()

	if *cli == "" {
		fmt.Fprintln(os.Stderr, "-cli is required: build one with `make build`")
		os.Exit(2)
	}
	abs, err := filepath.Abs(*cli)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if _, err := os.Stat(abs); err != nil {
		fmt.Fprintf(os.Stderr, "no tjo binary at %s\n", abs)
		os.Exit(2)
	}

	all := tasks()
	var selected []task
	for _, t := range all {
		if *only != "" && t.Name != *only {
			continue
		}
		if t.Prompt != "" && *agent == "" {
			continue
		}
		selected = append(selected, t)
	}
	if len(selected) == 0 {
		fmt.Fprintln(os.Stderr, "no tasks selected")
		os.Exit(2)
	}

	fmt.Printf("tjo evals — %d tasks", len(selected))
	if *agent != "" {
		fmt.Printf(", agent: %s", *agent)
	} else {
		fmt.Print(", deterministic only (pass -agent to include generative tasks)")
	}
	fmt.Printf("\n\n")

	var passed, failed int
	start := time.Now()

	for _, t := range selected {
		res := run(t, abs, *agent, *budget, *keep)

		status := "PASS"
		if !res.ok {
			status = "FAIL"
		}
		fmt.Printf("  %-4s %-26s %6.1fs  %s\n", status, t.Name, res.took.Seconds(), t.Origin)
		if !res.ok {
			for _, line := range strings.Split(strings.TrimSpace(res.reason), "\n") {
				fmt.Printf("       %s\n", line)
			}
			if res.dir != "" {
				fmt.Printf("       kept: %s\n", res.dir)
			}
			failed++
			continue
		}
		passed++
	}

	total := passed + failed
	fmt.Printf("\n%d/%d passed (%.0f%%) in %s\n",
		passed, total, float64(passed)/float64(total)*100, time.Since(start).Round(time.Second))

	// A deterministic failure is a framework bug and should break the build.
	// A generative failure is a measurement, and exiting non-zero on it would
	// make the number something to game rather than something to read.
	for _, t := range selected {
		if t.Prompt == "" && failed > 0 {
			for _, r := range results {
				if r.name == t.Name && !r.ok {
					os.Exit(1)
				}
			}
		}
	}
}

type result struct {
	name   string
	ok     bool
	reason string
	took   time.Duration
	dir    string
}

var results []result

func run(t task, cli, agent string, budget time.Duration, keep bool) result {
	start := time.Now()

	dir, err := os.MkdirTemp("", "tjo-eval-"+t.Name+"-")
	if err != nil {
		return record(t.Name, false, err.Error(), time.Since(start), "")
	}
	if !keep {
		defer os.RemoveAll(dir)
	}

	var log strings.Builder
	e := &env{dir: dir, cli: cli, log: &log}

	fail := func(reason string) result {
		kept := ""
		if keep {
			kept = dir
		}
		return record(t.Name, false, reason+"\n"+tail(log.String(), 12), time.Since(start), kept)
	}

	if t.Setup != nil {
		if err := t.Setup(e); err != nil {
			return fail("setup: " + err.Error())
		}
	}

	if t.Prompt != "" {
		if err := e.runAgent(agent, t.Prompt, budget); err != nil {
			return fail("agent: " + err.Error())
		}
	}

	if t.Check != nil {
		if err := t.Check(e); err != nil {
			return fail(err.Error())
		}
	}

	return record(t.Name, true, "", time.Since(start), "")
}

func record(name string, ok bool, reason string, took time.Duration, dir string) result {
	r := result{name: name, ok: ok, reason: reason, took: took, dir: dir}
	results = append(results, r)
	return r
}

// runAgent hands the prompt to the agent with the project as its working
// directory. The contract is deliberately minimal -- prompt on stdin, edit the
// files, exit -- so any tool can be measured rather than one vendor's.
func (e *env) runAgent(agent, prompt string, budget time.Duration) error {
	parts := strings.Fields(agent)
	if len(parts) == 0 {
		return fmt.Errorf("empty agent command")
	}

	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Dir = filepath.Join(e.dir, "app")
	cmd.Stdin = strings.NewReader(prompt)

	out, err := cmd.CombinedOutput()
	e.log.Write(out)
	return err
}

func (e *env) run(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir

	out, err := cmd.CombinedOutput()
	fmt.Fprintf(e.log, "$ %s %s\n%s", name, strings.Join(args, " "), out)
	if err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

func tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return "  " + strings.Join(lines, "\n  ")
}
