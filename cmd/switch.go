package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/bshakr/koh/internal/config"
	"github.com/bshakr/koh/internal/git"
	"github.com/bshakr/koh/internal/tmux"
	"github.com/bshakr/koh/internal/validation"
	"github.com/spf13/cobra"
)

var switchCmd = &cobra.Command{
	Use:   "switch <worktree-name>",
	Short: "Switch to an existing worktree's tmux session",
	Long: `Switch to an existing worktree's tmux window.

If the window no longer exists (e.g. after a tmux restart), it is
recreated with the panes configured in your .kohconfig.

Must be run from inside a tmux session.`,
	Args: cobra.ExactArgs(1),
	RunE: runSwitch,
}

func init() {
	rootCmd.AddCommand(switchCmd)
}

// switchToWorktree contains the core logic for switching to a worktree's tmux session.
// This function is used by both the 'switch' command and the interactive 'list' command.
func switchToWorktree(worktreeName string, quiet bool) error {
	// Validate worktree name for security
	if err := validation.ValidateWorktreeName(worktreeName); err != nil {
		return fmt.Errorf("invalid worktree name: %w", err)
	}

	// Check if we're in a tmux session
	if !tmux.IsInTmux() {
		return fmt.Errorf("not in a tmux session\nPlease run this command from within a tmux session")
	}

	// Check if we're in a git repository
	if !git.IsGitRepo() {
		return fmt.Errorf("not in a git repository\nPlease run this command from within a git repository")
	}

	// Determine the main repo root
	mainRepoRoot, err := git.GetMainRepoRootOrCwd()
	if err != nil {
		return fmt.Errorf("failed to get repository root: %w", err)
	}

	// Check if worktree exists
	worktreePath := filepath.Join(mainRepoRoot, ".koh", worktreeName)
	if _, err := os.Stat(worktreePath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("worktree .koh/%s does not exist\nUse 'koh new %s' to create it", worktreeName, worktreeName)
		}
		return fmt.Errorf("failed to check worktree path: %w", err)
	}

	// Check if tmux window already exists
	exists, err := tmux.WindowExists(worktreeName)
	if err != nil {
		return fmt.Errorf("failed to check for existing tmux window: %w", err)
	}

	if exists {
		// Window exists, just switch to it
		if !quiet {
			fmt.Printf("Switching to existing session: .koh/%s\n", worktreeName)
		}
		if err := tmux.SwitchToWindow(worktreeName); err != nil {
			return fmt.Errorf("failed to switch to tmux window: %w", err)
		}
		return nil
	}

	// Window doesn't exist, create it
	if !quiet {
		fmt.Printf("Creating new tmux session for existing worktree: .koh/%s\n", worktreeName)
	}

	// Check if config exists
	exists, err = config.ConfigExists()
	if err != nil {
		return fmt.Errorf("failed to check for .kohconfig: %w", err)
	}
	if !exists {
		return fmt.Errorf("no .kohconfig found\nPlease run 'koh init' to set up your configuration first")
	}

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Set up context with cancellation for long-running operations
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Only set up signal handler when not in quiet mode
	// In quiet mode (called from TUI), the caller handles interrupts
	if !quiet {
		// Handle interrupt signals (Ctrl+C)
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

		// Signal handler goroutine
		done := make(chan struct{})
		go func() {
			defer close(done)
			<-sigChan
			fmt.Println("\nOperation cancelled by user")
			cancel()
		}()
		defer func() {
			signal.Stop(sigChan)
			<-done // Wait for signal handler to finish
		}()
	}

	// Get repository name
	repoName, err := git.GetRepoName()
	if err != nil {
		return fmt.Errorf("failed to get repository name: %w", err)
	}

	// Create tmux session with config and context
	if err := tmux.CreateSessionWithContext(ctx, repoName, worktreeName, worktreePath, cfg); err != nil {
		return fmt.Errorf("failed to create tmux session: %w", err)
	}

	if !quiet {
		fmt.Println("Session created successfully!")
	}
	return nil
}

func runSwitch(_ *cobra.Command, args []string) error {
	worktreeName := args[0]
	return switchToWorktree(worktreeName, false)
}
