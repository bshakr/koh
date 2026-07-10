package cmd

// Integration tests for koh cleanup and koh prune. Each test builds a real
// git repository (with a local bare origin) in t.TempDir() and drives the
// actual command flows. TMUX is forced empty so no test can ever reach a
// real tmux server, and git config is isolated from the host machine.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// gitRun runs a git command in dir, failing the test on error.
func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gitTry(dir, args...)
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return out
}

// gitTry runs a git command in dir and returns its combined output.
func gitTry(dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// setupRepo creates a repo with one commit on main, pushed to a local bare
// origin (so refs/remotes/origin/main and origin/HEAD exist).
func setupRepo(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("cleanup/prune are not supported on Windows")
	}
	// Never let a test reach a real tmux server or the host's git config.
	t.Setenv("TMUX", "")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)

	base := t.TempDir()
	gitRun(t, base, "init", "-q", "--bare", "-b", "main", "origin.git")
	gitRun(t, base, "clone", "-q", "origin.git", "repo")
	repo := filepath.Join(base, "repo")
	gitRun(t, repo, "config", "user.email", "test@koh.local")
	gitRun(t, repo, "config", "user.name", "koh-test")
	gitRun(t, repo, "commit", "-q", "--allow-empty", "-m", "init")
	gitRun(t, repo, "push", "-q", "origin", "main")
	gitRun(t, repo, "remote", "set-head", "origin", "--auto")
	return repo
}

// addWorktree creates a worktree on a new branch under dir (".koh" or the
// legacy ".ko"). A zero-commit branch classifies as merged, so worktrees
// created this way are prunable unless the test commits to them.
func addWorktree(t *testing.T, repo, dir, name string) string {
	t.Helper()
	path := filepath.Join(repo, dir, name)
	gitRun(t, repo, "worktree", "add", "-q", path, "-b", name)
	return path
}

// setPruneFlags sets the package-level prune flags for one test.
func setPruneFlags(t *testing.T, dryRun, yes, deleteBranch, noFetch bool) {
	t.Helper()
	oldDry, oldYes, oldDel, oldNoFetch := pruneDryRun, pruneAssumeYes, pruneDeleteBranch, pruneNoFetch
	pruneDryRun, pruneAssumeYes, pruneDeleteBranch, pruneNoFetch = dryRun, yes, deleteBranch, noFetch
	t.Cleanup(func() {
		pruneDryRun, pruneAssumeYes, pruneDeleteBranch, pruneNoFetch = oldDry, oldYes, oldDel, oldNoFetch
	})
}

func isRegistered(t *testing.T, repo, name string) bool {
	t.Helper()
	out := gitRun(t, repo, "worktree", "list", "--porcelain")
	return strings.Contains(out, "/"+name)
}

func branchExistsIn(t *testing.T, repo, name string) bool {
	t.Helper()
	_, err := gitTry(repo, "show-ref", "--verify", "--quiet", "refs/heads/"+name)
	return err == nil
}

func assertDirGone(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err == nil {
		t.Errorf("expected %s to be removed, but it still exists", path)
	}
}

func assertDirExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected %s to exist: %v", path, err)
	}
}

// --- prune ---

func TestPruneRemovesBranchMergedOnlyOnOrigin(t *testing.T) {
	repo := setupRepo(t)
	wt := addWorktree(t, repo, ".koh", "wt-merged")
	if err := os.WriteFile(filepath.Join(wt, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, wt, "add", "f.txt")
	gitRun(t, wt, "commit", "-qm", "feat")
	gitRun(t, repo, "merge", "-q", "--no-ff", "wt-merged", "-m", "merge")
	gitRun(t, repo, "push", "-q", "origin", "main")
	// Local main no longer contains the merge — only origin/main does. The
	// classifier must compare against the remote-tracking ref.
	gitRun(t, repo, "reset", "-q", "--hard", "HEAD~1")

	t.Chdir(repo)
	setPruneFlags(t, false, true, false, true) // --yes --no-fetch
	if err := runPrune(nil, nil); err != nil {
		t.Fatalf("runPrune: %v", err)
	}
	assertDirGone(t, wt)
	if isRegistered(t, repo, "wt-merged") {
		t.Error("expected wt-merged registration to be gone")
	}
}

func TestPruneYesNeverRemovesCurrentWorktree(t *testing.T) {
	repo := setupRepo(t)
	wt := addWorktree(t, repo, ".koh", "wt-current") // zero commits → merged/prunable

	t.Chdir(wt)
	setPruneFlags(t, false, true, false, true) // --yes --no-fetch
	if err := runPrune(nil, nil); err != nil {
		t.Fatalf("runPrune: %v", err)
	}
	assertDirExists(t, wt)
	if !isRegistered(t, repo, "wt-current") {
		t.Error("expected current worktree to stay registered")
	}
}

func TestPruneSkipsWorktreeWithUncommittedChanges(t *testing.T) {
	repo := setupRepo(t)
	wt := addWorktree(t, repo, ".koh", "wt-dirty") // prunable (zero commits)
	precious := filepath.Join(wt, "uncommitted.txt")
	if err := os.WriteFile(precious, []byte("work in progress"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Chdir(repo)
	setPruneFlags(t, false, true, false, true)
	if err := runPrune(nil, nil); err != nil {
		t.Fatalf("runPrune: %v", err)
	}
	assertDirExists(t, wt)
	if _, err := os.Stat(precious); err != nil {
		t.Fatalf("uncommitted work was destroyed: %v", err)
	}
}

func TestPruneDryRunChangesNothing(t *testing.T) {
	repo := setupRepo(t)
	wt := addWorktree(t, repo, ".koh", "wt-dry")

	t.Chdir(repo)
	setPruneFlags(t, true, false, false, true) // --dry-run --no-fetch
	if err := runPrune(nil, nil); err != nil {
		t.Fatalf("runPrune: %v", err)
	}
	assertDirExists(t, wt)
	if !isRegistered(t, repo, "wt-dry") {
		t.Error("dry-run must not deregister worktrees")
	}
	if !branchExistsIn(t, repo, "wt-dry") {
		t.Error("dry-run must not delete branches")
	}
}

func TestPruneDeleteBranchForGoneWorktree(t *testing.T) {
	repo := setupRepo(t)
	wt := addWorktree(t, repo, ".koh", "wt-gone")
	if err := os.RemoveAll(wt); err != nil {
		t.Fatal(err)
	}

	t.Chdir(repo)
	setPruneFlags(t, false, true, true, true) // --yes --delete-branch --no-fetch
	if err := runPrune(nil, nil); err != nil {
		t.Fatalf("runPrune: %v", err)
	}
	if isRegistered(t, repo, "wt-gone") {
		t.Error("expected stale registration to be pruned")
	}
	// Deleting the branch requires the stale registration to be pruned
	// first — git refuses to delete a branch it considers checked out.
	if branchExistsIn(t, repo, "wt-gone") {
		t.Error("expected branch wt-gone to be deleted")
	}
}

func TestPruneRemovesWorktreeWithDeletedUpstream(t *testing.T) {
	repo := setupRepo(t)
	wt := addWorktree(t, repo, ".koh", "wt-squash")
	if err := os.WriteFile(filepath.Join(wt, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, wt, "add", "f.txt")
	gitRun(t, wt, "commit", "-qm", "feat")
	gitRun(t, wt, "push", "-q", "-u", "origin", "wt-squash")
	// Simulate a squash-merge: the remote branch disappears while the local
	// branch has commits that never land on main verbatim.
	gitRun(t, repo, "push", "-q", "origin", ":wt-squash")

	t.Chdir(repo)
	setPruneFlags(t, false, true, false, false) // --yes, with fetch --prune
	if err := runPrune(nil, nil); err != nil {
		t.Fatalf("runPrune: %v", err)
	}
	assertDirGone(t, wt)
}

func TestPruneSeesLegacyKoWorktree(t *testing.T) {
	repo := setupRepo(t)
	wt := addWorktree(t, repo, ".ko", "wt-legacy")

	t.Chdir(repo)
	setPruneFlags(t, false, true, false, true)
	if err := runPrune(nil, nil); err != nil {
		t.Fatalf("runPrune: %v", err)
	}
	assertDirGone(t, wt)
}

func TestPruneFromRepoSubdirectory(t *testing.T) {
	repo := setupRepo(t)
	wt := addWorktree(t, repo, ".koh", "wt-sub")
	deep := filepath.Join(repo, "some", "deep", "dir")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Chdir(deep)
	setPruneFlags(t, false, true, false, true)
	if err := runPrune(nil, nil); err != nil {
		t.Fatalf("runPrune: %v", err)
	}
	assertDirGone(t, wt)
}

func TestPruneRejectsPositionalArgs(t *testing.T) {
	if err := pruneCmd.Args(pruneCmd, []string{"some-worktree"}); err == nil {
		t.Error("expected positional args to be rejected — prune is all-or-picker, not per-name")
	}
}

// --- cleanup ---

func TestCleanupRemovesWorktree(t *testing.T) {
	repo := setupRepo(t)
	wt := addWorktree(t, repo, ".koh", "wt-basic")

	t.Chdir(repo)
	if err := runCleanup(nil, []string{"wt-basic"}); err != nil {
		t.Fatalf("runCleanup: %v", err)
	}
	assertDirGone(t, wt)
	if isRegistered(t, repo, "wt-basic") {
		t.Error("expected registration to be gone")
	}
}

func TestCleanupRemovesWorktreeWithReadOnlyDir(t *testing.T) {
	repo := setupRepo(t)
	wt := addWorktree(t, repo, ".koh", "wt-ro")
	// Read-only directories (package caches, vendored deps) make both
	// "git worktree remove --force" and plain os.RemoveAll fail — this is
	// the mode where cleanup used to close tmux but leave the files behind.
	cache := filepath.Join(wt, "cache", "child")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	//nolint:gosec // G302: deliberately read-only to reproduce the failure
	if err := os.Chmod(filepath.Join(wt, "cache"), 0o555); err != nil {
		t.Fatal(err)
	}

	t.Chdir(repo)
	if err := runCleanup(nil, []string{"wt-ro"}); err != nil {
		t.Fatalf("runCleanup: %v", err)
	}
	assertDirGone(t, wt)
	if isRegistered(t, repo, "wt-ro") {
		t.Error("expected registration to be gone")
	}
}

func TestCleanupRemovesLockedWorktree(t *testing.T) {
	repo := setupRepo(t)
	wt := addWorktree(t, repo, ".koh", "wt-locked")
	gitRun(t, repo, "worktree", "lock", wt)

	t.Chdir(repo)
	if err := runCleanup(nil, []string{"wt-locked"}); err != nil {
		t.Fatalf("runCleanup: %v", err)
	}
	assertDirGone(t, wt)
}

func TestCleanupRemovesOrphanedDirectory(t *testing.T) {
	repo := setupRepo(t)
	wt := addWorktree(t, repo, ".koh", "wt-orphan")
	// Reproduce a failed removal that dropped the registration but left
	// files: read-only content makes git delete what it can and give up.
	ro := filepath.Join(wt, "ro")
	if err := os.MkdirAll(ro, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ro, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	//nolint:gosec // G302: deliberately read-only to reproduce the failure
	if err := os.Chmod(ro, 0o555); err != nil {
		t.Fatal(err)
	}
	_, _ = gitTry(repo, "worktree", "remove", "--force", wt) // expected to fail partway
	if _, err := os.Stat(wt); err != nil {
		t.Skip("git fully removed the read-only worktree on this platform; orphan scenario not reproducible")
	}

	t.Chdir(repo)
	if err := runCleanup(nil, []string{"wt-orphan"}); err != nil {
		t.Fatalf("runCleanup: %v", err)
	}
	assertDirGone(t, wt)
}

func TestCleanupPrunesStaleRegistration(t *testing.T) {
	repo := setupRepo(t)
	wt := addWorktree(t, repo, ".koh", "wt-stale")
	if err := os.RemoveAll(wt); err != nil {
		t.Fatal(err)
	}

	t.Chdir(repo)
	if err := runCleanup(nil, []string{"wt-stale"}); err != nil {
		t.Fatalf("runCleanup: %v", err)
	}
	if isRegistered(t, repo, "wt-stale") {
		t.Error("expected stale registration to be pruned")
	}
}

func TestCleanupFindsLegacyKoWorktree(t *testing.T) {
	repo := setupRepo(t)
	wt := addWorktree(t, repo, ".ko", "wt-oldstyle")

	t.Chdir(repo)
	if err := runCleanup(nil, []string{"wt-oldstyle"}); err != nil {
		t.Fatalf("runCleanup: %v", err)
	}
	assertDirGone(t, wt)
}

func TestCleanupFromInsideTargetWorktree(t *testing.T) {
	repo := setupRepo(t)
	wt := addWorktree(t, repo, ".koh", "wt-inside")

	t.Chdir(wt)
	if err := runCleanup(nil, []string{"wt-inside"}); err != nil {
		t.Fatalf("runCleanup: %v", err)
	}
	assertDirGone(t, wt)
}

func TestCleanupNotFoundReturnsError(t *testing.T) {
	repo := setupRepo(t)

	t.Chdir(repo)
	if err := runCleanup(nil, []string{"does-not-exist"}); err == nil {
		t.Error("expected an error when nothing was found to clean up, got success")
	}
}

// --- new ---

// TestNewRefusesOutsideTmux verifies that koh new fails fast when not run from
// inside a tmux session, before any worktree is created or registered. Without
// the precheck, the worktree was created (and registered) first and only then
// did tmux session creation fail, leaving a half-created worktree that made
// re-running report "already exists". setupRepo forces TMUX="", so runNew is
// always "outside tmux" here.
func TestNewRefusesOutsideTmux(t *testing.T) {
	repo := setupRepo(t)
	// runNew checks for a .kohconfig; write a minimal one (empty setup script,
	// no pane commands) so the test exercises the tmux precheck rather than a
	// missing-config error.
	if err := os.WriteFile(filepath.Join(repo, ".kohconfig"), []byte(`{"setup_script":"","pane_commands":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Chdir(repo)
	err := runNew(nil, []string{"wt-notmux"})
	if err == nil {
		t.Fatal("expected runNew to fail when not in a tmux session, got success")
	}
	if !strings.Contains(err.Error(), "tmux") {
		t.Errorf("expected a tmux-related error, got: %v", err)
	}
	// The precheck must fire before the worktree is created on disk or
	// registered with git — otherwise a retry hits "already exists".
	assertDirGone(t, filepath.Join(repo, ".koh", "wt-notmux"))
	if isRegistered(t, repo, "wt-notmux") {
		t.Error("expected no worktree registration when the tmux precheck fails")
	}
}
