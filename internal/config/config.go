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
	"bufio"
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

	// Get the main repo root (handles both main repo and worktrees)
	var repoRoot string
	var err error

	if git.IsInWorktree() {
		repoRoot, err = git.GetMainRepoRoot()
		if err != nil {
			return "", fmt.Errorf("failed to get main repository root: %w", err)
		}
	} else {
		repoRoot, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("failed to get current directory: %w", err)
		}
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

// Setup runs an interactive setup to create a .kohconfig file
// Note: This is a simple fallback. Use 'koh init' for the full interactive wizard.
func Setup() error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("Koh Configuration Setup")
	fmt.Println("=======================")
	fmt.Println()

	config := DefaultConfig()

	// Ask for setup script
	fmt.Printf("Setup script (default: %s): ", config.SetupScript)
	setupScript, _ := reader.ReadString('\n')
	setupScript = strings.TrimSpace(setupScript)
	if setupScript != "" {
		config.SetupScript = setupScript
	}

	// Ask for pane commands
	fmt.Println()
	fmt.Println("Pane commands (one per line, empty line to finish):")
	var paneCommands []string
	for {
		fmt.Print("> ")
		cmd, _ := reader.ReadString('\n')
		cmd = strings.TrimSpace(cmd)
		if cmd == "" {
			break
		}
		paneCommands = append(paneCommands, cmd)
	}
	if len(paneCommands) > 0 {
		config.PaneCommands = paneCommands
	}

	// Save the configuration
	if err := config.Save(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	configPath, _ := ConfigPath()
	fmt.Println()
	fmt.Printf("Configuration saved to: %s\n", configPath)

	return nil
}
