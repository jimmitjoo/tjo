package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/fatih/color"
)

func doAuth() error {

	// check if there is a database connection
	if gem.Data.DB.DataType == "" {
		return errors.New("you have to define a database type to be able to use authentication")
	}

	// migrations
	dbType := gem.Data.DB.DataType
	fileName := fmt.Sprintf("%d_create_auth_tables", time.Now().UnixMicro())
	var pathBuilder strings.Builder
	
	// Build upFile path
	pathBuilder.WriteString(gem.RootPath)
	pathBuilder.WriteString("/migrations/")
	pathBuilder.WriteString(fileName)
	pathBuilder.WriteString(".up.sql")
	upFile := pathBuilder.String()
	
	// Build downFile path
	pathBuilder.Reset()
	pathBuilder.WriteString(gem.RootPath)
	pathBuilder.WriteString("/migrations/")
	pathBuilder.WriteString(fileName)
	pathBuilder.WriteString(".down.sql")
	downFile := pathBuilder.String()
	
	// Build routesFile path
	pathBuilder.Reset()
	pathBuilder.WriteString(gem.RootPath)
	pathBuilder.WriteString("/routes.go")
	routesFile := pathBuilder.String()

	err := copyFileFromTemplate("templates/migrations/auth_tables."+dbType+".up.sql", upFile)
	if err != nil {
		exitGracefully(err)
	}

	err = copyDataToFile([]byte("DROP TABLE IF EXISTS users CASCADE;DROP TABLE IF EXISTS tokens CASCADE;DROP TABLE IF EXISTS remember_tokens CASCADE;"), downFile)
	if err != nil {
		exitGracefully(err)
	}

	// run migrations
	err = doMigrate("up", "")
	if err != nil {
		exitGracefully(err)
	}

	// create models
	err = copyFileFromTemplate("templates/data/user.go.txt", gem.RootPath+"/data/user.go")
	if err != nil {
		exitGracefully(err)
	}

	err = copyFileFromTemplate("templates/data/token.go.txt", gem.RootPath+"/data/token.go")
	if err != nil {
		exitGracefully(err)
	}

	err = copyFileFromTemplate("templates/data/remember_token.go.txt", gem.RootPath+"/data/remember_token.go")
	if err != nil {
		exitGracefully(err)
	}

	// create middleware
	err = copyFileFromTemplate("templates/middleware/auth.go.txt", gem.RootPath+"/middleware/auth.go")
	if err != nil {
		exitGracefully(err)
	}

	err = copyFileFromTemplate("templates/middleware/auth-token.go.txt", gem.RootPath+"/middleware/auth-token.go")
	if err != nil {
		exitGracefully(err)
	}

	err = copyFileFromTemplate("templates/middleware/remember.go.txt", gem.RootPath+"/middleware/remember.go")
	if err != nil {
		exitGracefully(err)
	}

	err = copyFileFromTemplate("templates/handlers/auth-handlers.go.txt", gem.RootPath+"/handlers/auth-handlers.go")
	if err != nil {
		exitGracefully(err)
	}

	err = copyFileFromTemplate("templates/email/welcome.html.tmpl", gem.RootPath+"/email/welcome.html.tmpl")
	if err != nil {
		exitGracefully(err)
	}

	err = copyFileFromTemplate("templates/email/welcome.plain.tmpl", gem.RootPath+"/email/welcome.plain.tmpl")
	if err != nil {
		exitGracefully(err)
	}

	err = copyFileFromTemplate("templates/email/password-reset.html.tmpl", gem.RootPath+"/email/password-reset.html.tmpl")
	if err != nil {
		exitGracefully(err)
	}

	err = copyFileFromTemplate("templates/email/password-reset.plain.tmpl", gem.RootPath+"/email/password-reset.plain.tmpl")
	if err != nil {
		exitGracefully(err)
	}

	err = copyFileFromTemplate("templates/views/login.jet", gem.RootPath+"/views/login.jet")
	if err != nil {
		exitGracefully(err)
	}

	err = copyFileFromTemplate("templates/views/register.jet", gem.RootPath+"/views/register.jet")
	if err != nil {
		exitGracefully(err)
	}

	err = copyFileFromTemplate("templates/views/forgot.jet", gem.RootPath+"/views/forgot.jet")
	if err != nil {
		exitGracefully(err)
	}

	err = copyFileFromTemplate("templates/views/reset-password.jet", gem.RootPath+"/views/reset-password.jet")
	if err != nil {
		exitGracefully(err)
	}

	// read models.go
	modelsContent, err := os.ReadFile(gem.RootPath + "/data/models.go")
	if err != nil {
		exitGracefully(err)
	}

	// check if auth models are already added
	if bytes.Contains(modelsContent, []byte("// authentication models - added by make auth command")) {
		exitGracefully(errors.New("auth models are probably already added to data/models.go"))
	} else {
		// copy data/auth.models.txt into a variable
		authModels, err := templateFS.ReadFile("templates/data/auth.models.txt")
		if err != nil {
			exitGracefully(err)
		}

		returnAuthModels, err := templateFS.ReadFile("templates/data/return.auth.models.txt")
		if err != nil {
			exitGracefully(err)
		}

		// find the line with 'return models' in modelsContent
		output := bytes.Replace(modelsContent, []byte("type Models struct {"), []byte("type Models struct {\n\t"+string(authModels)+"\n"), 1)
		output = bytes.Replace(output, []byte("return Models{"), []byte("return Models{\n\t"+string(returnAuthModels)+"\n\t"), 1)
		if err = os.WriteFile(gem.RootPath+"/data/models.go", output, 0644); err != nil {
			exitGracefully(err)
		}
	}

	// read routes.go
	routesContent, err := os.ReadFile(routesFile)
	if err != nil {
		exitGracefully(err)
	}

	// check if auth routes are already added
	if bytes.Contains(routesContent, []byte("// authentication routes - added by make auth command")) {
		exitGracefully(errors.New("auth routes are probably already added to routes.go"))
		return nil
	}

	// copy templates/auth.routes.txt into a variable
	authRoutes, err := templateFS.ReadFile("templates/auth.routes.txt")
	if err != nil {
		exitGracefully(err)
	}

	// find the line with 'return route.App.Routes' in routesContent
	output := bytes.Replace(routesContent, []byte("return route.App.Routes"), []byte(string(authRoutes)+"\n\n\treturn route.App.Routes"), 1)
	if err = os.WriteFile(routesFile, output, 0644); err != nil {
		exitGracefully(err)
	}

	color.Yellow("  - users, tokens and remember_tokens migrations created and ran")
	color.Yellow("  - user and token models created")
	color.Yellow("  - auth middleware created")
	color.Yellow("")
	color.Yellow("Don't forget to add user and token models in data/models.go, and to add appropriate middlewares to your routes.")

	return nil
}
