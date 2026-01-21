package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// PlanReviewInput contains the data needed to review a plan
type PlanReviewInput struct {
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
	PlanContent    string `json:"plan_content"` // Extracted from transcript or provided
}

// AIReview represents feedback from one AI reviewer
type AIReview struct {
	Model    string `json:"model"`
	Feedback string `json:"feedback"`
	Error    string `json:"error,omitempty"`
	Duration string `json:"duration"`
}

// PlanReviewResult contains all AI reviews
type PlanReviewResult struct {
	Reviews []AIReview `json:"reviews"`
	Summary string     `json:"summary"`
}

// TranscriptEntry represents a single entry in the Claude Code transcript
type TranscriptEntry struct {
	Type    string `json:"type"`
	Message struct {
		Role    string `json:"role"`
		Content any    `json:"content"` // Can be string or array
	} `json:"message"`
}

// ReviewPlan runs the plan through multiple AI models for feedback
func ReviewPlan(input PlanReviewInput, verbose bool) (*PlanReviewResult, error) {
	plan := input.PlanContent

	// If no plan content provided, try to extract from transcript
	if plan == "" && input.TranscriptPath != "" {
		var err error
		plan, err = extractPlanFromTranscript(input.TranscriptPath, verbose)
		if err != nil {
			return nil, fmt.Errorf("failed to extract plan: %w", err)
		}
	}

	if plan == "" {
		return nil, fmt.Errorf("no plan content found to review")
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "🔍 Plan to review (%d chars):\n%s\n\n", len(plan), truncateForDisplay(plan, 500))
	}

	// Run all 3 AI reviews in parallel
	var wg sync.WaitGroup
	reviews := make([]AIReview, 3)

	reviewPrompt := buildReviewPrompt(plan)

	wg.Add(3)

	// 1. Claude Opus 4.5 review
	go func() {
		defer wg.Done()
		reviews[0] = runClaudeReview(reviewPrompt, verbose)
	}()

	// 2. OpenAI Codex/GPT-5.2 review
	go func() {
		defer wg.Done()
		reviews[1] = runCodexReview(reviewPrompt, verbose)
	}()

	// 3. Gemini review
	go func() {
		defer wg.Done()
		reviews[2] = runGeminiReview(reviewPrompt, verbose)
	}()

	wg.Wait()

	result := &PlanReviewResult{
		Reviews: reviews,
		Summary: buildReviewSummary(reviews),
	}

	return result, nil
}

// extractPlanFromTranscript reads the transcript and extracts the most recent plan
func extractPlanFromTranscript(transcriptPath string, verbose bool) (string, error) {
	if verbose {
		fmt.Fprintf(os.Stderr, "📖 Reading transcript from: %s\n", transcriptPath)
	}

	data, err := os.ReadFile(transcriptPath)
	if err != nil {
		return "", fmt.Errorf("failed to read transcript: %w", err)
	}

	// Transcript is JSONL format - read line by line
	lines := strings.Split(string(data), "\n")
	var lastAssistantContent string

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}

		var entry TranscriptEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue // Skip malformed lines
		}

		// Look for assistant messages that might contain a plan
		if entry.Message.Role == "assistant" {
			content := extractContentString(entry.Message.Content)
			if content != "" && (strings.Contains(strings.ToLower(content), "plan") ||
				strings.Contains(content, "##") ||
				strings.Contains(content, "1.") ||
				strings.Contains(content, "Step")) {
				lastAssistantContent = content
			}
		}
	}

	if lastAssistantContent == "" {
		return "", fmt.Errorf("no plan content found in transcript")
	}

	return lastAssistantContent, nil
}

// extractContentString extracts string content from various message formats
func extractContentString(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				if text, exists := m["text"]; exists {
					if s, ok := text.(string); ok {
						parts = append(parts, s)
					}
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// buildReviewPrompt creates the prompt for AI reviewers
func buildReviewPrompt(plan string) string {
	return fmt.Sprintf(`You are a senior software architect reviewing an implementation plan. Please review the following plan and provide constructive feedback.

Focus on:
1. **Potential issues or risks** - What could go wrong? What edge cases might be missed?
2. **Missing considerations** - Are there any important aspects not addressed?
3. **Better alternatives** - Are there simpler or more robust approaches?
4. **Security concerns** - Any potential vulnerabilities?
5. **Performance implications** - Will this scale well?

Be concise but thorough. If the plan looks solid, say so briefly and note any minor improvements.

## Plan to Review:

%s

## Your Review:`, plan)
}

// runClaudeReview runs the plan through Claude Opus 4.5
func runClaudeReview(prompt string, verbose bool) AIReview {
	start := time.Now()
	review := AIReview{Model: "Claude Opus 4.5"}

	if verbose {
		fmt.Fprintf(os.Stderr, "🤖 Starting Claude Opus 4.5 review...\n")
	}

	// Try using the claude CLI if available
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "claude",
		"-p", prompt,
		"--model", "claude-opus-4-5-20251101",
		"--dangerously-skip-permissions",
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	review.Duration = time.Since(start).Round(time.Second).String()

	if err != nil {
		// Check if claude CLI is not installed
		if strings.Contains(err.Error(), "executable file not found") {
			review.Error = "Claude CLI not installed (npm install -g @anthropic-ai/claude-code)"
			review.Feedback = "⚠️ Claude CLI not available - install with: npm install -g @anthropic-ai/claude-code"
		} else {
			review.Error = fmt.Sprintf("Claude review failed: %v - %s", err, stderr.String())
			review.Feedback = "⚠️ Claude review failed - see error"
		}
		if verbose {
			fmt.Fprintf(os.Stderr, "❌ Claude review error: %s\n", review.Error)
		}
		return review
	}

	review.Feedback = strings.TrimSpace(stdout.String())
	if verbose {
		fmt.Fprintf(os.Stderr, "✅ Claude review complete (%s)\n", review.Duration)
	}
	return review
}

// runCodexReview runs the plan through OpenAI Codex CLI with GPT-5.2
func runCodexReview(prompt string, verbose bool) AIReview {
	start := time.Now()
	review := AIReview{Model: "GPT-5.2 (Codex)"}

	if verbose {
		fmt.Fprintf(os.Stderr, "🤖 Starting Codex/GPT-5.2 review...\n")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "codex", "exec", prompt,
		"--dangerously-bypass-approvals-and-sandbox",
		"--model", "gpt-5.2",
		"--config", "model_reasoning_effort=high",
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	review.Duration = time.Since(start).Round(time.Second).String()

	if err != nil {
		if strings.Contains(err.Error(), "executable file not found") {
			review.Error = "Codex CLI not installed"
			review.Feedback = "⚠️ Codex CLI not available - install OpenAI's Codex CLI"
		} else {
			review.Error = fmt.Sprintf("Codex review failed: %v - %s", err, stderr.String())
			review.Feedback = "⚠️ Codex review failed - see error"
		}
		if verbose {
			fmt.Fprintf(os.Stderr, "❌ Codex review error: %s\n", review.Error)
		}
		return review
	}

	review.Feedback = strings.TrimSpace(stdout.String())
	if verbose {
		fmt.Fprintf(os.Stderr, "✅ Codex review complete (%s)\n", review.Duration)
	}
	return review
}

// runGeminiReview runs the plan through Google Gemini
func runGeminiReview(prompt string, verbose bool) AIReview {
	start := time.Now()
	review := AIReview{Model: "Gemini 3 Pro"}

	if verbose {
		fmt.Fprintf(os.Stderr, "🤖 Starting Gemini review...\n")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "gemini",
		"--yolo",
		"--model", "gemini-3-pro-preview",
		"-p", prompt,
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	review.Duration = time.Since(start).Round(time.Second).String()

	if err != nil {
		if strings.Contains(err.Error(), "executable file not found") {
			review.Error = "Gemini CLI not installed"
			review.Feedback = "⚠️ Gemini CLI not available - install Google's Gemini CLI"
		} else {
			review.Error = fmt.Sprintf("Gemini review failed: %v - %s", err, stderr.String())
			review.Feedback = "⚠️ Gemini review failed - see error"
		}
		if verbose {
			fmt.Fprintf(os.Stderr, "❌ Gemini review error: %s\n", review.Error)
		}
		return review
	}

	review.Feedback = strings.TrimSpace(stdout.String())
	if verbose {
		fmt.Fprintf(os.Stderr, "✅ Gemini review complete (%s)\n", review.Duration)
	}
	return review
}

// buildReviewSummary creates a summary of all reviews
func buildReviewSummary(reviews []AIReview) string {
	var sb strings.Builder

	sb.WriteString("## 🧠 AI Council Plan Review\n\n")
	sb.WriteString("Your plan has been reviewed by three AI models. Consider their feedback before finalizing.\n\n")

	successCount := 0
	for _, r := range reviews {
		if r.Error == "" {
			successCount++
		}
	}

	sb.WriteString(fmt.Sprintf("**Reviews completed:** %d/3\n\n", successCount))
	sb.WriteString("---\n\n")

	for i, r := range reviews {
		icon := "✅"
		if r.Error != "" {
			icon = "⚠️"
		}

		sb.WriteString(fmt.Sprintf("### %s %s (%s)\n\n", icon, r.Model, r.Duration))

		if r.Error != "" {
			sb.WriteString(fmt.Sprintf("*Error: %s*\n\n", r.Error))
		}

		sb.WriteString(r.Feedback)
		sb.WriteString("\n\n")

		if i < len(reviews)-1 {
			sb.WriteString("---\n\n")
		}
	}

	return sb.String()
}

// truncateForDisplay truncates text for display purposes
func truncateForDisplay(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
