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
	rootPath := getRootPath()

	// Prompt for database if not configured
	dbType, err := promptForDatabase()
	if err != nil {
		return err
	}

	// migrations
	fileName := fmt.Sprintf("%d_create_auth_tables", time.Now().UnixMicro())
	var pathBuilder strings.Builder

	// Build upFile path
	pathBuilder.WriteString(rootPath)
	pathBuilder.WriteString("/migrations/")
	pathBuilder.WriteString(fileName)
	pathBuilder.WriteString(".up.sql")
	upFile := pathBuilder.String()

	// Build downFile path
	pathBuilder.Reset()
	pathBuilder.WriteString(rootPath)
	pathBuilder.WriteString("/migrations/")
	pathBuilder.WriteString(fileName)
	pathBuilder.WriteString(".down.sql")
	downFile := pathBuilder.String()

	// Build routesFile path
	pathBuilder.Reset()
	pathBuilder.WriteString(rootPath)
	pathBuilder.WriteString("/routes.go")
	routesFile := pathBuilder.String()

	err = copyFileFromTemplate("templates/migrations/auth_tables."+dbType+".up.sql", upFile)
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
	err = copyFileFromTemplate("templates/data/user.go.txt", rootPath+"/data/user.go")
	if err != nil {
		exitGracefully(err)
	}

	err = copyFileFromTemplate("templates/data/token.go.txt", rootPath+"/data/token.go")
	if err != nil {
		exitGracefully(err)
	}

	err = copyFileFromTemplate("templates/data/remember_token.go.txt", rootPath+"/data/remember_token.go")
	if err != nil {
		exitGracefully(err)
	}

	// create middleware
	err = copyFileFromTemplate("templates/middleware/auth.go.txt", rootPath+"/middleware/auth.go")
	if err != nil {
		exitGracefully(err)
	}

	err = copyFileFromTemplate("templates/middleware/auth-token.go.txt", rootPath+"/middleware/auth-token.go")
	if err != nil {
		exitGracefully(err)
	}

	err = copyFileFromTemplate("templates/middleware/remember.go.txt", rootPath+"/middleware/remember.go")
	if err != nil {
		exitGracefully(err)
	}

	err = copyFileFromTemplate("templates/handlers/auth-handlers.go.txt", rootPath+"/handlers/auth-handlers.go")
	if err != nil {
		exitGracefully(err)
	}

	err = copyFileFromTemplate("templates/handlers/totp-handlers.go.txt", rootPath+"/handlers/totp-handlers.go")
	if err != nil {
		exitGracefully(err)
	}

	err = copyFileFromTemplate("templates/email/welcome.html.tmpl", rootPath+"/email/welcome.html.tmpl")
	if err != nil {
		exitGracefully(err)
	}

	err = copyFileFromTemplate("templates/email/welcome.plain.tmpl", rootPath+"/email/welcome.plain.tmpl")
	if err != nil {
		exitGracefully(err)
	}

	err = copyFileFromTemplate("templates/email/password-reset.html.tmpl", rootPath+"/email/password-reset.html.tmpl")
	if err != nil {
		exitGracefully(err)
	}

	err = copyFileFromTemplate("templates/email/password-reset.plain.tmpl", rootPath+"/email/password-reset.plain.tmpl")
	if err != nil {
		exitGracefully(err)
	}

	err = copyFileFromTemplate("templates/views/login.jet", rootPath+"/views/login.jet")
	if err != nil {
		exitGracefully(err)
	}

	err = copyFileFromTemplate("templates/views/register.jet", rootPath+"/views/register.jet")
	if err != nil {
		exitGracefully(err)
	}

	err = copyFileFromTemplate("templates/views/forgot.jet", rootPath+"/views/forgot.jet")
	if err != nil {
		exitGracefully(err)
	}

	err = copyFileFromTemplate("templates/views/reset-password.jet", rootPath+"/views/reset-password.jet")
	if err != nil {
		exitGracefully(err)
	}

	err = copyFileFromTemplate("templates/views/2fa-setup.jet", rootPath+"/views/2fa-setup.jet")
	if err != nil {
		exitGracefully(err)
	}

	err = copyFileFromTemplate("templates/views/2fa-challenge.jet", rootPath+"/views/2fa-challenge.jet")
	if err != nil {
		exitGracefully(err)
	}

	err = copyFileFromTemplate("templates/views/2fa-disable.jet", rootPath+"/views/2fa-disable.jet")
	if err != nil {
		exitGracefully(err)
	}

	// read models.go
	modelsContent, err := os.ReadFile(rootPath + "/data/models.go")
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
		if err = os.WriteFile(rootPath+"/data/models.go", output, 0644); err != nil {
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

	// copy templates/2fa.routes.txt into a variable
	tfaRoutes, err := templateFS.ReadFile("templates/2fa.routes.txt")
	if err != nil {
		exitGracefully(err)
	}

	// find the line with 'return route.App.Routes' in routesContent
	output := bytes.Replace(routesContent, []byte("return route.App.Routes"), []byte(string(authRoutes)+"\n\n"+string(tfaRoutes)+"\n\n\treturn route.App.Routes"), 1)
	if err = os.WriteFile(routesFile, output, 0644); err != nil {
		exitGracefully(err)
	}

	color.Yellow("  - users, tokens and remember_tokens migrations created and ran")
	color.Yellow("  - user and token models created")
	color.Yellow("  - auth middleware created")
	color.Yellow("  - 2FA (TOTP) support included")
	color.Yellow("")
	color.Yellow("Don't forget to add user and token models in data/models.go, and to add appropriate middlewares to your routes.")
	color.Yellow("To enable 2FA for a user, direct them to /user/2fa/setup")

	return nil
}
