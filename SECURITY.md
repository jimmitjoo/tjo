# Security Policy

## Reporting a vulnerability

Report privately through GitHub's [private vulnerability
reporting](https://github.com/jimmitjoo/tjo/security/advisories/new). Do not open
a public issue for a suspected vulnerability.

Tjo is maintained by one person. The response times below are what that can
actually sustain, not what sounds impressive:

| | |
|---|---|
| Acknowledgement | within 5 working days |
| Initial assessment | within 14 days |
| Fix or a dated plan | within 90 days of the assessment |

If you have not heard anything after 14 days, assume the report was missed and
say so in a follow-up — that is a failure on our side, not impatience on yours.

Disclosure is coordinated: we publish an advisory when a fix is released, and
credit you unless you would rather we did not.

## What counts as a vulnerability

**Generated code is in scope.** `tjo make auth` and the starter templates
produce code that ends up in real applications, and a flaw there is a flaw in
the framework. This is a wider bar than most frameworks claim, and it is
deliberate: v0.7.0 published GHSA-44g2-5v2v-xh66 for a missing authorisation
check in scaffolded password reset, because the person running the generator had
no way to know it was wrong.

In scope:

- Anything reachable by an unauthenticated or lower-privileged request against
  an application built with the framework.
- Authentication, session handling, CSRF, rate limiting, input validation and
  output escaping — including in generated code.
- Insecure defaults. A control that is off unless you know to turn it on, or one
  that silently removes itself when a dependency is missing, is a vulnerability.
  That second case was GHSA-9m5v-pvgv-cv8j.
- Vulnerable dependencies with a call path from framework code. `make vuln` is
  the arbiter; see below.

Not in scope:

- Findings that require an attacker to already control the host, the `.env`
  file, or the database.
- Denial of service through unbounded resource use in a development-only helper
  or in the CLI.
- Missing hardening headers with no demonstrated impact.
- Reports from automated scanners without a working reproducer. See below.

## Reports without a reproducer will be closed

GitHub's advisory database went from roughly 550 private reports per week in
January 2026 to over 3,000 by May, and a large share of that volume is
machine-generated and unverified.

A report needs a proof of concept, or an explicit call path from framework code
to the flaw. "A scanner flagged this file" is not a report, and triaging it takes
time that comes directly out of fixing real problems. Such reports will be closed
without further discussion. If you disagree with a closure, reopen it with a
reproducer.

## Supported versions

The major version is 0, so the API may still break in a minor release.

**Only the most recent minor line receives security fixes.** There is no LTS
branch and there will not be one at this size. As of this file: **v0.8.x**.

| Version | Security fixes |
|---|---|
| 0.8.x | Yes |
| 0.7.x and earlier | No — upgrade |

Users of v0.7.0 and earlier should upgrade. v0.8.0 fixes an unpatchable SQL
injection reachable through the query builder, a CSRF origin check that never
executed, and a rate-limit bypass. See the changelog.

## Scope of this policy

All five modules in this repository, each versioned separately:

- `github.com/jimmitjoo/tjo`
- `github.com/jimmitjoo/tjo/email`
- `github.com/jimmitjoo/tjo/otel`
- `github.com/jimmitjoo/tjo/sms`
- `github.com/jimmitjoo/tjo/websocket`

## What we do

- **`govulncheck` gates every build**, across all five modules, plus a weekly
  scheduled run so advisories that land against unchanged code are still caught.
  Run it yourself with `make vuln`.
- **Every security fix ships with a regression test verified to fail without
  it.** Not "a test was added" — the test is run against the unfixed code first
  and observed to fail. The v0.8.0 rate-limit bypass, for example, was pinned by
  a test showing 10 of 10 requests passing a limit of 2.
- **Advisories are filed to `golang.org/x/vulndb` as well as GHSA.**
  `govulncheck` reads `vuln.go.dev`, so a GHSA-only advisory is invisible to the
  exact tool Go users run. This is routinely missed and it is part of our release
  process.
- Published advisories are explained in the changelog with what was measured,
  not just what was changed.

## Past advisories

- GHSA-9m5v-pvgv-cv8j — session and CSRF middleware were never installed
- GHSA-2w6x-c7q3-qcgr — the Go renderer used `text/template`, so no output escaping
- GHSA-44g2-5v2v-xh66 — scaffolded password reset performed no authorisation check
- GHSA-hm83-wmj9-52fm — rate limiter trusted `X-Forwarded-For` from any peer
