package hooks

import (
	"fmt"
	"os"
	"os/exec"
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

	// Step 1: Run Prettier if available
	if err := h.runPrettier(files, verbose); err != nil {
		fmt.Fprintf(os.Stderr, "⚠️  prettier: %v\n", err)
	}

	// Step 2: Run ESLint
	if err := h.runESLint(files, verbose); err != nil {
		hasErrors = true
	}

	// Step 3: Run TypeScript compiler check
	if err := h.runTypeCheck(files, verbose); err != nil {
		hasErrors = true
	}

	if hasErrors {
		return fmt.Errorf("checks failed")
	}
	return nil
}

func (h *TypeScriptHook) runPrettier(files []string, verbose bool) error {
	if !isCommandAvailable("prettier") {
		if verbose {
			fmt.Println("prettier not found, skipping")
		}
		return nil
	}

	fmt.Println("\n===== Step 1/3: Running Prettier =====")
	args := append([]string{"--write"}, files...)
	cmd := exec.Command("prettier", args...)

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("prettier failed: %v\n%s", err, output)
	}

	fmt.Println("  ✓ Formatting complete")
	return nil
}

func (h *TypeScriptHook) runESLint(files []string, verbose bool) error {
	if !isCommandAvailable("eslint") {
		if verbose {
			fmt.Println("eslint not found, skipping")
		}
		return nil
	}

	fmt.Println("\n===== Step 2/3: Running ESLint =====")
	args := append([]string{"--fix"}, files...)
	cmd := exec.Command("eslint", args...)

	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ ESLint found issues:\n%s\n", output)
		return fmt.Errorf("eslint failed")
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

	fmt.Println("\n===== Step 3/3: Running TypeScript type check =====")
	cmd := exec.Command("tsc", "--noEmit")

	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Type errors found:\n%s\n", output)
		return fmt.Errorf("type check failed")
	}

	if verbose && len(output) > 0 {
		fmt.Printf("%s", output)
	}

	fmt.Println("  ✓ Type check passed")
	return nil
}
