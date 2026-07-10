package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestIsGitRepo(t *testing.T) {
	// This test assumes we're running in a git repo
	// If not in a git repo, this test will fail
	result := IsGitRepo()
	if !result {
		t.Skip("Not in a git repository, skipping test")
	}
}

func TestGetRepoName(t *testing.T) {
	// Only test if we're in a git repo
	if !IsGitRepo() {
		t.Skip("Not in a git repository, skipping test")
	}

	name, err := GetRepoName()
	if err != nil {
		t.Fatalf("GetRepoName() failed: %v", err)
	}

	if name == "" {
		t.Error("GetRepoName() returned empty string")
	}

	t.Logf("Repository name: %s", name)
}

func TestIsInWorktree(t *testing.T) {
	if !IsGitRepo() {
		t.Skip("Not in a git repository, skipping test")
	}

	// Test should work whether we're in a worktree or not
	result := IsInWorktree()
	t.Logf("IsInWorktree: %v", result)
}

func TestGetMainRepoRoot(t *testing.T) {
	if !IsGitRepo() {
		t.Skip("Not in a git repository, skipping test")
	}

	root, err := GetMainRepoRoot()
	if err != nil {
		t.Fatalf("GetMainRepoRoot() failed: %v", err)
	}

	if root == "" {
		t.Error("GetMainRepoRoot() returned empty string")
	}

	// Verify the path exists
	if _, err := os.Stat(root); os.IsNotExist(err) {
		t.Errorf("GetMainRepoRoot() returned non-existent path: %s", root)
	}

	t.Logf("Main repo root: %s", root)
}

func TestGetCurrentWorktreePath(t *testing.T) {
	if !IsGitRepo() {
		t.Skip("Not in a git repository, skipping test")
	}

	path, err := GetCurrentWorktreePath()
	if err != nil {
		t.Fatalf("GetCurrentWorktreePath() failed: %v", err)
	}

	if path == "" {
		t.Error("GetCurrentWorktreePath() returned empty string")
	}

	// Verify the path exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("GetCurrentWorktreePath() returned non-existent path: %s", path)
	}

	t.Logf("Current worktree path: %s", path)
}

// gitIn runs a git command in dir with user config isolated, failing the test
// on error. Identity is supplied via env so commits work without global config.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_SYSTEM="+os.DevNull,
		"GIT_AUTHOR_NAME=koh-test", "GIT_AUTHOR_EMAIL=koh@test",
		"GIT_COMMITTER_NAME=koh-test", "GIT_COMMITTER_EMAIL=koh@test",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// scratchRepoWithWorktree creates an isolated repo with one commit, a
// subdirectory, and a worktree (with its own subdirectory). Paths come back
// symlink-resolved so they compare cleanly against git's output on macOS,
// where t.TempDir lives under a /var -> /private/var symlink.
func scratchRepoWithWorktree(t *testing.T) (repo, repoSub, wt, wtSub string) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)

	repo = t.TempDir()
	gitIn(t, repo, "init", "-b", "main")
	gitIn(t, repo, "commit", "--allow-empty", "-m", "init")

	repoSub = filepath.Join(repo, "sub", "dir")
	if err := os.MkdirAll(repoSub, 0o755); err != nil {
		t.Fatal(err)
	}

	wt = filepath.Join(repo, ".koh", "feature")
	gitIn(t, repo, "worktree", "add", wt, "-b", "feature")
	wtSub = filepath.Join(wt, "inner")
	if err := os.MkdirAll(wtSub, 0o755); err != nil {
		t.Fatal(err)
	}

	resolve := func(p string) string {
		r, err := filepath.EvalSymlinks(p)
		if err != nil {
			t.Fatalf("EvalSymlinks(%q): %v", p, err)
		}
		return r
	}
	return resolve(repo), resolve(repoSub), resolve(wt), resolve(wtSub)
}

// TestIsInWorktreeByCwd pins IsInWorktree from every kind of directory. The
// regression it guards: git prints --git-dir / --git-common-dir relative from
// some cwds and absolute from others (varying by git version), and comparing
// them un-absolutized misreported main-repo directories as worktrees.
func TestIsInWorktreeByCwd(t *testing.T) {
	repo, repoSub, wt, wtSub := scratchRepoWithWorktree(t)

	cases := []struct {
		name string
		dir  string
		want bool
	}{
		{"main repo root", repo, false},
		{"main repo subdirectory", repoSub, false},
		{"worktree root", wt, true},
		{"worktree subdirectory", wtSub, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(tc.dir)
			if got := IsInWorktree(); got != tc.want {
				t.Errorf("IsInWorktree() from %s = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

// TestGetMainRepoRootByCwd verifies GetMainRepoRoot returns the same absolute
// main-repo root from the repo root, a subdirectory, the worktree, and a
// worktree subdirectory. From the repo root git prints just ".git", which the
// old code turned into the relative, cwd-dependent root ".".
func TestGetMainRepoRootByCwd(t *testing.T) {
	repo, repoSub, wt, wtSub := scratchRepoWithWorktree(t)

	for _, tc := range []struct {
		name string
		dir  string
	}{
		{"main repo root", repo},
		{"main repo subdirectory", repoSub},
		{"worktree root", wt},
		{"worktree subdirectory", wtSub},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(tc.dir)
			got, err := GetMainRepoRoot()
			if err != nil {
				t.Fatalf("GetMainRepoRoot() error: %v", err)
			}
			if !filepath.IsAbs(got) {
				t.Errorf("GetMainRepoRoot() = %q, want an absolute path", got)
			}
			resolved, err := filepath.EvalSymlinks(got)
			if err != nil {
				t.Fatalf("EvalSymlinks(%q): %v", got, err)
			}
			if resolved != repo {
				t.Errorf("GetMainRepoRoot() from %s = %q, want %q", tc.name, resolved, repo)
			}
		})
	}
}
