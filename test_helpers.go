package tjo

import (
	"log"
	"os"
)

// createTestLogger creates a logger for testing
func createTestLogger() *log.Logger {
	return log.New(os.Stdout, "TEST: ", log.Ldate|log.Ltime)
}

// createTestErrorLogger creates an error logger for testing
func createTestErrorLogger() *log.Logger {
	return log.New(os.Stderr, "ERROR: ", log.Ldate|log.Ltime|log.Lshortfile)
}