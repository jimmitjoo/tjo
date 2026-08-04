package main

import (
	"database/sql"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Introspection over the user's actual application, rather than prose about the
// framework in general.
//
// The failure mode a framework's MCP server should attack is not "the agent has
// not read the docs". It is the agent inventing an API that resembles Chi's or
// Gin's, or writing a handler against a table that does not exist. Neither is
// helped by a documentation dump: the one published controlled A/B of a
// docs-retrieval MCP server improved zero of ten questions, and Astro deleted
// its llms.txt after measuring that nobody fetched it.
//
// What does help is answering questions about *this* project that the agent
// cannot otherwise answer without reading every file.

// routeInfo is one registered route.
type routeInfo struct {
	Method  string
	Pattern string
	Handler string
	File    string
	Line    int
}

// findRoutes parses routes.go and reports what the application registers.
//
// Static analysis rather than running the app: an agent asking "what routes
// exist" is usually mid-edit, when the project may not compile, and that is
// exactly when the answer is most useful.
func findRoutes(root string) ([]routeInfo, error) {
	path := filepath.Join(root, "routes.go")
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("no routes.go in %s -- is this a Tjo project?", root)
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parsing routes.go: %w", err)
	}

	verbs := map[string]bool{
		"Get": true, "Post": true, "Put": true, "Patch": true,
		"Delete": true, "Head": true, "Options": true, "Handle": true,
	}

	var routes []routeInfo

	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !verbs[sel.Sel.Name] || len(call.Args) < 2 {
			return true
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}

		pos := fset.Position(call.Pos())
		routes = append(routes, routeInfo{
			Method:  strings.ToUpper(sel.Sel.Name),
			Pattern: strings.Trim(lit.Value, `"`),
			Handler: exprString(call.Args[1]),
			File:    "routes.go",
			Line:    pos.Line,
		})
		return true
	})

	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Pattern != routes[j].Pattern {
			return routes[i].Pattern < routes[j].Pattern
		}
		return routes[i].Method < routes[j].Method
	})

	return routes, nil
}

func exprString(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.SelectorExpr:
		return exprString(v.X) + "." + v.Sel.Name
	case *ast.Ident:
		return v.Name
	case *ast.CallExpr:
		return exprString(v.Fun) + "(...)"
	default:
		return "?"
	}
}

// describeSchema lists tables and columns from the live database.
//
// Reads the database rather than the migration files, because those describe
// what should have been applied and this answers what actually is. A handler
// written against a migration that failed halfway is a real and confusing bug.
func describeSchema(dbType, dsn string) (string, error) {
	driver := dbType
	switch dbType {
	case "postgres", "postgresql":
		driver = "pgx"
	case "sqlite3":
		driver = "sqlite"
	case "mariadb":
		driver = "mysql"
	}

	db, err := sql.Open(driver, dsn)
	if err != nil {
		return "", err
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		return "", fmt.Errorf("cannot reach the database (%s): %w", driver, err)
	}

	var query string
	switch driver {
	case "pgx":
		query = `SELECT table_name, column_name, data_type
			FROM information_schema.columns
			WHERE table_schema = 'public'
			ORDER BY table_name, ordinal_position`
	case "mysql":
		query = `SELECT table_name, column_name, data_type
			FROM information_schema.columns
			WHERE table_schema = DATABASE()
			ORDER BY table_name, ordinal_position`
	default:
		query = `SELECT m.name, p.name, p.type
			FROM sqlite_master m
			JOIN pragma_table_info(m.name) p
			WHERE m.type = 'table' AND m.name NOT LIKE 'sqlite_%'
			ORDER BY m.name, p.cid`
	}

	rows, err := db.Query(query)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var (
		b       strings.Builder
		current string
	)
	for rows.Next() {
		var table, column, typ string
		if err := rows.Scan(&table, &column, &typ); err != nil {
			return "", err
		}
		if table != current {
			if current != "" {
				b.WriteString("\n")
			}
			fmt.Fprintf(&b, "%s\n", table)
			current = table
		}
		fmt.Fprintf(&b, "  %-24s %s\n", column, typ)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}

	if current == "" {
		return "No tables. Run `tjo migrate up`.", nil
	}
	return b.String(), nil
}

// describeConfig reports which environment variables the project sets, which
// the framework recognises, and which are missing.
//
// Values are redacted. The point is to answer "is this configured", which an
// agent cannot determine from the code, without putting credentials into a
// transcript that will be sent to a model provider.
func describeConfig(root string) (string, error) {
	set := map[string]string{}

	data, err := os.ReadFile(filepath.Join(root, ".env"))
	if err != nil {
		return "", fmt.Errorf("no .env in %s: %w", root, err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		set[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}

	// Grouped so the answer to "what do I still need to configure" is readable
	// rather than a wall of names.
	groups := []struct {
		name string
		keys []string
	}{
		{"Application", []string{"APP_NAME", "APP_URL", "DEBUG", "PORT", "KEY", "RENDERER"}},
		{"Database", []string{"DATABASE_TYPE", "DATABASE_HOST", "DATABASE_PORT", "DATABASE_USER", "DATABASE_PASS", "DATABASE_NAME"}},
		{"Cache and session", []string{"CACHE", "SESSION_TYPE", "REDIS_HOST", "REDIS_PASSWORD", "COOKIE_SECURE", "COOKIE_DOMAIN"}},
		{"Security", []string{"CORS_ALLOWED_ORIGINS", "TRUSTED_PROXIES"}},
		{"Mail", []string{"SMTP_HOST", "SMTP_PORT", "SMTP_USERNAME", "SMTP_PASSWORD", "MAIL_FROM_ADDRESS"}},
		{"SMS", []string{"SMS_PROVIDER", "TWILIO_ACCOUNT_SID", "TWILIO_API_KEY", "VONAGE_API_KEY"}},
		{"Tracing", []string{"OTEL_ENABLED", "OTEL_SERVICE_NAME", "OTEL_EXPORTER", "OTEL_ENDPOINT"}},
	}

	var b strings.Builder
	for _, g := range groups {
		fmt.Fprintf(&b, "%s\n", g.name)
		for _, k := range g.keys {
			v, ok := set[k]
			switch {
			case !ok:
				fmt.Fprintf(&b, "  %-24s (not set)\n", k)
			case v == "":
				fmt.Fprintf(&b, "  %-24s (empty)\n", k)
			case isSecretKey(k):
				fmt.Fprintf(&b, "  %-24s (set, redacted)\n", k)
			default:
				fmt.Fprintf(&b, "  %-24s %s\n", k, v)
			}
		}
		b.WriteString("\n")
	}

	var unknown []string
	for k := range set {
		if !isKnownKey(k, groups) {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		fmt.Fprintf(&b, "Set in .env but not recognised by the framework:\n  %s\n",
			strings.Join(unknown, "\n  "))
	}

	return b.String(), nil
}

func isSecretKey(k string) bool {
	for _, marker := range []string{"KEY", "PASS", "SECRET", "TOKEN", "SID"} {
		if strings.Contains(k, marker) {
			return true
		}
	}
	return false
}

func isKnownKey(k string, groups []struct {
	name string
	keys []string
}) bool {
	for _, g := range groups {
		for _, known := range g.keys {
			if k == known {
				return true
			}
		}
	}
	return false
}
