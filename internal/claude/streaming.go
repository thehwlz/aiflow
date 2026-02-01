package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
)

// StreamingClient provides bidirectional communication with Claude Code
type StreamingClient struct {
	claudePath string
	workDir    string
	model      string

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser

	mu        sync.Mutex
	running   bool
	sessionID string
}

// StreamingClientConfig configures the streaming client
type StreamingClientConfig struct {
	ClaudePath string // Path to claude binary (empty = find in PATH)
	WorkDir    string // Working directory
	Model      string // Model to use (empty = default)
}

// NewStreamingClient creates a new streaming client
func NewStreamingClient(cfg StreamingClientConfig) *StreamingClient {
	return &StreamingClient{
		claudePath: cfg.ClaudePath,
		workDir:    cfg.WorkDir,
		model:      cfg.Model,
	}
}

// EventHandler is called for each event received from Claude
type EventHandler func(event *Event) error

// ToolHandler is called when Claude uses a tool, allowing interception
// Return the tool result to send back, or nil to let Claude handle it
type ToolHandler func(toolUse *ToolUse) (*ToolResult, error)

// StreamOptions configures a streaming session
type StreamOptions struct {
	// SystemPrompt to prepend to the conversation
	SystemPrompt string

	// OnEvent is called for each event received
	OnEvent EventHandler

	// OnToolUse is called when Claude calls a tool
	// Return a ToolResult to intercept the tool, or nil to let Claude handle it
	OnToolUse ToolHandler

	// OnText is called when Claude outputs text
	OnText func(text string)

	// OnError is called on errors
	OnError func(err error)

	// SkipPermissions enables --dangerously-skip-permissions
	SkipPermissions bool
}

// Start begins a streaming session with the given prompt
func (c *StreamingClient) Start(ctx context.Context, prompt string, opts StreamOptions) error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return fmt.Errorf("client already running")
	}
	c.running = true
	c.mu.Unlock()

	// Find Claude binary
	claudePath := c.claudePath
	if claudePath == "" {
		var err error
		claudePath, err = exec.LookPath("claude")
		if err != nil {
			return fmt.Errorf("claude not found in PATH")
		}
	}

	// Build args
	args := []string{
		"--print",
		"--output-format", "stream-json",
		"--input-format", "stream-json",
	}

	if opts.SkipPermissions {
		args = append(args, "--dangerously-skip-permissions")
	}

	if c.model != "" {
		args = append(args, "--model", c.model)
	}

	if opts.SystemPrompt != "" {
		args = append(args, "--system-prompt", opts.SystemPrompt)
	}

	// Create command
	c.cmd = exec.CommandContext(ctx, claudePath, args...)
	c.cmd.Dir = c.workDir

	// Set up pipes
	var err error
	c.stdin, err = c.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdin pipe: %w", err)
	}

	c.stdout, err = c.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	c.stderr, err = c.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to get stderr pipe: %w", err)
	}

	// Start process
	if err := c.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start claude: %w", err)
	}

	// Send initial prompt as user message
	if err := c.sendUserMessage(prompt); err != nil {
		c.cmd.Process.Kill()
		return fmt.Errorf("failed to send prompt: %w", err)
	}

	// Process events in goroutine
	go c.processEvents(opts)

	// Capture stderr
	go c.captureStderr(opts.OnError)

	return nil
}

// sendUserMessage sends a text message to Claude
func (c *StreamingClient) sendUserMessage(text string) error {
	msg := map[string]interface{}{
		"type": "user",
		"message": map[string]interface{}{
			"role": "user",
			"content": []map[string]interface{}{
				{"type": "text", "text": text},
			},
		},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(c.stdin, "%s\n", data)
	return err
}

// SendToolResult sends a tool result back to Claude
func (c *StreamingClient) SendToolResult(result ToolResult) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running || c.stdin == nil {
		return fmt.Errorf("client not running")
	}

	msg := map[string]interface{}{
		"type": "user",
		"message": map[string]interface{}{
			"role": "user",
			"content": []map[string]interface{}{
				{
					"type":        "tool_result",
					"tool_use_id": result.ToolUseID,
					"content":     result.Content,
					"is_error":    result.IsError,
				},
			},
		},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(c.stdin, "%s\n", data)
	return err
}

// processEvents reads and processes JSONL events from stdout
func (c *StreamingClient) processEvents(opts StreamOptions) {
	scanner := bufio.NewScanner(c.stdout)
	// Increase buffer size for large responses
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		event, err := ParseEvent(line)
		if err != nil {
			if opts.OnError != nil {
				opts.OnError(fmt.Errorf("failed to parse event: %w", err))
			}
			continue
		}

		// Track session ID
		if event.SessionID != "" {
			c.sessionID = event.SessionID
		}

		// Call event handler
		if opts.OnEvent != nil {
			if err := opts.OnEvent(event); err != nil {
				if opts.OnError != nil {
					opts.OnError(err)
				}
			}
		}

		// Handle text output
		if opts.OnText != nil && event.Type == EventTypeAssistant {
			text := event.GetText()
			if text != "" {
				opts.OnText(text)
			}
		}

		// Handle tool uses
		if opts.OnToolUse != nil && event.Type == EventTypeAssistant {
			for _, toolUse := range event.GetToolUses() {
				result, err := opts.OnToolUse(&toolUse)
				if err != nil {
					if opts.OnError != nil {
						opts.OnError(err)
					}
					continue
				}

				// If handler returned a result, send it back
				if result != nil {
					if err := c.SendToolResult(*result); err != nil {
						if opts.OnError != nil {
							opts.OnError(fmt.Errorf("failed to send tool result: %w", err))
						}
					}
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		if opts.OnError != nil && !strings.Contains(err.Error(), "file already closed") {
			opts.OnError(fmt.Errorf("scanner error: %w", err))
		}
	}

	c.mu.Lock()
	c.running = false
	c.mu.Unlock()
}

// captureStderr reads and reports stderr
func (c *StreamingClient) captureStderr(onError func(error)) {
	scanner := bufio.NewScanner(c.stderr)
	var stderrBuf strings.Builder

	for scanner.Scan() {
		line := scanner.Text()
		stderrBuf.WriteString(line)
		stderrBuf.WriteString("\n")
	}

	if stderrBuf.Len() > 0 && onError != nil {
		onError(fmt.Errorf("stderr: %s", strings.TrimSpace(stderrBuf.String())))
	}
}

// Wait waits for the session to complete
func (c *StreamingClient) Wait() error {
	if c.cmd == nil {
		return nil
	}
	return c.cmd.Wait()
}

// Stop terminates the session
func (c *StreamingClient) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return nil
	}

	c.running = false

	if c.stdin != nil {
		c.stdin.Close()
	}

	if c.cmd != nil && c.cmd.Process != nil {
		return c.cmd.Process.Kill()
	}

	return nil
}

// IsRunning returns whether the client is currently running
func (c *StreamingClient) IsRunning() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running
}

// SessionID returns the current session ID
func (c *StreamingClient) SessionID() string {
	return c.sessionID
}
