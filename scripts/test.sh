#!/bin/bash

# Colorful test runner for Tjo
# Usage: ./scripts/test.sh [options]
# Options are passed directly to go test

cd "$(dirname "$0")/.."
go run scripts/test-runner.go "$@"