// Package core provides minimal utilities for CLI tools and code generation.
// This package has minimal dependencies and can be used without the full framework.
package core

import (
	"crypto/rand"
	"os"

	"github.com/jimmitjoo/tjo/config"
)

// Version is the framework version (injected at build time via ldflags)
// Use: go build -ldflags "-X github.com/jimmitjoo/tjo/core.Version=x.y.z"
var Version = "dev"

// randomStringSource is the character set for random strings
const randomStringSource = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_+"

// RandomString generates a cryptographically secure random string of the given length.
// Uses crypto/rand for secure randomness.
func RandomString(n int) string {
	s, r := make([]rune, n), []rune(randomStringSource)
	for i := range s {
		p := make([]byte, 1)
		_, _ = rand.Read(p)
		s[i] = r[int(p[0])%len(r)]
	}
	return string(s)
}

// CLIConfig holds the minimal configuration needed for CLI operations
type CLIConfig struct {
	RootPath   string
	Version    string
	AppName    string
	DBType     string
	Config     *config.Config
}

// LoadCLIConfig loads the configuration for CLI operations
func LoadCLIConfig() (*CLIConfig, error) {
	path, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}

	return &CLIConfig{
		RootPath: path,
		Version:  Version,
		AppName:  cfg.App.Name,
		DBType:   cfg.Database.Type,
		Config:   cfg,
	}, nil
}

// CreateDirIfNotExists creates a directory if it doesn't exist
func CreateDirIfNotExists(path string) error {
	const mode = 0755
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.MkdirAll(path, mode); err != nil {
			return err
		}
	}
	return nil
}

// CreateFileIfNotExists creates an empty file if it doesn't exist
func CreateFileIfNotExists(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		file, err := os.Create(path)
		if err != nil {
			return err
		}
		defer file.Close()
	}
	return nil
}

// FileExists checks if a file exists
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
