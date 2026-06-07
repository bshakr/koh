package git

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
)

// PruneReason identifies why a worktree is considered prunable.
type PruneReason string

const (
	// ReasonGone means the worktree directory is missing on disk.
	// Git itself reports this via "git worktree list --porcelain" with a
	// "prunable" line.
	ReasonGone PruneReason = "gone"

	// ReasonMerged means the worktree's branch has been fully merged into
	// the repository's default branch.
	ReasonMerged PruneReason = "merged"

	// ReasonGoneFromRemote means the worktree's branch tracked an upstream
	// that no longer exists. This catches squash-merged PRs whose local
	// branches don't show up as merged.
	ReasonGoneFromRemote PruneReason = "gone-from-remote"
)

// WorktreeInfo describes a single worktree as parsed from the git porcelain
// output, augmented with prune classification reasons.
type WorktreeInfo struct {
	Path     string
	Branch   string
	Head     string
	Detached bool
	Bare     bool
	Reasons  []PruneReason
}

// IsPrunable reports whether the worktree has at least one prune reason.
func (w WorktreeInfo) IsPrunable() bool {
	return len(w.Reasons) > 0
}

// HasReason reports whether the worktree has the given prune reason.
func (w WorktreeInfo) HasReason(r PruneReason) bool {
	return slices.Contains(w.Reasons, r)
}

// ListWorktreesPorcelain runs "git worktree list --porcelain" and parses the
// output into WorktreeInfo values. The Reasons slice is populated only with
// ReasonGone (when git itself flags a worktree as prunable). Use
// ClassifyWorktrees to add the merged / gone-from-remote reasons.
func ListWorktreesPorcelain(ctx context.Context) ([]WorktreeInfo, error) {
	cmd := exec.CommandContext(ctx, "git", "worktree", "list", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git worktree list --porcelain failed: %w", err)
	}
	return parsePorcelain(string(output)), nil
}

// parsePorcelain parses the output of "git worktree list --porcelain" into
// WorktreeInfo records. Records are separated by blank lines.
func parsePorcelain(output string) []WorktreeInfo {
	var worktrees []WorktreeInfo
	var current *WorktreeInfo
	flush := func() {
		if current != nil && current.Path != "" {
			worktrees = append(worktrees, *current)
		}
		current = nil
	}

	for raw := range strings.SplitSeq(output, "\n") {
		line := strings.TrimRight(raw, "\r")
		if line == "" {
			flush()
			continue
		}
		if current == nil {
			current = &WorktreeInfo{}
		}
		key, value, _ := strings.Cut(line, " ")
		switch key {
		case "worktree":
			current.Path = value
		case "HEAD":
			current.Head = value
		case "branch":
			// "branch refs/heads/<name>" — strip the ref prefix.
			current.Branch = strings.TrimPrefix(value, "refs/heads/")
		case "detached":
			current.Detached = true
		case "bare":
			current.Bare = true
		case "prunable":
			current.Reasons = append(current.Reasons, ReasonGone)
		}
	}
	flush()
	return worktrees
}

// DefaultBranch returns the repository's default branch name. It first asks
// git for origin's HEAD, then falls back to "main" or "master" if either
// exists locally. Returns an error only when none of those work.
func DefaultBranch(ctx context.Context) (string, error) {
	if name, ok := originHeadBranch(ctx); ok {
		return name, nil
	}
	for _, candidate := range []string{"main", "master"} {
		if branchExists(ctx, candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("could not determine default branch")
}

func originHeadBranch(ctx context.Context) (string, bool) {
	cmd := exec.CommandContext(ctx, "git", "symbolic-ref", "--short", "refs/remotes/origin/HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	ref := strings.TrimSpace(string(out))
	// ref looks like "origin/main" — strip the remote prefix.
	if idx := strings.Index(ref, "/"); idx >= 0 && idx < len(ref)-1 {
		return ref[idx+1:], true
	}
	return "", false
}

func branchExists(ctx context.Context, name string) bool {
	cmd := exec.CommandContext(ctx, "git", "show-ref", "--verify", "--quiet", "refs/heads/"+name)
	return cmd.Run() == nil
}

// FetchPrune runs "git fetch --prune" so the upstream-gone classification is
// based on current remote state. Failures are non-fatal — the caller should
// log them and continue with whatever local state is available.
func FetchPrune(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "git", "fetch", "--prune")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git fetch --prune failed: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

// IsMerged reports whether branch is reachable from base. Both branches must
// exist locally; missing branches return (false, nil) so callers can skip.
func IsMerged(ctx context.Context, branch, base string) (bool, error) {
	if branch == "" || base == "" || branch == base {
		return false, nil
	}
	if !branchExists(ctx, branch) || !branchExists(ctx, base) {
		return false, nil
	}
	cmd := exec.CommandContext(ctx, "git", "merge-base", "--is-ancestor",
		"refs/heads/"+branch, "refs/heads/"+base)
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		// Exit 1 means "not an ancestor" — that's a clean negative answer.
		return false, nil
	}
	return false, fmt.Errorf("merge-base check failed: %w", err)
}

// UpstreamGone reports whether the branch's tracked upstream no longer exists
// on the remote. Returns (false, nil) for branches with no upstream.
func UpstreamGone(ctx context.Context, branch string) (bool, error) {
	if branch == "" {
		return false, nil
	}
	cmd := exec.CommandContext(ctx, "git", "for-each-ref",
		"--format=%(upstream:track)", "refs/heads/"+branch)
	out, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("for-each-ref failed: %w", err)
	}
	return strings.Contains(string(out), "[gone]"), nil
}

// DeleteBranch force-deletes a local branch.
func DeleteBranch(ctx context.Context, branch string) error {
	if branch == "" {
		return fmt.Errorf("branch name is empty")
	}
	cmd := exec.CommandContext(ctx, "git", "branch", "-D", branch)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git branch -D %s failed: %s", branch, strings.TrimSpace(string(output)))
	}
	return nil
}

// PruneRefs runs "git worktree prune" to drop administrative refs for
// worktrees whose directories are gone.
func PruneRefs(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "git", "worktree", "prune")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree prune failed: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

// ClassifyWorktrees fills in PruneReason values for each worktree.
// ReasonGone is read straight from porcelain output; ReasonMerged and
// ReasonGoneFromRemote require additional git calls.
func ClassifyWorktrees(ctx context.Context, worktrees []WorktreeInfo) ([]WorktreeInfo, error) {
	defaultBranch, _ := DefaultBranch(ctx) // may be empty; merged check skips when empty

	out := make([]WorktreeInfo, len(worktrees))
	for i, wt := range worktrees {
		copied := wt
		if wt.Branch == "" || wt.Detached || wt.Bare {
			out[i] = copied
			continue
		}

		if defaultBranch != "" {
			merged, err := IsMerged(ctx, wt.Branch, defaultBranch)
			if err != nil {
				return nil, err
			}
			if merged {
				copied.Reasons = appendUnique(copied.Reasons, ReasonMerged)
			}
		}

		gone, err := UpstreamGone(ctx, wt.Branch)
		if err != nil {
			return nil, err
		}
		if gone {
			copied.Reasons = appendUnique(copied.Reasons, ReasonGoneFromRemote)
		}

		out[i] = copied
	}
	return out, nil
}

func appendUnique(in []PruneReason, r PruneReason) []PruneReason {
	if slices.Contains(in, r) {
		return in
	}
	return append(in, r)
}
