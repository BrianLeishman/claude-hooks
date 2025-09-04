package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/brianleishman/claude-hooks/internal/hooks"
)

// ToolInput represents the input from Claude Code
type ToolInput struct {
	FilePath  string   `json:"file_path"`
	FilePaths []string `json:"file_paths"`
	Command   string   `json:"command"` // For Bash commands in PreToolUse
}

// Input represents the complete input structure
type Input struct {
	ToolName  string    `json:"tool_name"` // Tool being called (e.g., "Bash")
	ToolInput ToolInput `json:"tool_input"`
}

func main() {
	// Parse command-line flags
	var (
		hookType = flag.String("type", "post-edit", "Hook type (post-edit, pre-edit, pre-bash)")
		verbose  = flag.Bool("v", false, "Verbose output")
	)
	flag.Parse()

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
			err = hook.PostEdit(fileList, *verbose)
		case "pre-edit":
			err = hook.PreEdit(fileList, *verbose)
		default:
			fmt.Fprintf(os.Stderr, "Unknown hook type: %s\n", *hookType)
			os.Exit(2)
		}

		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %s hook failed: %v\n", fileType, err)
			hasErrors = true
		}
	}

	if hasErrors {
		os.Exit(2) // Exit code 2 makes it blocking in Claude Code
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

	// Check for MySQL/MariaDB commands
	if strings.Contains(command, "mysql") ||
		strings.Contains(command, "mysqldump") ||
		strings.Contains(command, "mariadb") {
		fmt.Fprintf(os.Stderr, "❌ BLOCKED: MySQL commands are not allowed\n")
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "You attempted to run: %s\n", command)
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "Please use the Go database connection methods instead.\n")
		fmt.Fprintf(os.Stderr, "The codebase already has database access configured through Go.\n")
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "Alternatives:\n")
		fmt.Fprintf(os.Stderr, "- Check existing Go code for database queries\n")
		fmt.Fprintf(os.Stderr, "- Look at the model definitions in the codebase\n")
		fmt.Fprintf(os.Stderr, "- Read the existing test files for schema information\n")
		os.Exit(2)
	}

	if verbose {
		fmt.Printf("Command '%s' is allowed\n", command)
	}

	// Command is allowed
	os.Exit(0)
}
