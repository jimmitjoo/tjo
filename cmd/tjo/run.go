package main

import (
	"os"
	"os/exec"

	"github.com/fatih/color"
)

func doRun(watch bool) error {
	if watch {
		return runWithWatch()
	}
	return runNormal()
}

func runNormal() error {
	cmd := exec.Command("go", "run", ".")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func runWithWatch() error {
	// Check if air is installed
	_, err := exec.LookPath("air")
	if err != nil {
		color.Yellow("Air not found, installing...")
		install := exec.Command("go", "install", "github.com/air-verse/air@latest")
		install.Stdout = os.Stdout
		install.Stderr = os.Stderr
		if err := install.Run(); err != nil {
			return err
		}
		color.Green("Air installed successfully!")
	}

	color.Green("Starting development server with hot-reload...")
	air := exec.Command("air")
	air.Stdout = os.Stdout
	air.Stderr = os.Stderr
	air.Stdin = os.Stdin
	return air.Run()
}
