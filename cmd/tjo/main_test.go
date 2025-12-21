package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateInput(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		expectedArg1 string
		expectedArg2 string
		expectedArg3 string
		expectError bool
	}{
		{
			name:        "No arguments",
			args:        []string{"cli"},
			expectedArg1: "",
			expectedArg2: "",
			expectedArg3: "",
			expectError: true,
		},
		{
			name:        "One argument",
			args:        []string{"cli", "help"},
			expectedArg1: "help",
			expectedArg2: "",
			expectedArg3: "",
			expectError: false,
		},
		{
			name:        "Two arguments",
			args:        []string{"cli", "new", "myapp"},
			expectedArg1: "new",
			expectedArg2: "myapp",
			expectedArg3: "",
			expectError: false,
		},
		{
			name:        "Three arguments",
			args:        []string{"cli", "make", "model", "user"},
			expectedArg1: "make",
			expectedArg2: "model",
			expectedArg3: "user",
			expectError: false,
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

func TestExitGracefully(t *testing.T) {
	// Test with no error and no message
	exitGracefully(nil)

	// Test with no error but with message
	exitGracefully(nil, "Test message")

	// Test with error
	exitGracefully(os.ErrNotExist)

	// Test with error and message
	exitGracefully(os.ErrNotExist, "Custom message")
}

func TestShowHelp(t *testing.T) {
	// Just test that showHelp doesn't panic
	showHelp()
}