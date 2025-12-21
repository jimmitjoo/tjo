package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/fatih/color"
)

var (
	green   = color.New(color.FgGreen, color.Bold)
	red     = color.New(color.FgRed, color.Bold)
	yellow  = color.New(color.FgYellow, color.Bold)
	cyan    = color.New(color.FgCyan)
	white   = color.New(color.FgWhite)
	magenta = color.New(color.FgMagenta)
	blue    = color.New(color.FgBlue, color.Bold)
)

type TestResult struct {
	Package  string
	Status   string
	Time     string
	Coverage string
}

func main() {
	fmt.Println()
	blue.Println("🚀 Running Tjo Test Suite")
	fmt.Println(strings.Repeat("─", 60))
	
	startTime := time.Now()
	
	// Run tests with verbose output and coverage
	args := []string{"test", "./...", "-v", "-cover"}
	
	// Add any additional arguments passed to the script
	if len(os.Args) > 1 {
		args = append(args, os.Args[1:]...)
	}
	
	cmd := exec.Command("go", args...)
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	
	if err := cmd.Start(); err != nil {
		red.Printf("❌ Failed to start tests: %v\n", err)
		os.Exit(1)
	}
	
	// Track test results
	var results []TestResult
	currentPackage := ""
	testCount := 0
	passCount := 0
	failCount := 0
	skipCount := 0
	
	// Process stdout
	scanner := bufio.NewScanner(stdout)
	go func() {
		for scanner.Scan() {
			line := scanner.Text()
			
			// Running a test
			if strings.HasPrefix(line, "=== RUN") {
				testName := strings.TrimSpace(strings.TrimPrefix(line, "=== RUN"))
				cyan.Printf("  🧪 Running %s\n", testName)
				testCount++
			} else if strings.HasPrefix(line, "--- PASS:") {
				// Test passed
				parts := strings.Fields(line)
				if len(parts) >= 3 {
					testName := strings.TrimSuffix(parts[2], ":")
					duration := ""
					if len(parts) >= 4 {
						duration = parts[3]
					}
					green.Printf("  ✅ PASS: %s %s\n", testName, duration)
					passCount++
				}
			} else if strings.HasPrefix(line, "--- FAIL:") {
				// Test failed
				parts := strings.Fields(line)
				if len(parts) >= 3 {
					testName := strings.TrimSuffix(parts[2], ":")
					duration := ""
					if len(parts) >= 4 {
						duration = parts[3]
					}
					red.Printf("  ❌ FAIL: %s %s\n", testName, duration)
					failCount++
				}
			} else if strings.HasPrefix(line, "--- SKIP:") {
				// Test skipped
				parts := strings.Fields(line)
				if len(parts) >= 3 {
					testName := strings.TrimSuffix(parts[2], ":")
					yellow.Printf("  ⚠️  SKIP: %s\n", testName)
					skipCount++
				}
			} else if strings.HasPrefix(line, "?") && strings.Contains(line, "[no test files]") {
				// Package with no tests
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					pkg := parts[1]
					yellow.Printf("\n📦 %s\n", pkg)
					yellow.Println("  ⚠️  No test files")
					results = append(results, TestResult{
						Package: pkg,
						Status:  "NO_TESTS",
					})
				}
			} else if strings.HasPrefix(line, "ok") || strings.HasPrefix(line, "FAIL") {
				// Package result
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					status := parts[0]
					pkg := parts[1]
					duration := ""
					coverage := ""
					
					if len(parts) >= 3 {
						duration = parts[2]
					}
					
					// Look for coverage info
					if strings.Contains(line, "coverage:") {
						idx := strings.Index(line, "coverage:")
						if idx != -1 {
							coverage = strings.TrimSpace(line[idx+9:])
						}
					}
					
					currentPackage = pkg
					
					fmt.Println()
					if status == "ok" {
						green.Printf("📦 %s\n", pkg)
						green.Printf("  ✅ PASS")
						if duration != "" {
							white.Printf(" (%s)", duration)
						}
						if coverage != "" {
							magenta.Printf(" - Coverage: %s", coverage)
						}
						fmt.Println()
					} else {
						red.Printf("📦 %s\n", pkg)
						red.Printf("  ❌ FAIL")
						if duration != "" {
							white.Printf(" (%s)", duration)
						}
						fmt.Println()
					}
					
					results = append(results, TestResult{
						Package:  pkg,
						Status:   status,
						Time:     duration,
						Coverage: coverage,
					})
				}
			} else if strings.Contains(line, "Error:") || strings.Contains(line, "error:") {
				// Error output
				red.Printf("    ⚠️  %s\n", line)
			} else if strings.HasPrefix(line, "    ") && currentPackage != "" {
				// Test output (indented)
				white.Printf("    %s\n", strings.TrimSpace(line))
			}
		}
	}()
	
	// Process stderr
	errScanner := bufio.NewScanner(stderr)
	go func() {
		for errScanner.Scan() {
			line := errScanner.Text()
			if strings.Contains(line, "warning") {
				yellow.Printf("  ⚠️  %s\n", line)
			} else {
				red.Printf("  ❌ %s\n", line)
			}
		}
	}()
	
	// Wait for command to finish
	if err := cmd.Wait(); err != nil {
		// Tests failed
	}
	
	// Print summary
	fmt.Println()
	fmt.Println(strings.Repeat("═", 60))
	blue.Println("📊 Test Summary")
	fmt.Println(strings.Repeat("─", 60))
	
	totalPackages := len(results)
	passedPackages := 0
	failedPackages := 0
	noTestPackages := 0
	totalCoverage := 0.0
	coverageCount := 0
	
	for _, result := range results {
		switch result.Status {
		case "ok":
			passedPackages++
			if result.Coverage != "" {
				// Parse coverage percentage
				if strings.Contains(result.Coverage, "%") {
					var cov float64
					fmt.Sscanf(result.Coverage, "%f%%", &cov)
					totalCoverage += cov
					coverageCount++
				}
			}
		case "FAIL":
			failedPackages++
		case "NO_TESTS":
			noTestPackages++
		}
	}
	
	// Package summary
	fmt.Printf("📦 Packages: ")
	green.Printf("%d passed", passedPackages)
	if failedPackages > 0 {
		fmt.Printf(", ")
		red.Printf("%d failed", failedPackages)
	}
	if noTestPackages > 0 {
		fmt.Printf(", ")
		yellow.Printf("%d no tests", noTestPackages)
	}
	fmt.Printf(" (total: %d)\n", totalPackages)
	
	// Test summary
	fmt.Printf("🧪 Tests: ")
	if passCount > 0 {
		green.Printf("%d passed", passCount)
	}
	if failCount > 0 {
		if passCount > 0 {
			fmt.Printf(", ")
		}
		red.Printf("%d failed", failCount)
	}
	if skipCount > 0 {
		if passCount > 0 || failCount > 0 {
			fmt.Printf(", ")
		}
		yellow.Printf("%d skipped", skipCount)
	}
	fmt.Printf(" (total: %d)\n", testCount)
	
	// Coverage summary
	if coverageCount > 0 {
		avgCoverage := totalCoverage / float64(coverageCount)
		fmt.Printf("📈 Average Coverage: ")
		if avgCoverage >= 80 {
			green.Printf("%.1f%%\n", avgCoverage)
		} else if avgCoverage >= 60 {
			yellow.Printf("%.1f%%\n", avgCoverage)
		} else {
			red.Printf("%.1f%%\n", avgCoverage)
		}
	}
	
	// Time
	elapsed := time.Since(startTime)
	fmt.Printf("⏱️  Time: %s\n", elapsed.Round(time.Millisecond))
	
	fmt.Println(strings.Repeat("═", 60))
	
	// Final status
	if failCount > 0 || failedPackages > 0 {
		red.Println("❌ Tests Failed!")
		os.Exit(1)
	} else if testCount == 0 {
		yellow.Println("⚠️  No tests were run")
		os.Exit(0)
	} else {
		green.Println("✅ All Tests Passed!")
		os.Exit(0)
	}
}