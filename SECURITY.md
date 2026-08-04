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
branch and there will not be one at this size. As of this file: **v0.9.x**.

| Version | Security fixes |
|---|---|
| 0.9.x | Yes |
| 0.8.x and earlier | No — upgrade |

Users of v0.7.0 and earlier should upgrade urgently. v0.8.0 fixes an unpatchable
SQL injection reachable through the query builder, a CSRF origin check that never
executed, and a rate-limit bypass that defeated a fix v0.7.0 had already
published. See the changelog.

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
- **Every advisory gets a CVE requested through GitHub at publication.** This is
  the step that makes an advisory visible, and it is easy to get wrong: a
  repository advisory published to this repo's Security tab does **not** enter
  GitHub's global advisory database on its own, so it never reaches OSV and
  never reaches `vuln.go.dev`. `govulncheck` reads `vuln.go.dev`. Without a CVE
  request the chain never starts, and the advisory is invisible to the exact
  tool this project tells you to run.

  We learned this the slow way: all four advisories below were published without
  one, and `govulncheck` reported this module clean at every affected version
  until it was noticed and corrected. See #67.
- Published advisories are explained in the changelog with what was measured,
  not just what was changed.

## Cyber Resilience Act

**This project is out of scope for the CRA**, and that is worth stating plainly
rather than leaving people to guess.

The regulation reaches software placed on the market *in the course of a
commercial activity*. An **open source steward** — the lighter category in
Article 24 — must be a *legal person*: a foundation or a company. This framework
is maintained by an individual, takes no fees, and sells nothing. Article 2 is
explicit that the CRA does not apply to developers contributing to free and open
source software that is not under their responsibility.

Consequently this project does not, and will not, produce CE markings,
Declarations of Conformity or Annex VII technical files. Stewards are told not
to, and out-of-scope projects producing them would be asserting something untrue.

**What this means if you are building a commercial product on Tjo.** You are the
manufacturer. Article 13 requires due diligence on every integrated third-party
component, including non-commercial open source ones, and that responsibility
cannot be passed upstream. What we can give you toward it:

| You need | Where it is |
|---|---|
| SBOM | CycloneDX 1.6 attached to every release since v0.9.0 |
| Build provenance | SLSA attestation per artifact; `gh attestation verify <file> --repo jimmitjoo/tjo` |
| Vulnerability disclosure process | this document |
| Known vulnerabilities | `make vuln`, gating every build and run weekly |
| Support and EOL policy | "Supported versions" above |
| Secure development practices | OpenSSF Scorecard badge in the README |

If you need something else for your technical file, open an issue. Answering
once in public is better for both of us than answering the same questionnaire
repeatedly in email.

Full application of the CRA is **2027-12-11**; reporting obligations for
in-scope parties started 2026-09-11.

## Past advisories

| Advisory | CVSS v3.1 | Affected | Fixed in |
|---|---|---|---|
| [GHSA-44g2-5v2v-xh66](https://github.com/jimmitjoo/tjo/security/advisories/GHSA-44g2-5v2v-xh66) — scaffolded password reset performed no authorisation check | 9.1 Critical | ≤ 0.6.1 | 0.7.0 |
| [GHSA-hm83-wmj9-52fm](https://github.com/jimmitjoo/tjo/security/advisories/GHSA-hm83-wmj9-52fm) — rate limiter trusted `X-Forwarded-For` from any peer | 8.2 High | ≤ 0.7.0 | 0.8.0 |
| [GHSA-9m5v-pvgv-cv8j](https://github.com/jimmitjoo/tjo/security/advisories/GHSA-9m5v-pvgv-cv8j) — session and CSRF middleware were never installed | 6.5 Medium | ≤ 0.6.1 | 0.7.0 |
| [GHSA-2w6x-c7q3-qcgr](https://github.com/jimmitjoo/tjo/security/advisories/GHSA-2w6x-c7q3-qcgr) — the Go renderer used `text/template`, so no output escaping | 6.1 Medium | ≤ 0.6.1 | 0.7.0 |

All four were originally labelled by impression rather than scored. CVSS
vectors were added on 2026-08-04 alongside the CVE requests, and two came out
lower than their original label. Nothing was retracted and no finding changed —
the scores reflect what each report actually demonstrates. Each advisory carries
a note explaining its own.

Note that GHSA-hm83-wmj9-52fm's affected range originally read `≤ 0.6.1`. It was
corrected to `≤ 0.7.0` because v0.7.0's fix was defeated by a middleware mounted
ahead of it — see the advisory's second correction.
