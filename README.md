# aiflow

AI Agent Orchestrator for Feature Implementation - a Go CLI that orchestrates Claude Code to implement features through isolated worktrees, interactive breakdown, and parallel execution.

## Features

- **Isolated Worktrees**: Each feature runs in its own git worktree
- **Interactive Breakdown**: Claude analyzes your codebase and decomposes features into tasks
- **Hybrid Context Management**: Fresh context + structured summaries from prior tasks
- **Parallel Execution**: Independent tasks run concurrently with file locking
- **Resume Support**: Interrupted runs can be resumed from where they left off

## Installation

```bash
# Clone the repository
git clone https://github.com/thehwlz/aiflow.git
cd aiflow

# Build
go build -o aiflow ./cmd/aiflow

# Install globally (optional)
sudo cp aiflow /usr/local/bin/
```

## Prerequisites

- Go 1.22+
- Git
- [Claude Code](https://claude.ai/claude-code) CLI installed and in PATH

## Usage

### Start a New Feature

```bash
cd /path/to/your/project
aiflow start "Add user authentication with JWT"
```

This will:
1. Create an isolated worktree at `.aiflow-worktrees/<feature>-<timestamp>/`
2. Launch interactive breakdown (Claude analyzes codebase, generates tasks)
3. Execute tasks in parallel (respecting dependencies)
4. Allow you to review and merge when complete

### Check Status

```bash
aiflow status           # Current run
aiflow status abc123    # Specific run
```

### List Runs

```bash
aiflow list             # List all runs
aiflow list -w          # List worktrees
```

### Resume Interrupted Run

```bash
aiflow resume           # Resume current run
aiflow resume abc123    # Resume specific run
```

### Clean Up

```bash
aiflow clean abc123     # Remove specific run
aiflow clean --all      # Remove all runs
aiflow clean -f abc123  # Force (no confirmation)
```

## Configuration

Create `~/.aiflow/config.toml`:

```toml
worktree_dir = ".aiflow-worktrees"
max_parallel = 3
claude_code_path = ""  # Empty = use PATH
default_branch = "main"
context_max_files = 20
context_max_tokens = 8000
state_dir = "~/.aiflow/state"
lock_timeout = "5m"

[summaries]
include_for_dependencies = true
include_for_same_feature = true
max_summary_tokens = 1000
```

## Architecture

```
aiflow/
├── cmd/aiflow/main.go           # Entry point
├── internal/
│   ├── cli/                     # Cobra commands
│   ├── config/                  # TOML config loading
│   ├── worktree/                # Git worktree management
│   ├── breakdown/               # Feature → tasks via Claude
│   ├── context/                 # Hybrid context builder
│   ├── scheduler/               # Dependency graph + parallel batching
│   ├── executor/                # Claude Code invocation
│   ├── state/                   # Persistence + resume
│   └── tui/                     # Bubble Tea terminal UI
├── pkg/git/                     # Git operations wrapper
└── configs/default.toml         # Default config template
```

## How It Works

```
User: aiflow start "Add user authentication"
         │
         ▼
┌─────────────────────────────────┐
│ 1. Create worktree from main    │
└─────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────┐
│ 2. Interactive breakdown (TUI)  │
│    • Claude analyzes codebase   │
│    • Asks clarifying questions  │
│    • Generates task list        │
└─────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────┐
│ 3. Analyze dependencies         │
│    • File overlap detection     │
│    • Build execution batches    │
└─────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────┐
│ 4. Execute tasks                │
│    • Parallel batches           │
│    • File locking               │
│    • Hybrid context per task    │
│    • Extract summary on done    │
└─────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────┐
│ 5. Review in worktree           │
│    • User validates changes     │
│    • Merge when ready           │
└─────────────────────────────────┘
```

## Hybrid Context Management

The key innovation is **hybrid context**: each task gets fresh file context plus structured summaries from completed tasks.

```go
type TaskSummary struct {
    FilesChanged    []string  // Modified files
    FilesCreated    []string  // New files
    FunctionsAdded  []string  // "NewUser(email string) *User"
    TypesAdded      []string  // "User struct", "AuthToken struct"
    PatternsUsed    []string  // "Repository pattern"
    Decisions       []string  // "Used JWT over sessions because X"
    Conventions     []string  // "Errors wrapped with fmt.Errorf"
    Gotchas         []string  // "Email must be unique constraint"
    PublicInterface string    // Key exports for dependent tasks
}
```

This preserves cross-task awareness without token bloat.

## License

MIT
