// Package config handles koh configuration file management.
//
// Configuration is stored in a .kohconfig file at the repository root.
// The configuration includes:
//   - setup_script: Path to a script that runs when creating a worktree
//   - pane_commands: Commands to run in additional tmux panes
//
// The configuration file is JSON-formatted and can be created interactively
// using the 'koh init' command or edited manually.
//
// Example .kohconfig:
//
//	{
//	  "setup_script": "./bin/setup",
//	  "pane_commands": [
//	    "vim",
//	    "npm run dev"
//	  ]
//	}
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bshakr/koh/internal/git"
)

// Config represents the koh configuration
type Config struct {
	SetupScript  string   `json:"setup_script"`
	PaneCommands []string `json:"pane_commands"`
}

// DefaultConfig returns a configuration with default values
func DefaultConfig() *Config {
	return &Config{
		SetupScript:  "./bin/setup",
		PaneCommands: []string{},
	}
}

// ConfigPath returns the path to the .kohconfig file in the repo root
//
//nolint:revive // config.ConfigPath() is clear and explicit
func ConfigPath() (string, error) {
	// Check if we're in a git repository
	if !git.IsGitRepo() {
		return "", fmt.Errorf("not in a git repository")
	}

	// Resolve via git-common-dir so the path is correct from the main repo
	// root, any subdirectory of it, or inside a worktree. The previous code
	// fell back to os.Getwd() outside a worktree, which pointed .kohconfig
	// at whatever subdirectory the command happened to run from.
	repoRoot, err := git.GetMainRepoRoot()
	if err != nil {
		return "", fmt.Errorf("failed to get main repository root: %w", err)
	}

	return filepath.Join(repoRoot, ".kohconfig"), nil
}

// ConfigExists checks if a .kohconfig file exists in the repo
//
//nolint:revive // config.ConfigExists() is clear and explicit
func ConfigExists() (bool, error) {
	configPath, err := ConfigPath()
	if err != nil {
		return false, err
	}

	_, err = os.Stat(configPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return true, nil
}

// Load loads the configuration from disk
func Load() (*Config, error) {
	configPath, err := ConfigPath()
	if err != nil {
		return nil, err
	}

	// If config doesn't exist, return an error
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("no .kohconfig found - run 'koh init' to set up configuration")
	}

	//nolint:gosec // G304: Reading config file from validated path is expected
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// A bare JSON `null` unmarshals without error but leaves a zero-value
	// config, which would silently mask a corrupt or empty config file.
	// Treat it as a parse error so the problem surfaces clearly.
	if strings.TrimSpace(string(data)) == "null" {
		return nil, fmt.Errorf("failed to parse config file: config is null")
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &config, nil
}

// Save saves the configuration to disk
func (c *Config) Save() error {
	configPath, err := ConfigPath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}
