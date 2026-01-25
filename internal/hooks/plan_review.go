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
	var bestPlan string
	var bestScore int

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
			score := scorePlan(content)
			// Keep the last candidate that has the highest or equal score
			// This ensures we get the most recent version of a plan if there are multiple
			if score > 0 && score >= bestScore {
				bestScore = score
				bestPlan = content
			}
		}
	}

	if bestPlan == "" {
		return "", fmt.Errorf("no plan content found in transcript")
	}

	return bestPlan, nil
}

// scorePlan evaluates how likely a message is to be a detailed plan
func scorePlan(content string) int {
	if content == "" {
		return 0
	}
	
	// Filter out very short messages (likely conversational fillers)
	// "Now let me write up the implementation plan." is ~44 chars
	if len(content) < 50 {
		return 0
	}

	score := 0
	lower := strings.ToLower(content)

	// High value indicators
	if strings.Contains(content, "## Plan") || 
	   strings.Contains(content, "## Implementation") || 
	   strings.Contains(content, "## Proposed Approach") {
		score += 100
	}

	// Structural indicators
	if strings.Contains(content, "1. ") && strings.Contains(content, "2. ") {
		score += 20
	}
	if strings.Contains(content, "- [ ]") || strings.Contains(content, "- [x]") {
		score += 20
	}

	// content keywords
	if strings.Contains(lower, "plan") {
		score += 10
	}
	if strings.Contains(lower, "step") {
		score += 5
	}

	return score
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

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// claude --print "prompt" --model ... --dangerously-skip-permissions
	cmd := exec.CommandContext(ctx, "claude",
		"--print",
		"--model", "claude-opus-4-5-20251101",
		"--dangerously-skip-permissions",
		prompt,
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	review.Duration = time.Since(start).Round(time.Second).String()

	if err != nil {
		if strings.Contains(err.Error(), "executable file not found") {
			review.Error = "Claude CLI not installed (npm install -g @anthropic-ai/claude-code)"
			review.Feedback = "⚠️ Claude CLI not available - install with: npm install -g @anthropic-ai/claude-code"
		} else if ctx.Err() == context.DeadlineExceeded {
			review.Error = "Claude review timed out (2m)"
			review.Feedback = "⚠️ Claude review timed out"
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

// runCodexReview runs the plan through OpenAI Codex CLI with o3
func runCodexReview(prompt string, verbose bool) AIReview {
	start := time.Now()
	review := AIReview{Model: "o3 (Codex)"}

	if verbose {
		fmt.Fprintf(os.Stderr, "🤖 Starting Codex/o3 review...\n")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// codex exec "prompt" --model o3 --dangerously-bypass-approvals-and-sandbox
	cmd := exec.CommandContext(ctx, "codex", "exec",
		"--model", "o3",
		"--dangerously-bypass-approvals-and-sandbox",
		prompt,
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
		} else if ctx.Err() == context.DeadlineExceeded {
			review.Error = "Codex review timed out (2m)"
			review.Feedback = "⚠️ Codex review timed out"
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

	// Shorter timeout for Gemini since it can get stuck
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Gemini CLI expects prompt as positional arg, not -p flag (deprecated)
	// Use --output-format text for clean output
	cmd := exec.CommandContext(ctx, "gemini",
		"--yolo",
		"--model", "gemini-2.5-pro",
		"--output-format", "text",
		prompt,
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
		} else if ctx.Err() == context.DeadlineExceeded {
			review.Error = "Gemini review timed out (60s)"
			review.Feedback = "⚠️ Gemini review timed out"
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
