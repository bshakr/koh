package git

import (
	"reflect"
	"testing"
)

func TestParsePorcelain(t *testing.T) {
	input := `worktree /repo
HEAD abc123
branch refs/heads/main

worktree /repo/.koh/feature
HEAD def456
branch refs/heads/feature

worktree /repo/.koh/missing
HEAD 000000
branch refs/heads/missing
prunable gitdir file points to non-existent location

worktree /repo/.koh/detached
HEAD 111111
detached
`
	got := parsePorcelain(input)

	want := []WorktreeInfo{
		{Path: "/repo", Head: "abc123", Branch: "main"},
		{Path: "/repo/.koh/feature", Head: "def456", Branch: "feature"},
		{Path: "/repo/.koh/missing", Head: "000000", Branch: "missing", Reasons: []PruneReason{ReasonGone}},
		{Path: "/repo/.koh/detached", Head: "111111", Detached: true},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parsePorcelain mismatch:\n got: %+v\nwant: %+v", got, want)
	}
}

func TestParsePorcelainEmpty(t *testing.T) {
	if got := parsePorcelain(""); len(got) != 0 {
		t.Errorf("expected empty slice, got %v", got)
	}
}

func TestParsePorcelainTrailingBlank(t *testing.T) {
	input := "worktree /repo\nHEAD abc\nbranch refs/heads/main\n\n\n"
	got := parsePorcelain(input)
	if len(got) != 1 {
		t.Fatalf("expected 1 worktree, got %d", len(got))
	}
	if got[0].Path != "/repo" || got[0].Branch != "main" {
		t.Errorf("unexpected worktree: %+v", got[0])
	}
}

func TestWorktreeInfoIsPrunable(t *testing.T) {
	tests := []struct {
		name    string
		reasons []PruneReason
		want    bool
	}{
		{"no reasons", nil, false},
		{"empty slice", []PruneReason{}, false},
		{"one reason", []PruneReason{ReasonMerged}, true},
		{"multiple reasons", []PruneReason{ReasonMerged, ReasonGoneFromRemote}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := WorktreeInfo{Reasons: tt.reasons}
			if got := w.IsPrunable(); got != tt.want {
				t.Errorf("IsPrunable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWorktreeInfoHasReason(t *testing.T) {
	w := WorktreeInfo{Reasons: []PruneReason{ReasonMerged, ReasonGone}}
	if !w.HasReason(ReasonMerged) {
		t.Error("expected HasReason(ReasonMerged) = true")
	}
	if !w.HasReason(ReasonGone) {
		t.Error("expected HasReason(ReasonGone) = true")
	}
	if w.HasReason(ReasonGoneFromRemote) {
		t.Error("expected HasReason(ReasonGoneFromRemote) = false")
	}
}


func TestAppendUnique(t *testing.T) {
	in := []PruneReason{ReasonMerged}
	got := appendUnique(in, ReasonMerged)
	if len(got) != 1 {
		t.Errorf("expected dedupe, got %v", got)
	}
	got = appendUnique(in, ReasonGone)
	if len(got) != 2 || got[1] != ReasonGone {
		t.Errorf("expected append, got %v", got)
	}
}

// Integration-style tests below exercise real git; they skip outside a repo.

func TestDefaultBranchInRepo(t *testing.T) {
	if !IsGitRepo() {
		t.Skip("not in a git repository")
	}
	ctx := t.Context()
	name, err := DefaultBranch(ctx)
	if err != nil {
		t.Fatalf("DefaultBranch() error: %v", err)
	}
	if name == "" {
		t.Error("expected non-empty default branch")
	}
}

func TestIsMergedSelfReturnsFalse(t *testing.T) {
	if !IsGitRepo() {
		t.Skip("not in a git repository")
	}
	ctx := t.Context()
	merged, err := IsMerged(ctx, "main", "main")
	if err != nil {
		t.Fatalf("IsMerged() error: %v", err)
	}
	if merged {
		t.Error("expected IsMerged(main, main) = false (self-compare guard)")
	}
}

func TestIsMergedMissingBranch(t *testing.T) {
	if !IsGitRepo() {
		t.Skip("not in a git repository")
	}
	ctx := t.Context()
	merged, err := IsMerged(ctx, "definitely-not-a-real-branch-xyz", "main")
	if err != nil {
		t.Fatalf("IsMerged() error: %v", err)
	}
	if merged {
		t.Error("expected IsMerged for missing branch = false")
	}
}

func TestUpstreamGoneNoBranch(t *testing.T) {
	if !IsGitRepo() {
		t.Skip("not in a git repository")
	}
	ctx := t.Context()
	gone, err := UpstreamGone(ctx, "")
	if err != nil {
		t.Fatalf("UpstreamGone() error: %v", err)
	}
	if gone {
		t.Error("expected empty branch -> false")
	}
}
