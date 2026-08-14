package docs

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Keeping docs/framework-comparison.md true.
//
// The file was wrong in both directions for an unknown number of releases and
// nothing here could have noticed: it claimed "SSE: No" from before the sse
// package existed, and "CSRF: Plugin" for two frameworks that ship it in core.
// Every one of those was found by hand, once, because somebody happened to
// look. That is not a process.
//
// This is the part that can be a process. A row that describes *this
// repository* is checkable against this repository, and those were exactly the
// rows that embarrassed us. Rows about other people's frameworks are not
// checkable offline, so they stay dated prose -- see the activity check below,
// which is run before a release rather than in CI.

const comparisonFile = "framework-comparison.md"

// claim is what a row asserts about this repository, and how to find out.
type claim struct {
	// present is what the row would have to mean. Derived from the cell: a cell
	// beginning with "No" says the capability is absent, anything else says it
	// is there.
	//
	// find reports what is actually true.
	find func(t *testing.T) bool

	// count, when set, is checked against a number in the cell. For the rows
	// that claim a quantity rather than a yes.
	count func(t *testing.T) int
}

// tjoClaims maps a feature row to the fact that settles it.
//
// Not every Tjo row is here. "Asset Pipeline" is true because the skeleton
// repository ships a Tailwind build, and this test cannot see another
// repository -- so it stays prose, marked as such in the file. A check that
// reached across repositories would be a check that fails for reasons having
// nothing to do with the change under test.
//
// Every entry is a row this repository can contradict. Adding a capability
// without updating its row fails here, which is the whole point: #90 will add
// pprof, and the "PPROF | No" row below is what will notice.
var tjoClaims = map[string]claim{
	"gRPC":                           {find: never},
	"OpenAPI generation":             {find: sourceContains("api/openapi.go", "func OpenAPI(")},
	"OpenAPI drift check":            {find: sourceContains("api/openapi.go", "func CheckResponse(")},
	"CSRF Protection":                {find: sourceContains("csrf.go", "func RotateCSRFToken")},
	"Rate Limiting":                  {find: fileExists("api/ratelimit.go")},
	"XSS Prevention":                 {find: required("github.com/microcosm-cc/bluemonday")},
	"Input Validation":               {find: fileExists("validator.go")},
	"Authentication":                 {find: fileExists("auth/login.go")},
	"2FA (TOTP)":                     {find: sourceContains("auth/twofactor.go", "func VerifyTOTP(")},
	"Passkeys / WebAuthn":            {find: sourceContains("auth/passkey_ceremony.go", "func (p *Passkeys) BeginLogin(")},
	"Social login":                   {find: sourceContains("auth/oauth.go", "func NewOAuth(")},
	"Roles & multi-tenancy":          {find: fileExists("auth/organization.go")},
	"Anti-Bot (CAPTCHA)":             {find: never},
	"Migrations":                     {find: required("github.com/golang-migrate/migrate/v4")},
	"Session Management":             {find: required("github.com/alexedwards/scs/v2")},
	"Job Queue":                      {find: fileExists("jobs/sqlqueue.go")},
	"Transactional enqueue":          {find: sourceContains("jobs/sqlqueue.go", "func (q *SQLQueue) PushTx(")},
	"Durable steps / workflows":      {find: sourceContains("jobs/workflow.go", "func (w *Workflow)")},
	"Cron Scheduler":                 {find: required("github.com/robfig/cron/v3")},
	"WebSocket":                      {find: fileExists("websocket/go.mod")},
	"SSE":                            {find: sourceContains("sse/broadcast.go", "func (b *Broker) Broadcast(")},
	"LLM chat / tools / embeddings":  {find: fileExists("llm/go.mod")},
	"Vector search in query builder": {find: sourceContains("database/vector.go", "func (qb *QueryBuilder) Nearest(")},
	"MCP server":                     {find: fileExists("cmd/tjo/mcp.go"), count: countMCPTools},
	"Template Engine":                {find: required("github.com/CloudyKit/jet/v6")},
	"Hot Reload":                     {find: sourceContains("cmd/tjo/main.go", `arg2 == "--watch"`)},
	"WebDAV":                         {find: never},
	"OpenTelemetry":                  {find: fileExists("otel/go.mod")},
	"PPROF":                          {find: sourceContains("ops/pprof.go", "func ProfilePage()")},
	"i18n":                           {find: sourceContains("i18n/printer.go", "plural.Cardinal.MatchPlural")},
}

// A row that says No about something that exists fails the build, and a row
// that says Yes about something that does not fails it the other way.
func TestTjosOwnRowsAreTrue(t *testing.T) {
	rows := tjoRows(t)

	for feature, c := range tjoClaims {
		cell, found := rows[feature]
		if !found {
			t.Errorf("the table has no %q row, so the check for it is asserting nothing", feature)
			continue
		}

		claimed := !strings.HasPrefix(strings.ToLower(cell), "no")
		actual := c.find(t)

		if claimed != actual {
			t.Errorf("%s: the table says %q, and this repository says %v",
				feature, cell, actual)
		}

		if c.count != nil {
			want := c.count(t)
			if !strings.Contains(cell, strconv.Itoa(want)) {
				t.Errorf("%s: the table says %q, and this repository has %d", feature, cell, want)
			}
		}
	}
}

// The file names the rows this test covers, so a reader can tell an asserted
// claim from an inherited one. That list is kept true here rather than by
// diligence -- an unlisted check is a claim nobody knows is checked, and a
// listed row with no check is worse.
func TestTheFileListsTheRowsThisTestChecks(t *testing.T) {
	body := comparison(t)

	const heading = "### Rows this repository asserts about itself"

	start := strings.Index(body, heading)
	if start < 0 {
		t.Fatalf("the file has no %q section", heading)
	}

	section := body[start+len(heading):]
	if end := strings.Index(section, "\n#"); end >= 0 {
		section = section[:end]
	}

	listed := map[string]bool{}
	for _, line := range strings.Split(section, "\n") {
		if item, ok := strings.CutPrefix(strings.TrimSpace(line), "- "); ok {
			listed[strings.Trim(item, "`")] = true
		}
	}

	for feature := range tjoClaims {
		if !listed[feature] {
			t.Errorf("%q is checked by this test and the file does not say so", feature)
		}
		delete(listed, feature)
	}
	for feature := range listed {
		t.Errorf("the file says %q is checked and nothing checks it", feature)
	}
}

// tjoRows parses the feature tables and returns the Tjo column.
func tjoRows(t *testing.T) map[string]string {
	t.Helper()

	rows := map[string]string{}

	for _, line := range strings.Split(comparison(t), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			continue
		}

		cells := strings.Split(strings.Trim(line, "|"), "|")
		if len(cells) < 3 {
			continue
		}

		feature := strings.TrimSpace(cells[0])
		if feature == "" || feature == "Feature" || strings.HasPrefix(feature, "-") {
			continue
		}

		// The landscape table's first column is a linked repository, not a
		// feature, and its second column is a star count.
		if strings.HasPrefix(feature, "[") {
			continue
		}

		rows[feature] = strings.TrimSpace(cells[1])
	}

	if len(rows) < 30 {
		t.Fatalf("only %d feature rows parsed, so the table's shape has changed", len(rows))
	}

	return rows
}

func comparison(t *testing.T) string {
	t.Helper()

	body, err := os.ReadFile(comparisonFile)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// repoPath resolves a path in the repository, which is this package's parent.
func repoPath(rel string) string { return filepath.Join("..", rel) }

func fileExists(rel string) func(*testing.T) bool {
	return func(t *testing.T) bool {
		_, err := os.Stat(repoPath(rel))
		return err == nil
	}
}

// sourceContains reports whether a file exists and holds a string. Both halves
// matter: a moved function is a missing capability as far as a reader is
// concerned, and a renamed one should fail loudly rather than silently keep
// claiming the row.
func sourceContains(rel, needle string) func(*testing.T) bool {
	return func(t *testing.T) bool {
		body, err := os.ReadFile(repoPath(rel))
		if err != nil {
			return false
		}
		return strings.Contains(string(body), needle)
	}
}

// required reports whether a module is a dependency of the root module.
func required(module string) func(*testing.T) bool {
	return func(t *testing.T) bool {
		body, err := os.ReadFile(repoPath("go.mod"))
		if err != nil {
			t.Fatal(err)
		}
		return strings.Contains(string(body), module+" v")
	}
}

// never is for the rows that say No, so that the day one of them becomes true
// the row is what fails.
func never(*testing.T) bool { return false }

// Each tool is one AddTool call, which is the definition the row means.
var mcpTool = regexp.MustCompile(`AddTool\(`)

func countMCPTools(t *testing.T) int {
	t.Helper()

	var count int

	body, err := os.ReadFile(repoPath("cmd/tjo/mcp.go"))
	if err != nil {
		t.Fatalf("counting MCP tools: %v", err)
	}
	count = len(mcpTool.FindAllString(string(body), -1))

	return count
}

// The activity check.
//
// Not in CI: it needs the network, so it would fail for reasons unrelated to
// whatever change is under test, and a CI job that depends on the GitHub API is
// a CI job that goes red when GitHub does. Run it before a release --
// `make comparison-check` -- which is where a stale column actually matters.
func TestTheTabulatedFrameworksAreStillAlive(t *testing.T) {
	if os.Getenv("TJO_COMPARISON_CHECK") == "" {
		t.Skip("set TJO_COMPARISON_CHECK=1, or run `make comparison-check`")
	}

	type row struct {
		name, repo, stars, pushed string
	}

	pattern := regexp.MustCompile(`^\| \[([^\]]+)\]\(https://github\.com/([^)]+)\) \| ([0-9,]+) \| \*{0,2}([0-9-]+)\*{0,2} \|`)

	var rows []row
	for _, line := range strings.Split(comparison(t), "\n") {
		if m := pattern.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			rows = append(rows, row{m[1], m[2], m[3], m[4]})
		}
	}

	if len(rows) == 0 {
		t.Fatal("no framework rows parsed from the landscape table")
	}

	sort.Slice(rows, func(i, j int) bool { return rows[i].name < rows[j].name })

	client := &http.Client{Timeout: 20 * time.Second}
	var quiet, drifted []string

	for _, r := range rows {
		stars, pushed, err := repoStats(client, r.repo)
		if err != nil {
			t.Errorf("%s: %v", r.name, err)
			continue
		}

		recorded, err := time.Parse("2006-01-02", r.pushed)
		if err != nil {
			t.Errorf("%s: the recorded date %q is not a date", r.name, r.pushed)
			continue
		}

		age := time.Since(pushed)
		marker := "  "
		if age > 365*24*time.Hour {
			marker = "!!"
			quiet = append(quiet, fmt.Sprintf("%s (%.0f days)", r.name, age.Hours()/24))
		}

		t.Logf("%s %-12s %7d stars (table: %s)   pushed %s (table: %s)",
			marker, r.name, stars, r.stars, pushed.Format("2006-01-02"), r.pushed)

		if !pushed.Truncate(24 * time.Hour).Equal(recorded) {
			drifted = append(drifted, r.name)
		}
	}

	if len(quiet) > 0 {
		t.Logf("\nNot pushed in a year, and should not read as live options:\n  %s",
			strings.Join(quiet, "\n  "))
	}
	if len(drifted) > 0 {
		t.Logf("\nThe recorded last-push date is out of date for:\n  %s\n\nUpdate the landscape table and the date in \"How this was checked\".",
			strings.Join(drifted, "\n  "))
	}
}

func repoStats(client *http.Client, repo string) (int, time.Time, error) {
	request, err := http.NewRequest("GET", "https://api.github.com/repos/"+repo, nil)
	if err != nil {
		return 0, time.Time{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")

	// Authenticated when a token is around, because the anonymous rate limit is
	// sixty an hour and this makes fourteen calls.
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}

	response, err := client.Do(request)
	if err != nil {
		return 0, time.Time{}, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return 0, time.Time{}, fmt.Errorf("github returned %s", response.Status)
	}

	var repository struct {
		Stars    int       `json:"stargazers_count"`
		PushedAt time.Time `json:"pushed_at"`
	}
	if err := json.NewDecoder(response.Body).Decode(&repository); err != nil {
		return 0, time.Time{}, err
	}

	return repository.Stars, repository.PushedAt, nil
}
