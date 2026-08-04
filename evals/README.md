# Evals

Does a coding agent produce a working Tjo application?

This measures it and publishes the number, including when the number is bad.
Nobody else in this ecosystem does, which is the main argument for doing it.

## Why this exists

The Matthew Effect paper (Gu, Liang, Ma, Li — arXiv 2509.23261, ICLR 2026,
135,495 generations across 9 languages and 5 models) established that LLM code
generation quality tracks training-data popularity at p < 0.001. Two findings
apply directly:

- **Go the language is fine.** 76.82% pass@1 on the strongest model, three
  points behind Python.
- **Go the *stack* is not.** The paper's own example of a niche stack requiring
  five or more attempts is **Preact + Gin + GORM** — and Gin is the *most*
  popular Go web framework. A smaller one sits further out on that curve.

The countermeasures everyone is shipping are almost entirely unvalidated. Astro
deleted its `llms.txt` in April 2026 after measuring that nobody fetched it. The
one published controlled A/B of a documentation-retrieval MCP server improved
zero of ten questions. So the honest position is that we do not know whether our
MCP server or our `AGENTS.md` help, and the only way to stop guessing is to
measure.

## How it works

The compiler is the grader. A task passes if the resulting project **builds,
vets and passes its own tests** — no model-based judging, no similarity scoring,
no rubric to argue about. Go makes this unusually cheap, and it is the reason
this suite can be run by anyone rather than only by whoever holds an API key for
a judge model.

Tasks come from real defects rather than imagination. Issues #26, #27, #28, #33
and #34 were all "the generated code did not compile or did not run", which is
exactly what an agent will reproduce.

## Running it

```bash
# Every task, against a CLI built from this checkout
go run ./evals -cli $(pwd)/dist/tjo

# One task
go run ./evals -cli $(pwd)/dist/tjo -task scaffold-default

# With an agent in the loop
go run ./evals -cli $(pwd)/dist/tjo -agent 'claude -p'
```

Without `-agent` the suite runs the **deterministic** tasks only: the ones that
exercise the CLI's own scaffolding. Those are the baseline, and they should
always be 100% — a failure there is a framework bug, not a model limitation.
With `-agent` it also runs the **generative** tasks, where a model is asked to
produce code and the result is compiled.

## Baseline

Deterministic tasks, 2026-08-04, CLI built from this checkout:

```
  PASS scaffold-default              7.4s  #26, #27
  PASS scaffold-blog                 5.6s  #33
  PASS scaffold-api                  6.0s  #33
  PASS scaffold-saas                 5.6s  #33
  PASS generators-together           8.0s  #28

5/5 passed (100%) in 32s
```

**No generative baseline has been recorded yet.** Publishing a number requires
running an agent, and a number produced without naming the model, the prompt and
the date is worse than no number. That is the next step, not a completed one,
and this file will say so until it is done.

## Why there is no CI job for this

CI's `scaffold` job already generates all four templates, builds them, adds
`make auth`, `make controller` and `make handler`, and builds again — which is
the deterministic half of this suite. A second job asserting the same things
would be cost without coverage.

What CI cannot run is the generative half, because that needs a model and an API
key. This runner exists so those can be run deliberately, with the result
recorded above rather than buried in a job log.

## Interpreting the number

A generative score is a measurement of a model, a prompt, a framework and a day.
Report all four or the number means nothing. It is also not a leaderboard
position: what it is for is noticing when a change to the framework, the
templates, `AGENTS.md` or the MCP server moves it.

Watch for saturation. When a task stops discriminating, replace it — a suite
that everything passes has stopped measuring.
