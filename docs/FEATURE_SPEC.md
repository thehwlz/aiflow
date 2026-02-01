# Build: Real Claude Code Orchestration for aiflow

## Reference Implementation

See `/Users/howell-aikit/side-projects/ralph-tui` for patterns. Key files:
- `src/plugins/agents/builtin/claude.ts` - Claude CLI spawning
- `src/plugins/agents/base.ts` - Subprocess execution with stdin prompt delivery
- `src/engine/index.ts` - Main orchestration loop
- `src/plugins/agents/tracing/parser.ts` - JSONL output parsing

## Current Problem

aiflow has placeholder code in `internal/tui/breakdown.go`:
- `fetchNextQuestion()` returns hardcoded questions (not real Claude calls)
- `simulateBreakdown()` returns 6 generic tasks (not real breakdown)
- Execution logic doesn't actually invoke Claude

## What to Build

### 1. Claude CLI Executor

Create `internal/claude/executor.go`:

```go
type Executor struct {
    Command string // "claude" or full path
    Model   string // optional model override
    CWD     string // working directory
}

type ExecuteOptions struct {
    Prompt      string
    AddDirs     []string // --add-dir paths for context
    Timeout     time.Duration
    DangerMode  bool // --dangerously-skip-permissions
}

type ExecuteResult struct {
    Output      string
    ExitCode    int
    RateLimited bool
    Error       error
}

func (e *Executor) Execute(ctx context.Context, opts ExecuteOptions) (*ExecuteResult, error) {
    args := []string{
        "--print",
        "--verbose",
        "--output-format", "stream-json",
    }

    if e.Model != "" {
        args = append(args, "--model", e.Model)
    }

    if opts.DangerMode {
        args = append(args, "--dangerously-skip-permissions")
    }

    for _, dir := range opts.AddDirs {
        args = append(args, "--add-dir", dir)
    }

    cmd := exec.CommandContext(ctx, e.Command, args...)
    cmd.Dir = e.CWD

    // Pass prompt via stdin (not args) to avoid escaping issues
    stdin, _ := cmd.StdinPipe()
    go func() {
        stdin.Write([]byte(opts.Prompt))
        stdin.Close()
    }()

    // Capture output
    var stdout, stderr bytes.Buffer
    cmd.Stdout = &stdout
    cmd.Stderr = &stderr

    err := cmd.Run()
    // ... parse output, detect rate limits, etc.
}
```

### 2. JSONL Output Parser

Create `internal/claude/parser.go`:

```go
type JSONLMessage struct {
    Type    string          `json:"type"`
    Content json.RawMessage `json:"content"`
}

type AssistantContent struct {
    Type  string `json:"type"` // "text", "tool_use", "tool_result"
    Name  string `json:"name,omitempty"` // for tool_use
    Text  string `json:"text,omitempty"`
    Input json.RawMessage `json:"input,omitempty"`
}

func ParseJSONLOutput(output string) ([]JSONLMessage, error) {
    // Parse line-by-line JSONL
    // Detect subagent spawning (tool_use with name="Task")
    // Extract text responses
}

func DetectCompletion(output string) bool {
    // Look for completion signals in output
    return strings.Contains(output, "<promise>COMPLETE</promise>") ||
           strings.Contains(output, "Task completed")
}

func DetectRateLimit(output string) bool {
    return strings.Contains(output, "rate limit") ||
           strings.Contains(output, "429") ||
           strings.Contains(output, "overloaded")
}
```

### 3. Specification Flow (Real Q&A)

Update `internal/breakdown/conversation.go`:

```go
func (c *SpecConversation) FetchNextQuestion(executor *claude.Executor) (*ConversationQuestion, bool, error) {
    prompt := fmt.Sprintf(`You are gathering requirements for implementing: "%s"

Project type: %s
Project path: %s

Previous conversation:
%s

Your task: Ask ONE clarifying question to better understand what to build.
- Provide 2-4 options, each with a label and description
- Mark one option as recommended if appropriate
- If you have enough information, respond with "ready" instead

Respond in this exact JSON format:
{"type": "question", "question": {"text": "...", "options": [{"label": "...", "description": "...", "recommended": true/false}]}}

OR if ready to generate tasks:
{"type": "ready"}`,
        c.FeatureDesc, c.ProjectType, c.ProjectPath, c.FormatHistory())

    result, err := executor.Execute(context.Background(), claude.ExecuteOptions{
        Prompt:   prompt,
        AddDirs:  []string{c.ProjectPath},
        Timeout:  30 * time.Second,
    })

    // Parse JSON response from Claude's output
    // Return question or signal ready
}
```

### 4. Task Breakdown Generation

Update `internal/breakdown/breakdown.go`:

```go
func GenerateBreakdown(executor *claude.Executor, conv *SpecConversation) ([]*state.Task, error) {
    prompt := fmt.Sprintf(`Generate a task breakdown for implementing: "%s"

Project context:
- Type: %s
- Path: %s

Requirements gathered:
%s

IMPORTANT INSTRUCTIONS:
1. Before implementing anything, check for existing code that can be reused
2. Check global Claude Code skills available: run 'claude /help' to see skills
3. After implementation, check for code duplication and refactor if needed

Generate tasks in this JSON format:
{
  "tasks": [
    {
      "id": "task-1",
      "title": "Short imperative title",
      "description": "Detailed description of what to implement",
      "priority": 1,
      "parallel_group": "setup|core|testing|etc",
      "depends_on": ["task-id", ...]
    }
  ]
}

Include these automatic tasks:
1. First task: "Check for reusable code" - search codebase before implementing
2. Last task: "Technical debt check" - review for duplication after implementation
3. Include unit tests for core functionality`,
        conv.FeatureDesc, conv.ProjectType, conv.ProjectPath, conv.FormatHistory())

    result, err := executor.Execute(context.Background(), claude.ExecuteOptions{
        Prompt:   prompt,
        AddDirs:  []string{conv.ProjectPath},
        Timeout:  60 * time.Second,
    })

    // Parse JSON task list from output
}
```

### 5. Task Execution Loop

Update `internal/executor/executor.go`:

```go
func (e *Executor) ExecuteTask(task *state.Task) error {
    prompt := fmt.Sprintf(`Execute this task:

Title: %s
Description: %s

Project path: %s

INSTRUCTIONS:
- Implement the task completely
- Write clean, idiomatic code
- Follow existing patterns in the codebase
- When done, output: <promise>COMPLETE</promise>

If you encounter blockers, explain them clearly.`,
        task.Title, task.Description, e.run.WorktreePath)

    result, err := e.claude.Execute(context.Background(), claude.ExecuteOptions{
        Prompt:     prompt,
        AddDirs:    []string{e.run.WorktreePath},
        Timeout:    5 * time.Minute,
        DangerMode: true, // Allow autonomous file operations
    })

    if err != nil {
        return e.handleTaskError(task, err, result)
    }

    // Commit changes after successful task
    sha, err := e.commitTask(task)
    if err != nil {
        return err
    }
    task.CommitSHA = sha

    return nil
}

func (e *Executor) commitTask(task *state.Task) (string, error) {
    // git add -A && git commit -m "feat(<scope>): <title>"
    // Return commit SHA for rollback support
}

func (e *Executor) rollbackToCommit(sha string) error {
    // git reset --hard <sha>
}
```

### 6. Error Handling & Rollback

```go
func (e *Executor) handleTaskError(task *state.Task, err error, result *claude.ExecuteResult) error {
    if result.RateLimited {
        // Exponential backoff: 5s, 15s, 45s
        return e.retryWithBackoff(task)
    }

    // Offer rollback options via TUI
    e.tui.ShowErrorOptions(task, err, []string{
        "Rollback to last commit",
        "Retry task",
        "Skip and continue",
        "Abort run",
    })
}
```

### 7. Resume Command

Create `internal/cli/resume.go`:

```go
var resumeCmd = &cobra.Command{
    Use:   "resume [run-id]",
    Short: "Resume an interrupted run",
    RunE: func(cmd *cobra.Command, args []string) error {
        store, _ := state.NewStore(cfg.StateDir)

        var run *state.Run
        if len(args) > 0 {
            run, _ = store.GetRun(args[0])
        } else {
            run, _ = store.GetLatestRun()
        }

        // Find first pending task
        // Resume TUI from execution phase
        return tui.Run(cfg, run)
    },
}
```

### 8. Post-Completion: CLAUDE.md Updates

```go
func (e *Executor) updateClaudeMD() error {
    prompt := `Analyze what was just implemented and update CLAUDE.md with:
- New patterns and conventions used
- Key files created or modified
- Architectural decisions made
- New dependencies added

Read the current CLAUDE.md (if exists) and append new sections.
Do not duplicate existing content.`

    // Execute with Claude
}
```

### 9. PR/Merge Options

Update `internal/tui/completion.go` to show:
1. "Create PR" → then "Merge now?"
2. "Merge to main directly"
3. "Keep on branch"

## Files to Create/Modify

### New Files
- `internal/claude/executor.go` - Claude CLI subprocess management
- `internal/claude/parser.go` - JSONL output parsing
- `internal/cli/resume.go` - Resume command

### Modify
- `internal/tui/breakdown.go` - Replace simulated logic with real Claude calls
- `internal/breakdown/conversation.go` - Real Q&A with Claude
- `internal/breakdown/breakdown.go` - Real task generation
- `internal/executor/executor.go` - Real task execution + commits
- `internal/state/state.go` - Add CommitSHA field to Task
- `internal/tui/completion.go` - Add PR/merge options

## Testing

After implementation:
1. `go build ./...` - Verify compilation
2. `go test ./...` - Run tests
3. Manual test: `aiflow start "Add a hello world endpoint"` in a test project
