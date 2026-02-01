package scheduler

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gofrs/flock"
)

const lockSuffix = ".aiflow.lock"

// FileLock manages file locking for parallel execution
type FileLock struct {
	workDir     string
	timeout     time.Duration
	locks       map[string]*flock.Flock
	mu          sync.Mutex
}

// NewFileLock creates a new file lock manager
func NewFileLock(workDir string, timeout time.Duration) *FileLock {
	return &FileLock{
		workDir: workDir,
		timeout: timeout,
		locks:   make(map[string]*flock.Flock),
	}
}

// LockFiles acquires locks for the given files
func (fl *FileLock) LockFiles(files []string) error {
	fl.mu.Lock()
	defer fl.mu.Unlock()

	var acquired []string

	for _, file := range files {
		lockPath := fl.lockPath(file)

		// Ensure lock directory exists
		lockDir := filepath.Dir(lockPath)
		if err := os.MkdirAll(lockDir, 0755); err != nil {
			fl.unlockFilesLocked(acquired)
			return fmt.Errorf("failed to create lock directory: %w", err)
		}

		lock := flock.New(lockPath)
		ctx, cancel := contextWithTimeout(fl.timeout)
		defer cancel()

		locked, err := lock.TryLockContext(ctx, 100*time.Millisecond)
		if err != nil {
			fl.unlockFilesLocked(acquired)
			return fmt.Errorf("failed to acquire lock for %s: %w", file, err)
		}
		if !locked {
			fl.unlockFilesLocked(acquired)
			return fmt.Errorf("timeout waiting for lock on %s", file)
		}

		fl.locks[file] = lock
		acquired = append(acquired, file)
	}

	return nil
}

// UnlockFiles releases locks for the given files
func (fl *FileLock) UnlockFiles(files []string) error {
	fl.mu.Lock()
	defer fl.mu.Unlock()

	return fl.unlockFilesLocked(files)
}

// unlockFilesLocked releases locks (must hold fl.mu)
func (fl *FileLock) unlockFilesLocked(files []string) error {
	var lastErr error

	for _, file := range files {
		lock, ok := fl.locks[file]
		if !ok {
			continue
		}

		if err := lock.Unlock(); err != nil {
			lastErr = err
		}

		// Remove lock file
		lockPath := fl.lockPath(file)
		os.Remove(lockPath)

		delete(fl.locks, file)
	}

	return lastErr
}

// UnlockAll releases all held locks
func (fl *FileLock) UnlockAll() error {
	fl.mu.Lock()
	defer fl.mu.Unlock()

	var files []string
	for file := range fl.locks {
		files = append(files, file)
	}

	return fl.unlockFilesLocked(files)
}

// IsLocked checks if a file is currently locked
func (fl *FileLock) IsLocked(file string) bool {
	fl.mu.Lock()
	defer fl.mu.Unlock()

	_, ok := fl.locks[file]
	return ok
}

// TryLockFiles attempts to lock files without blocking
func (fl *FileLock) TryLockFiles(files []string) (bool, error) {
	fl.mu.Lock()
	defer fl.mu.Unlock()

	var acquired []string

	for _, file := range files {
		lockPath := fl.lockPath(file)

		// Ensure lock directory exists
		lockDir := filepath.Dir(lockPath)
		if err := os.MkdirAll(lockDir, 0755); err != nil {
			fl.unlockFilesLocked(acquired)
			return false, fmt.Errorf("failed to create lock directory: %w", err)
		}

		lock := flock.New(lockPath)
		locked, err := lock.TryLock()
		if err != nil {
			fl.unlockFilesLocked(acquired)
			return false, fmt.Errorf("failed to try lock for %s: %w", file, err)
		}
		if !locked {
			fl.unlockFilesLocked(acquired)
			return false, nil
		}

		fl.locks[file] = lock
		acquired = append(acquired, file)
	}

	return true, nil
}

// lockPath returns the lock file path for a given file
func (fl *FileLock) lockPath(file string) string {
	// Store locks in a .aiflow-locks directory
	lockDir := filepath.Join(fl.workDir, ".aiflow-locks")
	// Use the file path as the lock name (replace path separators)
	lockName := file + lockSuffix
	return filepath.Join(lockDir, lockName)
}

// CleanupStaleLocks removes any stale lock files
func (fl *FileLock) CleanupStaleLocks() error {
	lockDir := filepath.Join(fl.workDir, ".aiflow-locks")

	entries, err := os.ReadDir(lockDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		lockPath := filepath.Join(lockDir, entry.Name())
		lock := flock.New(lockPath)

		// Try to acquire lock - if successful, it's stale
		locked, err := lock.TryLock()
		if err != nil {
			continue
		}
		if locked {
			lock.Unlock()
			os.Remove(lockPath)
		}
	}

	return nil
}

// LockSet represents a set of locks for a task
type LockSet struct {
	fl    *FileLock
	files []string
}

// AcquireLockSet acquires locks for all task files
func (fl *FileLock) AcquireLockSet(writeFiles, createFiles []string) (*LockSet, error) {
	allFiles := append(writeFiles, createFiles...)
	if len(allFiles) == 0 {
		return &LockSet{fl: fl, files: nil}, nil
	}

	if err := fl.LockFiles(allFiles); err != nil {
		return nil, err
	}

	return &LockSet{fl: fl, files: allFiles}, nil
}

// Release releases all locks in the set
func (ls *LockSet) Release() error {
	if ls == nil || len(ls.files) == 0 {
		return nil
	}
	return ls.fl.UnlockFiles(ls.files)
}

// contextWithTimeout creates a context that times out
// Simple implementation since we don't want to import context package
type timeoutContext struct {
	deadline time.Time
}

func contextWithTimeout(timeout time.Duration) (*timeoutContext, func()) {
	ctx := &timeoutContext{deadline: time.Now().Add(timeout)}
	return ctx, func() {}
}

func (c *timeoutContext) Done() <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		time.Sleep(time.Until(c.deadline))
		close(ch)
	}()
	return ch
}

func (c *timeoutContext) Err() error {
	if time.Now().After(c.deadline) {
		return fmt.Errorf("context deadline exceeded")
	}
	return nil
}

func (c *timeoutContext) Deadline() (time.Time, bool) {
	return c.deadline, true
}

func (c *timeoutContext) Value(key interface{}) interface{} {
	return nil
}
