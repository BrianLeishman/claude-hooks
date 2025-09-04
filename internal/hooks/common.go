package hooks

import (
	"os"
	"os/exec"
	"path/filepath"
)

// isCommandAvailable checks if a command is available on the system PATH
func isCommandAvailable(name string) bool {
	cmd := exec.Command("which", name)
	err := cmd.Run()
	return err == nil
}

// findModuleRoot finds the Go module root directory by looking for go.mod
// starting from the given directory and walking up the parent directories
func findModuleRoot(dir string) (string, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}

	current := absDir
	for {
		goMod := filepath.Join(current, "go.mod")
		if _, err := os.Stat(goMod); err == nil {
			return current, nil
		}

		parent := filepath.Dir(current)
		if parent == current {
			// Reached the root directory
			break
		}
		current = parent
	}

	// No go.mod found, return the original directory
	return absDir, nil
}
