package main

import (
	"errors"
	"fmt"
	"time"
)

func doSession() error {
	rootPath := getRootPath()

	// Prompt for database if not configured
	dbType, err := promptForDatabase()
	if err != nil {
		return err
	}
	if dbType == "pgx" || dbType == "postgresql" {
		dbType = "postgres"
	} else if dbType == "mariadb" {
		dbType = "mysql"
	}

	fileName := fmt.Sprintf("%d_create_sessions_table", time.Now().UnixMicro())
	if fileExists(fileName) {
		exitGracefully(errors.New(fileName + " already exists."))
	}

	upFile := rootPath + "/migrations/" + fileName + "." + dbType + ".up.sql"
	downFile := rootPath + "/migrations/" + fileName + "." + dbType + ".down.sql"

	err = copyFileFromTemplate("templates/migrations/"+dbType+"_session.sql", upFile)
	if err != nil {
		exitGracefully(err)
	}

	err = copyDataToFile([]byte("DROP TABLE IF EXISTS sessions;"), downFile)
	if err != nil {
		exitGracefully(err)
	}

	err = doMigrate("up", "")
	if err != nil {
		exitGracefully(err)
	}

	return nil
}
