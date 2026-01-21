package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionStartHook(t *testing.T) {
	// Create a temporary directory with an agents.md file
	tmpDir := t.TempDir()
	agentsContent := "# Test Agent Instructions\n\nThis is a test."
	agentsPath := filepath.Join(tmpDir, "agents.md")

	err := os.WriteFile(agentsPath, []byte(agentsContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test agents.md: %v", err)
	}

	// Create SessionStart input
	input := SessionStartInput{
		SessionID:      "test123",
		TranscriptPath: "",
		PermissionMode: "default",
		HookEventName:  "SessionStart",
		Source:         "startup",
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("Failed to marshal input: %v", err)
	}

	// Get the project root directory (where main.go is located)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	projectRoot := filepath.Dir(filepath.Dir(wd)) // Go up two levels from cmd/claude-hook

	// Run the hook
	cmd := exec.Command("go", "run", filepath.Join(projectRoot, "cmd/claude-hook/main.go"), "-type", "session-start")
	cmd.Stdin = strings.NewReader(string(inputJSON))
	cmd.Env = append(os.Environ(), "CLAUDE_CODE_CWD="+tmpDir)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Hook failed: %v\nOutput: %s", err, string(output))
	}

	// Verify output contains the agents.md content
	if !strings.Contains(string(output), agentsContent) {
		t.Errorf("Expected output to contain agents.md content, got: %s", string(output))
	}
}

func TestSessionStartHookMissingFile(t *testing.T) {
	// Create a temporary directory WITHOUT an agents.md file
	tmpDir := t.TempDir()

	// Create SessionStart input
	input := SessionStartInput{
		SessionID:      "test123",
		TranscriptPath: "",
		PermissionMode: "default",
		HookEventName:  "SessionStart",
		Source:         "compact",
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("Failed to marshal input: %v", err)
	}

	// Get the project root directory
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	projectRoot := filepath.Dir(filepath.Dir(wd))

	// Run the hook
	cmd := exec.Command("go", "run", filepath.Join(projectRoot, "cmd/claude-hook/main.go"), "-type", "session-start")
	cmd.Stdin = strings.NewReader(string(inputJSON))
	cmd.Env = append(os.Environ(), "CLAUDE_CODE_CWD="+tmpDir)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Hook should not fail when agents.md is missing: %v\nOutput: %s", err, string(output))
	}

	// Verify output is empty (or just whitespace)
	trimmed := strings.TrimSpace(string(output))
	if trimmed != "" {
		t.Errorf("Expected empty output when agents.md is missing, got: %s", trimmed)
	}
}
