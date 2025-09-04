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
		hasErrors = true
	}

	// Step 4: Run tests for modified files
	if err := h.runTests(files, verbose); err != nil {
		hasErrors = true
	}

	// Step 5: Run go mod tidy if go.mod exists
	if _, err := os.Stat("go.mod"); err == nil {
		if err := h.runGoModTidy(verbose); err != nil {
			fmt.Fprintf(os.Stderr, "⚠️  go mod tidy: %v\n", err)
		}
	}

	if hasErrors {
		return fmt.Errorf("checks failed")
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
		// Run linting on directories instead of individual files
		// This ensures proper package-level analysis
		dirs := getUniqueDirectories(files)
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

		// Add directory paths with ./prefix for relative paths
		for _, dir := range dirs {
			if !strings.HasPrefix(dir, "/") {
				args = append(args, "./"+dir)
			} else {
				args = append(args, dir)
			}
		}

		cmd := exec.Command("golangci-lint", args...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			// Parse output to show summary
			lines := strings.Split(string(output), "\n")
			var issueCount string
			for _, line := range lines {
				if strings.Contains(line, "issue") && strings.Contains(line, ":") {
					issueCount = line
				}
			}

			fmt.Fprintf(os.Stderr, "\n❌ Linting issues found\n")
			if issueCount != "" {
				fmt.Fprintf(os.Stderr, "  %s\n", issueCount)
			}

			if verbose {
				fmt.Fprintf(os.Stderr, "\nFull output:\n%s\n", output)
			} else {
				// Show first few issues
				shown := 0
				for _, line := range lines {
					if strings.Contains(line, ".go:") && shown < 5 {
						fmt.Fprintf(os.Stderr, "  %s\n", line)
						shown++
					}
				}
				if shown > 0 {
					fmt.Fprintf(os.Stderr, "  (use -v to see all issues)\n")
				}
			}
			return fmt.Errorf("linting failed")
		}
		fmt.Println("  ✓ No linting issues")
	} else {
		// Fall back to go vet
		if verbose {
			fmt.Println("  golangci-lint not found, using go vet")
		}

		dirs := getUniqueDirectories(files)
		for _, dir := range dirs {
			cmd := exec.Command("go", "vet", "./"+dir)
			if output, err := cmd.CombinedOutput(); err != nil {
				fmt.Fprintf(os.Stderr, "❌ go vet failed for %s:\n%s\n", dir, output)
				return fmt.Errorf("go vet failed")
			}
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

func getUniqueDirectories(files []string) []string {
	dirs := make(map[string]bool)
	for _, file := range files {
		dirs[filepath.Dir(file)] = true
	}

	var result []string
	for dir := range dirs {
		result = append(result, dir)
	}
	return result
}
