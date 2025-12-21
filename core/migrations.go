// Package core provides minimal utilities for CLI tools and code generation.
package core

import (
	"log"
	"strings"

	_ "github.com/go-sql-driver/mysql"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/mysql"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// MigrateUp runs all pending migrations
func MigrateUp(rootPath, dsn string) error {
	var pathBuilder strings.Builder
	pathBuilder.WriteString("file://")
	pathBuilder.WriteString(rootPath)
	pathBuilder.WriteString("/migrations")

	m, err := migrate.New(pathBuilder.String(), dsn)
	if err != nil {
		return err
	}
	defer m.Close()

	if err = m.Up(); err != nil {
		log.Println("Error while migrating up:", err)
		return err
	}

	return nil
}

// MigrateDownAll rolls back all migrations
func MigrateDownAll(rootPath, dsn string) error {
	var pathBuilder strings.Builder
	pathBuilder.WriteString("file://")
	pathBuilder.WriteString(rootPath)
	pathBuilder.WriteString("/migrations")

	m, err := migrate.New(pathBuilder.String(), dsn)
	if err != nil {
		return err
	}
	defer m.Close()

	if err = m.Down(); err != nil {
		return err
	}

	return nil
}

// MigrateSteps runs n migrations (positive = up, negative = down)
func MigrateSteps(steps int, rootPath, dsn string) error {
	var pathBuilder strings.Builder
	pathBuilder.WriteString("file://")
	pathBuilder.WriteString(rootPath)
	pathBuilder.WriteString("/migrations")

	m, err := migrate.New(pathBuilder.String(), dsn)
	if err != nil {
		return err
	}
	defer m.Close()

	if err = m.Steps(steps); err != nil {
		return err
	}

	return nil
}

// MigrateForce forces the migration version without running migrations
func MigrateForce(rootPath, dsn string) error {
	var pathBuilder strings.Builder
	pathBuilder.WriteString("file://")
	pathBuilder.WriteString(rootPath)
	pathBuilder.WriteString("/migrations")

	m, err := migrate.New(pathBuilder.String(), dsn)
	if err != nil {
		return err
	}
	defer m.Close()

	if err = m.Force(-1); err != nil {
		return err
	}

	return nil
}
