package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/howell-aikit/aiflow/internal/state"
	"github.com/howell-aikit/aiflow/internal/worktree"
	"github.com/howell-aikit/aiflow/pkg/git"
	"github.com/spf13/cobra"
)

var (
	baseBranch string
	noWorktree bool
)

var startCmd = &cobra.Command{
	Use:   "start <feature-description>",
	Short: "Start a new feature implementation",
	Long: `Start a new feature implementation with aiflow.

This command will:
1. Create an isolated git worktree for the feature
2. Run an interactive breakdown session to decompose the feature into tasks
3. Execute tasks with hybrid context management
4. Allow you to review and merge changes when complete`,
	Args: cobra.ExactArgs(1),
	RunE: runStart,
}

func init() {
	startCmd.Flags().StringVarP(&baseBranch, "branch", "b", "", "base branch (default: from config)")
	startCmd.Flags().BoolVar(&noWorktree, "no-worktree", false, "run in current directory without creating a worktree")
}

func runStart(cmd *cobra.Command, args []string) error {
	featureDesc := args[0]

	// Find repo root
	repoPath, err := git.FindRepoRootFromCwd()
	if err != nil {
		return fmt.Errorf("must be in a git repository: %w", err)
	}

	// Open repository
	repo, err := git.Open(repoPath)
	if err != nil {
		return fmt.Errorf("failed to open repository: %w", err)
	}

	// Check for dirty state
	dirty, err := repo.IsDirty()
	if err != nil {
		return fmt.Errorf("failed to check repository status: %w", err)
	}
	if dirty && !noWorktree {
		return fmt.Errorf("repository has uncommitted changes; commit or stash them first")
	}

	// Determine base branch
	branch := baseBranch
	if branch == "" {
		branch = cfg.DefaultBranch
		if branch == "" {
			branch = repo.GetDefaultBranch()
		}
	}

	// Verify branch exists
	if !repo.HasBranch(branch) {
		return fmt.Errorf("branch %q does not exist", branch)
	}

	// Initialize state store
	store, err := state.NewStore(cfg.StateDir)
	if err != nil {
		return fmt.Errorf("failed to initialize state: %w", err)
	}

	var workingDir string

	if noWorktree {
		// Use current directory
		workingDir = repoPath
		fmt.Printf("Using current directory: %s\n", workingDir)
	} else {
		// Create worktree
		wtManager, err := worktree.NewManager(repoPath, cfg.WorktreeDir)
		if err != nil {
			return fmt.Errorf("failed to initialize worktree manager: %w", err)
		}

		fmt.Printf("Creating worktree from %s...\n", branch)
		workingDir, err = wtManager.Create(featureDesc, branch)
		if err != nil {
			return fmt.Errorf("failed to create worktree: %w", err)
		}
		fmt.Printf("Created worktree: %s\n", workingDir)
	}

	// Create run
	run, err := store.CreateRun(featureDesc, workingDir, branch)
	if err != nil {
		return fmt.Errorf("failed to create run: %w", err)
	}

	fmt.Printf("\nStarted run: %s\n", run.ID)
	fmt.Printf("Feature: %s\n", featureDesc)
	fmt.Printf("Working directory: %s\n", workingDir)
	fmt.Printf("\nNext steps:\n")
	fmt.Printf("  1. The TUI will guide you through feature breakdown\n")
	fmt.Printf("  2. Tasks will be executed with hybrid context\n")
	fmt.Printf("  3. Review changes and merge when ready\n")
	fmt.Printf("\nRun 'aiflow status %s' to check progress\n", run.ID)

	// TODO: Launch TUI for breakdown
	// For now, just return success
	return nil
}

// EnsureStateDir ensures the state directory exists
func EnsureStateDir() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	stateDir := filepath.Join(homeDir, ".aiflow", "state", "runs")
	return os.MkdirAll(stateDir, 0755)
}
