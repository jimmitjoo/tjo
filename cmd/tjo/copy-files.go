package main

import (
	"embed"
	"errors"
	"os"
)

//go:embed templates
var templateFS embed.FS

func copyFileFromTemplate(templatePath, targetPath string) error {
	// check to ensure targetPath does not already exist
	if fileExists(targetPath) {
		return errors.New(targetPath + " does already exist.")
	}

	// read template file
	data, err := templateFS.ReadFile(templatePath)
	if err != nil {
		exitGracefully(err)
	}

	// write to targetPath
	err = copyDataToFile(data, targetPath)
	if err != nil {
		exitGracefully(err)
	}

	return nil
}

func copyDataToFile(data []byte, targetPath string) error {

	err := os.WriteFile(targetPath, data, 0644)
	if err != nil {
		exitGracefully(err)
	}

	return nil
}

func fileExists(fileToCheck string) bool {
	if _, err := os.Stat(fileToCheck); os.IsNotExist(err) {
		return false
	}

	return true
}
