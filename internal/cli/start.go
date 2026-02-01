package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/howell-aikit/aiflow/internal/state"
	"github.com/howell-aikit/aiflow/internal/tui"
	"github.com/howell-aikit/aiflow/internal/worktree"
	"github.com/howell-aikit/aiflow/pkg/git"
	"github.com/spf13/cobra"
)

var (
	baseBranch string
	noWorktree bool
)

var startCmd = &cobra.Command{
	Use:   "start [feature-description]",
	Short: "Start a new feature implementation",
	Long: `Start a new feature implementation with aiflow.

This command will:
1. Ask what you want to build (or use provided description)
2. Detect if this is a new or existing project
3. Run an adaptive Q&A session to gather requirements
4. Break down the feature into implementable tasks
5. Execute tasks with hybrid context management
6. Allow you to review and merge changes when complete`,
	Args: cobra.MaximumNArgs(1),
	RunE: runStart,
}

func init() {
	startCmd.Flags().StringVarP(&baseBranch, "branch", "b", "", "base branch (default: from config)")
	startCmd.Flags().BoolVar(&noWorktree, "no-worktree", false, "run in current directory without creating a worktree")
}

func runStart(cmd *cobra.Command, args []string) error {
	// Feature description is optional - TUI will ask if not provided
	var featureDesc string
	if len(args) > 0 {
		featureDesc = args[0]
	}

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

	// Detect project type
	projectType := DetectProjectType(repoPath)

	// Initialize state store
	store, err := state.NewStore(cfg.StateDir)
	if err != nil {
		return fmt.Errorf("failed to initialize state: %w", err)
	}

	var workingDir string

	if noWorktree {
		// Use current directory
		workingDir = repoPath
	} else {
		// Create worktree (use placeholder name if no feature desc yet)
		wtManager, err := worktree.NewManager(repoPath, cfg.WorktreeDir)
		if err != nil {
			return fmt.Errorf("failed to initialize worktree manager: %w", err)
		}

		wtName := featureDesc
		if wtName == "" {
			wtName = "new-feature"
		}

		workingDir, err = wtManager.Create(wtName, branch)
		if err != nil {
			return fmt.Errorf("failed to create worktree: %w", err)
		}
	}

	// Create run with optional feature description
	run, err := store.CreateRun(featureDesc, workingDir, branch)
	if err != nil {
		return fmt.Errorf("failed to create run: %w", err)
	}

	// Set project type
	run.ProjectType = string(projectType)

	// Initialize spec conversation
	run.SpecConversation = &state.SpecConversation{
		Turns:    []state.SpecTurn{},
		MaxTurns: cfg.Spec.SafetyLimit,
	}

	// Save run with updated fields
	if err := store.SaveRun(run); err != nil {
		return fmt.Errorf("failed to save run: %w", err)
	}

	// Launch TUI for interactive breakdown
	return tui.Run(cfg, run, store)
}

// DetectProjectType checks if the directory contains code files
func DetectProjectType(workDir string) string {
	// Code file extensions to look for
	codeExtensions := []string{
		".go", ".py", ".js", ".ts", ".jsx", ".tsx",
		".java", ".rb", ".rs", ".c", ".cpp", ".h",
		".cs", ".php", ".swift", ".kt", ".scala",
	}

	// Project files that indicate an existing project
	projectFiles := []string{
		"go.mod", "go.sum",
		"package.json", "package-lock.json", "yarn.lock", "pnpm-lock.yaml",
		"Cargo.toml", "Cargo.lock",
		"requirements.txt", "setup.py", "pyproject.toml", "Pipfile",
		"Gemfile", "Gemfile.lock",
		"pom.xml", "build.gradle", "build.gradle.kts",
		"Makefile", "CMakeLists.txt",
		"composer.json",
	}

	// Check for project files first
	for _, pf := range projectFiles {
		if _, err := os.Stat(filepath.Join(workDir, pf)); err == nil {
			return "existing"
		}
	}

	// Walk directory looking for code files (limit depth to avoid slow scans)
	hasCodeFiles := false
	maxDepth := 3
	baseDepth := len(filepath.SplitList(workDir))

	filepath.Walk(workDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		// Skip hidden directories and common non-code dirs
		if info.IsDir() {
			name := info.Name()
			if name[0] == '.' || name == "node_modules" || name == "vendor" || name == "__pycache__" {
				return filepath.SkipDir
			}
			// Limit depth
			depth := len(filepath.SplitList(path)) - baseDepth
			if depth > maxDepth {
				return filepath.SkipDir
			}
			return nil
		}

		// Check file extension
		ext := filepath.Ext(path)
		for _, codeExt := range codeExtensions {
			if ext == codeExt {
				hasCodeFiles = true
				return filepath.SkipAll // Found one, stop searching
			}
		}
		return nil
	})

	if hasCodeFiles {
		return "existing"
	}
	return "empty"
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
