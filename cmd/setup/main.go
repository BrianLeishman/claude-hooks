package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ClaudeSettings represents the structure of Claude's settings.json
type ClaudeSettings struct {
	Model string                   `json:"model,omitempty"`
	Hooks map[string][]HookMatcher `json:"hooks,omitempty"`
}

// HookMatcher represents a hook matcher configuration
type HookMatcher struct {
	Matcher string `json:"matcher"`
	Hooks   []Hook `json:"hooks"`
}

// Hook represents a single hook configuration
type Hook struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

func main() {
	fmt.Println("Setting up Claude Hooks...")

	// Get user's home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Error getting home directory: %v\n", err)
		os.Exit(1)
	}

	// Get current working directory (claude-hooks repo)
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Error getting current directory: %v\n", err)
		os.Exit(1)
	}

	settingsPath := filepath.Join(homeDir, ".claude", "settings.json")

	// Read existing settings or create new ones
	settings, err := readOrCreateSettings(settingsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Error handling settings file: %v\n", err)
		os.Exit(1)
	}

	// Create the go run commands that will work from any directory
	postHookCommand := fmt.Sprintf("bash -c \"cd %s && go run cmd/claude-hook/main.go -type post-edit\"", cwd)
	preHookCommand := fmt.Sprintf("bash -c \"cd %s && go run cmd/claude-hook/main.go -type pre-bash\"", cwd)
	planReviewCommand := fmt.Sprintf("bash -c \"cd %s && go run cmd/claude-hook/main.go -type plan-review\"", cwd)

	// Add our hook configurations
	err = addPostToolUseHook(settings, postHookCommand)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Error configuring PostToolUse hook: %v\n", err)
		os.Exit(1)
	}

	err = addPreToolUseHook(settings, preHookCommand)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Error configuring PreToolUse hook: %v\n", err)
		os.Exit(1)
	}

	err = addPlanReviewHook(settings, planReviewCommand)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Error configuring PlanReview hook: %v\n", err)
		os.Exit(1)
	}

	// Write settings back to file
	err = writeSettings(settingsPath, settings)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Error writing settings: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("")
	fmt.Println("✅ Setup complete!")
	fmt.Println("✅ Hooks automatically configured in Claude Code!")
	fmt.Println("")
	fmt.Printf("Hooks configured in: %s\n", settingsPath)
	fmt.Println("  PostToolUse Event: Write|Edit|MultiEdit")
	fmt.Printf("    Command: %s\n", postHookCommand)
	fmt.Println("  PreToolUse Event: Bash (MySQL blocking + git commit protection)")
	fmt.Printf("    Command: %s\n", preHookCommand)
	fmt.Println("  PreToolUse Event: ExitPlanMode (AI Council plan review)")
	fmt.Printf("    Command: %s\n", planReviewCommand)
	fmt.Println("")
	fmt.Println("🔄 Live reloading enabled - changes to hook code take effect immediately!")
	fmt.Println("")
	fmt.Println("The hook will automatically:")
	fmt.Println("  - Format Go files (goimports, gofumpt)")
	fmt.Println("  - Run linters (golangci-lint or go vet)")
	fmt.Println("  - Run tests for modified files")
	fmt.Println("  - Tidy go.mod")
	fmt.Println("  - Format TypeScript/JavaScript (prettier, eslint)")
	fmt.Println("  - Type-check TypeScript files")
	fmt.Println("  - Block MySQL commands (use Go database methods instead)")
	fmt.Println("  - Block git commits on master/main branches (create feature branches instead)")
	fmt.Println("  - 🧠 Review plans with AI Council (Claude Opus, GPT-5.2, Gemini 3 Pro)")
}

func readOrCreateSettings(settingsPath string) (*ClaudeSettings, error) {
	// Ensure .claude directory exists
	claudeDir := filepath.Dir(settingsPath)
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating .claude directory: %w", err)
	}

	// Try to read existing settings
	if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
		// File doesn't exist, create new settings
		fmt.Printf("Creating %s...\n", settingsPath)
		return &ClaudeSettings{
			Hooks: make(map[string][]HookMatcher),
		}, nil
	}

	// File exists, read it
	file, err := os.Open(settingsPath)
	if err != nil {
		return nil, fmt.Errorf("opening settings file: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to close file: %v\n", closeErr)
		}
	}()

	data, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("reading settings file: %w", err)
	}

	var settings ClaudeSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("parsing settings JSON: %w", err)
	}

	// Ensure hooks map exists
	if settings.Hooks == nil {
		settings.Hooks = make(map[string][]HookMatcher)
	}

	return &settings, nil
}

func addPostToolUseHook(settings *ClaudeSettings, hookCommand string) error {
	// Check if our hook already exists in PostToolUse
	postToolUse := settings.Hooks["PostToolUse"]

	for i, matcher := range postToolUse {
		if matcher.Matcher == "Write|Edit|MultiEdit" {
			// Check if our command already exists
			for _, hook := range matcher.Hooks {
				if hook.Command == hookCommand {
					fmt.Println("PostToolUse hook already configured, skipping...")
					return nil
				}
			}

			// Add our hook to existing matcher
			postToolUse[i].Hooks = append(postToolUse[i].Hooks, Hook{
				Type:    "command",
				Command: hookCommand,
			})
			settings.Hooks["PostToolUse"] = postToolUse
			return nil
		}
	}

	// No existing matcher found, create new one
	newMatcher := HookMatcher{
		Matcher: "Write|Edit|MultiEdit",
		Hooks: []Hook{{
			Type:    "command",
			Command: hookCommand,
		}},
	}

	settings.Hooks["PostToolUse"] = append(postToolUse, newMatcher)
	return nil
}

func addPreToolUseHook(settings *ClaudeSettings, hookCommand string) error {
	// Check if our hook already exists in PreToolUse
	preToolUse := settings.Hooks["PreToolUse"]

	for i, matcher := range preToolUse {
		if matcher.Matcher == "Bash" {
			// Check if our command already exists
			for _, hook := range matcher.Hooks {
				if hook.Command == hookCommand {
					fmt.Println("PreToolUse hook already configured, skipping...")
					return nil
				}
			}

			// Add our hook to existing matcher
			preToolUse[i].Hooks = append(preToolUse[i].Hooks, Hook{
				Type:    "command",
				Command: hookCommand,
			})
			settings.Hooks["PreToolUse"] = preToolUse
			return nil
		}
	}

	// No existing matcher found, create new one
	newMatcher := HookMatcher{
		Matcher: "Bash",
		Hooks: []Hook{{
			Type:    "command",
			Command: hookCommand,
		}},
	}

	settings.Hooks["PreToolUse"] = append(preToolUse, newMatcher)
	return nil
}

func addPlanReviewHook(settings *ClaudeSettings, hookCommand string) error {
	// This hook matches ExitPlanMode to review plans with multiple AI models
	preToolUse := settings.Hooks["PreToolUse"]

	for i, matcher := range preToolUse {
		if matcher.Matcher == "ExitPlanMode" {
			// Check if our command already exists
			for _, hook := range matcher.Hooks {
				if hook.Command == hookCommand {
					fmt.Println("PlanReview hook already configured, skipping...")
					return nil
				}
			}

			// Add our hook to existing matcher
			preToolUse[i].Hooks = append(preToolUse[i].Hooks, Hook{
				Type:    "command",
				Command: hookCommand,
			})
			settings.Hooks["PreToolUse"] = preToolUse
			return nil
		}
	}

	// No existing matcher found, create new one
	newMatcher := HookMatcher{
		Matcher: "ExitPlanMode",
		Hooks: []Hook{{
			Type:    "command",
			Command: hookCommand,
		}},
	}

	settings.Hooks["PreToolUse"] = append(preToolUse, newMatcher)
	return nil
}

func writeSettings(settingsPath string, settings *ClaudeSettings) error {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling settings to JSON: %w", err)
	}

	file, err := os.Create(settingsPath)
	if err != nil {
		return fmt.Errorf("creating settings file: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to close file: %v\n", closeErr)
		}
	}()

	_, err = file.Write(data)
	if err != nil {
		return fmt.Errorf("writing settings file: %w", err)
	}

	return nil
}
