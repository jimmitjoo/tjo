package tjo

import (
	"log"
	"strings"

	_ "github.com/go-sql-driver/mysql"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/mysql"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

func (g *Tjo) MigrateUp(dsn string) error {
	var pathBuilder strings.Builder
	pathBuilder.WriteString("file://")
	pathBuilder.WriteString(g.RootPath)
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

func (g *Tjo) MigrateDownAll(dsn string) error {
	var pathBuilder strings.Builder
	pathBuilder.WriteString("file://")
	pathBuilder.WriteString(g.RootPath)
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

func (g *Tjo) Steps(steps int, dsn string) error {
	var pathBuilder strings.Builder
	pathBuilder.WriteString("file://")
	pathBuilder.WriteString(g.RootPath)
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

func (g *Tjo) MigrateForce(dsn string) error {
	var pathBuilder strings.Builder
	pathBuilder.WriteString("file://")
	pathBuilder.WriteString(g.RootPath)
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
