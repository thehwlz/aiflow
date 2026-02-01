package worktree

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// Manager handles git worktree operations
type Manager struct {
	repoPath    string
	worktreeDir string
	repo        *git.Repository
}

// WorktreeInfo contains information about a worktree
type WorktreeInfo struct {
	Path      string
	Branch    string
	FeatureID string
	CreatedAt time.Time
}

// NewManager creates a new worktree manager
func NewManager(repoPath, worktreeDir string) (*Manager, error) {
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open repository: %w", err)
	}

	// Ensure worktree directory exists
	wtDir := filepath.Join(repoPath, worktreeDir)
	if err := os.MkdirAll(wtDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create worktree directory: %w", err)
	}

	return &Manager{
		repoPath:    repoPath,
		worktreeDir: wtDir,
		repo:        repo,
	}, nil
}

// slugify converts a feature description to a safe directory name
func slugify(s string) string {
	// Convert to lowercase
	s = strings.ToLower(s)
	// Replace spaces and special chars with hyphens
	reg := regexp.MustCompile(`[^a-z0-9]+`)
	s = reg.ReplaceAllString(s, "-")
	// Trim hyphens from ends
	s = strings.Trim(s, "-")
	// Limit length
	if len(s) > 50 {
		s = s[:50]
	}
	return s
}

// Create creates a new worktree for a feature by cloning the repo
func (m *Manager) Create(featureDesc, baseBranch string) (string, error) {
	slug := slugify(featureDesc)
	timestamp := time.Now().Format("20060102-150405")
	wtName := fmt.Sprintf("%s-%s", slug, timestamp)
	wtPath := filepath.Join(m.worktreeDir, wtName)

	// Get the base branch reference
	branchRef := plumbing.NewBranchReferenceName(baseBranch)
	ref, err := m.repo.Reference(branchRef, true)
	if err != nil {
		// Try remote branch
		remoteRef := plumbing.NewRemoteReferenceName("origin", baseBranch)
		ref, err = m.repo.Reference(remoteRef, true)
		if err != nil {
			return "", fmt.Errorf("branch %s not found: %w", baseBranch, err)
		}
	}

	// Create worktree directory
	if err := os.MkdirAll(wtPath, 0755); err != nil {
		return "", fmt.Errorf("failed to create worktree directory: %w", err)
	}

	// Clone the repository to the worktree path
	wtRepo, err := git.PlainClone(wtPath, false, &git.CloneOptions{
		URL:           m.repoPath,
		ReferenceName: ref.Name(),
		SingleBranch:  true,
	})
	if err != nil {
		os.RemoveAll(wtPath)
		return "", fmt.Errorf("failed to clone for worktree: %w", err)
	}

	// Create and checkout feature branch
	featureBranch := fmt.Sprintf("aiflow/%s", wtName)
	wt, err := wtRepo.Worktree()
	if err != nil {
		os.RemoveAll(wtPath)
		return "", fmt.Errorf("failed to get worktree: %w", err)
	}

	err = wt.Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(featureBranch),
		Create: true,
	})
	if err != nil {
		os.RemoveAll(wtPath)
		return "", fmt.Errorf("failed to checkout feature branch: %w", err)
	}

	return wtPath, nil
}

// List returns all aiflow worktrees
func (m *Manager) List() ([]WorktreeInfo, error) {
	entries, err := os.ReadDir(m.worktreeDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read worktree directory: %w", err)
	}

	var worktrees []WorktreeInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		wtPath := filepath.Join(m.worktreeDir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}

		// Try to get branch info
		branch := ""
		wtRepo, err := git.PlainOpen(wtPath)
		if err == nil {
			head, err := wtRepo.Head()
			if err == nil {
				branch = head.Name().Short()
			}
		}

		worktrees = append(worktrees, WorktreeInfo{
			Path:      wtPath,
			Branch:    branch,
			FeatureID: entry.Name(),
			CreatedAt: info.ModTime(),
		})
	}

	return worktrees, nil
}

// Remove removes a worktree
func (m *Manager) Remove(wtPath string) error {
	// Verify it's within our worktree directory
	if !strings.HasPrefix(wtPath, m.worktreeDir) {
		return fmt.Errorf("path is not within worktree directory")
	}

	if err := os.RemoveAll(wtPath); err != nil {
		return fmt.Errorf("failed to remove worktree: %w", err)
	}

	return nil
}

// RemoveByID removes a worktree by its feature ID
func (m *Manager) RemoveByID(featureID string) error {
	wtPath := filepath.Join(m.worktreeDir, featureID)
	return m.Remove(wtPath)
}

// Prune removes all worktrees
func (m *Manager) Prune() error {
	worktrees, err := m.List()
	if err != nil {
		return err
	}

	for _, wt := range worktrees {
		if err := m.Remove(wt.Path); err != nil {
			return err
		}
	}

	return nil
}

// GetPath returns the full path for a worktree by feature ID
func (m *Manager) GetPath(featureID string) string {
	return filepath.Join(m.worktreeDir, featureID)
}

// Exists checks if a worktree exists
func (m *Manager) Exists(featureID string) bool {
	wtPath := m.GetPath(featureID)
	_, err := os.Stat(wtPath)
	return err == nil
}

// GetWorktreeDir returns the base worktree directory
func (m *Manager) GetWorktreeDir() string {
	return m.worktreeDir
}
