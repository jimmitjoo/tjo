package config

import (
	"os"
	"strings"
	"testing"
)

// setEnv sets the minimum needed for Load to succeed, plus the overrides given.
func setEnv(t *testing.T, overrides map[string]string) {
	t.Helper()

	base := map[string]string{
		"APP_NAME":      "TestApp",
		"KEY":           "12345678901234567890123456789012",
		"SESSION_TYPE":  "cookie",
		"DATABASE_TYPE": "",
		"CACHE":         "",
	}
	for k, v := range overrides {
		base[k] = v
	}
	for k, v := range base {
		t.Setenv(k, v)
	}
}

// TestMalformedEnvValuesAreReported covers issue #16. envInt/envBool/envFloat
// discarded the strconv error and returned the default, so PORT=80O0 silently
// bound 4000 and Validate never saw the typo -- the bad value had already been
// replaced by a valid one.
func TestMalformedEnvValuesAreReported(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
		want  string
	}{
		{name: "integer with a letter", key: "PORT", value: "80O0", want: "not a valid integer"},
		{name: "non-numeric worker count", key: "JOB_WORKERS", value: "eight", want: "not a valid integer"},
		{name: "non-boolean flag", key: "DEBUG", value: "yesplease", want: "not a valid boolean"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setEnv(t, map[string]string{tt.key: tt.value})

			_, err := Load()
			if err == nil {
				t.Fatalf("%s=%q was accepted; a typo must not silently become the default", tt.key, tt.value)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error does not name the problem: %v", err)
			}
			if !strings.Contains(err.Error(), tt.key) {
				t.Errorf("error does not name the variable: %v", err)
			}
		})
	}
}

func TestUnsetValuesStillUseDefaults(t *testing.T) {
	setEnv(t, nil)
	os.Unsetenv("PORT")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("a clean environment should load: %v", err)
	}
	if cfg.Server.Port != 4000 {
		t.Errorf("expected the default port 4000, got %d", cfg.Server.Port)
	}
}

// TestSessionTypeValidation pins the vocabulary shared with session.InitSession.
func TestSessionTypeValidation(t *testing.T) {
	for _, valid := range []string{"cookie", "redis", "database"} {
		t.Run("accepts "+valid, func(t *testing.T) {
			overrides := map[string]string{"SESSION_TYPE": valid}
			if valid == "redis" {
				overrides["REDIS_HOST"] = "localhost:6379"
			}
			setEnv(t, overrides)

			if _, err := Load(); err != nil {
				t.Errorf("SESSION_TYPE=%s should be valid: %v", valid, err)
			}
		})
	}

	t.Run("rejects badger", func(t *testing.T) {
		setEnv(t, map[string]string{"SESSION_TYPE": "badger"})
		if _, err := Load(); err == nil {
			t.Error("SESSION_TYPE=badger was accepted, but no badger session store exists")
		}
	})
}
