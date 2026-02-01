package config

import (
	"os"
	"path/filepath"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// Config holds all aiflow configuration
type Config struct {
	WorktreeDir      string        `toml:"worktree_dir"`
	MaxParallel      int           `toml:"max_parallel"`
	ClaudeCodePath   string        `toml:"claude_code_path"`
	DefaultBranch    string        `toml:"default_branch"`
	ContextMaxFiles  int           `toml:"context_max_files"`
	ContextMaxTokens int           `toml:"context_max_tokens"`
	StateDir         string        `toml:"state_dir"`
	LockTimeout      string        `toml:"lock_timeout"`
	SourceDir        string        `toml:"source_dir"` // aiflow source directory for self-update
	Summaries        SummaryConfig `toml:"summaries"`
	Spec             SpecConfig    `toml:"spec"`
}

// SummaryConfig holds settings for task summary inclusion
type SummaryConfig struct {
	IncludeForDependencies bool `toml:"include_for_dependencies"`
	IncludeForSameFeature  bool `toml:"include_for_same_feature"`
	MaxSummaryTokens       int  `toml:"max_summary_tokens"`
}

// SpecConfig holds settings for the adaptive specification flow
type SpecConfig struct {
	SafetyLimit int `toml:"safety_limit"` // Max questions before forcing breakdown (safety valve)
}

// Default returns the default configuration
func Default() *Config {
	homeDir, _ := os.UserHomeDir()
	return &Config{
		WorktreeDir:      ".aiflow-worktrees",
		MaxParallel:      3,
		ClaudeCodePath:   "", // Use PATH
		DefaultBranch:    "main",
		ContextMaxFiles:  20,
		ContextMaxTokens: 8000,
		StateDir:         filepath.Join(homeDir, ".aiflow", "state"),
		LockTimeout:      "5m",
		Summaries: SummaryConfig{
			IncludeForDependencies: true,
			IncludeForSameFeature:  true,
			MaxSummaryTokens:       1000,
		},
		Spec: SpecConfig{
			SafetyLimit: 10,
		},
	}
}

// LockTimeoutDuration returns the lock timeout as a duration
func (c *Config) LockTimeoutDuration() time.Duration {
	d, err := time.ParseDuration(c.LockTimeout)
	if err != nil {
		return 5 * time.Minute
	}
	return d
}

// Load reads configuration from the config file
func Load() (*Config, error) {
	cfg := Default()

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return cfg, nil
	}

	configPath := filepath.Join(homeDir, ".aiflow", "config.toml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}

	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	// Expand ~ in StateDir
	if len(cfg.StateDir) > 0 && cfg.StateDir[0] == '~' {
		cfg.StateDir = filepath.Join(homeDir, cfg.StateDir[1:])
	}

	return cfg, nil
}

// Save writes configuration to the config file
func (c *Config) Save() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	configDir := filepath.Join(homeDir, ".aiflow")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return err
	}

	configPath := filepath.Join(configDir, "config.toml")
	data, err := toml.Marshal(c)
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0644)
}

// ConfigDir returns the aiflow config directory path
func ConfigDir() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".aiflow")
}
