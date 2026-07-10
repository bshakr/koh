package cmd

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"github.com/bshakr/koh/internal/git"
	"github.com/bshakr/koh/internal/signals"
	"github.com/bshakr/koh/internal/tmux"
	"github.com/bshakr/koh/internal/validation"
	"github.com/spf13/cobra"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup [worktree-name]",
	Short: "Close tmux session and remove worktree",
	Long: `Remove the git worktree and close its tmux window.

Removal is forced: uncommitted changes in the worktree are discarded
without prompting. Commit, stash, or push anything you want to keep
before running cleanup. To bulk-remove only worktrees that are safe to
delete, use 'koh prune' — it skips worktrees with uncommitted changes.

If no worktree name is provided and you're currently in a worktree,
the current worktree is cleaned up.

Not supported on Windows.`,
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
	// Absolutize before any chdir: git returns the root relative to the
	// original cwd (e.g. "." or "../..") when run inside the main repo.
	if abs, err := filepath.Abs(mainRepoRoot); err == nil {
		mainRepoRoot = abs
	}

	// Resolve the worktree from git's own registry rather than assuming
	// .koh/<name> — this also finds legacy .ko/ worktrees (created before
	// the rename) and registrations whose directory was already deleted.
	worktreePath, registered := findKohWorktreeByName(ctx, mainRepoRoot, worktreeName)
	if worktreePath == "" {
		worktreePath = filepath.Join(mainRepoRoot, ".koh", worktreeName)
	}

	// Check if worktree exists on disk
	worktreeExists := true
	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		worktreeExists = false
		if !registered {
			fmt.Printf("Warning: Worktree %s not found\n", worktreeName)
			fmt.Println("Will attempt to clean up tmux window only")
		}
	}

	// Get current directory
	currentDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	// Step 1: Always run the removal from the main repo root — older gits
	// refuse to remove the worktree the process is standing in ("cannot
	// remove the current working tree"), and a symlinked cwd can defeat any
	// path comparison, so don't rely on one.
	if pathInside(currentDir, worktreePath) {
		fmt.Println("Running from within target worktree, switching to parent repository...")
	}
	if err := os.Chdir(mainRepoRoot); err != nil {
		return fmt.Errorf("failed to change to parent directory: %w", err)
	}

	// Step 2: Remove the git worktree
	if worktreeExists {
		fmt.Printf("Removing git worktree: %s\n", worktreePath)
		removeFailed := false
		// Double --force: git requires it for locked worktrees and (on older
		// gits) ones containing submodules; cleanup is an explicit request.
		if err := git.RemoveWorktreeForcedWithContext(ctx, worktreePath); err != nil {
			fmt.Printf("Warning: Failed to remove worktree: %v\n", err)
			removeFailed = true
		} else {
			fmt.Println("Worktree removed successfully")
		}

		// Clean up any remaining directory: git worktree remove leaves the
		// directory behind when it fails partway (e.g. read-only
		// subdirectories from package caches — it may even drop the
		// registration while files survive).
		if err := forceRemoveAll(worktreePath); err != nil {
			fmt.Printf("Warning: Failed to remove worktree directory: %v\n", err)
		}

		// Verify before touching tmux, so a failure (and its output) doesn't
		// die with the window we're about to close.
		if _, err := os.Stat(worktreePath); err == nil {
			return fmt.Errorf("worktree directory still exists after cleanup: %s", worktreePath)
		}

		if removeFailed {
			// The directory is gone but git may still hold the registration.
			if err := git.PruneRefs(ctx); err != nil {
				fmt.Printf("Warning: Failed to prune stale worktree registration: %v\n", err)
			}
		}
	} else if registered {
		// Stale registration whose directory is already gone — drop the refs
		// so git (and koh list) stop reporting the worktree.
		if err := git.PruneRefs(ctx); err != nil {
			fmt.Printf("Warning: Failed to prune stale worktree registration: %v\n", err)
		} else {
			fmt.Println("Removed stale worktree registration")
		}
	}

	// Step 3: Close tmux window (tmux will automatically switch to previous window)
	windowClosed := false
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
			windowClosed = true
			fmt.Println("Tmux window closed (switched to previous window)")
		}
	} else {
		fmt.Println("Not in a tmux session, skipping tmux cleanup")
	}

	// Exit non-zero when there was nothing to clean at all, instead of
	// reporting success for a worktree that was never found.
	if !worktreeExists && !registered && !windowClosed {
		return fmt.Errorf("nothing to clean up for %q: no worktree, registration, or tmux window found", worktreeName)
	}

	fmt.Println("Cleanup complete!")
	return nil
}

// findKohWorktreeByName looks up a registered worktree by directory basename,
// limited to this repo's .koh (or legacy .ko) directory. It returns the
// registered path and whether a registration exists — the directory itself
// may already be deleted.
func findKohWorktreeByName(ctx context.Context, mainRepoRoot, name string) (string, bool) {
	worktrees, err := git.ListWorktreesPorcelain(ctx)
	if err != nil {
		return "", false
	}
	for _, wt := range filterKohWorktrees(worktrees, filepath.Join(mainRepoRoot, ".koh")) {
		if filepath.Base(wt.Path) == name {
			return wt.Path, true
		}
	}
	return "", false
}

// forceRemoveAll removes path like os.RemoveAll, but on failure makes every
// directory in the tree writable and retries — read-only directories (package
// caches, vendored deps) otherwise survive removal and keep the worktree
// half-deleted on disk.
func forceRemoveAll(path string) error {
	err := os.RemoveAll(path)
	if err == nil {
		return nil
	}
	_ = filepath.WalkDir(path, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			// Unreadable entry — try to repair its permissions and move on.
			//nolint:gosec // G302: directories need the execute bit; tree is being deleted
			_ = os.Chmod(p, 0o700)
			return nil
		}
		if d.IsDir() {
			//nolint:gosec // G302: directories need the execute bit; tree is being deleted
			_ = os.Chmod(p, 0o700)
		}
		return nil
	})
	return os.RemoveAll(path)
}
