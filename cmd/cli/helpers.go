package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fatih/color"
	"github.com/jimmitjoo/gemquick/config"
	"github.com/joho/godotenv"
)

func setup(arg1, arg2 string) {
	if arg1 != "new" && arg1 != "version" && arg1 != "help" {
		err := godotenv.Load()
		if err != nil {
			exitGracefully(err)
		}

		path, err := os.Getwd()
		if err != nil {
			exitGracefully(err)
		}

		// Load configuration
		cfg, err := config.Load()
		if err != nil {
			exitGracefully(err)
		}

		gem.RootPath = path
		gem.Version = "0.0.1"
		gem.Config = cfg
		gem.DB.DataType = cfg.Database.Type
	}
}

func getDSN() string {
	dbType := gem.DB.DataType
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

	return "mysql://" + gem.BuildDSN()
}

func showHelp() {
	color.Yellow(`Available commands:

	help 					- show this help
	version 				- show Gemquick version
	mcp 					- start MCP server for AI assistants
	migrate 				- runs all migrations up
	migrate down 			- runs the last migration down
	migrate reset 			- drops all tables and migrates them back up
	make auth				- creates things for autentications
	make handler <name>		- creates a new stub handler in the handlers directory
	make migration <name>	- creates two new migrations, up and down
	make model <name>		- creates a new model in the data directory
	make session			- creates a table in the database to store sessions
	make mail <name>		- creates a new email in the email directory

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
