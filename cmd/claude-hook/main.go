package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/brianleishman/claude-hooks/internal/hooks"
)

// ToolInput represents the input from Claude Code
type ToolInput struct {
	FilePath  string   `json:"file_path"`
	FilePaths []string `json:"file_paths"`
	Command   string   `json:"command"` // For Bash commands in PreToolUse
	Content   string   `json:"content"` // For Write tool content
}

// Input represents the complete input structure
type Input struct {
	ToolName       string    `json:"tool_name"` // Tool being called (e.g., "Bash")
	ToolInput      ToolInput `json:"tool_input"`
	TranscriptPath string    `json:"transcript_path"` // Path to conversation transcript
	Cwd            string    `json:"cwd"`             // Current working directory
}

// HookOutput represents the JSON response for PostToolUse hooks
type HookOutput struct {
	Decision string `json:"decision,omitempty"` // "block" to notify Claude of issues
	Reason   string `json:"reason,omitempty"`   // Detailed explanation for Claude
}

// PreToolUseOutput represents the JSON response for PreToolUse hooks
type PreToolUseOutput struct {
	HookSpecificOutput PreToolUseHookOutput `json:"hookSpecificOutput"`
}

type PreToolUseHookOutput struct {
	HookEventName            string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision"` // "allow", "deny", or "ask"
	PermissionDecisionReason string `json:"permissionDecisionReason"`
}

// SessionStartInput represents the input for SessionStart hooks
type SessionStartInput struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	PermissionMode string `json:"permission_mode"`
	HookEventName  string `json:"hook_event_name"`
	Source         string `json:"source"` // "startup", "resume", "clear", or "compact"
}

// getCurrentBranch returns the current git branch name, or empty string if not in a git repo
// workingDir specifies which directory to check (empty string uses current directory)
func getCurrentBranch(workingDir string, verbose bool) string {
	if verbose {
		if workingDir != "" {
			fmt.Fprintf(os.Stderr, "🔍 Detecting git branch in directory: %s\n", workingDir)
		} else {
			fmt.Fprintf(os.Stderr, "🔍 Detecting current git branch...\n")
		}
	}

	var cmd *exec.Cmd
	if workingDir != "" {
		cmd = exec.Command("git", "-C", workingDir, "branch", "--show-current")
	} else {
		cmd = exec.Command("git", "branch", "--show-current")
	}

	output, err := cmd.Output()
	if err != nil {
		if verbose {
			fmt.Fprintf(os.Stderr, "🔍 First method failed (%v), trying fallback...\n", err)
		}
		// Try alternative method for older git versions or detached HEAD
		if workingDir != "" {
			cmd = exec.Command("git", "-C", workingDir, "rev-parse", "--abbrev-ref", "HEAD")
		} else {
			cmd = exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
		}
		output, err = cmd.Output()
		if err != nil {
			if verbose {
				fmt.Fprintf(os.Stderr, "🔍 Fallback method also failed (%v), not in git repo\n", err)
			}
			return "" // Not in a git repo or other error
		}
	}

	branch := strings.TrimSpace(string(output))
	if verbose {
		fmt.Fprintf(os.Stderr, "🔍 Raw branch output: %q\n", branch)
	}

	// Handle detached HEAD case
	if branch == "HEAD" {
		if verbose {
			fmt.Fprintf(os.Stderr, "🔍 Detected detached HEAD state\n")
		}
		return ""
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "🔍 Current branch: %q\n", branch)
	}

	return branch
}

// isProtectedBranch checks if the given branch is protected from direct commits
func isProtectedBranch(branch string) bool {
	protectedBranches := []string{"master", "main"}
	return slices.Contains(protectedBranches, branch)
}

// getTargetWorkingDirectory determines the target project directory from available context
// Returns empty string if we can't confidently determine the target directory
func getTargetWorkingDirectory(input Input, verbose bool) string {
	// Option 1: Check for CLAUDE_CODE_CWD environment variable
	if claudeDir := os.Getenv("CLAUDE_CODE_CWD"); claudeDir != "" {
		if verbose {
			fmt.Fprintf(os.Stderr, "🔍 Using working directory from CLAUDE_CODE_CWD: %s\n", claudeDir)
		}
		return claudeDir
	}

	// Option 2: Infer from file paths (most reliable)
	files := collectFiles(input.ToolInput)
	if len(files) > 0 {
		// Find the git repository root for any file
		for _, file := range files {
			if dir := findGitRoot(file, verbose); dir != "" {
				if verbose {
					fmt.Fprintf(os.Stderr, "🔍 Inferred working directory from file path: %s\n", dir)
				}
				return dir
			}
		}
	}

	// Option 3: Smart fallback - only use current directory if it seems reasonable
	if cwd, err := os.Getwd(); err == nil {
		// Check if we're running from the claude-hooks directory
		if strings.Contains(cwd, "claude-hooks") {
			if verbose {
				fmt.Fprintf(os.Stderr, "🚫 Skipping branch protection - running from claude-hooks directory without file context\n")
				fmt.Fprintf(os.Stderr, "   This prevents false positives when the hook can't determine the target project\n")
			}
			return "" // Return empty to skip protection
		}

		if verbose {
			fmt.Fprintf(os.Stderr, "🔍 Using current working directory as fallback: %s\n", cwd)
		}
		return cwd
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "🔍 Could not determine target working directory\n")
	}
	return ""
}

// findGitRoot finds the git repository root for a given file path
func findGitRoot(filePath string, verbose bool) string {
	dir := filepath.Dir(filePath)

	// Walk up the directory tree looking for .git
	for {
		gitDir := filepath.Join(dir, ".git")
		if _, err := os.Stat(gitDir); err == nil {
			if verbose {
				fmt.Fprintf(os.Stderr, "🔍 Found git root at: %s\n", dir)
			}
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached the root directory
			break
		}
		dir = parent
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "🔍 No git root found for file: %s\n", filePath)
	}
	return ""
}

func handleSessionStart(verbose bool) {
	// Read SessionStart input from stdin
	var input SessionStartInput
	decoder := json.NewDecoder(os.Stdin)
	if err := decoder.Decode(&input); err != nil {
		if verbose {
			fmt.Fprintf(os.Stderr, "Failed to parse SessionStart input: %v\n", err)
		}
		os.Exit(0)
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "SessionStart hook triggered (source: %s)\n", input.Source)
	}

	// Determine the working directory
	workingDir := os.Getenv("CLAUDE_CODE_CWD")
	if workingDir == "" && input.TranscriptPath != "" {
		// Try to infer from transcript path
		workingDir = filepath.Dir(input.TranscriptPath)
		// Go up one level from .claude/projects/...
		workingDir = filepath.Dir(filepath.Dir(workingDir))
	}

	if workingDir == "" {
		// Fallback to current directory
		var err error
		workingDir, err = os.Getwd()
		if err != nil {
			if verbose {
				fmt.Fprintf(os.Stderr, "Could not determine working directory: %v\n", err)
			}
			os.Exit(0)
		}
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "Looking for agents.md in: %s\n", workingDir)
	}

	// Look for agents.md in the working directory
	agentsPath := filepath.Join(workingDir, "agents.md")
	content, err := os.ReadFile(agentsPath)
	if err != nil {
		if verbose {
			if os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "agents.md not found, skipping injection\n")
			} else {
				fmt.Fprintf(os.Stderr, "Error reading agents.md: %v\n", err)
			}
		}
		os.Exit(0)
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "Injecting agents.md content (%d bytes)\n", len(content))
	}

	// Output the content to stdout (this gets injected into Claude's context)
	fmt.Println(string(content))
	os.Exit(0)
}

func main() {
	// Parse command-line flags
	var (
		hookType = flag.String("type", "post-edit", "Hook type (post-edit, pre-edit, pre-bash, session-start)")
		verbose  = flag.Bool("v", false, "Verbose output")
	)
	flag.Parse()

	// Handle session-start hook separately (different input format)
	if *hookType == "session-start" {
		handleSessionStart(*verbose)
		return
	}

	// Read input from stdin (Claude Code sends JSON via stdin)
	var input Input
	decoder := json.NewDecoder(os.Stdin)
	if err := decoder.Decode(&input); err != nil {
		// If no JSON input, check if file paths were passed as arguments
		if flag.NArg() > 0 {
			input.ToolInput.FilePaths = flag.Args()
		} else {
			if *verbose {
				log.Printf("No input provided or failed to parse JSON: %v\n", err)
			}
			os.Exit(0)
		}
	}

	// Handle pre-bash blocking for MySQL commands
	if *hookType == "pre-bash" {
		handlePreBashBlocking(input, *verbose)
		return
	}

	// Handle plan review for ExitPlanMode
	if *hookType == "plan-review" {
		handlePlanReview(input, *verbose)
		return
	}

	// Collect all files to process
	files := collectFiles(input.ToolInput)
	if len(files) == 0 {
		if *verbose {
			log.Println("No files to process")
		}
		os.Exit(0)
	}

	// Process files based on their type
	filesByType := groupFilesByType(files)

	hasErrors := false
	var errorMessages []string

	for fileType, fileList := range filesByType {
		if *verbose {
			fmt.Printf("Processing %d %s files...\n", len(fileList), fileType)
		}

		hook := hooks.GetHook(fileType)
		if hook == nil {
			if *verbose {
				fmt.Printf("No hook registered for %s files\n", fileType)
			}
			continue
		}

		// Run the hook based on type
		var err error
		switch *hookType {
		case "post-edit":
			err = hook.PostEditJSON(fileList, *verbose)
		case "pre-edit":
			err = hook.PreEdit(fileList, *verbose)
		default:
			fmt.Fprintf(os.Stderr, "Unknown hook type: %s\n", *hookType)
			os.Exit(2)
		}

		if err != nil {
			errorMsg := fmt.Sprintf("%s hook failed: %v", fileType, err)
			fmt.Fprintf(os.Stderr, "❌ %s\n", errorMsg)
			errorMessages = append(errorMessages, errorMsg)
			hasErrors = true
		}
	}

	if hasErrors {
		// For PostToolUse hooks, output JSON to communicate with Claude
		if *hookType == "post-edit" {
			output := HookOutput{
				Decision: "block",
				Reason:   strings.Join(errorMessages, "\n\n"),
			}

			jsonOutput, err := json.Marshal(output)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to marshal JSON output: %v\n", err)
				os.Exit(2)
			}

			fmt.Println(string(jsonOutput))
			os.Exit(0) // Exit with 0 when using JSON output
		} else {
			os.Exit(2) // Use exit code 2 for non-PostToolUse hooks
		}
	}

	fmt.Println("✅ All checks passed!")
}

func collectFiles(input ToolInput) []string {
	seen := make(map[string]bool)
	var files []string

	// Add single file if present
	if input.FilePath != "" {
		if !seen[input.FilePath] {
			seen[input.FilePath] = true
			files = append(files, input.FilePath)
		}
	}

	// Add multiple files
	for _, f := range input.FilePaths {
		if !seen[f] {
			seen[f] = true
			files = append(files, f)
		}
	}

	// Filter out vendor and generated files
	var filtered []string
	for _, f := range files {
		if strings.Contains(f, "/vendor/") ||
			strings.HasSuffix(f, ".pb.go") ||
			strings.HasSuffix(f, ".gen.go") {
			continue
		}
		filtered = append(filtered, f)
	}

	return filtered
}

func groupFilesByType(files []string) map[string][]string {
	groups := make(map[string][]string)

	for _, f := range files {
		ext := strings.ToLower(filepath.Ext(f))
		var fileType string

		switch ext {
		case ".go":
			fileType = "go"
		case ".ts", ".tsx":
			fileType = "typescript"
		case ".js", ".jsx":
			fileType = "javascript"
		case ".py":
			fileType = "python"
		default:
			continue // Skip unknown types
		}

		groups[fileType] = append(groups[fileType], f)
	}

	return groups
}

func handlePreBashBlocking(input Input, verbose bool) {
	// Check if this is a Bash tool call
	if input.ToolName != "Bash" && input.ToolName != "bash" {
		if verbose {
			fmt.Printf("Tool %s is not Bash, allowing\n", input.ToolName)
		}
		os.Exit(0)
	}

	command := input.ToolInput.Command
	if command == "" {
		if verbose {
			fmt.Println("No command found in input, allowing")
		}
		os.Exit(0)
	}

	// Check for MySQL/MariaDB commands in compound commands
	// Split by common shell operators to check all sub-commands
	subCommands := parseCompoundCommand(command)

	for _, subCmd := range subCommands {
		parts := strings.Fields(strings.TrimSpace(subCmd))
		if len(parts) > 0 {
			executable := strings.ToLower(filepath.Base(parts[0]))

			// Check if the executable itself is a MySQL/MariaDB command
			if executable == "mysql" || executable == "mysqldump" || executable == "mariadb" {
				reason := fmt.Sprintf("MySQL commands are not allowed. You attempted to run: %s\n\nDetected MySQL command in: %s\n\nPlease use the Go database connection methods instead. The codebase already has database access configured through Go.\n\nAlternatives:\n- Check existing Go code for database queries\n- Look at the model definitions in the codebase\n- Read the existing test files for schema information", command, subCmd)

				output := PreToolUseOutput{
					HookSpecificOutput: PreToolUseHookOutput{
						HookEventName:            "PreToolUse",
						PermissionDecision:       "deny", // Automatically block without prompting
						PermissionDecisionReason: reason,
					},
				}

				jsonOutput, err := json.Marshal(output)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Failed to marshal JSON output: %v\n", err)
					os.Exit(1)
				}

				// Output JSON to stdout for Claude
				fmt.Println(string(jsonOutput))

				// Also output user-friendly message to stderr
				fmt.Fprintf(os.Stderr, "❌ BLOCKED: MySQL commands are not allowed\n")
				fmt.Fprintf(os.Stderr, "\n")
				fmt.Fprintf(os.Stderr, "You attempted to run: %s\n", command)
				fmt.Fprintf(os.Stderr, "Detected MySQL command in: %s\n", subCmd)
				fmt.Fprintf(os.Stderr, "\n")
				fmt.Fprintf(os.Stderr, "Please use the Go database connection methods instead.\n")
				fmt.Fprintf(os.Stderr, "The codebase already has database access configured through Go.\n")
				fmt.Fprintf(os.Stderr, "\n")
				fmt.Fprintf(os.Stderr, "Alternatives:\n")
				fmt.Fprintf(os.Stderr, "- Check existing Go code for database queries\n")
				fmt.Fprintf(os.Stderr, "- Look at the model definitions in the codebase\n")
				fmt.Fprintf(os.Stderr, "- Read the existing test files for schema information\n")

				os.Exit(0) // Exit successfully since we provided JSON
			}

			// Check if the command is a git commit on a protected branch
			if executable == "git" && len(parts) >= 2 && parts[1] == "commit" {
				if verbose {
					fmt.Fprintf(os.Stderr, "🔍 Detected git commit command, checking branch protection...\n")
				}

				// Determine the target working directory for git branch check
				targetDir := getTargetWorkingDirectory(input, verbose)

				// Skip protection if we can't confidently determine the target directory
				if targetDir == "" {
					if verbose {
						fmt.Fprintf(os.Stderr, "✅ Skipping branch protection check - cannot determine target project directory\n")
					}
					// Allow the command to proceed by continuing to next subcommand
					continue
				}

				currentBranch := getCurrentBranch(targetDir, verbose)

				if verbose {
					fmt.Fprintf(os.Stderr, "🔍 Checking if branch %q is protected...\n", currentBranch)
				}
				if currentBranch != "" && isProtectedBranch(currentBranch) {
					if verbose {
						fmt.Fprintf(os.Stderr, "🚫 Branch %q is protected - blocking commit\n", currentBranch)
					}
					reason := fmt.Sprintf("Direct commits to the '%s' branch are not allowed. You attempted to run: %s\n\nDetected git commit command in: %s\n\nPlease create a feature branch instead:\n\n1. Create and switch to a new branch:\n   git checkout -b feature/your-feature-name\n\n2. Make your commits on the feature branch:\n   git commit -m \"your commit message\"\n\n3. Push the feature branch:\n   git push -u origin feature/your-feature-name\n\n4. Create a pull request to merge into %s", currentBranch, command, subCmd, currentBranch)

					output := PreToolUseOutput{
						HookSpecificOutput: PreToolUseHookOutput{
							HookEventName:            "PreToolUse",
							PermissionDecision:       "deny", // Automatically block without prompting
							PermissionDecisionReason: reason,
						},
					}

					jsonOutput, err := json.Marshal(output)
					if err != nil {
						fmt.Fprintf(os.Stderr, "Failed to marshal JSON output: %v\n", err)
						os.Exit(1)
					}

					// Output JSON to stdout for Claude
					fmt.Println(string(jsonOutput))

					// Also output user-friendly message to stderr
					fmt.Fprintf(os.Stderr, "❌ BLOCKED: Direct commits to '%s' branch are not allowed\n", currentBranch)
					fmt.Fprintf(os.Stderr, "\n")
					fmt.Fprintf(os.Stderr, "You attempted to run: %s\n", command)
					fmt.Fprintf(os.Stderr, "Detected git commit in: %s\n", subCmd)
					fmt.Fprintf(os.Stderr, "\n")
					fmt.Fprintf(os.Stderr, "Please create a feature branch instead:\n")
					fmt.Fprintf(os.Stderr, "\n")
					fmt.Fprintf(os.Stderr, "1. Create and switch to a new branch:\n")
					fmt.Fprintf(os.Stderr, "   git checkout -b feature/your-feature-name\n")
					fmt.Fprintf(os.Stderr, "\n")
					fmt.Fprintf(os.Stderr, "2. Make your commits on the feature branch:\n")
					fmt.Fprintf(os.Stderr, "   git commit -m \"your commit message\"\n")
					fmt.Fprintf(os.Stderr, "\n")
					fmt.Fprintf(os.Stderr, "3. Push the feature branch:\n")
					fmt.Fprintf(os.Stderr, "   git push -u origin feature/your-feature-name\n")
					fmt.Fprintf(os.Stderr, "\n")
					fmt.Fprintf(os.Stderr, "4. Create a pull request to merge into %s\n", currentBranch)

					os.Exit(0) // Exit successfully since we provided JSON
				} else if verbose {
					if currentBranch == "" {
						fmt.Fprintf(os.Stderr, "✅ Not in a git repo or detached HEAD - allowing commit\n")
					} else {
						fmt.Fprintf(os.Stderr, "✅ Branch %q is not protected - allowing commit\n", currentBranch)
					}
				}
			}
		}
	}

	if verbose {
		fmt.Printf("Command '%s' is allowed\n", command)
	}

	// Command is allowed
	os.Exit(0)
}

// handlePlanReview runs the plan through multiple AI models for feedback
func handlePlanReview(input Input, verbose bool) {
	// Always log to stderr so we can see if the hook is being called
	fmt.Fprintf(os.Stderr, "🧠 AI Council hook triggered!\n")
	fmt.Fprintf(os.Stderr, "   Tool: %s\n", input.ToolName)
	fmt.Fprintf(os.Stderr, "   Transcript: %s\n", input.TranscriptPath)
	fmt.Fprintf(os.Stderr, "   CWD: %s\n", input.Cwd)

	// Only run for ExitPlanMode tool
	if input.ToolName != "ExitPlanMode" {
		fmt.Fprintf(os.Stderr, "⏭️  Skipping - not ExitPlanMode (got: %s)\n", input.ToolName)
		os.Exit(0)
	}

	reviewInput := hooks.PlanReviewInput{
		TranscriptPath: input.TranscriptPath,
		Cwd:            input.Cwd,
	}

	result, err := hooks.ReviewPlan(reviewInput, verbose)
	if err != nil {
		fmt.Fprintf(os.Stderr, "⚠️  Plan review error: %v\n", err)
		// Don't block on review errors, just warn
		os.Exit(0)
	}

	// Use JSON output with "deny" to block and show feedback to Claude
	// This is the same pattern used by pre-bash blocking
	output := PreToolUseOutput{
		HookSpecificOutput: PreToolUseHookOutput{
			HookEventName:            "PreToolUse",
			PermissionDecision:       "deny", // Block so Claude sees the feedback
			PermissionDecisionReason: result.Summary + "\n\n📋 Please review the AI Council feedback above and adjust your plan if needed. Then call ExitPlanMode again to finalize.",
		},
	}

	jsonOutput, err := json.Marshal(output)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to marshal JSON output: %v\n", err)
		os.Exit(1)
	}

	// Output JSON to stdout for Claude to read
	fmt.Println(string(jsonOutput))

	// Also show human-readable summary to stderr for the user
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, strings.Repeat("=", 60))
	fmt.Fprintln(os.Stderr, result.Summary)
	fmt.Fprintln(os.Stderr, strings.Repeat("=", 60))

	os.Exit(0) // Exit 0 since we provided JSON
}

// parseCompoundCommand splits a shell command by common operators to extract sub-commands
func parseCompoundCommand(command string) []string {
	// Replace shell operators with a delimiter we can split on
	// Handle &&, ||, ;, and | (pipe)
	delim := "||SPLIT||"

	// Replace operators with our delimiter
	command = strings.ReplaceAll(command, "&&", delim)
	command = strings.ReplaceAll(command, "||", delim)
	command = strings.ReplaceAll(command, ";", delim)

	// Handle pipes - but be careful not to break quoted strings
	// Simple approach: split on | only if not inside quotes
	command = splitPipes(command, delim)

	// Split by our delimiter and clean up
	parts := strings.Split(command, delim)
	var subCommands []string

	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			subCommands = append(subCommands, trimmed)
		}
	}

	return subCommands
}

// splitPipes replaces pipes with delimiter, avoiding pipes inside quotes
func splitPipes(command, delim string) string {
	var result strings.Builder
	inQuotes := false
	var quoteChar rune

	for i, char := range command {
		switch char {
		case '"', '\'', '`':
			if !inQuotes {
				inQuotes = true
				quoteChar = char
			} else if char == quoteChar {
				inQuotes = false
			}
			result.WriteRune(char)
		case '|':
			if !inQuotes {
				// Check if it's not part of ||
				if i+1 < len(command) && rune(command[i+1]) == '|' {
					result.WriteRune(char) // Let || be handled by the main replacement
				} else {
					result.WriteString(delim)
				}
			} else {
				result.WriteRune(char)
			}
		default:
			result.WriteRune(char)
		}
	}

	return result.String()
}
