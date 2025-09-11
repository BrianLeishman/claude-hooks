package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestGetCurrentBranch(t *testing.T) {
	// Test in current git repo
	branch := getCurrentBranch("", false)

	// Should return a non-empty string since we're in a git repo
	if branch == "" {
		// This might be expected if we're in detached HEAD
		// Let's verify we're actually in a git repo
		cmd := exec.Command("git", "status")
		err := cmd.Run()
		if err != nil {
			t.Skip("Not in a git repository, skipping test")
		}

		// If git status works but branch is empty, we might be in detached HEAD
		t.Logf("Branch is empty - might be in detached HEAD state")
	} else {
		t.Logf("Current branch: %q", branch)
	}

	// Branch name should not contain whitespace if present
	if strings.TrimSpace(branch) != branch {
		t.Errorf("Branch name contains leading/trailing whitespace: %q", branch)
	}
}

func TestIsProtectedBranch(t *testing.T) {
	tests := []struct {
		branch   string
		expected bool
	}{
		{"master", true},
		{"main", true},
		{"feature/test", false},
		{"develop", false},
		{"", false},
		{"MASTER", false}, // Case sensitive
		{"Main", false},   // Case sensitive
	}

	for _, tt := range tests {
		t.Run(tt.branch, func(t *testing.T) {
			result := isProtectedBranch(tt.branch)
			if result != tt.expected {
				t.Errorf("isProtectedBranch(%q) = %v, want %v", tt.branch, result, tt.expected)
			}
		})
	}
}

func TestGetCurrentBranchVerbose(t *testing.T) {
	// Capture stderr to check verbose output
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	_ = getCurrentBranch("", true)

	w.Close()
	os.Stderr = oldStderr

	// Read the captured output
	buf := make([]byte, 1024)
	n, _ := r.Read(buf)
	output := string(buf[:n])

	// Should contain debug output when verbose is true
	if !strings.Contains(output, "🔍 Detecting current git branch") {
		t.Errorf("Verbose output missing expected debug message. Got: %q", output)
	}
}

func TestGetCurrentBranchWithDirectory(t *testing.T) {
	// Test with current directory specified explicitly
	cwd, err := os.Getwd()
	if err != nil {
		t.Skip("Could not get current working directory")
	}

	branch := getCurrentBranch(cwd, false)

	// Should be same as calling without directory
	branchDefault := getCurrentBranch("", false)
	if branch != branchDefault {
		t.Errorf("Branch from explicit directory %q != branch from default %q", branch, branchDefault)
	}
}

func TestFindGitRoot(t *testing.T) {
	// Test with a file in the current repo
	cwd, err := os.Getwd()
	if err != nil {
		t.Skip("Could not get current working directory")
	}

	// Use an absolute path to a file we know exists
	testFile := cwd + "/main_test.go"
	root := findGitRoot(testFile, false)

	// Should find the git root
	if root == "" {
		t.Error("Could not find git root for test file")
	}

	// Root should be a parent of current directory
	if !strings.HasPrefix(cwd, root) {
		t.Errorf("Git root %q is not a parent of current directory %q", root, cwd)
	}
}
