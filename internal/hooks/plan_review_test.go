package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractPlanFromTranscript(t *testing.T) {
	// Create a temporary directory for the transcript
	tmpDir := t.TempDir()
	transcriptPath := filepath.Join(tmpDir, "transcript.jsonl")

	// Create a sample transcript
	// 1. User asks for a plan
	// 2. Assistant provides a detailed plan
	// 3. User approves
	// 4. Assistant says "Now let me write up the implementation plan." (The problem case)
	
	realPlan := `## Implementation Plan

1. Create the database schema
2. Implement the API endpoints
3. Add authentication middleware
4. Write tests
`
	
	entries := []TranscriptEntry{
		{
			Type: "message",
			Message: struct {
				Role    string `json:"role"`
				Content any    `json:"content"`
			}{
				Role:    "user",
				Content: "Please create a plan for the new feature.",
			},
		},
		{
			Type: "message",
			Message: struct {
				Role    string `json:"role"`
				Content any    `json:"content"`
			}{
				Role:    "assistant",
				Content: realPlan,
			},
		},
		{
			Type: "message",
			Message: struct {
				Role    string `json:"role"`
				Content any    `json:"content"`
			}{
				Role:    "user",
				Content: "Looks good, proceed.",
			},
		},
		{
			Type: "message",
			Message: struct {
				Role    string `json:"role"`
				Content any    `json:"content"`
			}{
				Role:    "assistant",
				Content: "Now let me write up the implementation plan.",
			},
		},
	}

	// Write transcript to file
	f, err := os.Create(transcriptPath)
	if err != nil {
		t.Fatalf("Failed to create transcript file: %v", err)
	}
	
	encoder := json.NewEncoder(f)
	for _, entry := range entries {
		if err := encoder.Encode(entry); err != nil {
			t.Fatalf("Failed to encode transcript entry: %v", err)
		}
	}
	f.Close()

	// Run extraction
	extractedPlan, err := extractPlanFromTranscript(transcriptPath, false)
	if err != nil {
		t.Fatalf("Failed to extract plan: %v", err)
	}

	// Verify the result
	// The current buggy implementation will likely return the last message
	// We want it to return the realPlan
	if extractedPlan != realPlan {
		t.Errorf("Expected extracted plan to be:\n%q\nGot:\n%q", realPlan, extractedPlan)
	}
}
