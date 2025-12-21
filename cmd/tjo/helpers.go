package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fatih/color"
	"github.com/jimmitjoo/tjo/core"
	"github.com/joho/godotenv"
)

func setup(arg1, arg2 string) {
	if arg1 != "new" && arg1 != "version" && arg1 != "help" {
		err := godotenv.Load()
		if err != nil {
			exitGracefully(err)
		}

		// Load CLI configuration using the core package
		cfg, err = core.LoadCLIConfig()
		if err != nil {
			exitGracefully(err)
		}
	}
}

func getDSN() string {
	dbType := cfg.DBType
	if dbType == "pgx" {
		dbType = "postgres"
	}

	if dbType == "postgres" || dbType == "postgresql" {
		var dsn string
		if os.Getenv("DATABASE_PASS") != "" {
			dsn = fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s&timezone=UTC&connect_timeout=5",
				os.Getenv("DATABASE_USER"),
				os.Getenv("DATABASE_PASS"),
				os.Getenv("DATABASE_HOST"),
				os.Getenv("DATABASE_PORT"),
				os.Getenv("DATABASE_NAME"),
				os.Getenv("DATABASE_SSL_MODE"))
		} else {
			dsn = fmt.Sprintf("postgres://%s@%s:%s/%s?sslmode=%s&timezone=UTC&connect_timeout=5",
				os.Getenv("DATABASE_USER"),
				os.Getenv("DATABASE_HOST"),
				os.Getenv("DATABASE_PORT"),
				os.Getenv("DATABASE_NAME"),
				os.Getenv("DATABASE_SSL_MODE"))
		}

		return dsn
	}

	// Build MySQL DSN from config
	return "mysql://" + cfg.Config.Database.DSN(cfg.RootPath)
}

func showHelp() {
	color.Yellow(`Available commands:

	help                     - show this help
	version                  - show Tjo version
	new <name>               - create a new Tjo project
	  -t, --template <name>  - use starter template (default, blog, api, saas)
	  -d, --db <type>        - configure database (postgres, mysql, mariadb, sqlite)
	run                      - run the application
	run --watch (-w)         - run with hot-reload (uses air)
	mcp                      - start MCP server for AI assistants
	migrate                  - runs all migrations up
	migrate down             - runs the last migration down
	migrate reset            - drops all tables and migrates them back up
	make auth                - creates authentication system
	make handler <name>      - creates a new handler
	make migration <name>    - creates up/down migrations
	make model <name>        - creates a new model
	make session             - creates session table
	make mail <name>         - creates email template

Examples:
	tjo new myapp
	tjo new myapp -d postgres
	tjo new myapp -t blog -d mysql
	tjo new myapp --template=saas --db=postgres

	`)
}

func updateSourceFiles(path string, fi os.FileInfo, err error) error {
	if err != nil {
		return err
	}

	if fi.IsDir() {
		return nil
	}

	if filepath.Ext(path) == ".go" {
		color.Yellow("Updating %s", path)
	}

	read, err := os.ReadFile(path)
	if err != nil {
		exitGracefully(err)
	}

	newContents := string(read)
	newContents = strings.Replace(newContents, "myapp", appUrl, -1)

	err = os.WriteFile(path, []byte(newContents), 0)
	if err != nil {
		exitGracefully(err)
	}

	return nil
}

func updateSource() {
	err := filepath.Walk(".", updateSourceFiles)
	if err != nil {
		exitGracefully(err)
	}

	color.Green("Source updated successfully!")
}
