.PHONY: setup clean test run-hook

setup:
	@echo "Setting up Claude hooks with live reloading..."
	go run cmd/setup/main.go

clean:
	@echo "No binaries to clean (using go run)"

test:
	go test ./...

run-hook:
	@echo "Testing hook with example files..."
	echo '{"tool_input": {"file_paths": ["cmd/claude-hook/main.go"]}}' | go run cmd/claude-hook/main.go -v

# Development helpers
run-example-go:
	echo '{"tool_input": {"file_paths": ["example.go"]}}' | go run cmd/claude-hook/main.go -v

run-example-ts:
	echo '{"tool_input": {"file_paths": ["example.ts"]}}' | go run cmd/claude-hook/main.go -v
