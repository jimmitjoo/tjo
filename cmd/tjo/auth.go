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

	// Create required directories
	dirs := []string{
		rootPath + "/migrations",
		rootPath + "/data",
		rootPath + "/middleware",
		rootPath + "/handlers",
		rootPath + "/email",
		rootPath + "/views",
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
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

	// Map the driver name to the dialect family that actually has a migration.
	// This used to interpolate dbType straight into the filename, so `make auth`
	// failed outright on two of the four databases `tjo new -d` accepts:
	// sqlite had no file, and mariadb looked for one instead of reusing mysql's.
	err = copyFileFromTemplate("templates/migrations/auth_tables."+migrationDialect(dbType)+".up.sql", upFile)
	if err != nil {
		return err
	}

	err = copyDataToFile([]byte("DROP TABLE IF EXISTS users CASCADE;DROP TABLE IF EXISTS tokens CASCADE;DROP TABLE IF EXISTS remember_tokens CASCADE;"), downFile)
	if err != nil {
		return err
	}

	// run migrations
	err = doMigrate("up", "")
	if err != nil {
		return err
	}

	// create models
	err = copyFileFromTemplate("templates/data/user.go.txt", rootPath+"/data/user.go")
	if err != nil {
		return err
	}

	err = copyFileFromTemplate("templates/data/token.go.txt", rootPath+"/data/token.go")
	if err != nil {
		return err
	}

	// create middleware
	err = copyFileFromTemplate("templates/middleware/auth.go.txt", rootPath+"/middleware/auth.go")
	if err != nil {
		return err
	}

	err = copyFileFromTemplate("templates/middleware/auth-token.go.txt", rootPath+"/middleware/auth-token.go")
	if err != nil {
		return err
	}

	err = copyFileFromTemplate("templates/middleware/remember.go.txt", rootPath+"/middleware/remember.go")
	if err != nil {
		return err
	}

	err = copyFileFromTemplate("templates/handlers/auth-handlers.go.txt", rootPath+"/handlers/auth-handlers.go")
	if err != nil {
		return err
	}

	err = copyFileFromTemplate("templates/handlers/totp-handlers.go.txt", rootPath+"/handlers/totp-handlers.go")
	if err != nil {
		return err
	}

	err = copyFileFromTemplate("templates/email/welcome.html.tmpl", rootPath+"/email/welcome.html.tmpl")
	if err != nil {
		return err
	}

	err = copyFileFromTemplate("templates/email/welcome.plain.tmpl", rootPath+"/email/welcome.plain.tmpl")
	if err != nil {
		return err
	}

	err = copyFileFromTemplate("templates/email/password-reset.html.tmpl", rootPath+"/email/password-reset.html.tmpl")
	if err != nil {
		return err
	}

	err = copyFileFromTemplate("templates/email/password-reset.plain.tmpl", rootPath+"/email/password-reset.plain.tmpl")
	if err != nil {
		return err
	}

	err = copyFileFromTemplate("templates/views/login.jet", rootPath+"/views/login.jet")
	if err != nil {
		return err
	}

	err = copyFileFromTemplate("templates/views/2fa-recovery-codes.jet", rootPath+"/views/2fa-recovery-codes.jet")
	if err != nil {
		return err
	}

	err = copyFileFromTemplate("templates/views/register.jet", rootPath+"/views/register.jet")
	if err != nil {
		return err
	}

	err = copyFileFromTemplate("templates/views/forgot.jet", rootPath+"/views/forgot.jet")
	if err != nil {
		return err
	}

	err = copyFileFromTemplate("templates/views/reset-password.jet", rootPath+"/views/reset-password.jet")
	if err != nil {
		return err
	}

	err = copyFileFromTemplate("templates/views/2fa-setup.jet", rootPath+"/views/2fa-setup.jet")
	if err != nil {
		return err
	}

	err = copyFileFromTemplate("templates/views/2fa-challenge.jet", rootPath+"/views/2fa-challenge.jet")
	if err != nil {
		return err
	}

	err = copyFileFromTemplate("templates/views/2fa-disable.jet", rootPath+"/views/2fa-disable.jet")
	if err != nil {
		return err
	}

	// read models.go
	modelsContent, err := os.ReadFile(rootPath + "/data/models.go")
	if err != nil {
		return err
	}

	// check if auth models are already added
	if bytes.Contains(modelsContent, []byte("// authentication models - added by make auth command")) {
		return errors.New("auth models are probably already added to data/models.go")
	}

	// copy data/auth.models.txt into a variable
	authModels, err := templateFS.ReadFile("templates/data/auth.models.txt")
	if err != nil {
		return err
	}

	returnAuthModels, err := templateFS.ReadFile("templates/data/return.auth.models.txt")
	if err != nil {
		return err
	}

	// find the line with 'return models' in modelsContent
	output := bytes.Replace(modelsContent, []byte("type Models struct {"), []byte("type Models struct {\n\t"+string(authModels)+"\n"), 1)
	output = bytes.Replace(output, []byte("return Models{"), []byte("return Models{\n\t"+string(returnAuthModels)+"\n\t"), 1)
	if err = os.WriteFile(rootPath+"/data/models.go", output, 0644); err != nil {
		return err
	}

	// read routes.go
	routesContent, err := os.ReadFile(routesFile)
	if err != nil {
		return err
	}

	// check if auth routes are already added
	if bytes.Contains(routesContent, []byte("// authentication routes - added by make auth command")) {
		return errors.New("auth routes are probably already added to routes.go")
	}

	// copy templates/auth.routes.txt into a variable
	authRoutes, err := templateFS.ReadFile("templates/auth.routes.txt")
	if err != nil {
		return err
	}

	// copy templates/2fa.routes.txt into a variable
	tfaRoutes, err := templateFS.ReadFile("templates/2fa.routes.txt")
	if err != nil {
		return err
	}

	// Insert before the router is returned.
	//
	// The marker used to be "return route.App.Routes", which the routes.go
	// template has not contained for several releases -- so make auth wrote the
	// handlers, wrote nothing to routes.go, and reported success. Every project
	// scaffolded in that window had a complete authentication system that no
	// URL reached. The scaffold job did not catch it because unreachable
	// handlers compile perfectly.
	//
	// Failing loudly now, because "generated an auth system you cannot log in
	// to" is not an outcome worth reporting as done.
	const (
		returnMarker     = "return a.App.HTTP.Router"
		middlewareMarker = "a.App.HTTP.Router.Use(security.SecureMiddleware(a.securityConfig()))"
	)

	if !bytes.Contains(routesContent, []byte(returnMarker)) {
		return fmt.Errorf("could not find %q in routes.go, so the authentication routes were not added -- add them by hand or restore the generated routes() function", returnMarker)
	}
	// chi panics if a middleware is registered after a route, so CheckRemember
	// goes next to the security middleware rather than beside the routes it
	// belongs to.
	if !bytes.Contains(routesContent, []byte(middlewareMarker)) {
		return fmt.Errorf("could not find %q in routes.go, so the remember-me middleware was not added -- add a.App.HTTP.Router.Use(a.Middleware.CheckRemember) above your first route", middlewareMarker)
	}

	rememberMiddleware := middlewareMarker + `

	// Negotiates the reader's language from Accept-Language, a ?lang= parameter
	// or a cookie, and puts it in the request context. Without it every
	// framework-produced string -- validation, flash messages, the admin panel
	// -- comes out in the fallback language.
	a.App.HTTP.Router.Use(i18n.Default().Middleware)

	// Logs a returning visitor back in from their remember cookie, renewing the
	// session as it does. Registered here because chi requires every middleware
	// to be declared before the first route.
	a.App.HTTP.Router.Use(a.Middleware.CheckRemember)`

	// The import, without which the middleware line does not compile. Added
	// next to the security import the marker sits in, so the block stays
	// gofmt-clean.
	routesOutput := bytes.Replace(routesContent,
		[]byte(`"github.com/jimmitjoo/tjo/security"`),
		[]byte("\"github.com/jimmitjoo/tjo/i18n\"\n\t\"github.com/jimmitjoo/tjo/security\""), 1)

	routesOutput = bytes.Replace(routesOutput, []byte(middlewareMarker), []byte(rememberMiddleware), 1)
	routesOutput = bytes.Replace(routesOutput, []byte(returnMarker),
		[]byte(string(authRoutes)+"\n"+string(tfaRoutes)+"\n\t"+returnMarker), 1)

	if err = os.WriteFile(routesFile, routesOutput, 0644); err != nil {
		return err
	}

	color.Yellow("  - users and tokens migrations created and ran")
	color.Yellow("  - user and token models created")
	color.Yellow("  - auth middleware created")
	color.Yellow("  - 2FA (TOTP) support included")
	color.Yellow("")
	color.Yellow("Don't forget to add user and token models in data/models.go, and to add appropriate middlewares to your routes.")
	color.Yellow("To enable 2FA for a user, direct them to /user/2fa/setup")

	return nil
}

// migrationDialect maps a driver name to the migration file family it shares.
func migrationDialect(dbType string) string {
	switch strings.ToLower(dbType) {
	case "postgres", "postgresql", "pgx":
		return "postgres"
	case "mysql", "mariadb":
		return "mysql"
	case "sqlite", "sqlite3":
		return "sqlite"
	default:
		return strings.ToLower(dbType)
	}
}
