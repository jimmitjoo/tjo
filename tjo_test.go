package tjo

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/jimmitjoo/tjo/config"
)

func TestTjo_New(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "tjo_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// Create required directories
	dirs := []string{"handlers", "migrations", "views", "email", "data", "public", "tmp", "logs", "middleware"}
	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(tempDir, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}

	// Create a test .env file
	envContent := `
APP_NAME=TestApp
DEBUG=false
PORT=4000
SESSION_TYPE=cookie
COOKIE_DOMAIN=localhost
COOKIE_NAME=tjo
COOKIE_LIFETIME=1440
COOKIE_PERSIST=true
COOKIE_SECURE=false
DATABASE_TYPE=
DSN=
CACHE=
REDIS_HOST=
REDIS_PASSWORD=
REDIS_PREFIX=tjo
`
	envFile := filepath.Join(tempDir, ".env")
	if err := os.WriteFile(envFile, []byte(envContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Test New function
	g := &Tjo{}
	err = g.New(tempDir)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	// Verify basic initialization
	if g.AppName != "TestApp" {
		t.Errorf("Expected AppName to be TestApp, got %s", g.AppName)
	}

	if g.Debug != false {
		t.Errorf("Expected Debug to be false, got %v", g.Debug)
	}

	if g.RootPath != tempDir {
		t.Errorf("Expected RootPath to be %s, got %s", tempDir, g.RootPath)
	}
}

func TestTjo_Init(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "tjo_init_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	g := &Tjo{}

	paths := initPaths{
		rootPath:    tempDir,
		folderNames: []string{"test1", "test2"},
	}

	err = g.Init(paths)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	// Check if directories were created
	for _, folder := range paths.folderNames {
		path := filepath.Join(tempDir, folder)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("Expected directory %s to exist", path)
		}
	}
}

func TestTjo_CreateDirIfNotExists(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "tjo_dir_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	g := Tjo{}
	testDir := filepath.Join(tempDir, "newdir")

	// Test creating a new directory
	err = g.CreateDirIfNotExists(testDir)
	if err != nil {
		t.Errorf("Expected no error creating directory, got %v", err)
	}

	// Verify directory exists
	if _, err := os.Stat(testDir); os.IsNotExist(err) {
		t.Error("Expected directory to be created")
	}

	// Test with existing directory (should not error)
	err = g.CreateDirIfNotExists(testDir)
	if err != nil {
		t.Errorf("Expected no error for existing directory, got %v", err)
	}
}

func TestTjo_CreateFileIfNotExists(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "tjo_file_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	g := Tjo{}
	testFile := filepath.Join(tempDir, "test.txt")

	// Test creating a new file
	err = g.CreateFileIfNotExists(testFile)
	if err != nil {
		t.Errorf("Expected no error creating file, got %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(testFile); os.IsNotExist(err) {
		t.Error("Expected file to be created")
	}

	// Test with existing file (should not error)
	err = g.CreateFileIfNotExists(testFile)
	if err != nil {
		t.Errorf("Expected no error for existing file, got %v", err)
	}
}

func TestConfig_Cookie(t *testing.T) {
	c := config.CookieConfig{
		Name:     "test_cookie",
		Lifetime: 1440,
		Persist:  true,
		Secure:   false,
		Domain:   "localhost",
	}

	if c.Name != "test_cookie" {
		t.Errorf("Expected cookie name to be test_cookie, got %s", c.Name)
	}

	if c.Lifetime != 1440 {
		t.Errorf("Expected cookie lifetime to be 1440, got %d", c.Lifetime)
	}
}

func TestServer_Configuration(t *testing.T) {
	s := Server{
		ServerName: "TestServer",
		Port:       "8080",
		Secure:     false,
		URL:        "http://localhost:8080",
	}

	if s.ServerName != "TestServer" {
		t.Errorf("Expected ServerName to be TestServer, got %s", s.ServerName)
	}

	if s.Port != "8080" {
		t.Errorf("Expected Port to be 8080, got %s", s.Port)
	}

	if s.Secure != false {
		t.Errorf("Expected Secure to be false, got %v", s.Secure)
	}
}

func TestBuildDSN(t *testing.T) {
	g := &Tjo{}

	tests := []struct {
		name     string
		expected string
		env      map[string]string
	}{
		{
			name: "PostgreSQL DSN",
			env: map[string]string{
				"DATABASE_TYPE":     "pgx",
				"DATABASE_HOST":     "localhost",
				"DATABASE_PORT":     "5432",
				"DATABASE_USER":     "user",
				"DATABASE_PASS":     "pass",
				"DATABASE_NAME":     "testdb",
				"DATABASE_SSL_MODE": "disable",
			},
			expected: "host=localhost port=5432 user=user dbname=testdb sslmode=disable timezone=UTC connect_timeout=5 password=pass",
		},
		{
			name: "MySQL DSN",
			env: map[string]string{
				"DATABASE_TYPE": "mysql",
				"DATABASE_HOST": "localhost",
				"DATABASE_PORT": "3306",
				"DATABASE_USER": "root",
				"DATABASE_PASS": "pass",
				"DATABASE_NAME": "testdb",
			},
			expected: "root:pass@tcp(localhost:3306)/testdb?collation=utf8mb4_unicode_ci&parseTime=true&loc=UTC&timeout=5s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			for k, v := range tt.env {
				os.Setenv(k, v)
				defer os.Unsetenv(k)
			}

			// Load config after setting environment variables
			cfg, err := config.Load()
			if err != nil {
				t.Fatalf("Failed to load config: %v", err)
			}
			g.Config = cfg

			dsn := g.BuildDSN()
			if dsn != tt.expected {
				t.Errorf("Expected DSN %s, got %s", tt.expected, dsn)
			}
		})
	}
}

func TestTjo_SessionManager(t *testing.T) {
	g := &Tjo{
		HTTP: &HTTPService{
			Session: scs.New(),
		},
		Logging: &LoggingService{
			Info: createTestLogger(),
		},
		Config: &config.Config{
			Cookie: config.CookieConfig{
				Secure: false,
				Domain: "localhost",
			},
		},
	}

	// Test SessionLoad middleware
	handler := g.SessionLoad(nil)
	if handler == nil {
		t.Error("Expected SessionLoad to return a handler")
	}

	// Test NoSurf middleware
	handler = g.NoSurf(nil)
	if handler == nil {
		t.Error("Expected NoSurf to return a handler")
	}
}

func TestCheckDotEnv(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "tjo_env_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	g := &Tjo{}

	// Test when .env doesn't exist (should create it)
	err = g.checkDotEnv(tempDir)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	// Check if .env was created
	envPath := filepath.Join(tempDir, ".env")
	if _, err := os.Stat(envPath); os.IsNotExist(err) {
		t.Error("Expected .env file to be created")
	}

	// Test when .env exists (should not error)
	err = g.checkDotEnv(tempDir)
	if err != nil {
		t.Errorf("Expected no error for existing .env, got %v", err)
	}
}

// TestNewRegistersHealthChecks guards the initialisation order in New().
// setupStructuredLogging used to run before the database connected and before
// AppName/Version were assigned, so its nil guards were always false: /health
// reported an empty check set no matter what, and every log line carried an
// empty service name. See issue #9.
func TestNewRegistersHealthChecks(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "tjo_health_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	for _, dir := range []string{"handlers", "migrations", "views", "email", "data", "public", "tmp", "logs", "middleware"} {
		if err := os.MkdirAll(filepath.Join(tempDir, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}

	envContent := `
APP_NAME=HealthApp
DEBUG=false
PORT=4000
SESSION_TYPE=cookie
COOKIE_NAME=tjo
COOKIE_LIFETIME=1440
DATABASE_TYPE=sqlite3
DATABASE_NAME=health_test.db
CACHE=
`
	if err := os.WriteFile(filepath.Join(tempDir, ".env"), []byte(envContent), 0644); err != nil {
		t.Fatal(err)
	}

	g := &Tjo{}
	if err := g.New(tempDir); err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	if g.Data.DB.Pool == nil {
		t.Fatal("test setup is wrong: no database pool, so this proves nothing")
	}

	status := g.Logging.Health.CheckHealth()

	if _, ok := status.Checks["database"]; !ok {
		t.Errorf("no 'database' health check registered; got checks: %v", status.Checks)
	}

	if status.Version == "" {
		t.Error("health status carries no version")
	}
}

// TestNewAcceptsModules is the test whose absence let issue #6 ship: nothing in
// the repo ever passed a module to New(), so nobody noticed that no module
// could satisfy the Module interface and that the example in New's own doc
// comment did not compile.
func TestNewAcceptsModules(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "tjo_module_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	for _, dir := range []string{"handlers", "migrations", "views", "email", "data", "public", "tmp", "logs", "middleware"} {
		if err := os.MkdirAll(filepath.Join(tempDir, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}

	envContent := `
APP_NAME=ModuleApp
DEBUG=false
PORT=4000
SESSION_TYPE=cookie
COOKIE_NAME=tjo
COOKIE_LIFETIME=1440
DATABASE_TYPE=
CACHE=
`
	if err := os.WriteFile(filepath.Join(tempDir, ".env"), []byte(envContent), 0644); err != nil {
		t.Fatal(err)
	}

	mod := &testModule{name: "probe"}

	g := &Tjo{}
	if err := g.New(tempDir, mod); err != nil {
		t.Fatalf("New() with a module failed: %v", err)
	}

	if !mod.initCalled {
		t.Error("module was registered but Initialize was never called")
	}

	if g.Modules.Get("probe") == nil {
		t.Error("module is not in the registry after New()")
	}
}

// TestSchedulerActuallyRuns covers issue #11. The cron scheduler was created
// and handed a daily badger GC job, and ScheduleCron returned entry IDs for
// user jobs -- but Start() was never called, so nothing ever fired. Badger
// therefore never reclaimed value-log space and every user cron silently
// no-opped while reporting success.
func TestSchedulerActuallyRuns(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "tjo_cron_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	for _, dir := range []string{"handlers", "migrations", "views", "email", "data", "public", "tmp", "logs", "middleware"} {
		if err := os.MkdirAll(filepath.Join(tempDir, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}

	envContent := "\nAPP_NAME=CronApp\nDEBUG=false\nPORT=4000\nSESSION_TYPE=cookie\nCOOKIE_NAME=tjo\nCOOKIE_LIFETIME=1440\nDATABASE_TYPE=\nCACHE=\n"
	if err := os.WriteFile(filepath.Join(tempDir, ".env"), []byte(envContent), 0644); err != nil {
		t.Fatal(err)
	}

	g := &Tjo{}
	if err := g.New(tempDir); err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer g.Background.Scheduler.Stop()

	fired := make(chan struct{}, 1)
	if _, err := g.Background.ScheduleCron("probe", "@every 1s", func() {
		select {
		case fired <- struct{}{}:
		default:
		}
	}); err != nil {
		t.Fatalf("ScheduleCron failed: %v", err)
	}

	select {
	case <-fired:
	case <-time.After(5 * time.Second):
		t.Fatal("scheduled cron never fired; the scheduler was never started")
	}
}
