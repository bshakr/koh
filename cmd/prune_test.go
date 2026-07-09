package cmd

import (
	"path/filepath"
	"testing"

	"github.com/bshakr/koh/internal/git"
	tea "github.com/charmbracelet/bubbletea"
)

func makeCandidate(name, branch string, reasons []git.PruneReason, isCurrent bool) pruneCandidate {
	wt := git.WorktreeInfo{
		Path:    filepath.Join("/repo/.koh", name),
		Branch:  branch,
		Reasons: reasons,
	}
	return pruneCandidate{
		wt:        wt,
		name:      name,
		selected:  wt.IsPrunable() && !isCurrent,
		isCurrent: isCurrent,
	}
}

func TestFilterPrunable(t *testing.T) {
	candidates := []pruneCandidate{
		makeCandidate("clean", "main", nil, false),
		makeCandidate("merged", "feat", []git.PruneReason{git.ReasonMerged}, false),
		makeCandidate("gone", "old", []git.PruneReason{git.ReasonGone, git.ReasonGoneFromRemote}, false),
		// Prunable but current — must never be offered to --yes / --dry-run.
		makeCandidate("current", "feat-cur", []git.PruneReason{git.ReasonMerged}, true),
	}
	got := filterPrunable(candidates)
	if len(got) != 2 {
		t.Fatalf("expected 2 prunable, got %d", len(got))
	}
	if got[0].name != "merged" || got[1].name != "gone" {
		t.Errorf("unexpected order/contents: %+v", got)
	}
}

func TestPathInside(t *testing.T) {
	tests := []struct {
		name   string
		child  string
		parent string
		want   bool
	}{
		{"nested", "/repo/.koh/feature", "/repo/.koh", true},
		{"same", "/repo/.koh", "/repo/.koh", true},
		{"sibling", "/repo/other/feature", "/repo/.koh", false},
		{"parent", "/repo", "/repo/.koh", false},
		{"dotdot-prefixed name", "/repo/.koh/..archive", "/repo/.koh", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pathInside(tt.child, tt.parent)
			if got != tt.want {
				t.Errorf("pathInside(%q, %q) = %v, want %v", tt.child, tt.parent, got, tt.want)
			}
		})
	}
}

func TestSamePath(t *testing.T) {
	if !samePath("/a/b", "/a/b") {
		t.Error("expected samePath equal paths = true")
	}
	if samePath("/a/b", "/a/c") {
		t.Error("expected samePath different paths = false")
	}
}

func TestDisplayBranch(t *testing.T) {
	if got := displayBranch(git.WorktreeInfo{Branch: "main"}); got != "main" {
		t.Errorf("expected branch name, got %q", got)
	}
	if got := displayBranch(git.WorktreeInfo{Detached: true}); got != "(detached)" {
		t.Errorf("expected detached, got %q", got)
	}
	if got := displayBranch(git.WorktreeInfo{}); got != "(unknown)" {
		t.Errorf("expected unknown, got %q", got)
	}
}

func TestPruneModelToggle(t *testing.T) {
	m := pruneModel{
		candidates: []pruneCandidate{
			makeCandidate("a", "feat-a", []git.PruneReason{git.ReasonMerged}, false),
			makeCandidate("b", "feat-b", nil, false),
		},
	}
	m.cursor = 1
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	got := updated.(pruneModel)
	if !got.candidates[1].selected {
		t.Error("expected candidate b to be toggled on")
	}
	updated, _ = got.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	got = updated.(pruneModel)
	if got.candidates[1].selected {
		t.Error("expected candidate b to be toggled off again")
	}
}

func TestPruneModelToggleSkipsCurrent(t *testing.T) {
	m := pruneModel{
		candidates: []pruneCandidate{
			makeCandidate("a", "feat-a", nil, true),
		},
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	got := updated.(pruneModel)
	if got.candidates[0].selected {
		t.Error("expected current worktree to remain unselected")
	}
}

func TestPruneModelSelectAllSkipsCurrent(t *testing.T) {
	m := pruneModel{
		candidates: []pruneCandidate{
			makeCandidate("a", "feat-a", nil, true),
			makeCandidate("b", "feat-b", nil, false),
		},
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	got := updated.(pruneModel)
	if got.candidates[0].selected {
		t.Error("expected current worktree to stay unselected after 'a'")
	}
	if !got.candidates[1].selected {
		t.Error("expected non-current worktree selected after 'a'")
	}
}

func TestPruneModelSelectNone(t *testing.T) {
	m := pruneModel{
		candidates: []pruneCandidate{
			makeCandidate("a", "feat-a", []git.PruneReason{git.ReasonMerged}, false),
			makeCandidate("b", "feat-b", []git.PruneReason{git.ReasonGone}, false),
		},
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	got := updated.(pruneModel)
	for _, c := range got.candidates {
		if c.selected {
			t.Errorf("expected %q unselected", c.name)
		}
	}
}

func TestPruneModelToggleDeleteBranch(t *testing.T) {
	m := pruneModel{}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	got := updated.(pruneModel)
	if !got.deleteBranch {
		t.Error("expected delete-branch toggled on")
	}
	updated, _ = got.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	got = updated.(pruneModel)
	if got.deleteBranch {
		t.Error("expected delete-branch toggled back off")
	}
}

func TestPruneModelEnterConfirms(t *testing.T) {
	m := pruneModel{}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(pruneModel)
	if !got.confirm {
		t.Error("expected confirm = true on enter")
	}
	if cmd == nil {
		t.Error("expected quit cmd on enter")
	}
}

func TestPruneModelQuitCancels(t *testing.T) {
	m := pruneModel{}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	got := updated.(pruneModel)
	if !got.cancelled {
		t.Error("expected cancelled = true on q")
	}
	if cmd == nil {
		t.Error("expected quit cmd on q")
	}
}

func TestGoneReasonShortCircuit(t *testing.T) {
	// A candidate flagged ReasonGone should report HasReason(ReasonGone)
	// so executePrune skips the "git worktree remove" call. Regression
	// guard for the case where the dir is already missing.
	c := makeCandidate("orphan", "old-branch", []git.PruneReason{git.ReasonGone}, false)
	if !c.wt.HasReason(git.ReasonGone) {
		t.Fatal("expected HasReason(ReasonGone) = true")
	}
	if !c.wt.IsPrunable() {
		t.Error("expected IsPrunable = true for gone candidate")
	}
}

func TestPruneModelNavigationBounds(t *testing.T) {
	m := pruneModel{
		candidates: []pruneCandidate{
			makeCandidate("a", "feat-a", nil, false),
			makeCandidate("b", "feat-b", nil, false),
		},
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	got := updated.(pruneModel)
	if got.cursor != 0 {
		t.Errorf("expected cursor clamped at 0, got %d", got.cursor)
	}
	updated, _ = got.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	got = updated.(pruneModel)
	if got.cursor != 1 {
		t.Errorf("expected cursor at end (1), got %d", got.cursor)
	}
	updated, _ = got.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	got = updated.(pruneModel)
	if got.cursor != 1 {
		t.Errorf("expected cursor clamped at 1, got %d", got.cursor)
	}
}
