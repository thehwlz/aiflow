package context

import (
	"strings"
	"unicode/utf8"
)

// EstimateTokens provides a rough estimate of token count for text
// Uses the approximation of ~4 characters per token for English text
func EstimateTokens(text string) int {
	// Count characters (not bytes)
	charCount := utf8.RuneCountInString(text)
	// Rough approximation: ~4 chars per token for English
	// This is conservative; actual token count may be lower
	return (charCount + 3) / 4
}

// EstimateTokensForFile estimates tokens for file content with path
func EstimateTokensForFile(path, content string) int {
	// Include path in estimation (will be in prompt)
	headerTokens := EstimateTokens(path) + 10 // Extra for formatting
	contentTokens := EstimateTokens(content)
	return headerTokens + contentTokens
}

// TruncateToTokens truncates text to approximately maxTokens
func TruncateToTokens(text string, maxTokens int) string {
	if maxTokens <= 0 {
		return ""
	}

	// Approximate character limit
	maxChars := maxTokens * 4

	runes := []rune(text)
	if len(runes) <= maxChars {
		return text
	}

	// Truncate and add indicator
	truncated := string(runes[:maxChars-20])
	// Find last newline for clean break
	lastNewline := strings.LastIndex(truncated, "\n")
	if lastNewline > len(truncated)/2 {
		truncated = truncated[:lastNewline]
	}

	return truncated + "\n\n... [truncated]"
}

// CountLines counts the number of lines in text
func CountLines(text string) int {
	if text == "" {
		return 0
	}
	return strings.Count(text, "\n") + 1
}

// TruncateLines truncates text to maxLines
func TruncateLines(text string, maxLines int) string {
	if maxLines <= 0 {
		return ""
	}

	lines := strings.Split(text, "\n")
	if len(lines) <= maxLines {
		return text
	}

	truncated := strings.Join(lines[:maxLines], "\n")
	return truncated + "\n\n... [" + string(rune(len(lines)-maxLines)) + " more lines]"
}

// TokenBudget helps manage token allocation across multiple items
type TokenBudget struct {
	Total     int
	Used      int
	Reserved  int // Reserved for static parts of prompt
}

// NewTokenBudget creates a budget with total tokens and reserved amount
func NewTokenBudget(total, reserved int) *TokenBudget {
	return &TokenBudget{
		Total:    total,
		Reserved: reserved,
	}
}

// Available returns remaining available tokens
func (b *TokenBudget) Available() int {
	return b.Total - b.Used - b.Reserved
}

// CanFit returns true if the given tokens fit in the budget
func (b *TokenBudget) CanFit(tokens int) bool {
	return b.Available() >= tokens
}

// Use marks tokens as used, returns true if successful
func (b *TokenBudget) Use(tokens int) bool {
	if !b.CanFit(tokens) {
		return false
	}
	b.Used += tokens
	return true
}

// TryFitContent attempts to fit content within budget, truncating if needed
func (b *TokenBudget) TryFitContent(content string, minTokens int) (string, bool) {
	tokens := EstimateTokens(content)

	// Fits as-is
	if b.CanFit(tokens) {
		b.Used += tokens
		return content, true
	}

	// Try truncating
	available := b.Available()
	if available < minTokens {
		return "", false
	}

	truncated := TruncateToTokens(content, available)
	truncatedTokens := EstimateTokens(truncated)
	b.Used += truncatedTokens
	return truncated, true
}
