package executor

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/howell-aikit/aiflow/internal/config"
	ctxpkg "github.com/howell-aikit/aiflow/internal/context"
	"github.com/howell-aikit/aiflow/internal/scheduler"
	"github.com/howell-aikit/aiflow/internal/state"
	"github.com/howell-aikit/aiflow/pkg/git"
)

// Executor handles Claude Code invocation for tasks
type Executor struct {
	cfg       *config.Config
	workDir   string
	store     *state.Store
	run       *state.Run
	fileLock  *scheduler.FileLock
	ctxBuilder *ctxpkg.Builder
}

// NewExecutor creates a new executor
func NewExecutor(cfg *config.Config, workDir string, store *state.Store, run *state.Run) *Executor {
	return &Executor{
		cfg:        cfg,
		workDir:    workDir,
		store:      store,
		run:        run,
		fileLock:   scheduler.NewFileLock(workDir, cfg.LockTimeoutDuration()),
		ctxBuilder: ctxpkg.NewBuilder(workDir, cfg, run),
	}
}

// TaskResult contains the result of task execution
type TaskResult struct {
	TaskID    string
	Success   bool
	Output    string
	Error     error
	Summary   *state.TaskSummary
	Duration  time.Duration
}

// ExecuteTask executes a single task with Claude Code
func (e *Executor) ExecuteTask(ctx context.Context, task *state.Task) *TaskResult {
	startTime := time.Now()
	result := &TaskResult{TaskID: task.ID}

	// Acquire file locks
	lockSet, err := e.fileLock.AcquireLockSet(task.FilesWrite, task.FilesCreate)
	if err != nil {
		result.Error = fmt.Errorf("failed to acquire locks: %w", err)
		return result
	}
	defer lockSet.Release()

	// Update task status
	if err := e.store.SetTaskStatus(e.run.ID, task.ID, state.TaskStatusRunning); err != nil {
		result.Error = fmt.Errorf("failed to update task status: %w", err)
		return result
	}

	// Build the prompt
	prompt, err := e.ctxBuilder.BuildTaskPrompt(task)
	if err != nil {
		result.Error = fmt.Errorf("failed to build prompt: %w", err)
		e.store.SetTaskError(e.run.ID, task.ID, result.Error.Error())
		return result
	}

	// Execute Claude Code
	output, err := e.runClaudeCode(ctx, prompt)
	result.Output = output
	result.Duration = time.Since(startTime)

	if err != nil {
		result.Error = err
		e.store.SetTaskError(e.run.ID, task.ID, err.Error())
		return result
	}

	// Extract summary
	summary, err := e.extractSummary(ctx, task.ID)
	if err != nil {
		// Non-fatal: log warning but continue
		fmt.Printf("Warning: failed to extract summary for task %s: %v\n", task.ID, err)
	} else {
		result.Summary = summary
		e.store.SetTaskSummary(e.run.ID, task.ID, summary)
	}

	// Mark completed
	if err := e.store.SetTaskStatus(e.run.ID, task.ID, state.TaskStatusCompleted); err != nil {
		result.Error = fmt.Errorf("failed to update task status: %w", err)
		return result
	}

	// Create git commit for this task
	if sha, err := e.commitTask(task); err != nil {
		// Non-fatal: log warning but continue
		fmt.Printf("Warning: failed to create commit for task %s: %v\n", task.ID, err)
	} else if sha != "" {
		task.CommitSHA = sha
		e.store.UpdateTask(e.run.ID, task.ID, func(t *state.Task) {
			t.CommitSHA = sha
		})
	}

	result.Success = true
	return result
}

// commitTask creates a git commit for the completed task
func (e *Executor) commitTask(task *state.Task) (string, error) {
	repo, err := git.Open(e.workDir)
	if err != nil {
		return "", fmt.Errorf("failed to open repository: %w", err)
	}

	// Check if there are changes to commit
	dirty, err := repo.IsDirty()
	if err != nil {
		return "", fmt.Errorf("failed to check status: %w", err)
	}
	if !dirty {
		return "", nil // Nothing to commit
	}

	// Stage all changes
	if err := repo.StageAll(); err != nil {
		return "", fmt.Errorf("failed to stage changes: %w", err)
	}

	// Create commit
	commitMsg := fmt.Sprintf("aiflow: %s", task.Title)
	sha, err := repo.Commit(commitMsg)
	if err != nil {
		return "", fmt.Errorf("failed to commit: %w", err)
	}

	return sha, nil
}

// runClaudeCode invokes Claude Code with the given prompt
func (e *Executor) runClaudeCode(ctx context.Context, prompt string) (string, error) {
	// Find Claude Code binary
	claudePath := e.cfg.ClaudeCodePath
	if claudePath == "" {
		var err error
		claudePath, err = exec.LookPath("claude")
		if err != nil {
			return "", fmt.Errorf("claude code not found in PATH")
		}
	}

	// Write prompt to temp file
	promptFile, err := os.CreateTemp("", "aiflow-prompt-*.md")
	if err != nil {
		return "", fmt.Errorf("failed to create prompt file: %w", err)
	}
	defer os.Remove(promptFile.Name())

	if _, err := promptFile.WriteString(prompt); err != nil {
		return "", fmt.Errorf("failed to write prompt: %w", err)
	}
	promptFile.Close()

	// Build command
	cmd := exec.CommandContext(ctx, claudePath,
		"--print",           // Non-interactive mode
		"--dangerously-skip-permissions", // Skip permission prompts
	)
	cmd.Dir = e.workDir

	// Set up stdin with the prompt
	cmd.Stdin = strings.NewReader(prompt)

	// Capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Run
	err = cmd.Run()
	if err != nil {
		return stdout.String(), fmt.Errorf("claude code failed: %w: %s", err, stderr.String())
	}

	return stdout.String(), nil
}

// extractSummary asks Claude to extract a summary of the changes
func (e *Executor) extractSummary(ctx context.Context, taskID string) (*state.TaskSummary, error) {
	prompt := ctxpkg.SummaryExtractionPrompt

	output, err := e.runClaudeCode(ctx, prompt)
	if err != nil {
		return nil, err
	}

	return ctxpkg.ParseSummary(taskID, output)
}

// ExecuteBatch executes a batch of tasks in parallel
func (e *Executor) ExecuteBatch(ctx context.Context, tasks []*state.Task) []*TaskResult {
	results := make([]*TaskResult, len(tasks))
	var wg sync.WaitGroup

	for i, task := range tasks {
		wg.Add(1)
		go func(idx int, t *state.Task) {
			defer wg.Done()
			results[idx] = e.ExecuteTask(ctx, t)
		}(i, task)
	}

	wg.Wait()
	return results
}

// ExecuteAll executes all tasks in the run respecting dependencies
func (e *Executor) ExecuteAll(ctx context.Context, progressFn func(completed, total int)) error {
	sched := scheduler.NewScheduler(e.run, e.cfg.MaxParallel)
	total := len(e.run.Tasks)
	completed := len(e.run.GetCompletedTasks())

	if progressFn != nil {
		progressFn(completed, total)
	}

	for {
		// Check context
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Get next batch
		batch := sched.GetNextBatch()
		if len(batch) == 0 {
			break
		}

		// Execute batch
		results := e.ExecuteBatch(ctx, batch)

		// Process results
		for _, result := range results {
			if !result.Success {
				// Update run status
				e.run.Status = state.RunStatusFailed
				e.store.SaveRun(e.run)
				return fmt.Errorf("task %s failed: %v", result.TaskID, result.Error)
			}
			completed++
		}

		// Reload run state (tasks updated)
		updatedRun, err := e.store.LoadRun(e.run.ID)
		if err != nil {
			return fmt.Errorf("failed to reload run: %w", err)
		}
		e.run = updatedRun

		if progressFn != nil {
			progressFn(completed, total)
		}
	}

	// Mark run complete
	if e.run.IsComplete() {
		e.run.Status = state.RunStatusCompleted
		e.store.SaveRun(e.run)
	}

	return nil
}

// StreamingExecutor provides streaming output during execution
type StreamingExecutor struct {
	*Executor
	outputChan chan OutputEvent
}

// OutputEvent represents a streaming output event
type OutputEvent struct {
	TaskID string
	Type   string // "start", "output", "complete", "error"
	Data   string
}

// NewStreamingExecutor creates an executor with streaming output
func NewStreamingExecutor(cfg *config.Config, workDir string, store *state.Store, run *state.Run) *StreamingExecutor {
	return &StreamingExecutor{
		Executor:   NewExecutor(cfg, workDir, store, run),
		outputChan: make(chan OutputEvent, 100),
	}
}

// OutputChannel returns the channel for output events
func (se *StreamingExecutor) OutputChannel() <-chan OutputEvent {
	return se.outputChan
}

// ExecuteTaskStreaming executes a task with streaming output
func (se *StreamingExecutor) ExecuteTaskStreaming(ctx context.Context, task *state.Task) *TaskResult {
	se.outputChan <- OutputEvent{TaskID: task.ID, Type: "start", Data: task.Title}

	result := se.ExecuteTask(ctx, task)

	if result.Success {
		se.outputChan <- OutputEvent{TaskID: task.ID, Type: "complete", Data: "success"}
	} else {
		se.outputChan <- OutputEvent{TaskID: task.ID, Type: "error", Data: result.Error.Error()}
	}

	return result
}

// runClaudeCodeStreaming runs Claude Code with streaming output
func (se *StreamingExecutor) runClaudeCodeStreaming(ctx context.Context, taskID, prompt string) (string, error) {
	claudePath := se.cfg.ClaudeCodePath
	if claudePath == "" {
		var err error
		claudePath, err = exec.LookPath("claude")
		if err != nil {
			return "", fmt.Errorf("claude code not found in PATH")
		}
	}

	cmd := exec.CommandContext(ctx, claudePath, "--print")
	cmd.Dir = se.workDir
	cmd.Stdin = strings.NewReader(prompt)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}

	var fullOutput bytes.Buffer

	if err := cmd.Start(); err != nil {
		return "", err
	}

	// Stream output
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		fullOutput.WriteString(line + "\n")
		se.outputChan <- OutputEvent{TaskID: taskID, Type: "output", Data: line}
	}

	if err := cmd.Wait(); err != nil {
		return fullOutput.String(), err
	}

	return fullOutput.String(), nil
}

// WritePromptFile writes a prompt to a file for debugging
func WritePromptFile(dir, taskID, prompt string) (string, error) {
	filename := filepath.Join(dir, fmt.Sprintf(".aiflow-prompt-%s.md", taskID))
	if err := os.WriteFile(filename, []byte(prompt), 0644); err != nil {
		return "", err
	}
	return filename, nil
}

// CaptureOutput captures command output to both a buffer and an io.Writer
func CaptureOutput(w io.Writer) (*bytes.Buffer, io.Writer) {
	buf := &bytes.Buffer{}
	return buf, io.MultiWriter(buf, w)
}
