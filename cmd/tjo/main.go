package main

import (
	"errors"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/jimmitjoo/tjo/core"
)

// cfg holds the CLI configuration (minimal subset of framework)
var cfg *core.CLIConfig

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
		name := projectNameFrom(os.Args[2:])
		if name == "" {
			exitGracefully(errors.New("new requires a project name, e.g. tjo new myapp -d sqlite"))
		}
		template := parseTemplateFlag()
		dbType := parseDBFlag()
		err := doNew(name, template, dbType)
		if err != nil {
			exitGracefully(err)
		}
	case "version":
		color.Green("Tjo version: %s", core.Version)
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

	case "deploy":
		root, wderr := os.Getwd()
		if wderr != nil {
			exitGracefully(wderr)
		}
		if arg2 == "init" {
			err = doDeployInit(root)
		} else {
			err = doDeploy(root)
		}
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

// exitGracefully reports the problem and exits.
//
// It used to only print and return, so every one of its 28 call sites carried
// on as though nothing had happened and the process ended with status 0. The
// CLI would print "Error: ..." and then tell the shell it had succeeded, which
// means no script, Makefile or CI job could detect a failed scaffold.
//
// exitCode is a variable so tests can substitute a recorder instead of killing
// the test binary.
var exitCode = os.Exit

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

	if err != nil {
		exitCode(1)
		return
	}

	exitCode(0)
}

// flagsTakingValue are the flags whose next argument is their value, so the
// project name is not mistaken for one.
var flagsTakingValue = map[string]bool{
	"-t": true, "--template": true,
	"-d": true, "--db": true,
}

// projectNameFrom picks the project name out of the arguments, stepping over
// flags and the values they consume.
//
// The name used to be read positionally as os.Args[2], so `tjo new -d sqlite
// myapp` -- flags before the name, which is how most people type it -- created
// a directory literally called "-d" containing `module -d`, which is not a
// valid module path and is awkward to delete.
func projectNameFrom(args []string) string {
	for i := 0; i < len(args); i++ {
		arg := args[i]

		if !strings.HasPrefix(arg, "-") {
			return arg
		}

		// --flag=value carries its value inline; --flag takes the next one.
		if !strings.Contains(arg, "=") && flagsTakingValue[arg] {
			i++
		}
	}

	return ""
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

func parseDBFlag() string {
	for i, arg := range os.Args {
		if arg == "--db" || arg == "-d" {
			if i+1 < len(os.Args) {
				return os.Args[i+1]
			}
		}
		// Support --db=value format
		if len(arg) > 5 && arg[:5] == "--db=" {
			return arg[5:]
		}
		if len(arg) > 3 && arg[:3] == "-d=" {
			return arg[3:]
		}
	}
	return ""
}
