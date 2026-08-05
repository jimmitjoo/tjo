package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jimmitjoo/tjo/core"
	"github.com/jimmitjoo/tjo/docs"
	"github.com/joho/godotenv"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// NameArg is the argument struct for tools that require a name
type NameArg struct {
	Name string `json:"name" jsonschema_description:"The name for the entity to create"`
}

// StepsArg is the argument struct for migration tools
type StepsArg struct {
	Steps int `json:"steps,omitempty" jsonschema_description:"Number of steps (default: all for up, 1 for down)"`
}

// Helper to create text result
func textResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: msg},
		},
	}
}

// Helper to create error result
func errorResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: msg},
		},
		IsError: true,
	}
}

func getModuleInfo(module string) string {
	switch module {
	case "sms":
		return `SMS Module - Send SMS via Twilio or Vonage

Add to main.go imports:
  "github.com/jimmitjoo/tjo/sms"

Add to app.New():
  sms.NewModule()

Environment variables:
  SMS_PROVIDER=twilio  # or vonage
  TWILIO_ACCOUNT_SID=xxx
  TWILIO_API_KEY=xxx
  TWILIO_API_SECRET=xxx
  TWILIO_FROM_NUMBER=+1234567890

Usage:
  if m := app.GetModule("sms"); m != nil {
      m.(*sms.Module).Send("+46701234567", "Hello!", false)
  }`

	case "email":
		return `Email Module - Send emails via SMTP or API providers

Add to main.go imports:
  "github.com/jimmitjoo/tjo/email"

Add to app.New():
  email.NewModule()

Environment variables:
  SMTP_HOST=smtp.example.com
  SMTP_PORT=587
  SMTP_USERNAME=user
  SMTP_PASSWORD=pass
  SMTP_ENCRYPTION=tls
  MAIL_FROM_ADDRESS=noreply@example.com
  MAIL_FROM_NAME=My App

Usage:
  if m := app.GetModule("email"); m != nil {
      m.(*email.Module).Send(email.Message{
          To:       "user@example.com",
          Subject:  "Welcome!",
          Template: "welcome",
          Data:     map[string]interface{}{"name": "John"},
      })
  }`

	case "websocket":
		return `WebSocket Module - Real-time communication

Add to main.go imports:
  "github.com/jimmitjoo/tjo/websocket"

Add to app.New():
  websocket.NewModule()

Mount the handler in routes.go:
  if ws := app.GetModule("websocket"); ws != nil {
      app.HTTP.Router.Get("/ws", ws.(*websocket.Module).Handler())
  }

Usage:
  // Broadcast to all clients
  ws.(*websocket.Module).Broadcast([]byte("Hello everyone!"))

  // Broadcast to room
  ws.(*websocket.Module).BroadcastToRoom("chat:123", []byte("Hello room!"))`

	case "otel":
		return `OpenTelemetry Module - Distributed tracing

Add to main.go imports:
  "github.com/jimmitjoo/tjo/otel"

Add to app.New():
  otel.NewModule(
      otel.WithServiceName("my-app"),
      otel.WithOTLPExporter("localhost:4317", true),
  )

Environment variables:
  OTEL_ENABLED=true
  OTEL_SERVICE_NAME=my-app
  OTEL_ENDPOINT=localhost:4317
  OTEL_INSECURE=true

Usage:
  ctx, span := otel.Start(r.Context(), "operation_name")
  defer span.End()
  otel.SetAttributes(ctx, otel.String("key", "value"))

Local dev with Jaeger:
  docker run -d -p 16686:16686 -p 4317:4317 jaegertracing/all-in-one:latest
  # View traces at http://localhost:16686`

	default:
		return ""
	}
}

func doMCP() error {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "tjo",
		Version: core.Version,
	}, nil)

	registerTools(server)

	return server.Run(context.Background(), &mcp.StdioTransport{})
}

// registerTools installs every tool on server.
//
// Separate from doMCP so the registration can be exercised without starting a
// stdio server -- which is how the tool list is checked for stable ordering,
// the property a client relies on to notice a server redefining a tool.
func registerTools(server *mcp.Server) {
	// Tell clients how long a tool list stays fresh.
	//
	// The 2026-07-28 revision made caching first-class, and it is worth setting
	// rather than leaving to the default of "immediately stale". The tool list
	// changes only when this binary changes, so an hour is conservative.
	//
	// It pairs with ordering: tools are registered in a fixed sequence with no
	// map iteration anywhere in the path, so tools/list is byte-identical
	// across calls. That is what lets a client cache the list and diff it to
	// notice a server quietly redefining a tool -- a cached list that reorders
	// itself every call makes that check useless.
	server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			res, err := next(ctx, method, req)
			if err != nil {
				return res, err
			}
			if list, ok := res.(*mcp.ListToolsResult); ok {
				list.TTLMs = int(time.Hour / time.Millisecond)
				list.CacheScope = "private"
			}
			return res, nil
		}
	})

	// Tool 1: Create new project
	type CreateProjectArgs struct {
		Name     string `json:"name" jsonschema_description:"Name of the project"`
		Template string `json:"template,omitempty" jsonschema_description:"Starter template (default/blog/api/saas)"`
		Database string `json:"database,omitempty" jsonschema_description:"Database type (postgres/mysql/mariadb/sqlite)"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "tjo_create_project",
		Description: "Create a new Tjo project with full scaffolding. Templates: default, blog, api, saas. Databases: postgres, mysql, mariadb, sqlite",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args CreateProjectArgs) (*mcp.CallToolResult, any, error) {
		template := args.Template
		if template == "" {
			template = "default"
		}
		if err := doNew(args.Name, template, args.Database); err != nil {
			return errorResult(err.Error()), nil, nil
		}
		msg := "Created project: " + args.Name + " (template: " + template + ")"
		if args.Database != "" {
			msg += " with " + args.Database + " database"
		}
		return textResult(msg), nil, nil
	})

	// Tool 2: Create model
	mcp.AddTool(server, &mcp.Tool{
		Name:        "tjo_create_model",
		Description: "Create a new database model with fields",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args NameArg) (*mcp.CallToolResult, any, error) {
		if err := doMake("model", args.Name); err != nil {
			return errorResult(err.Error()), nil, nil
		}
		return textResult("Created model: " + args.Name), nil, nil
	})

	// Tool 3: Create handler
	mcp.AddTool(server, &mcp.Tool{
		Name:        "tjo_create_handler",
		Description: "Create a new HTTP handler",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args NameArg) (*mcp.CallToolResult, any, error) {
		if err := doMake("handler", args.Name); err != nil {
			return errorResult(err.Error()), nil, nil
		}
		return textResult("Created handler: " + args.Name), nil, nil
	})

	// Tool 4: Create migration
	mcp.AddTool(server, &mcp.Tool{
		Name:        "tjo_create_migration",
		Description: "Create a new database migration",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args NameArg) (*mcp.CallToolResult, any, error) {
		if err := doMake("migration", args.Name); err != nil {
			return errorResult(err.Error()), nil, nil
		}
		return textResult("Created migration: " + args.Name), nil, nil
	})

	// Tool 5: Run migrations
	mcp.AddTool(server, &mcp.Tool{
		Name:        "tjo_run_migrations",
		Description: "Run pending database migrations",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args StepsArg) (*mcp.CallToolResult, any, error) {
		steps := ""
		if args.Steps > 0 {
			steps = fmt.Sprintf("%d", args.Steps)
		}
		if err := doMigrate("up", steps); err != nil {
			return errorResult(err.Error()), nil, nil
		}
		return textResult("Migrations completed"), nil, nil
	})

	// Tool 6: Rollback migrations
	mcp.AddTool(server, &mcp.Tool{
		Name:        "tjo_rollback",
		Description: "Rollback database migrations",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args StepsArg) (*mcp.CallToolResult, any, error) {
		steps := "1"
		if args.Steps > 0 {
			steps = fmt.Sprintf("%d", args.Steps)
		}
		if err := doMigrate("down", steps); err != nil {
			return errorResult(err.Error()), nil, nil
		}
		return textResult("Rollback completed"), nil, nil
	})

	// Tool 7: Setup authentication
	type EmptyArgs struct{}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "tjo_setup_auth",
		Description: "Setup complete authentication system with login, register, password reset, and 2FA support. Creates migrations, models, handlers, middleware, views, and email templates.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args EmptyArgs) (*mcp.CallToolResult, any, error) {
		if err := doAuth(); err != nil {
			return errorResult(err.Error()), nil, nil
		}
		return textResult("Authentication system created with 2FA support. Routes added for /login, /register, /logout, /forgot-password, /reset-password, and /2fa/*"), nil, nil
	})

	// Tool 8: Create session table
	mcp.AddTool(server, &mcp.Tool{
		Name:        "tjo_create_session_table",
		Description: "Create database table for session storage",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args EmptyArgs) (*mcp.CallToolResult, any, error) {
		if err := doSession(); err != nil {
			return errorResult(err.Error()), nil, nil
		}
		return textResult("Session table created"), nil, nil
	})

	// Tool 9: Create middleware
	mcp.AddTool(server, &mcp.Tool{
		Name:        "tjo_create_middleware",
		Description: "Create a new custom middleware",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args NameArg) (*mcp.CallToolResult, any, error) {
		if err := doMake("middleware", args.Name); err != nil {
			return errorResult(err.Error()), nil, nil
		}
		return textResult("Created middleware: " + args.Name), nil, nil
	})

	// Tool 10: Create mail template
	mcp.AddTool(server, &mcp.Tool{
		Name:        "tjo_create_mail",
		Description: "Create a new email template (HTML and plain text)",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args NameArg) (*mcp.CallToolResult, any, error) {
		if err := doMail(args.Name); err != nil {
			return errorResult(err.Error()), nil, nil
		}
		return textResult("Created email template: " + args.Name + " (both .html.tmpl and .plain.tmpl)"), nil, nil
	})

	// Tool 11: Setup Docker
	mcp.AddTool(server, &mcp.Tool{
		Name:        "tjo_setup_docker",
		Description: "Generate Docker and docker-compose configuration files for the project",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args EmptyArgs) (*mcp.CallToolResult, any, error) {
		if err := doMake("docker", ""); err != nil {
			return errorResult(err.Error()), nil, nil
		}
		return textResult("Docker configuration created (Dockerfile, docker-compose.yml, nginx.conf)"), nil, nil
	})

	// Tool 12: Module info
	type ModuleArgs struct {
		Module string `json:"module" jsonschema_description:"Module name: sms, email, websocket, or otel"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "tjo_module_info",
		Description: "Get information on how to enable a specific module (sms, email, websocket, otel)",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args ModuleArgs) (*mcp.CallToolResult, any, error) {
		info := getModuleInfo(args.Module)
		if info == "" {
			return errorResult("Unknown module: " + args.Module + ". Available: sms, email, websocket, otel"), nil, nil
		}
		return textResult(info), nil, nil
	})

	// Tools 13-15: introspection over the user's actual application.
	//
	// These answer questions about *this* project that an agent cannot answer
	// without reading every file, which is the failure mode worth attacking.

	mcp.AddTool(server, &mcp.Tool{
		Name: "tjo_routes_list",
		Description: "List the HTTP routes this application registers, with method, pattern, handler and line number. " +
			"Parses routes.go statically, so it works while the project does not compile.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args EmptyArgs) (*mcp.CallToolResult, any, error) {
		root, err := os.Getwd()
		if err != nil {
			return errorResult(err.Error()), nil, nil
		}
		routes, err := findRoutes(root)
		if err != nil {
			return errorResult(err.Error()), nil, nil
		}
		if len(routes) == 0 {
			return textResult("No routes registered in routes.go."), nil, nil
		}

		var b strings.Builder
		for _, r := range routes {
			fmt.Fprintf(&b, "%-7s %-30s %-28s routes.go:%d\n", r.Method, r.Pattern, r.Handler, r.Line)
		}
		return textResult(b.String()), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "tjo_schema_describe",
		Description: "List the tables and columns in this application's database, read from the live database rather than " +
			"from migration files -- so it reflects what was actually applied.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args EmptyArgs) (*mcp.CallToolResult, any, error) {
		// setup() populates the package-level cfg from .env, the same way every
		// other command reaches configuration.
		if err := godotenv.Load(); err != nil {
			return errorResult("no .env in the working directory: " + err.Error()), nil, nil
		}
		conf, err := core.LoadCLIConfig()
		if err != nil {
			return errorResult(err.Error()), nil, nil
		}
		out, err := describeSchema(conf.Config.Database.Type, conf.Config.Database.DSN(conf.RootPath))
		if err != nil {
			return errorResult(err.Error()), nil, nil
		}
		return textResult(out), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "tjo_config_describe",
		Description: "Report which environment variables this project sets, which the framework recognises, and which are " +
			"missing. Secrets are redacted.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args EmptyArgs) (*mcp.CallToolResult, any, error) {
		root, err := os.Getwd()
		if err != nil {
			return errorResult(err.Error()), nil, nil
		}
		out, err := describeConfig(root)
		if err != nil {
			return errorResult(err.Error()), nil, nil
		}
		return textResult(out), nil, nil
	})

	// Tool 16: the documentation of the version that is installed.
	//
	// This file used to argue that documentation retrieval is not worth
	// attacking, on the evidence that the one published controlled A/B of a
	// docs-retrieval MCP server improved zero of ten questions and that Astro
	// deleted its llms.txt after measuring that nobody fetched it. That
	// evidence has not changed and the argument still holds against the thing
	// it was aimed at: fetching a project's documentation over the network.
	//
	// This is a different thing, and the difference is the version. Fetching
	// from the internet gets whatever is on the default branch; a model's own
	// idea of this framework is some months old and averaged over every
	// release that existed then. These pages are compiled into the binary, so
	// the answer describes the CLI the caller is holding.
	//
	// Whether that is worth anything is a measurable question, and #76's
	// definition of done is that the eval suite reports the difference with
	// and without -- including when the difference is zero, which is the
	// outcome this comment expects.
	type DocsArgs struct {
		Topic  string `json:"topic,omitempty" jsonschema_description:"Document to read. Omit to list what is available."`
		Search string `json:"search,omitempty" jsonschema_description:"Return the lines mentioning this term, across every document."`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name: "tjo_docs",
		Description: "Read this framework's documentation for the installed version (" + core.Version + "). " +
			"Compiled into the binary, so it describes the CLI in use rather than the default branch. " +
			"Call with no arguments to list the topics.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args DocsArgs) (*mcp.CallToolResult, any, error) {
		switch {
		case args.Search != "":
			hits := docs.Search(args.Search)
			if len(hits) == 0 {
				return textResult("No mention of " + args.Search + " in " + strings.Join(docs.Topics(), ", ")), nil, nil
			}

			var b strings.Builder
			// Sorted, because tool output that reorders itself between calls
			// is output a client cannot diff.
			topics := make([]string, 0, len(hits))
			for topic := range hits {
				topics = append(topics, topic)
			}
			sort.Strings(topics)

			for _, topic := range topics {
				fmt.Fprintf(&b, "## %s\n", topic)
				for _, line := range hits[topic] {
					fmt.Fprintf(&b, "  %s\n", line)
				}
			}
			return textResult(b.String()), nil, nil

		case args.Topic != "":
			content, err := docs.Read(args.Topic)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return textResult(content), nil, nil

		default:
			return textResult("Documentation for tjo " + core.Version + ". Topics: " +
				strings.Join(docs.Topics(), ", ") +
				"\n\nAlso in the repository: AGENTS.md (how to work in it), llms.txt (what models get wrong about it)."), nil, nil
		}
	})
}

// EmptyArgs is the argument struct for tools that take none.
type EmptyArgs struct{}
