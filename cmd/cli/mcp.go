package main

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// NameArg is the argument struct for tools that require a name
type NameArg struct {
	Name string `json:"name" jsonschema:"The name for the entity to create"`
}

// StepsArg is the argument struct for migration tools
type StepsArg struct {
	Steps int `json:"steps,omitempty" jsonschema:"Number of steps (default: all for up, 1 for down)"`
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

func doMCP() error {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "gemquick",
		Version: "1.0.0",
	}, nil)

	// Tool 1: Create new project
	mcp.AddTool(server, &mcp.Tool{
		Name:        "gemquick_create_project",
		Description: "Create a new GemQuick project with full scaffolding",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args NameArg) (*mcp.CallToolResult, any, error) {
		if err := doNew(args.Name); err != nil {
			return errorResult(err.Error()), nil, nil
		}
		return textResult("Created project: " + args.Name), nil, nil
	})

	// Tool 2: Create model
	mcp.AddTool(server, &mcp.Tool{
		Name:        "gemquick_create_model",
		Description: "Create a new database model with fields",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args NameArg) (*mcp.CallToolResult, any, error) {
		if err := doMake("model", args.Name); err != nil {
			return errorResult(err.Error()), nil, nil
		}
		return textResult("Created model: " + args.Name), nil, nil
	})

	// Tool 3: Create handler
	mcp.AddTool(server, &mcp.Tool{
		Name:        "gemquick_create_handler",
		Description: "Create a new HTTP handler",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args NameArg) (*mcp.CallToolResult, any, error) {
		if err := doMake("handler", args.Name); err != nil {
			return errorResult(err.Error()), nil, nil
		}
		return textResult("Created handler: " + args.Name), nil, nil
	})

	// Tool 4: Create migration
	mcp.AddTool(server, &mcp.Tool{
		Name:        "gemquick_create_migration",
		Description: "Create a new database migration",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args NameArg) (*mcp.CallToolResult, any, error) {
		if err := doMake("migration", args.Name); err != nil {
			return errorResult(err.Error()), nil, nil
		}
		return textResult("Created migration: " + args.Name), nil, nil
	})

	// Tool 5: Run migrations
	mcp.AddTool(server, &mcp.Tool{
		Name:        "gemquick_run_migrations",
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
		Name:        "gemquick_rollback",
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

	return server.Run(context.Background(), &mcp.StdioTransport{})
}
