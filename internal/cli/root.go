package cli

import (
	"fmt"

	"github.com/howell-aikit/aiflow/internal/config"
	"github.com/spf13/cobra"
)

var (
	cfgFile string
	cfg     *config.Config
)

var rootCmd = &cobra.Command{
	Use:   "aiflow",
	Short: "AI Agent Orchestrator for Feature Implementation",
	Long: `aiflow orchestrates Claude Code to implement features through:
  • Isolated git worktrees per feature run
  • Interactive feature breakdown into tasks
  • Hybrid context management with structured summaries
  • Parallel execution of independent tasks with file locking`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		var err error
		cfg, err = config.Load()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
		return nil
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default ~/.aiflow/config.toml)")

	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(cleanCmd)
	rootCmd.AddCommand(resumeCmd)
	rootCmd.AddCommand(updateCmd)
}

// Execute runs the root command
func Execute() error {
	return rootCmd.Execute()
}

// GetConfig returns the loaded configuration
func GetConfig() *config.Config {
	return cfg
}
