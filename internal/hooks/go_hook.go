package hooks

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type GoHook struct{}

func (h *GoHook) PreEdit(files []string, verbose bool) error {
	// Pre-edit: could run go vet or other checks
	return nil
}

func (h *GoHook) PostEdit(files []string, verbose bool) error {
	if len(files) == 0 {
		return nil
	}

	fmt.Println("==========================================")
	fmt.Printf("Running Go hooks on %d file(s)\n", len(files))
	fmt.Println("==========================================")

	var hasErrors bool
	var allErrors []string

	// Step 1: Run goimports
	if err := h.runGoimports(files, verbose); err != nil {
		fmt.Fprintf(os.Stderr, "⚠️  goimports: %v\n", err)
		// Don't fail on goimports errors, just warn
	}

	// Step 2: Run gofumpt if available
	if isCommandAvailable("gofumpt") {
		if err := h.runGofumpt(files, verbose); err != nil {
			fmt.Fprintf(os.Stderr, "⚠️  gofumpt: %v\n", err)
		}
	}

	// Step 3: Run linters
	if err := h.runLinters(files, verbose); err != nil {
		allErrors = append(allErrors, err.Error())
		hasErrors = true
	}

	// Step 4: Run tests for modified files
	if err := h.runTests(files, verbose); err != nil {
		allErrors = append(allErrors, err.Error())
		hasErrors = true
	}

	// Step 5: Run go mod tidy if go.mod exists
	if _, err := os.Stat("go.mod"); err == nil {
		if err := h.runGoModTidy(verbose); err != nil {
			fmt.Fprintf(os.Stderr, "⚠️  go mod tidy: %v\n", err)
		}
	}

	if hasErrors {
		return fmt.Errorf("Go checks failed:\n\n%s", strings.Join(allErrors, "\n\n"))
	}
	return nil
}

func (h *GoHook) runGoimports(files []string, verbose bool) error {
	if !isCommandAvailable("goimports") {
		if verbose {
			fmt.Println("goimports not found, skipping")
		}
		return nil
	}

	fmt.Println("\n===== Step 1/5: Running goimports =====")
	for _, file := range files {
		if verbose {
			fmt.Printf("  Formatting %s\n", file)
		}
		cmd := exec.Command("goimports", "-w", file)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed on %s: %v\n%s", file, err, output)
		}
	}
	fmt.Println("  ✓ Import formatting complete")
	return nil
}

func (h *GoHook) runGofumpt(files []string, verbose bool) error {
	fmt.Println("\n===== Step 2/5: Running gofumpt =====")
	for _, file := range files {
		if verbose {
			fmt.Printf("  Formatting %s\n", file)
		}
		cmd := exec.Command("gofumpt", "-w", file)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed on %s: %v\n%s", file, err, output)
		}
	}
	fmt.Println("  ✓ Modern formatting complete")
	return nil
}

func (h *GoHook) runLinters(files []string, verbose bool) error {
	fmt.Println("\n===== Step 3/5: Running linters =====")

	if isCommandAvailable("golangci-lint") {
		// Group files by their module root for proper linting context
		moduleRoots := make(map[string][]string) // module root -> list of directories to lint
		for _, file := range files {
			dir := filepath.Dir(file)

			// Find the module root for this file
			moduleRoot, err := findModuleRoot(dir)
			if err != nil {
				if verbose {
					fmt.Printf("  Warning: Could not find module root for %s: %v\n", file, err)
				}
				continue
			}

			// Get absolute path for the directory to ensure consistency
			absDir, err := filepath.Abs(dir)
			if err != nil {
				if verbose {
					fmt.Printf("  Warning: Could not get absolute path for %s: %v\n", dir, err)
				}
				continue
			}

			// Get relative path from module root to file directory
			relDir, err := filepath.Rel(moduleRoot, absDir)
			if err != nil {
				if verbose {
					fmt.Printf("  Warning: Could not get relative path from %s to %s: %v\n", moduleRoot, absDir, err)
				}
				continue
			}

			if relDir == "." {
				relDir = ""
			}

			if moduleRoots[moduleRoot] == nil {
				moduleRoots[moduleRoot] = []string{}
			}
			// Avoid duplicates
			found := false
			for _, existing := range moduleRoots[moduleRoot] {
				if existing == relDir {
					found = true
					break
				}
			}
			if !found {
				moduleRoots[moduleRoot] = append(moduleRoots[moduleRoot], relDir)
			}
		}

		hasErrors := false
		var allErrorLines []string

		for moduleRoot, dirs := range moduleRoots {
			// Save current directory
			originalDir, err := os.Getwd()
			if err != nil {
				fmt.Fprintf(os.Stderr, "❌ Could not get current directory: %v\n", err)
				hasErrors = true
				continue
			}

			// Change to module root
			if err := os.Chdir(moduleRoot); err != nil {
				fmt.Fprintf(os.Stderr, "❌ Could not change to module root %s: %v\n", moduleRoot, err)
				hasErrors = true
				continue
			}

			if verbose {
				fmt.Printf("  Linting in module: %s\n", moduleRoot)
			}

			// Build arguments for golangci-lint
			args := []string{
				"run",
				"--timeout=5m",
				"--enable=gocritic",
				"--enable=staticcheck",
				"--enable=govet",
				"--enable=ineffassign",
				"--enable=unused",
				"--enable=errcheck",
			}

			// Add directory paths
			for _, dir := range dirs {
				if dir == "" {
					args = append(args, ".")
				} else {
					args = append(args, "./"+dir)
				}
			}

			if verbose {
				fmt.Printf("    Running: golangci-lint %s\n", strings.Join(args, " "))
			}

			cmd := exec.Command("golangci-lint", args...)
			output, err := cmd.CombinedOutput()

			// Restore original directory
			if restoreErr := os.Chdir(originalDir); restoreErr != nil {
				fmt.Fprintf(os.Stderr, "❌ Could not restore directory to %s: %v\n", originalDir, restoreErr)
			}
			if err != nil {
				hasErrors = true
				// Parse output to collect error lines for Claude
				lines := strings.Split(string(output), "\n")
				var issueCount string
				var errorLines []string

				for _, line := range lines {
					line = strings.TrimSpace(line)
					if line == "" {
						continue
					}

					if strings.Contains(line, "issue") && strings.Contains(line, ":") {
						issueCount = line
					}

					// Collect actual error lines - updated pattern to match golangci-lint output
					if strings.Contains(line, ".go:") && strings.Contains(line, ":") {
						errorLines = append(errorLines, line)
					} else if strings.Contains(line, "undefined:") || strings.Contains(line, "error:") ||
						strings.Contains(line, "warning:") || strings.Contains(line, "note:") {
						errorLines = append(errorLines, line)
					}
				}

				// Add error lines to the collection for Claude
				if len(errorLines) > 0 {
					allErrorLines = append(allErrorLines, fmt.Sprintf("Linting issues in %s:", moduleRoot))
					// Limit to first 10 issues to avoid overwhelming Claude
					limit := 10
					if len(errorLines) > limit {
						allErrorLines = append(allErrorLines, errorLines[:limit]...)
						allErrorLines = append(allErrorLines, fmt.Sprintf("... and %d more issues", len(errorLines)-limit))
					} else {
						allErrorLines = append(allErrorLines, errorLines...)
					}
				}

				fmt.Fprintf(os.Stderr, "\n❌ Linting issues found in %s\n", moduleRoot)
				if issueCount != "" {
					fmt.Fprintf(os.Stderr, "  %s\n", issueCount)
				}

				if verbose {
					fmt.Fprintf(os.Stderr, "\nFull output:\n%s\n", output)
				} else {
					// Show first few issues
					shown := 0
					for _, line := range errorLines {
						if shown < 5 {
							fmt.Fprintf(os.Stderr, "  %s\n", line)
							shown++
						}
					}
					if len(errorLines) == 0 {
						// Fallback: show first non-empty lines if no standard format found
						for _, line := range lines {
							line = strings.TrimSpace(line)
							if line != "" && shown < 3 {
								fmt.Fprintf(os.Stderr, "  %s\n", line)
								shown++
							}
						}
					}
					if shown > 0 {
						fmt.Fprintf(os.Stderr, "  (use -v to see all issues)\n")
					}
				}
			}
		}

		if hasErrors {
			if len(allErrorLines) > 0 {
				return fmt.Errorf("linting failed:\n\n%s", strings.Join(allErrorLines, "\n"))
			}
			return fmt.Errorf("linting failed")
		}
		fmt.Println("  ✓ No linting issues")
	} else {
		// Fall back to go vet
		if verbose {
			fmt.Println("  golangci-lint not found, using go vet")
		}

		// Group files by module for go vet as well
		moduleRoots := make(map[string][]string)
		for _, file := range files {
			dir := filepath.Dir(file)
			moduleRoot, err := findModuleRoot(dir)
			if err != nil {
				if verbose {
					fmt.Printf("  Warning: Could not find module root for %s: %v\n", file, err)
				}
				continue
			}

			absDir, err := filepath.Abs(dir)
			if err != nil {
				continue
			}

			relDir, err := filepath.Rel(moduleRoot, absDir)
			if err != nil {
				continue
			}

			if relDir == "." {
				relDir = ""
			}

			if moduleRoots[moduleRoot] == nil {
				moduleRoots[moduleRoot] = []string{}
			}
			found := false
			for _, existing := range moduleRoots[moduleRoot] {
				if existing == relDir {
					found = true
					break
				}
			}
			if !found {
				moduleRoots[moduleRoot] = append(moduleRoots[moduleRoot], relDir)
			}
		}

		hasErrors := false
		for moduleRoot, dirs := range moduleRoots {
			originalDir, err := os.Getwd()
			if err != nil {
				fmt.Fprintf(os.Stderr, "❌ Could not get current directory: %v\n", err)
				hasErrors = true
				continue
			}

			if err := os.Chdir(moduleRoot); err != nil {
				fmt.Fprintf(os.Stderr, "❌ Could not change to module root %s: %v\n", moduleRoot, err)
				hasErrors = true
				continue
			}

			for _, dir := range dirs {
				var pkg string
				if dir == "" {
					pkg = "."
				} else {
					pkg = "./" + dir
				}

				cmd := exec.Command("go", "vet", pkg)
				if output, err := cmd.CombinedOutput(); err != nil {
					fmt.Fprintf(os.Stderr, "❌ go vet failed for %s in %s:\n%s\n", pkg, moduleRoot, output)
					hasErrors = true
				}
			}

			if restoreErr := os.Chdir(originalDir); restoreErr != nil {
				fmt.Fprintf(os.Stderr, "❌ Could not restore directory to %s: %v\n", originalDir, restoreErr)
			}
		}

		if hasErrors {
			return fmt.Errorf("go vet failed")
		}
		fmt.Println("  ✓ go vet passed")
	}
	return nil
}

func (h *GoHook) runTests(files []string, verbose bool) error {
	fmt.Println("\n===== Step 4/5: Running tests =====")

	// Find test files for the modified files and group by module root
	moduleRoots := make(map[string][]string) // module root -> list of test directories
	for _, file := range files {
		dir := filepath.Dir(file)
		base := filepath.Base(file)

		var shouldTest bool
		if strings.HasSuffix(base, "_test.go") {
			shouldTest = true
		} else if strings.HasSuffix(base, ".go") {
			// Check if corresponding test file exists
			testFile := strings.TrimSuffix(file, ".go") + "_test.go"
			if _, err := os.Stat(testFile); err == nil {
				shouldTest = true
			}
		}

		if shouldTest {
			// Find the module root for this directory
			moduleRoot, err := findModuleRoot(dir)
			if err != nil {
				if verbose {
					fmt.Printf("  Warning: Could not find module root for %s: %v\n", dir, err)
				}
				continue
			}

			// Get absolute path for the directory to ensure consistency
			absDir, err := filepath.Abs(dir)
			if err != nil {
				if verbose {
					fmt.Printf("  Warning: Could not get absolute path for %s: %v\n", dir, err)
				}
				continue
			}

			// Get relative path from module root to test directory
			relDir, err := filepath.Rel(moduleRoot, absDir)
			if err != nil {
				if verbose {
					fmt.Printf("  Warning: Could not get relative path from %s to %s: %v\n", moduleRoot, absDir, err)
				}
				continue
			}

			if relDir == "." {
				relDir = ""
			}

			if moduleRoots[moduleRoot] == nil {
				moduleRoots[moduleRoot] = []string{}
			}
			// Avoid duplicates
			found := false
			for _, existing := range moduleRoots[moduleRoot] {
				if existing == relDir {
					found = true
					break
				}
			}
			if !found {
				moduleRoots[moduleRoot] = append(moduleRoots[moduleRoot], relDir)
			}
		}
	}

	if len(moduleRoots) == 0 {
		fmt.Println("  No test files found")
		return nil
	}

	hasFailures := false
	var allTestErrors []string

	for moduleRoot, testDirs := range moduleRoots {
		// Save current directory
		originalDir, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ Could not get current directory: %v\n", err)
			hasFailures = true
			continue
		}

		// Change to module root
		if err := os.Chdir(moduleRoot); err != nil {
			fmt.Fprintf(os.Stderr, "❌ Could not change to module root %s: %v\n", moduleRoot, err)
			hasFailures = true
			continue
		}

		if verbose {
			fmt.Printf("  Testing in module: %s\n", moduleRoot)
		}

		// Test each directory in this module
		for _, testDir := range testDirs {
			var pkg string
			if testDir == "" {
				pkg = "."
			} else {
				pkg = "./" + testDir
			}

			if verbose {
				fmt.Printf("    Testing package: %s\n", pkg)
			}

			cmd := exec.Command("go", "test", "-timeout=30s", pkg)
			output, err := cmd.CombinedOutput()

			if err != nil {
				// Collect test failure details for Claude
				testError := fmt.Sprintf("Tests failed in %s (module: %s):", pkg, moduleRoot)
				outputStr := strings.TrimSpace(string(output))
				if outputStr != "" {
					// Limit output length to avoid overwhelming Claude
					if len(outputStr) > 1000 {
						outputStr = outputStr[:1000] + "\n... (output truncated)"
					}
					testError = fmt.Sprintf("%s\n%s", testError, outputStr)
				}
				allTestErrors = append(allTestErrors, testError)

				fmt.Fprintf(os.Stderr, "\n❌ Tests failed in %s (module: %s):\n", pkg, moduleRoot)
				fmt.Fprintf(os.Stderr, "%s\n", output)
				hasFailures = true
			} else if verbose && len(output) > 0 {
				fmt.Printf("%s", output)
			}
		}

		// Restore original directory
		if err := os.Chdir(originalDir); err != nil {
			fmt.Fprintf(os.Stderr, "❌ Could not restore directory to %s: %v\n", originalDir, err)
		}
	}

	if hasFailures {
		if len(allTestErrors) > 0 {
			return fmt.Errorf("tests failed:\n\n%s", strings.Join(allTestErrors, "\n\n"))
		}
		return fmt.Errorf("tests failed")
	}

	fmt.Println("  ✓ All tests passed")
	return nil
}

func (h *GoHook) runGoModTidy(verbose bool) error {
	fmt.Println("\n===== Step 5/5: Running go mod tidy =====")

	cmd := exec.Command("go", "mod", "tidy")
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ go mod tidy failed:\n%s\n", output)
		return fmt.Errorf("go mod tidy failed")
	}

	if verbose && len(output) > 0 {
		fmt.Printf("%s", output)
	}

	fmt.Println("  ✓ Dependencies tidied")
	return nil
}
