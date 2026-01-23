package hooks

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestSelfEdit tests editing Go files within the hooks package itself
func TestSelfEdit(t *testing.T) {
	// Use common.go instead of test file to avoid recursive testing
	testFile, err := filepath.Abs("common.go")
	if err != nil {
		t.Fatalf("Failed to get absolute path: %v", err)
	}

	// Simulate Claude hook input
	input := struct {
		ToolInput struct {
			FilePaths []string `json:"file_paths"`
		} `json:"tool_input"`
	}{
		ToolInput: struct {
			FilePaths []string `json:"file_paths"`
		}{
			FilePaths: []string{testFile},
		},
	}

	// Run the hook
	_, err = runHookWithInput(input, "post-edit")
	if err != nil {
		t.Fatalf("Hook failed on self-edit: %v", err)
	}
}

// TestTypeScriptHook tests TypeScript file processing
func TestTypeScriptHook(t *testing.T) {
	// Create a temporary TypeScript file
	tmpDir := t.TempDir()
	tsFile := filepath.Join(tmpDir, "test.ts")

	content := `interface User {
	name: string;
	age: number;
}

function greetUser(user: User): string {
	return ` + "`Hello, ${user.name}!`" + `;
}

console.log(greetUser({ name: "Claude", age: 1 }));`

	err := os.WriteFile(tsFile, []byte(content), 0o644)
	if err != nil {
		t.Fatalf("Failed to create test TypeScript file: %v", err)
	}

	// Simulate Claude hook input
	input := struct {
		ToolInput struct {
			FilePaths []string `json:"file_paths"`
		} `json:"tool_input"`
	}{
		ToolInput: struct {
			FilePaths []string `json:"file_paths"`
		}{
			FilePaths: []string{tsFile},
		},
	}

	// Run the hook
	_, err = runHookWithInput(input, "post-edit")
	if err != nil {
		t.Fatalf("Hook failed on TypeScript file: %v", err)
	}
}

// TestMultiFileEdit tests editing multiple files in the same package
func TestMultiFileEdit(t *testing.T) {
	// Test with multiple files from the hooks package
	files := []string{
		"common.go",
		"hook.go",
		"go_hook.go",
	}

	// Convert to absolute paths
	var absPaths []string
	for _, file := range files {
		absPath, err := filepath.Abs(file)
		if err != nil {
			t.Fatalf("Failed to get absolute path for %s: %v", file, err)
		}
		absPaths = append(absPaths, absPath)
	}

	// Simulate Claude hook input
	input := struct {
		ToolInput struct {
			FilePaths []string `json:"file_paths"`
		} `json:"tool_input"`
	}{
		ToolInput: struct {
			FilePaths []string `json:"file_paths"`
		}{
			FilePaths: absPaths,
		},
	}

	// Run the hook
	_, err := runHookWithInput(input, "post-edit")
	if err != nil {
		t.Fatalf("Hook failed on multi-file edit: %v", err)
	}
}

// TestErrorHandling tests that hooks properly report errors
func TestErrorHandling(t *testing.T) {
	// Create a temporary Go file with syntax error
	tmpDir := t.TempDir()
	goFile := filepath.Join(tmpDir, "broken.go")

	content := `package main

// This has a syntax error
func main() {
	fmt.Println("Hello"
	// Missing closing parenthesis
}`

	err := os.WriteFile(goFile, []byte(content), 0o644)
	if err != nil {
		t.Fatalf("Failed to create broken Go file: %v", err)
	}

	// Simulate Claude hook input
	input := struct {
		ToolInput struct {
			FilePaths []string `json:"file_paths"`
		} `json:"tool_input"`
	}{
		ToolInput: struct {
			FilePaths []string `json:"file_paths"`
		}{
			FilePaths: []string{goFile},
		},
	}

	// Run the hook - this should return JSON with "block" decision
	output, err := runHookWithInput(input, "post-edit")
	if err != nil {
		t.Fatalf("Hook command failed: %v", err)
	}

	// Check that output contains block decision
	if !strings.Contains(output, `"decision":"block"`) && !strings.Contains(output, `"decision": "block"`) {
		t.Fatalf("Expected hook to block on broken Go file, got output: %s", output)
	}
}

// Helper function to run hook with input
func runHookWithInput(input any, hookType string) (string, error) {
	// Marshal input to JSON
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("failed to marshal input: %v", err)
	}

	// Get the project root directory
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get working directory: %v", err)
	}

	// Navigate to project root (two levels up from internal/hooks)
	projectRoot := filepath.Dir(filepath.Dir(wd))

	// Run the hook command
	cmd := exec.Command("go", "run", "cmd/claude-hook/main.go", "-type", hookType)
	cmd.Dir = projectRoot
	cmd.Stdin = strings.NewReader(string(inputJSON))

	output, err := cmd.CombinedOutput()
	outputStr := string(output)
	if err != nil {
		return outputStr, fmt.Errorf("hook command failed: %v\nOutput: %s", err, output)
	}

	return outputStr, nil
}

// TestHookRegistry ensures all hooks are properly registered
func TestHookRegistry(t *testing.T) {
	// Test that Go hook is registered
	goHook := GetHook("go")
	if goHook == nil {
		t.Fatal("Go hook not registered")
	}

	// Test that TypeScript hook is registered
	tsHook := GetHook("typescript")
	if tsHook == nil {
		t.Fatal("TypeScript hook not registered")
	}

	// Test that JavaScript uses TypeScript hook
	jsHook := GetHook("javascript")
	if jsHook == nil {
		t.Fatal("JavaScript hook not registered")
	}

	// Verify they implement the Hook interface
	testFiles := []string{"test.go"}

	err := goHook.PreEdit(testFiles, false)
	if err != nil {
		t.Fatalf("Go hook PreEdit failed: %v", err)
	}

	err = tsHook.PreEdit(testFiles, false)
	if err != nil {
		t.Fatalf("TypeScript hook PreEdit failed: %v", err)
	}
}

// TestCommandAvailability tests the common utility function
func TestCommandAvailability(t *testing.T) {
	// Test with a command that should exist
	if !isCommandAvailable("go") {
		t.Fatal("Expected 'go' command to be available")
	}

	// Test with a command that shouldn't exist
	if isCommandAvailable("nonexistent-command-12345") {
		t.Fatal("Expected 'nonexistent-command-12345' to not be available")
	}
}
