package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bshakr/koh/internal/config"
	"github.com/bshakr/koh/internal/git"
	"github.com/bshakr/koh/internal/signals"
	"github.com/bshakr/koh/internal/tmux"
	"github.com/bshakr/koh/internal/validation"
	"github.com/spf13/cobra"
)

var newCmd = &cobra.Command{
	Use:   "new <worktree-name>",
	Short: "Create a new worktree and tmux session",
	Long: `Create a new git worktree in .koh/<worktree-name> and open a tmux
window with one pane running your setup script and additional panes
running the commands from your .kohconfig.

Must be run from inside a tmux session. If the setup script is missing
from the new worktree (e.g. not committed yet), it is copied over from
the main repository automatically.`,
	Args: cobra.ExactArgs(1),
	RunE: runNew,
}

func init() {
	rootCmd.AddCommand(newCmd)
}

func runNew(_ *cobra.Command, args []string) error {
	worktreeName := args[0]

	// Validate worktree name for security
	if err := validation.ValidateWorktreeName(worktreeName); err != nil {
		return fmt.Errorf("invalid worktree name: %w", err)
	}

	// Set up context with cancellation for long-running operations and signal handling
	ctx, cleanup := signals.SetupCancellableContext()
	defer cleanup()

	// Check if we're in a git repository
	if !git.IsGitRepo() {
		return fmt.Errorf("not in a git repository\nPlease run this command from within a git repository")
	}

	// Check if we're in a tmux session before creating anything on disk. The
	// worktree is created further down and the tmux session after it, so
	// without this precheck a run outside tmux would create and register the
	// worktree and only then fail — leaving a half-created worktree that makes
	// re-running report "already exists".
	if !tmux.IsInTmux() {
		return fmt.Errorf("not in a tmux session\nPlease run this command from within a tmux session")
	}

	// Check if config exists, if not prompt user to run init
	exists, err := config.ConfigExists()
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

	// Determine the main repo root (handles both main repo and worktrees)
	mainRepoRoot, err := git.GetMainRepoRootOrCwd()
	if err != nil {
		return fmt.Errorf("failed to get repository root: %w", err)
	}

	// Check if setup script exists and is within repository boundaries
	if cfg.SetupScript != "" {
		setupPath := filepath.Join(mainRepoRoot, cfg.SetupScript)

		// Validate setup script is within repository (security check)
		if err := validation.ValidatePathWithinRepository(setupPath, mainRepoRoot); err != nil {
			return fmt.Errorf("setup script %w\nAttempted path: %s", err, cfg.SetupScript)
		}

		// Check if the script exists
		if _, err := os.Stat(setupPath); os.IsNotExist(err) {
			return fmt.Errorf("%s not found\nPlease create a setup script at %s", cfg.SetupScript, cfg.SetupScript)
		}
	}

	// Create .koh directory if it doesn't exist
	koDir := filepath.Join(mainRepoRoot, ".koh")
	//nolint:gosec // G301: 0755 is standard permission for user directories
	if err := os.MkdirAll(koDir, 0755); err != nil {
		return fmt.Errorf("failed to create .koh directory: %w", err)
	}

	// Check if worktree already exists
	worktreePath := filepath.Join(koDir, worktreeName)
	if _, err := os.Stat(worktreePath); err == nil {
		return fmt.Errorf("worktree .koh/%s already exists", worktreeName)
	}

	// Create git worktree with context
	fmt.Printf("Creating git worktree: .koh/%s\n", worktreeName)
	if err := git.CreateWorktreeWithContext(ctx, worktreePath); err != nil {
		return fmt.Errorf("failed to create worktree: %w", err)
	}

	// Get repository name
	repoName, err := git.GetRepoName()
	if err != nil {
		return fmt.Errorf("failed to get repository name: %w", err)
	}

	// Create tmux session with config and context
	if err := tmux.CreateSessionWithContext(ctx, repoName, worktreeName, worktreePath, cfg); err != nil {
		// The worktree was created moments ago; roll it back so a failed setup
		// doesn't leave a registered, half-created worktree behind (defense in
		// depth — the tmux precheck above covers the common cause).
		rollbackNewWorktree(worktreeName, worktreePath)
		return fmt.Errorf("failed to create tmux session: %w", err)
	}

	fmt.Println("Worktree setup complete!")
	return nil
}

// rollbackNewWorktree removes a worktree that runNew just created, used when a
// later step fails and would otherwise leave a half-created, already-registered
// worktree behind.
func rollbackNewWorktree(worktreeName, worktreePath string) {
	// Use a background context: the caller's context may already be cancelled
	// (e.g. Ctrl+C during setup), and the rollback must still run to completion.
	if err := git.RemoveWorktreeWithContext(context.Background(), worktreePath); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to deregister worktree during rollback: %v\n", err)
	}
	// git worktree remove can leave the directory behind on partial failure, so
	// make sure the path is gone for a clean retry of koh new.
	if err := forceRemoveAll(worktreePath); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to remove worktree directory during rollback: %v\n", err)
	}
	fmt.Printf("Rolled back worktree .koh/%s\n", worktreeName)
}
