# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a Go-based tool that provides automatic code formatting, linting, and testing hooks for Claude Code. It processes files after they are edited by Claude Code and runs language-specific quality checks.

The binary `claude-hook` reads JSON from stdin (provided by Claude Code) containing file paths that were modified, then runs appropriate formatters, linters, and tests based on file extensions.

## Setup and Development Commands

### One-Command Setup
```bash
# Setup with live reloading - no installation needed!
make setup
```

### Development
```bash
# Run tests
make test

# Test hook manually
make run-hook

# Test with example Go files
make run-example-go

# Test with TypeScript files  
make run-example-ts
```

## Architecture

### Core Components

- **`cmd/claude-hook/main.go`**: Entry point that reads JSON from stdin, parses file paths, groups files by type, and dispatches to appropriate hooks
- **`internal/hooks/hook.go`**: Defines the Hook interface and maintains a registry of language-specific hooks
- **`internal/hooks/go_hook.go`**: Go-specific formatting (goimports, gofumpt), linting (golangci-lint), testing, and go mod tidy
- **`internal/hooks/typescript_hook.go`**: TypeScript/JavaScript formatting (prettier), linting (eslint), and type checking (tsc)

### Hook System Design

The hook system uses a registry pattern where language-specific hooks implement the `Hook` interface:

```go
type Hook interface {
    PostEdit(files []string, verbose bool) error
    PreEdit(files []string, verbose bool) error
}
```

File types are determined by extension and mapped to hooks in `internal/hooks/hook.go:14-19`.

### Go Hook Pipeline
1. **goimports**: Import organization and formatting
2. **gofumpt**: Modern Go formatting (if available)
3. **Linting**: golangci-lint with multiple linters, fallback to go vet
4. **Testing**: Runs tests for modified files and their test counterparts
5. **go mod tidy**: Dependency management

### TypeScript Hook Pipeline
1. **prettier**: Code formatting
2. **eslint**: Linting with auto-fix
3. **tsc --noEmit**: Type checking

## Integration with Claude Code

The setup command automatically configures Claude Code hooks using:

### PostToolUse Hook (Code Quality)
- Event: `PostToolUse`
- Matcher: `Write|Edit|MultiEdit` 
- Command: `bash -c "cd /path/to/claude-hooks && go run cmd/claude-hook/main.go -type post-edit"`

### PreToolUse Hook (Security)
- Event: `PreToolUse`
- Matcher: `Bash`
- Command: `bash -c "cd /path/to/claude-hooks && go run cmd/claude-hook/main.go -type pre-bash"`
- **Blocks MySQL commands** (mysql, mysqldump, mariadb) to prevent accidental database access via CLI

**🔄 Live Reloading**: Changes to hook code take effect immediately - no rebuild or reinstall needed!

The hooks will exit with code 2 on failures to make them blocking in Claude Code, preventing further operations until issues are resolved.

## File Filtering

The tool automatically filters out:
- `/vendor/` directories
- Generated files (`*.pb.go`, `*.gen.go`)

Supported file types: `.go`, `.ts`, `.tsx`, `.js`, `.jsx`, `.py` (Python hook not yet implemented)