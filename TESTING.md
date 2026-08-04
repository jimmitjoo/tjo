# 🧪 Tjo Testing Guide

## Running Tests

Tjo includes a beautiful, colorful test runner that makes it easy to see test results at a glance.

### Quick Start

```bash
# Run all tests with colors
make test

# Run tests for a specific package
./run-tests -p ./cache/...

# Run tests with coverage
./run-tests -c
```

## Test Commands

### Using Make

```bash
make test         # Run all tests with colors and detailed output
make test-simple  # Run standard go test without colors
make cover        # Generate coverage report and open in browser
make coverage     # Display test coverage in terminal
```

### Using the Test Runner Script

The `run-tests` script provides more control over test execution:

```bash
./run-tests [options] [package]
```

#### Options

- `-h, --help` - Show help message
- `-v, --verbose` - Run with verbose output
- `-c, --coverage` - Show coverage information
- `-b, --bench` - Run benchmarks
- `-s, --short` - Skip Docker-dependent tests
- `-p, --package` - Test specific package

#### Examples

```bash
./run-tests                    # Run all tests with colors
./run-tests -v                 # Verbose output
./run-tests -c                 # With coverage
./run-tests -p ./cache/...     # Test specific package
./run-tests -s                 # Skip Docker tests
./run-tests -c -v -p ./email   # Combine options
```

### Using Go Test Directly

For standard Go testing without colors:

```bash
go test ./...                  # Run all tests
go test -v ./...              # Verbose mode
go test -cover ./...          # With coverage
go test -short ./...          # Skip long tests
go test -race ./...           # Race condition detection
```

## Color Legend

The test runner uses colors to make results easy to understand:

- 🟢 **Green** - Test passed ✅
- 🔴 **Red** - Test failed ❌
- 🟡 **Yellow** - Test skipped or warning ⚠️
- 🔵 **Blue** - Headers and summaries
- 🟣 **Magenta** - Coverage information
- 🔷 **Cyan** - Currently running test

### Coverage Colors

Coverage percentages are color-coded:

- 🟢 **Green** - Coverage ≥ 80%
- 🟡 **Yellow** - Coverage 60-79%
- 🔴 **Red** - Coverage < 60%

## Writing Tests

### Test File Structure

Tests should be placed in files ending with `_test.go`:

```go
package mypackage

import "testing"

func TestMyFunction(t *testing.T) {
    // Test implementation
}
```

### Table-Driven Tests

Use table-driven tests for comprehensive coverage:

```go
func TestValidation(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        expected bool
    }{
        {"valid email", "test@example.com", true},
        {"invalid email", "not-an-email", false},
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := IsValidEmail(tt.input)
            if result != tt.expected {
                t.Errorf("got %v, want %v", result, tt.expected)
            }
        })
    }
}
```

### Security Tests

The framework includes comprehensive security tests in `security_test.go`:

- Path traversal protection
- Cryptographic randomness
- Input validation
- XSS prevention
- CSRF protection

## Test Data

Test data files should be placed in `testdata` directories:

```
package/
├── mycode.go
├── mycode_test.go
└── testdata/
    └── test_files...
```

## Docker Tests

Some tests require Docker (e.g., email tests with MailHog). These tests will automatically skip if Docker is not available.

To run tests without Docker:

```bash
./run-tests -s  # Short mode skips Docker tests
```

## Continuous Integration

### Modules

This repository is a Go workspace (see `go.work`). `go test ./...` from the root
covers the **root module only** — it does not reach `email`, `otel`, `sms` or
`websocket`, which are separate modules. To run everything:

```bash
for m in . email otel sms websocket; do (cd $m && go test -short -race ./...); done
```

CI does exactly this, one job per module.

For CI/CD pipelines, use the standard test command without colors:

```bash
go test -v -cover ./...
```

Or set up the environment to skip Docker tests:

```bash
go test -short ./...
```

## Troubleshooting

### Tests Not Running

If `make test` says "test is up to date", ensure the Makefile includes `.PHONY`:

```makefile
.PHONY: test
```

### Missing Test Data

If tests fail due to missing files, check that `testdata` directories exist:

```bash
mkdir -p package/testdata
```

### Docker Tests Failing

If Docker tests fail, ensure Docker is running:

```bash
docker version
```

Or skip Docker tests:

```bash
./run-tests -s
```

## Best Practices

1. **Run tests before committing** - Always ensure tests pass
2. **Write tests for new features** - Maintain test coverage
3. **Use table-driven tests** - Better coverage and readability
4. **Test edge cases** - Include boundary conditions
5. **Keep tests fast** - Use `-short` flag for slow tests
6. **Clean test data** - Don't leave temporary files

## Coverage Goals

Aim for these coverage targets:

- **Critical paths**: > 90%
- **Core functionality**: > 80%
- **Utilities**: > 70%
- **Overall**: > 60%

Run coverage report:

```bash
make cover  # Opens HTML report in browser
```

## Test Output Example

```
🚀 Running Tjo Test Suite
────────────────────────────────────────────────────
  🧪 Running TestRandomString
  ✅ PASS: TestRandomString (0.00s)
  🧪 Running TestEncryption
  ✅ PASS: TestEncryption (0.01s)

📦 github.com/jimmitjoo/tjo
  ✅ PASS (0.883s) - Coverage: 85.4%

════════════════════════════════════════════════════
📊 Test Summary
────────────────────────────────────────────────────
📦 Packages: 7 passed, 1 no tests (total: 8)
🧪 Tests: 39 passed (total: 39)
📈 Average Coverage: 75.2%
⏱️  Time: 5.623s
════════════════════════════════════════════════════
✅ All Tests Passed!
```