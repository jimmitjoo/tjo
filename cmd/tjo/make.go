package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/gertd/go-pluralize"
	"github.com/iancoleman/strcase"
	"github.com/jimmitjoo/tjo/core"
)

func doMake(arg2, arg3 string) error {

	switch arg2 {
	case "key":
		handleKey()

	case "auth":
		return doAuth()

	case "mail":
		return doMail(arg3)

	case "handler":
		return doHandler(arg3)

	case "migration":
		return doMigration(arg3)

	case "model":
		return doModel(arg3)

	case "session":
		return doSession()

	case "api-controller":
		return doAPIController(arg3)

	case "controller":
		return doResourceController(arg3)

	case "middleware":
		return doMiddleware(arg3)

	case "test":
		return doTest(arg3)

	case "docker":
		return doDocker()

	case "deploy":
		return doDeploy()

	default:
		return errors.New("Unknown subcommand " + arg2)
	}

	return nil
}

func handleKey() {
	rnd := core.RandomString(32)
	color.Green("Your new encryption key is: %s", rnd)
}

// getRootPath returns the root path, falling back to current directory if cfg is nil
func getRootPath() string {
	if cfg != nil && cfg.RootPath != "" {
		return cfg.RootPath
	}
	// Fallback to current directory for tests
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

func doHandler(name string) error {
	if name == "" {
		return errors.New("you must give the handler a name")
	}

	rootPath := getRootPath()
	fileName := rootPath + "/handlers/" + strings.ToLower(name) + ".go"
	if fileExists(fileName) {
		return errors.New(fileName + " already exists.")
	}

	data, err := templateFS.ReadFile("templates/handlers/handler.go.txt")
	if err != nil {
		return err
	}

	handler := string(data)
	handler = strings.ReplaceAll(handler, "$HANDLERNAME$", strcase.ToCamel(name))

	err = os.WriteFile(fileName, []byte(handler), 0644)
	if err != nil {
		return err
	}
	
	return nil
}

func doMigration(name string) error {
	if name == "" {
		return errors.New("migration name is required")
	}

	// check if there is a database connection
	if cfg == nil || cfg.DBType == "" {
		return errors.New("you have to define a database type to create migrations")
	}

	dbType := cfg.DBType
	fileName := fmt.Sprintf("%d_%s.%s", time.Now().UnixMicro(), name, dbType)

	rootPath := getRootPath()
	migrationUpFile := rootPath + "/migrations/" + fileName + ".up.sql"
	migrationDownFile := rootPath + "/migrations/" + fileName + ".down.sql"

	err := copyFileFromTemplate("templates/migrations/migration."+dbType+".up.sql", migrationUpFile)
	if err != nil {
		return err
	}

	err = copyFileFromTemplate("templates/migrations/migration."+dbType+".down.sql", migrationDownFile)
	if err != nil {
		return err
	}

	err = reformatMigration(migrationUpFile, name)
	if err != nil {
		return err
	}
	
	err = reformatMigration(migrationDownFile, name)
	if err != nil {
		return err
	}
	
	return nil
}

func reformatMigration(migrationFile string, tableName string) error {
	content, err := os.ReadFile(migrationFile)
	if err != nil {
		return err
	}

	if bytes.Contains(content, []byte("TABLENAME")) {
		content = bytes.ReplaceAll(content, []byte("TABLENAME"), []byte(tableName))
		if err = os.WriteFile(migrationFile, content, 0644); err != nil {
			return err
		}
	}
	
	return nil
}

func doModel(name string) error {
	if name == "" {
		return errors.New("model name is required")
	}

	data, err := templateFS.ReadFile("templates/data/model.go.txt")
	if err != nil {
		return err
	}

	model := string(data)
	plural := pluralize.NewClient()
	var modelName = name
	var tableName = name

	if plural.IsPlural(name) {
		modelName = plural.Singular(name)
		tableName = strings.ToLower(name)
	} else {
		tableName = strings.ToLower(plural.Plural(name))
	}

	rootPath := getRootPath()
	fileName := rootPath + "/data/" + strings.ToLower(modelName) + ".go"
	if fileExists(fileName) {
		return errors.New(fileName + " already exists.")
	}

	modelCamelName := strcase.ToCamel(name)
	modelCamelNamePlural := pluralize.NewClient().Plural(modelCamelName)
	model = strings.ReplaceAll(model, "$MODELNAME$", modelCamelName)
	model = strings.ReplaceAll(model, "$TABLENAME$", tableName)

	err = copyDataToFile([]byte(model), fileName)
	if err != nil {
		return err
	}

	color.Green(modelCamelName+" created: %s", fileName)

	// create a migration for the model
	if cfg != nil && cfg.DBType != "" {

		dbType := cfg.DBType
		migrationFileName := fmt.Sprintf("%d_%s.%s", time.Now().UnixMicro(), "create_"+tableName+"_table", dbType)

		migrationUpFile := rootPath + "/migrations/" + migrationFileName + ".up.sql"
		migrationDownFile := rootPath + "/migrations/" + migrationFileName + ".down.sql"

		err = copyFileFromTemplate("templates/migrations/migration."+dbType+".up.sql", migrationUpFile)
		if err != nil {
			return err
		}

		err = copyFileFromTemplate("templates/migrations/migration."+dbType+".down.sql", migrationDownFile)
		if err != nil {
			return err
		}

		err = reformatMigration(migrationUpFile, tableName)
		if err != nil {
			return err
		}
		
		err = reformatMigration(migrationDownFile, tableName)
		if err != nil {
			return err
		}

		color.Green("Migrations for model %s created: %s", modelCamelName, migrationFileName)

		// add model to models.go
		modelsContent, err := os.ReadFile(rootPath + "/data/models.go")
		if err != nil {
			return err
		}

		// replace stubfile with the new model
		if bytes.Contains(modelsContent, []byte(modelCamelName)) {
			return errors.New(modelCamelName + " already exists in models.go")
		} else {
			modelsContent = bytes.Replace(modelsContent, []byte("type Models struct {"), []byte("type Models struct {\n\t"+modelCamelNamePlural+" "+modelCamelName+"\n"), 1)
			modelsContent = bytes.Replace(modelsContent, []byte("return Models{"), []byte("return Models{\n\t\t"+modelCamelNamePlural+": "+modelCamelName+"{},\n"), 1)
			if err = os.WriteFile(rootPath+"/data/models.go", modelsContent, 0644); err != nil {
				return err
			}

			color.Green(modelCamelName + " added to models.go")
		}
	}
	
	return nil
}

// doAPIController creates a new API controller
func doAPIController(name string) error {
	if name == "" {
		return errors.New("you must give the API controller a name")
	}

	rootPath := getRootPath()
	controllerName := strcase.ToCamel(name)
	fileName := rootPath + "/controllers/" + strings.ToLower(name) + "_controller.go"
	
	if fileExists(fileName) {
		return errors.New(fileName + " already exists.")
	}

	data, err := templateFS.ReadFile("templates/controllers/api-controller.go.txt")
	if err != nil {
		return err
	}

	controller := string(data)
	controller = strings.ReplaceAll(controller, "$CONTROLLERNAME$", controllerName)
	controller = strings.ReplaceAll(controller, "$CONTROLLERNAME_LOWER$", strings.ToLower(name))
	controller = strings.ReplaceAll(controller, "$MODEL_NAME$", controllerName)
	controller = strings.ReplaceAll(controller, "$ROUTE_PREFIX$", pluralize.NewClient().Plural(strings.ToLower(name)))

	// Create controllers directory if it doesn't exist
	if err := os.MkdirAll(rootPath+"/controllers", 0755); err != nil {
		return err
	}

	err = os.WriteFile(fileName, []byte(controller), 0644)
	if err != nil {
		return err
	}

	color.Green("API Controller created: %s", fileName)
	
	// Also create the test file
	testFileName := rootPath + "/controllers/" + strings.ToLower(name) + "_controller_test.go"
	
	testData, err := templateFS.ReadFile("templates/tests/api_controller_test.go.txt")
	if err == nil {
		test := string(testData)
		test = strings.ReplaceAll(test, "$CONTROLLERNAME$", controllerName)
		test = strings.ReplaceAll(test, "$ROUTE_PREFIX$", pluralize.NewClient().Plural(strings.ToLower(name)))
		
		err = os.WriteFile(testFileName, []byte(test), 0644)
		if err == nil {
			color.Green("Test file created: %s", testFileName)
		}
	}

	return nil
}

// doResourceController creates a new resource controller
func doResourceController(name string) error {
	if name == "" {
		return errors.New("you must give the resource controller a name")
	}

	rootPath := getRootPath()
	controllerName := strcase.ToCamel(name)
	fileName := rootPath + "/handlers/" + strings.ToLower(name) + "_handlers.go"
	
	if fileExists(fileName) {
		return errors.New(fileName + " already exists.")
	}

	data, err := templateFS.ReadFile("templates/controllers/resource-controller.go.txt")
	if err != nil {
		return err
	}

	controller := string(data)
	controller = strings.ReplaceAll(controller, "$CONTROLLERNAME$", controllerName)
	controller = strings.ReplaceAll(controller, "$CONTROLLERNAME_LOWER$", strings.ToLower(name))

	err = os.WriteFile(fileName, []byte(controller), 0644)
	if err != nil {
		return err
	}

	color.Green("Resource Controller created: %s", fileName)
	
	// Also create the test file
	testFileName := rootPath + "/handlers/" + strings.ToLower(name) + "_handlers_test.go"
	
	testData, err := templateFS.ReadFile("templates/tests/handler_test.go.txt")
	if err == nil {
		test := string(testData)
		test = strings.ReplaceAll(test, "$HANDLERNAME$", controllerName+"Index")
		
		err = os.WriteFile(testFileName, []byte(test), 0644)
		if err == nil {
			color.Green("Test file created: %s", testFileName)
		}
	}

	return nil
}

// doMiddleware creates a new middleware
func doMiddleware(name string) error {
	if name == "" {
		return errors.New("you must give the middleware a name")
	}

	rootPath := getRootPath()
	middlewareName := strcase.ToCamel(name)
	fileName := rootPath + "/middleware/" + strings.ToLower(name) + ".go"
	
	if fileExists(fileName) {
		return errors.New(fileName + " already exists.")
	}

	// Create middleware directory if it doesn't exist
	if err := os.MkdirAll(rootPath+"/middleware", 0755); err != nil {
		return err
	}

	var templateName string
	if strings.ToLower(name) == "cors" {
		templateName = "templates/middleware/cors.go.txt"
	} else {
		templateName = "templates/middleware/middleware.go.txt"
	}

	data, err := templateFS.ReadFile(templateName)
	if err != nil {
		return err
	}

	middleware := string(data)
	middleware = strings.ReplaceAll(middleware, "$MIDDLEWARENAME$", middlewareName)
	middleware = strings.ReplaceAll(middleware, "$MIDDLEWARE_DESCRIPTION$", "performs "+strings.ToLower(name)+" middleware logic")

	err = os.WriteFile(fileName, []byte(middleware), 0644)
	if err != nil {
		return err
	}

	color.Green("Middleware created: %s", fileName)
	return nil
}

// doTest creates a test file for existing handlers/controllers
func doTest(name string) error {
	if name == "" {
		return errors.New("you must specify what to create a test for")
	}

	rootPath := getRootPath()
	testName := strcase.ToCamel(name)
	fileName := rootPath + "/handlers/" + strings.ToLower(name) + "_test.go"
	
	if fileExists(fileName) {
		return errors.New(fileName + " already exists.")
	}

	data, err := templateFS.ReadFile("templates/tests/handler_test.go.txt")
	if err != nil {
		return err
	}

	test := string(data)
	test = strings.ReplaceAll(test, "$HANDLERNAME$", testName)

	err = os.WriteFile(fileName, []byte(test), 0644)
	if err != nil {
		return err
	}

	color.Green("Test file created: %s", fileName)
	return nil
}

// doDocker creates Docker configuration files
func doDocker() error {
	files := map[string]string{
		"Dockerfile":              "templates/docker/Dockerfile.txt",
		"Dockerfile.dev":          "templates/docker/Dockerfile.dev.txt",
		"docker-compose.yml":      "templates/docker/docker-compose.yml.txt",
		"docker-compose.dev.yml":  "templates/docker/docker-compose.dev.yml.txt",
		"nginx.conf":              "templates/docker/nginx.conf.txt",
		".air.toml":               "templates/docker/air.toml.txt",
	}

	rootPath := getRootPath()
	for fileName, templatePath := range files {
		filePath := rootPath + "/" + fileName
		
		if fileExists(filePath) {
			color.Yellow("File %s already exists, skipping...", fileName)
			continue
		}

		data, err := templateFS.ReadFile(templatePath)
		if err != nil {
			return err
		}

		content := string(data)
		// Replace placeholders with actual app name
		var appName string
		if cfg != nil && cfg.AppName != "" {
			appName = strings.ToLower(cfg.AppName)
		} else {
			appName = "testapp"
		}
		content = strings.ReplaceAll(content, "$APPNAME", appName)
		content = strings.ReplaceAll(content, "$PORT", os.Getenv("PORT"))

		err = os.WriteFile(filePath, []byte(content), 0644)
		if err != nil {
			return err
		}

		color.Green("Docker file created: %s", fileName)
	}

	return nil
}

// doDeploy creates deployment scripts and configuration
func doDeploy() error {
	files := map[string]string{
		"deploy.sh":                       "templates/deploy/deploy.sh.txt",
		".github/workflows/deploy.yml":   "templates/deploy/github-actions.yml.txt",
		"Makefile":                       "templates/deploy/Makefile.txt",
	}

	rootPath := getRootPath()
	for fileName, templatePath := range files {
		filePath := rootPath + "/" + fileName
		
		// Create directory if it doesn't exist
		dir := rootPath + "/" + strings.Join(strings.Split(fileName, "/")[:len(strings.Split(fileName, "/"))-1], "/")
		if dir != rootPath+"/" {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return err
			}
		}
		
		if fileExists(filePath) {
			color.Yellow("File %s already exists, skipping...", fileName)
			continue
		}

		data, err := templateFS.ReadFile(templatePath)
		if err != nil {
			return err
		}

		content := string(data)
		// Replace placeholders with actual app name
		var appName string
		if cfg != nil && cfg.AppName != "" {
			appName = strings.ToLower(cfg.AppName)
		} else {
			appName = "testapp"
		}
		content = strings.ReplaceAll(content, "$APPNAME", appName)
		content = strings.ReplaceAll(content, "$GITHUB_REPOSITORY", "user/"+appName)
		content = strings.ReplaceAll(content, "$SERVER_NAME", appName+".com")

		// Make deploy.sh executable
		var fileMode os.FileMode = 0644
		if fileName == "deploy.sh" {
			fileMode = 0755
		}

		err = os.WriteFile(filePath, []byte(content), fileMode)
		if err != nil {
			return err
		}

		color.Green("Deploy file created: %s", fileName)
	}

	return nil
}
