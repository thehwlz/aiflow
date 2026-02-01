package git

import (
	"fmt"
	"os"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// Repository wraps go-git operations
type Repository struct {
	repo *git.Repository
	path string
}

// Open opens a git repository at the given path
func Open(path string) (*Repository, error) {
	repo, err := git.PlainOpen(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open repository: %w", err)
	}
	return &Repository{repo: repo, path: path}, nil
}

// Path returns the repository path
func (r *Repository) Path() string {
	return r.path
}

// CurrentBranch returns the current branch name
func (r *Repository) CurrentBranch() (string, error) {
	head, err := r.repo.Head()
	if err != nil {
		return "", fmt.Errorf("failed to get HEAD: %w", err)
	}
	return head.Name().Short(), nil
}

// HasBranch checks if a branch exists
func (r *Repository) HasBranch(name string) bool {
	ref := plumbing.NewBranchReferenceName(name)
	_, err := r.repo.Reference(ref, true)
	if err == nil {
		return true
	}

	// Check remote branches
	remoteRef := plumbing.NewRemoteReferenceName("origin", name)
	_, err = r.repo.Reference(remoteRef, true)
	return err == nil
}

// GetDefaultBranch returns the default branch (main or master)
func (r *Repository) GetDefaultBranch() string {
	if r.HasBranch("main") {
		return "main"
	}
	if r.HasBranch("master") {
		return "master"
	}
	// Fallback to current branch
	branch, err := r.CurrentBranch()
	if err != nil {
		return "main"
	}
	return branch
}

// IsDirty returns true if there are uncommitted changes
func (r *Repository) IsDirty() (bool, error) {
	wt, err := r.repo.Worktree()
	if err != nil {
		return false, fmt.Errorf("failed to get worktree: %w", err)
	}

	status, err := wt.Status()
	if err != nil {
		return false, fmt.Errorf("failed to get status: %w", err)
	}

	return !status.IsClean(), nil
}

// GetCommitHash returns the current commit hash
func (r *Repository) GetCommitHash() (string, error) {
	head, err := r.repo.Head()
	if err != nil {
		return "", fmt.Errorf("failed to get HEAD: %w", err)
	}
	return head.Hash().String(), nil
}

// GetCommitMessage returns the current commit message
func (r *Repository) GetCommitMessage() (string, error) {
	head, err := r.repo.Head()
	if err != nil {
		return "", fmt.Errorf("failed to get HEAD: %w", err)
	}

	commit, err := r.repo.CommitObject(head.Hash())
	if err != nil {
		return "", fmt.Errorf("failed to get commit: %w", err)
	}

	return commit.Message, nil
}

// ListFiles returns all tracked files in the repository
func (r *Repository) ListFiles() ([]string, error) {
	head, err := r.repo.Head()
	if err != nil {
		return nil, fmt.Errorf("failed to get HEAD: %w", err)
	}

	commit, err := r.repo.CommitObject(head.Hash())
	if err != nil {
		return nil, fmt.Errorf("failed to get commit: %w", err)
	}

	tree, err := commit.Tree()
	if err != nil {
		return nil, fmt.Errorf("failed to get tree: %w", err)
	}

	var files []string
	err = tree.Files().ForEach(func(f *object.File) error {
		files = append(files, f.Name)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list files: %w", err)
	}

	return files, nil
}

// IsGitRepo checks if the given path is a git repository
func IsGitRepo(path string) bool {
	_, err := git.PlainOpen(path)
	return err == nil
}

// FindRepoRoot finds the root of the git repository from the given path
func FindRepoRoot(startPath string) (string, error) {
	path := startPath
	for {
		if IsGitRepo(path) {
			return path, nil
		}

		parent := path[:len(path)-len(path[len(path)-1:])]
		if parent == path || parent == "" || parent == "/" {
			return "", fmt.Errorf("not in a git repository")
		}
		path = parent
	}
}

// FindRepoRootFromCwd finds the repo root from current working directory
func FindRepoRootFromCwd() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}

	// Walk up looking for .git
	path := cwd
	for {
		gitPath := path + "/.git"
		if _, err := os.Stat(gitPath); err == nil {
			return path, nil
		}

		parent := path[:len(path)-len(path[len(path)-1:])-1]
		if parent == path || parent == "" {
			return "", fmt.Errorf("not in a git repository")
		}

		// Handle root directory
		if parent == "/" || (len(parent) == 2 && parent[1] == ':') {
			if _, err := os.Stat(parent + "/.git"); err == nil {
				return parent, nil
			}
			return "", fmt.Errorf("not in a git repository")
		}

		path = parent
	}
}
