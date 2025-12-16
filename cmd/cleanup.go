package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/bshakr/koh/internal/git"
	"github.com/bshakr/koh/internal/signals"
	"github.com/bshakr/koh/internal/tmux"
	"github.com/bshakr/koh/internal/validation"
	"github.com/spf13/cobra"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup [worktree-name]",
	Short: "Close tmux session and remove worktree",
	Long: `Closes the associated tmux window and removes the git worktree.

If no worktree name is provided and you're currently in a worktree,
it will automatically clean up the current worktree.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runCleanup,
}

func init() {
	rootCmd.AddCommand(cleanupCmd)
}

func extractWorkTreeName() (string, error) {
	if !git.IsInWorktree() {
		return "", fmt.Errorf("not in a worktree")
	}

	currentPath, err := git.GetCurrentWorktreePath()
	if err != nil {
		return "", fmt.Errorf("failed to get current worktree path: %w", err)
	}

	return filepath.Base(currentPath), nil
}

func runCleanup(_ *cobra.Command, args []string) error {
	// Windows is not supported due to differences in process management
	if runtime.GOOS == "windows" {
		return fmt.Errorf("cleanup command is not supported on Windows")
	}

	var worktreeName string

	// If no argument provided, try to detect current worktree
	if len(args) == 0 {
		var err error
		worktreeName, err = extractWorkTreeName()
		if err != nil {
			return fmt.Errorf("failed to extract worktree name: %w", err)
		}
		fmt.Printf("Detected current worktree: %s\n", worktreeName)
	} else {
		worktreeName = args[0]
	}

	// Validate worktree name for security
	if err := validation.ValidateWorktreeName(worktreeName); err != nil {
		return fmt.Errorf("invalid worktree name: %w", err)
	}

	// Set up context with cancellation for long-running operations and signal handling
	ctx, cleanup := signals.SetupCancellableContext()
	defer cleanup()

	// Get main repository root
	mainRepoRoot, err := git.GetMainRepoRoot()
	if err != nil {
		return fmt.Errorf("failed to get main repository root: %w", err)
	}

	// Build worktree path
	worktreePath := filepath.Join(mainRepoRoot, ".koh", worktreeName)

	// Check if worktree exists
	worktreeExists := true
	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		fmt.Printf("Warning: Worktree .koh/%s not found\n", worktreeName)
		fmt.Println("Will attempt to clean up tmux window only")
		worktreeExists = false
	}

	// Get current directory
	currentDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	// Check if we're running from within the worktree being cleaned up
	currentPath, err := filepath.Abs(currentDir)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}
	absWorktreePath, err := filepath.Abs(worktreePath)
	if err != nil {
		return fmt.Errorf("failed to get absolute worktree path: %w", err)
	}

	// Use filepath.Rel for robust path comparison
	rel, err := filepath.Rel(absWorktreePath, currentPath)
	isInTargetWorktree := err == nil && !strings.HasPrefix(rel, "..")

	// Step 1: If in target worktree, switch to parent directory
	if isInTargetWorktree {
		fmt.Println("Running from within target worktree, switching to parent repository...")
		if err := os.Chdir(mainRepoRoot); err != nil {
			return fmt.Errorf("failed to change to parent directory: %w", err)
		}
		fmt.Printf("Changed directory to: %s\n", mainRepoRoot)
	}

	// Step 2: Remove the git worktree
	if worktreeExists {
		fmt.Printf("Removing git worktree: .koh/%s\n", worktreeName)
		if err := git.RemoveWorktreeWithContext(ctx, worktreePath); err != nil {
			fmt.Printf("Warning: Failed to remove worktree: %v\n", err)
		} else {
			fmt.Println("Worktree removed successfully")
		}

		// Clean up any remaining directory (git worktree remove may leave empty dirs)
		if err := os.RemoveAll(worktreePath); err != nil {
			fmt.Printf("Warning: Failed to remove worktree directory: %v\n", err)
		}
	}

	// Step 3: Close tmux window (tmux will automatically switch to previous window)
	if tmux.IsInTmux() {
		repoName, err := git.GetRepoName()
		if err != nil {
			fmt.Printf("Warning: Failed to get repository name: %v\n", err)
			repoName = ""
		}

		windowName := fmt.Sprintf("%s|%s", repoName, worktreeName)
		if err := tmux.CloseWindow(windowName, worktreeName); err != nil {
			fmt.Printf("Warning: %v\n", err)
		} else {
			fmt.Println("Tmux window closed (switched to previous window)")
		}
	} else {
		fmt.Println("Not in a tmux session, skipping tmux cleanup")
	}

	fmt.Println("Cleanup complete!")
	return nil
}
