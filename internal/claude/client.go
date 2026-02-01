package claude

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Client provides a reusable interface for calling Claude Code
type Client struct {
	claudePath string
	workDir    string
}

// NewClient creates a new Claude client
func NewClient(claudePath, workDir string) *Client {
	return &Client{
		claudePath: claudePath,
		workDir:    workDir,
	}
}

// Execute runs Claude Code with the given prompt and returns the output
func (c *Client) Execute(ctx context.Context, prompt string) (string, error) {
	claudePath := c.claudePath
	if claudePath == "" {
		var err error
		claudePath, err = exec.LookPath("claude")
		if err != nil {
			return "", fmt.Errorf("claude code not found in PATH")
		}
	}

	// Build command
	cmd := exec.CommandContext(ctx, claudePath,
		"--print",                        // Non-interactive mode
		"--dangerously-skip-permissions", // Skip permission prompts
	)
	cmd.Dir = c.workDir

	// Set up stdin with the prompt
	cmd.Stdin = strings.NewReader(prompt)

	// Capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Run
	err := cmd.Run()
	if err != nil {
		return stdout.String(), fmt.Errorf("claude code failed: %w: %s", err, stderr.String())
	}

	return stdout.String(), nil
}

// ExecuteWithModel runs Claude Code with a specific model
func (c *Client) ExecuteWithModel(ctx context.Context, prompt, model string) (string, error) {
	claudePath := c.claudePath
	if claudePath == "" {
		var err error
		claudePath, err = exec.LookPath("claude")
		if err != nil {
			return "", fmt.Errorf("claude code not found in PATH")
		}
	}

	args := []string{
		"--print",
		"--dangerously-skip-permissions",
	}
	if model != "" {
		args = append(args, "--model", model)
	}

	cmd := exec.CommandContext(ctx, claudePath, args...)
	cmd.Dir = c.workDir
	cmd.Stdin = strings.NewReader(prompt)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return stdout.String(), fmt.Errorf("claude code failed: %w: %s", err, stderr.String())
	}

	return stdout.String(), nil
}
