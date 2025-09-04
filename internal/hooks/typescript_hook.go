package hooks

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type TypeScriptHook struct{}

func (h *TypeScriptHook) PreEdit(files []string, verbose bool) error {
	return nil
}

func (h *TypeScriptHook) PostEdit(files []string, verbose bool) error {
	if len(files) == 0 {
		return nil
	}

	fmt.Println("==========================================")
	fmt.Printf("Running TypeScript hooks on %d file(s)\n", len(files))
	fmt.Println("==========================================")

	var hasErrors bool
	var allErrors []string

	// Step 1: Run ESLint
	if err := h.runESLint(files, verbose); err != nil {
		allErrors = append(allErrors, err.Error())
		hasErrors = true
	}

	// Step 2: Run TypeScript compiler check
	if err := h.runTypeCheck(files, verbose); err != nil {
		allErrors = append(allErrors, err.Error())
		hasErrors = true
	}

	if hasErrors {
		return fmt.Errorf("TypeScript/JavaScript checks failed:\n\n%s", strings.Join(allErrors, "\n\n"))
	}
	return nil
}

func (h *TypeScriptHook) runESLint(files []string, verbose bool) error {
	if !isCommandAvailable("eslint") {
		if verbose {
			fmt.Println("eslint not found, skipping")
		}
		return nil
	}

	fmt.Println("\n===== Step 1/2: Running ESLint =====")
	args := append([]string{"--fix"}, files...)
	cmd := exec.Command("eslint", args...)

	output, err := cmd.CombinedOutput()
	if err != nil {
		outputStr := strings.TrimSpace(string(output))
		fmt.Fprintf(os.Stderr, "❌ ESLint found issues:\n%s\n", output)

		// Return detailed error information for Claude
		if outputStr != "" {
			// Limit output length to avoid overwhelming Claude
			if len(outputStr) > 1500 {
				outputStr = outputStr[:1500] + "\n... (output truncated, use verbose mode to see all issues)"
			}
			return fmt.Errorf("ESLint found issues:\n%s", outputStr)
		}
		return fmt.Errorf("ESLint failed with unknown errors")
	}

	if verbose && len(output) > 0 {
		fmt.Printf("%s", output)
	}

	fmt.Println("  ✓ No linting issues")
	return nil
}

func (h *TypeScriptHook) runTypeCheck(files []string, verbose bool) error {
	if !isCommandAvailable("tsc") {
		if verbose {
			fmt.Println("tsc not found, skipping type check")
		}
		return nil
	}

	fmt.Println("\n===== Step 2/2: Running TypeScript type check =====")
	cmd := exec.Command("tsc", "--noEmit")

	output, err := cmd.CombinedOutput()
	if err != nil {
		outputStr := strings.TrimSpace(string(output))
		fmt.Fprintf(os.Stderr, "❌ Type errors found:\n%s\n", output)

		// Return detailed error information for Claude
		if outputStr != "" {
			// Limit output length to avoid overwhelming Claude
			if len(outputStr) > 1500 {
				outputStr = outputStr[:1500] + "\n... (output truncated, use verbose mode to see all issues)"
			}
			return fmt.Errorf("TypeScript type check failed:\n%s", outputStr)
		}
		return fmt.Errorf("TypeScript type check failed with unknown errors")
	}

	if verbose && len(output) > 0 {
		fmt.Printf("%s", output)
	}

	fmt.Println("  ✓ Type check passed")
	return nil
}

func (h *TypeScriptHook) PostEditJSON(files []string, verbose bool) error {
	if len(files) == 0 {
		return nil
	}

	// Redirect stdout to stderr to keep it clean for JSON
	origStdout := os.Stdout
	os.Stdout = os.Stderr
	defer func() {
		os.Stdout = origStdout
	}()

	// Run the regular PostEdit which will now output to stderr
	return h.PostEdit(files, verbose)
}
