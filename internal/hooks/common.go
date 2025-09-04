package hooks

import (
	"os/exec"
)

// isCommandAvailable checks if a command is available on the system PATH
func isCommandAvailable(name string) bool {
	cmd := exec.Command("which", name)
	err := cmd.Run()
	return err == nil
}
