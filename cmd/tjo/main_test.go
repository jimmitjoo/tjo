package main

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateInput(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		expectedArg1 string
		expectedArg2 string
		expectedArg3 string
		expectError  bool
	}{
		{
			name:         "No arguments",
			args:         []string{"cli"},
			expectedArg1: "",
			expectedArg2: "",
			expectedArg3: "",
			expectError:  true,
		},
		{
			name:         "One argument",
			args:         []string{"cli", "help"},
			expectedArg1: "help",
			expectedArg2: "",
			expectedArg3: "",
			expectError:  false,
		},
		{
			name:         "Two arguments",
			args:         []string{"cli", "new", "myapp"},
			expectedArg1: "new",
			expectedArg2: "myapp",
			expectedArg3: "",
			expectError:  false,
		},
		{
			name:         "Three arguments",
			args:         []string{"cli", "make", "model", "user"},
			expectedArg1: "make",
			expectedArg2: "model",
			expectedArg3: "user",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original os.Args
			oldArgs := os.Args
			defer func() {
				os.Args = oldArgs
			}()

			// Set test args
			os.Args = tt.args

			arg1, arg2, arg3, err := validateInput()

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedArg1, arg1)
				assert.Equal(t, tt.expectedArg2, arg2)
				assert.Equal(t, tt.expectedArg3, arg3)
			}
		})
	}
}

// withExitRecorder swaps os.Exit for a recorder so exitGracefully can be
// called from a test without taking the test binary down with it.
func withExitRecorder(t *testing.T) *int {
	t.Helper()

	var got int
	original := exitCode
	exitCode = func(code int) { got = code }
	t.Cleanup(func() { exitCode = original })

	return &got
}

// TestExitGracefully pins the exit status. It used to only print and return,
// so the CLI reported "Error: ..." and then told the shell it had succeeded --
// no script, Makefile or CI job could detect a failed scaffold.
func TestExitGracefully(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		msg      []string
		wantCode int
	}{
		{name: "no error", err: nil, wantCode: 0},
		{name: "no error with message", err: nil, msg: []string{"Test message"}, wantCode: 0},
		{name: "error", err: os.ErrNotExist, wantCode: 1},
		{name: "error with message", err: os.ErrNotExist, msg: []string{"Custom message"}, wantCode: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := withExitRecorder(t)
			exitGracefully(tt.err, tt.msg...)

			if *got != tt.wantCode {
				t.Errorf("exit code %d, want %d -- a failure that exits 0 is invisible to CI", *got, tt.wantCode)
			}
		})
	}
}

func TestShowHelp(t *testing.T) {
	// Just test that showHelp doesn't panic
	showHelp()
}

// TestProjectNameFrom covers issue #31. The name was read positionally as
// os.Args[2], so `tjo new -d sqlite myapp` created a directory called "-d"
// containing `module -d` -- not a valid module path, and awkward to delete.
func TestProjectNameFrom(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"name only", []string{"myapp"}, "myapp"},
		{"name then flags", []string{"myapp", "-d", "sqlite"}, "myapp"},
		{"flags then name", []string{"-d", "sqlite", "myapp"}, "myapp"},
		{"long flags then name", []string{"--db", "sqlite", "myapp"}, "myapp"},
		{"equals form then name", []string{"--db=sqlite", "myapp"}, "myapp"},
		{"both flags before the name", []string{"-t", "blog", "-d", "sqlite", "myapp"}, "myapp"},
		{"flags on both sides", []string{"-t", "blog", "myapp", "-d", "sqlite"}, "myapp"},
		{"mixed equals and spaced", []string{"--template=blog", "-d", "sqlite", "myapp"}, "myapp"},
		{"no name at all", []string{"-d", "sqlite"}, ""},
		{"nothing", []string{}, ""},
		{"a value must not be taken as the name", []string{"-d", "sqlite"}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := projectNameFrom(tt.args)
			if got != tt.want {
				t.Errorf("projectNameFrom(%q) = %q, want %q", tt.args, got, tt.want)
			}
			if strings.HasPrefix(got, "-") {
				t.Errorf("returned a flag as the project name: %q", got)
			}
		})
	}
}
