package cmd

// Tests for the root dashboard's worktree accounting. These reuse the git
// scaffolding in integration_test.go (setupRepo, addWorktree) and focus on the
// bug where the dashboard reported "0 active" when run from a subdirectory of
// the main repo because it derived the repo root from the cwd instead of git.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveMainRepoRootFromSubdirectory(t *testing.T) {
	repo := setupRepo(t)
	deep := filepath.Join(repo, "some", "deep", "dir")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Chdir(deep)
	root := resolveMainRepoRoot()
	if root == "" {
		t.Fatal("resolveMainRepoRoot returned empty string")
	}
	if !filepath.IsAbs(root) {
		t.Errorf("resolveMainRepoRoot should be absolute, got %q", root)
	}
	// From a subdirectory it must point at the repo root, not the cwd.
	if !samePath(root, repo) {
		t.Errorf("resolveMainRepoRoot = %q, want the repo root %q", root, repo)
	}
}

func TestCountKohWorktreesFromRepoRoot(t *testing.T) {
	repo := setupRepo(t)
	addWorktree(t, repo, ".koh", "wt-a")
	addWorktree(t, repo, ".koh", "wt-b")

	t.Chdir(repo)
	if got := countKohWorktrees(context.Background(), resolveMainRepoRoot()); got != 2 {
		t.Errorf("countKohWorktrees from repo root = %d, want 2", got)
	}
}

// The regression under test: from a subdirectory the count used to stay 0
// because it stat'd <subdir>/.koh instead of resolving the real repo root.
func TestCountKohWorktreesFromSubdirectory(t *testing.T) {
	repo := setupRepo(t)
	addWorktree(t, repo, ".koh", "wt-a")
	addWorktree(t, repo, ".koh", "wt-b")
	deep := filepath.Join(repo, "some", "deep", "dir")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Chdir(deep)
	if got := countKohWorktrees(context.Background(), resolveMainRepoRoot()); got != 2 {
		t.Errorf("countKohWorktrees from subdirectory = %d, want 2", got)
	}
}

// The count shares filterKohWorktrees with list/prune, so a legacy .ko
// worktree is counted alongside .koh ones — the dashboard agrees with them.
func TestCountKohWorktreesIncludesLegacyKo(t *testing.T) {
	repo := setupRepo(t)
	addWorktree(t, repo, ".koh", "wt-new")
	addWorktree(t, repo, ".ko", "wt-legacy")

	t.Chdir(repo)
	if got := countKohWorktrees(context.Background(), resolveMainRepoRoot()); got != 2 {
		t.Errorf("countKohWorktrees = %d, want 2 (.koh + legacy .ko)", got)
	}
}

func TestCountKohWorktreesWithNoWorktrees(t *testing.T) {
	repo := setupRepo(t)

	t.Chdir(repo)
	if got := countKohWorktrees(context.Background(), resolveMainRepoRoot()); got != 0 {
		t.Errorf("countKohWorktrees with no worktrees = %d, want 0", got)
	}
}

// From inside a worktree the count still reflects every koh worktree (the
// current one included, matching what `koh list` shows).
func TestCountKohWorktreesFromInsideWorktree(t *testing.T) {
	repo := setupRepo(t)
	wt := addWorktree(t, repo, ".koh", "wt-a")
	addWorktree(t, repo, ".koh", "wt-b")

	t.Chdir(wt)
	if got := countKohWorktrees(context.Background(), resolveMainRepoRoot()); got != 2 {
		t.Errorf("countKohWorktrees from inside a worktree = %d, want 2", got)
	}
}

func TestCurrentWorktreeLabelFromRepoRoot(t *testing.T) {
	repo := setupRepo(t)

	t.Chdir(repo)
	if got := currentWorktreeLabel(resolveMainRepoRoot()); got != "main" {
		t.Errorf("currentWorktreeLabel from repo root = %q, want \"main\"", got)
	}
}

// A subdirectory of the main repo is still the primary checkout — it must read
// as "main", not as a worktree named after the repo directory.
func TestCurrentWorktreeLabelFromSubdirectory(t *testing.T) {
	repo := setupRepo(t)
	deep := filepath.Join(repo, "some", "deep", "dir")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Chdir(deep)
	if got := currentWorktreeLabel(resolveMainRepoRoot()); got != "main" {
		t.Errorf("currentWorktreeLabel from subdirectory = %q, want \"main\"", got)
	}
}

func TestCurrentWorktreeLabelFromInsideWorktree(t *testing.T) {
	repo := setupRepo(t)
	wt := addWorktree(t, repo, ".koh", "wt-a")

	t.Chdir(wt)
	if got := currentWorktreeLabel(resolveMainRepoRoot()); got != "wt-a" {
		t.Errorf("currentWorktreeLabel from inside worktree = %q, want \"wt-a\"", got)
	}
}
