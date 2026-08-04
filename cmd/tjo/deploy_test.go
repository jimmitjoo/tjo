package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDeployConfig(t *testing.T) {
	t.Run("reads settings and applies defaults", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, `HOST=deploy@app.example
APP_NAME=myapp
DOMAIN=app.example
# a comment

PORT=9000
`)

		cfg, err := loadDeployConfig(dir)
		if err != nil {
			t.Fatal(err)
		}

		if cfg.Host != "deploy@app.example" || cfg.AppName != "myapp" || cfg.Domain != "app.example" {
			t.Errorf("got %+v", cfg)
		}
		if cfg.Port != 9000 {
			t.Errorf("Port = %d, want 9000", cfg.Port)
		}
		if cfg.Path != "/opt" || cfg.GOARCH != "amd64" {
			t.Errorf("defaults not applied: %+v", cfg)
		}
	})

	t.Run("missing file names the fix", func(t *testing.T) {
		_, err := loadDeployConfig(t.TempDir())
		if err == nil {
			t.Fatal("expected an error")
		}
		// An error that only says something is wrong makes the reader guess.
		if !strings.Contains(err.Error(), "tjo deploy init") {
			t.Errorf("error does not say what to do: %v", err)
		}
	})

	t.Run("required settings are required", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, "APP_NAME=myapp\n")

		if _, err := loadDeployConfig(dir); err == nil || !strings.Contains(err.Error(), "HOST") {
			t.Errorf("err = %v, want one naming HOST", err)
		}
	})

	// An empty or absent PORT must not become 0, which would bind the socket
	// unit to a random port and make the health check fail for a reason that
	// has nothing to do with the app.
	t.Run("empty port falls back rather than becoming zero", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, "HOST=h\nAPP_NAME=a\nPORT=\n")

		cfg, err := loadDeployConfig(dir)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Port != 8080 {
			t.Errorf("Port = %d, want 8080", cfg.Port)
		}
	})
}

func TestDeployInitWritesAUsableConfig(t *testing.T) {
	dir := t.TempDir()

	if err := doDeployInit(dir); err != nil {
		t.Fatal(err)
	}

	// What init writes must be what load accepts; those drifting apart is the
	// obvious way for a generated config file to be wrong.
	cfg, err := loadDeployConfig(dir)
	if err != nil {
		t.Fatalf("the generated deploy.conf does not load: %v", err)
	}
	if cfg.AppName != filepath.Base(dir) {
		t.Errorf("APP_NAME = %q, want the directory name %q", cfg.AppName, filepath.Base(dir))
	}
	if cfg.Port != 8080 || cfg.GOARCH != "amd64" {
		t.Errorf("got %+v", cfg)
	}

	if err := doDeployInit(dir); err == nil {
		t.Error("init overwrote an existing deploy.conf")
	}
}

// The systemd units are what make restarts drop no connections, so the
// properties that matter are asserted rather than eyeballed.
func TestSystemdUnitsUseSocketActivation(t *testing.T) {
	unit := strings.ReplaceAll(systemdUnit, "%[1]s", "myapp")
	unit = strings.ReplaceAll(unit, "%[2]s", "/opt")

	for _, want := range []string{
		// Without Requires the socket unit is not started with the service and
		// the whole mechanism silently does not apply.
		"Requires=myapp.socket",
		"Type=notify",
		"ExecStart=/opt/myapp/current/myapp",
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("service unit is missing %q", want)
		}
	}

	socket := strings.ReplaceAll(systemdSocket, "%[1]s", "myapp")
	socket = strings.ReplaceAll(socket, "%[3]d", "8080")
	if !strings.Contains(socket, "ListenStream=8080") {
		t.Errorf("socket unit does not listen on the configured port:\n%s", socket)
	}
}

func write(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "deploy.conf"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
