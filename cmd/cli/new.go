package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/fatih/color"
	"github.com/go-git/go-git/v5"
	"github.com/jimmitjoo/gemquick/core"
)

var appUrl string

// Available templates
var validTemplates = map[string]bool{
	"default": true,
	"blog":    true,
	"api":     true,
	"saas":    true,
}

func doNew(appName string, template string) error {
	appname := strings.ToLower(appName)
	appUrl = appname

	// Validate template
	if !validTemplates[template] {
		return fmt.Errorf("unknown template: %s. Available templates: default, blog, api, saas", template)
	}

	// Sanitize the new application name
	if strings.Contains(appname, "/") {
		exploded := strings.SplitAfter(appname, "/")
		appname = exploded[len(exploded)-1]
	}

	if template != "default" {
		color.Green("Creating new application: %s (template: %s)", appname, template)
	} else {
		color.Green("Creating new application: " + appname)
	}

	// Git clone the skeleton application
	color.Green("\tCloning skeleton application...")
	_, err := git.PlainClone("./"+appname, false, &git.CloneOptions{
		URL:      "https://github.com/jimmitjoo/gemquick-bare.git",
		Progress: os.Stdout,
		Depth:    1,
	})
	if err != nil {
		exitGracefully(err)
	}

	// Remove .git directory
	err = os.RemoveAll("./" + appname + "/.git")
	if err != nil {
		exitGracefully(err)
	}

	// Create a ready to go .env file
	color.Green("\tCreating .env file...")
	data, err := templateFS.ReadFile("templates/env.txt")
	if err != nil {
		exitGracefully(err)
	}

	env := string(data)
	env = strings.ReplaceAll(env, "${APP_NAME}", appname)
	env = strings.ReplaceAll(env, "${KEY}", core.RandomString(32))

	err = copyDataToFile([]byte(env), "./"+appname+"/.env")
	if err != nil {
		exitGracefully(err)
	}

	// Create a Makefile
	color.Green("\tCreating Makefile...")

	if runtime.GOOS == "windows" {
		source, err := os.Open(fmt.Sprintf("./%s/Makefile.windows", appname))
		if err != nil {
			exitGracefully(err)
		}
		defer source.Close()

		destination, err := os.Create(fmt.Sprintf("./%s/Makefile", appname))
		if err != nil {
			exitGracefully(err)
		}
		defer destination.Close()

		_, err = io.Copy(destination, source)
		if err != nil {
			exitGracefully(err)
		}

	} else {
		source, err := os.Open(fmt.Sprintf("./%s/Makefile.mac", appname))
		if err != nil {
			exitGracefully(err)
		}
		defer source.Close()

		destination, err := os.Create(fmt.Sprintf("./%s/Makefile", appname))
		if err != nil {
			exitGracefully(err)
		}
		defer destination.Close()

		_, err = io.Copy(destination, source)
		if err != nil {
			exitGracefully(err)
		}
	}

	os.Remove(fmt.Sprintf("./%s/Makefile.windows", appname))
	os.Remove(fmt.Sprintf("./%s/Makefile.mac", appname))

	// Update the go.mod file
	color.Green("\tCreating go.mod file...")
	os.Remove(fmt.Sprintf("./%s/go.mod", appname))

	data, err = templateFS.ReadFile("templates/go.mod.txt")
	if err != nil {
		exitGracefully(err)
	}

	gomod := string(data)
	gomod = strings.ReplaceAll(gomod, "${APP_NAME}", appname)

	err = copyDataToFile([]byte(gomod), "./"+appname+"/go.mod")
	if err != nil {
		exitGracefully(err)
	}

	// Update the existing .go files with correct name/imports
	color.Green("\tUpdating source files...")
	err = os.Chdir("./" + appname)
	if err != nil {
		return err
	}
	updateSource()

	// Apply starter template if not default
	if template != "default" {
		color.Green("\tApplying %s template...", template)
		err = applyStarterTemplate(template, appname)
		if err != nil {
			return err
		}
	}

	// Run go mod tidy
	color.Green("\tRunning go mod tidy...")
	cmd := exec.Command("go", "mod", "tidy")
	err = cmd.Start()
	if err != nil {
		exitGracefully(err)
	}

	color.Green("\tDone building " + appname + "!")
	color.Green("\tGo build something great!")

	return nil
}

// applyStarterTemplate copies starter template files into the project
func applyStarterTemplate(template, appname string) error {
	templateDir := "templates/starters/" + template

	return fs.WalkDir(templateFS, templateDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip the root directory
		if path == templateDir {
			return nil
		}

		// Calculate relative path from template dir
		relPath := strings.TrimPrefix(path, templateDir+"/")
		targetPath := relPath

		// Convert .go.txt back to .go
		if strings.HasSuffix(targetPath, ".go.txt") {
			targetPath = strings.TrimSuffix(targetPath, ".txt")
		}

		if d.IsDir() {
			// Create directory
			return os.MkdirAll(targetPath, 0755)
		}

		// Read template file
		data, err := templateFS.ReadFile(path)
		if err != nil {
			return err
		}

		// Replace placeholders
		content := string(data)
		content = strings.ReplaceAll(content, "${APP_NAME}", appname)
		content = strings.ReplaceAll(content, "{{.ProjectName}}", appname)

		// Ensure parent directory exists
		parentDir := filepath.Dir(targetPath)
		if parentDir != "." {
			if err := os.MkdirAll(parentDir, 0755); err != nil {
				return err
			}
		}

		// Write file (overwrite if exists)
		return os.WriteFile(targetPath, []byte(content), 0644)
	})
}
