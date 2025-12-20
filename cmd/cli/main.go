package main

import (
	"errors"
	"os"

	"github.com/fatih/color"
	"github.com/jimmitjoo/gemquick"
)

var gem gemquick.Gemquick

func main() {
	var message string
	arg1, arg2, arg3, err := validateInput()
	if err != nil {
		exitGracefully(err)
	}

	// MCP runs standalone without .env
	if arg1 == "mcp" {
		if err := doMCP(); err != nil {
			exitGracefully(err)
		}
		return
	}

	setup(arg1, arg2)

	switch arg1 {
	case "new":
		if arg2 == "" {
			exitGracefully(errors.New("new requires a project name"))
		}
		template := parseTemplateFlag()
		err := doNew(arg2, template)
		if err != nil {
			exitGracefully(err)
		}
	case "version":
		color.Green("Gemquick version: %s", gem.Version)
	case "help":
		showHelp()
	case "make":
		if arg2 == "" {
			exitGracefully(errors.New("make requires a subcommand"))
		}
		err = doMake(arg2, arg3)
		if err != nil {
			exitGracefully(err)
		}
	case "migrate":
		if arg2 == "" {
			arg2 = "up"
		}

		err = doMigrate(arg2, arg3)
		if err != nil {
			exitGracefully(err)
		}

		message = "Migrations completed"

	case "run":
		watch := arg2 == "--watch" || arg2 == "-w"
		err = doRun(watch)
		if err != nil {
			exitGracefully(err)
		}

	default:
		showHelp()
	}

	exitGracefully(nil, message)
}

func validateInput() (string, string, string, error) {
	var arg1, arg2, arg3 string

	if len(os.Args) > 1 {
		arg1 = os.Args[1]

		if len(os.Args) > 2 {
			arg2 = os.Args[2]
		}

		if len(os.Args) > 3 {
			arg3 = os.Args[3]
		}
	} else {
		color.Red("Please provide a command")
		showHelp()
		return "", "", "", errors.New("no command provided")
	}

	return arg1, arg2, arg3, nil
}

func exitGracefully(err error, msg ...string) {
	message := ""
	if len(msg) > 0 {
		message = msg[0]
	}

	if err != nil {
		color.Red("Error: %v\n", err)
	}

	if message != "" {
		color.Yellow(message)
	}
}

func parseTemplateFlag() string {
	for i, arg := range os.Args {
		if arg == "--template" || arg == "-t" {
			if i+1 < len(os.Args) {
				return os.Args[i+1]
			}
		}
		// Support --template=value format
		if len(arg) > 11 && arg[:11] == "--template=" {
			return arg[11:]
		}
		if len(arg) > 3 && arg[:3] == "-t=" {
			return arg[3:]
		}
	}
	return "default"
}
