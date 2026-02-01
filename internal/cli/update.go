package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var (
	updateInstall bool
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update aiflow to the latest version",
	Long: `Pull the latest aiflow source code and rebuild.

This command will:
  1. Pull the latest changes from git
  2. Show what changed
  3. Rebuild the binary
  4. Optionally install to /usr/local/bin (with --install or -i)

The source directory is determined by:
  1. The source_dir config option in ~/.aiflow/config.toml
  2. Auto-detection from the running executable

Examples:
  aiflow update           # Pull and rebuild
  aiflow update -i        # Pull, rebuild, and install to /usr/local/bin`,
	RunE: runUpdate,
}

func init() {
	updateCmd.Flags().BoolVarP(&updateInstall, "install", "i", false, "install to /usr/local/bin after building")
}

func runUpdate(cmd *cobra.Command, args []string) error {
	sourceDir, err := findSourceDir()
	if err != nil {
		return err
	}

	fmt.Printf("Source directory: %s\n\n", sourceDir)

	// Check for uncommitted changes
	statusCmd := exec.Command("git", "status", "--porcelain")
	statusCmd.Dir = sourceDir
	statusOut, _ := statusCmd.Output()
	if len(statusOut) > 0 {
		fmt.Println("Warning: You have uncommitted changes in the source directory")
		fmt.Println(string(statusOut))
	}

	// Get current commit
	currentCmd := exec.Command("git", "rev-parse", "--short", "HEAD")
	currentCmd.Dir = sourceDir
	currentOut, _ := currentCmd.Output()
	currentCommit := strings.TrimSpace(string(currentOut))

	// Fetch and check for updates
	fmt.Println("Fetching latest changes...")
	fetchCmd := exec.Command("git", "fetch")
	fetchCmd.Dir = sourceDir
	fetchCmd.Stdout = os.Stdout
	fetchCmd.Stderr = os.Stderr
	if err := fetchCmd.Run(); err != nil {
		return fmt.Errorf("git fetch failed: %w", err)
	}

	// Check if we're behind
	behindCmd := exec.Command("git", "rev-list", "--count", "HEAD..@{u}")
	behindCmd.Dir = sourceDir
	behindOut, err := behindCmd.Output()
	if err != nil {
		// Might not have upstream set
		fmt.Println("Warning: Could not check upstream status")
	} else {
		behind := strings.TrimSpace(string(behindOut))
		if behind == "0" {
			fmt.Println("\nAlready up to date!")
			return nil
		}
		fmt.Printf("\n%s commit(s) behind upstream\n", behind)
	}

	// Show what will change
	fmt.Println("\nChanges to be pulled:")
	logCmd := exec.Command("git", "log", "--oneline", "HEAD..@{u}")
	logCmd.Dir = sourceDir
	logCmd.Stdout = os.Stdout
	logCmd.Stderr = os.Stderr
	logCmd.Run()

	// Pull
	fmt.Println("\nPulling changes...")
	pullCmd := exec.Command("git", "pull", "--ff-only")
	pullCmd.Dir = sourceDir
	pullCmd.Stdout = os.Stdout
	pullCmd.Stderr = os.Stderr
	if err := pullCmd.Run(); err != nil {
		return fmt.Errorf("git pull failed: %w", err)
	}

	// Get new commit
	newCmd := exec.Command("git", "rev-parse", "--short", "HEAD")
	newCmd.Dir = sourceDir
	newOut, _ := newCmd.Output()
	newCommit := strings.TrimSpace(string(newOut))

	fmt.Printf("\nUpdated: %s -> %s\n", currentCommit, newCommit)

	// Build
	fmt.Println("\nBuilding...")
	binaryPath := filepath.Join(sourceDir, "aiflow")
	buildCmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/aiflow")
	buildCmd.Dir = sourceDir
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		return fmt.Errorf("build failed: %w", err)
	}

	fmt.Printf("Built: %s\n", binaryPath)

	// Install if requested
	if updateInstall {
		installPath := "/usr/local/bin/aiflow"
		fmt.Printf("\nInstalling to %s...\n", installPath)

		// Use cp with sudo
		cpCmd := exec.Command("sudo", "cp", binaryPath, installPath)
		cpCmd.Stdout = os.Stdout
		cpCmd.Stderr = os.Stderr
		cpCmd.Stdin = os.Stdin
		if err := cpCmd.Run(); err != nil {
			return fmt.Errorf("install failed: %w", err)
		}
		fmt.Println("Installed successfully!")
	} else {
		// Check if running from a different location
		execPath, _ := os.Executable()
		execPath, _ = filepath.EvalSymlinks(execPath)
		if execPath != "" && execPath != binaryPath {
			fmt.Printf("\nNote: You're running aiflow from %s\n", execPath)
			fmt.Println("Run 'aiflow update -i' to install the new version globally")
		}
	}

	fmt.Println("\nUpdate complete!")
	return nil
}

func findSourceDir() (string, error) {
	// First, check config
	if cfg != nil && cfg.SourceDir != "" {
		sourceDir := cfg.SourceDir
		// Expand ~
		if strings.HasPrefix(sourceDir, "~") {
			homeDir, _ := os.UserHomeDir()
			sourceDir = filepath.Join(homeDir, sourceDir[1:])
		}
		if isAiflowRepo(sourceDir) {
			return sourceDir, nil
		}
		return "", fmt.Errorf("configured source_dir %s is not a valid aiflow repository", cfg.SourceDir)
	}

	// Try to find from executable path
	execPath, err := os.Executable()
	if err == nil {
		execPath, _ = filepath.EvalSymlinks(execPath)
		execDir := filepath.Dir(execPath)

		// Check if executable is in the source dir
		if isAiflowRepo(execDir) {
			return execDir, nil
		}

		// Check parent (in case binary is in cmd/aiflow)
		parentDir := filepath.Dir(execDir)
		if isAiflowRepo(parentDir) {
			return parentDir, nil
		}
	}

	// Check common locations
	homeDir, _ := os.UserHomeDir()
	commonPaths := []string{
		filepath.Join(homeDir, "side-projects", "aiflow"),
		filepath.Join(homeDir, "projects", "aiflow"),
		filepath.Join(homeDir, "src", "aiflow"),
		filepath.Join(homeDir, "code", "aiflow"),
		filepath.Join(homeDir, "aiflow"),
	}

	for _, p := range commonPaths {
		if isAiflowRepo(p) {
			return p, nil
		}
	}

	return "", fmt.Errorf(`could not find aiflow source directory

Please set source_dir in ~/.aiflow/config.toml:
  source_dir = "/path/to/aiflow"`)
}

func isAiflowRepo(dir string) bool {
	// Check if it's a git repo with aiflow structure
	gitDir := filepath.Join(dir, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		return false
	}

	mainGo := filepath.Join(dir, "cmd", "aiflow", "main.go")
	if _, err := os.Stat(mainGo); os.IsNotExist(err) {
		return false
	}

	return true
}
