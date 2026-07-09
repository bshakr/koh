package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/bshakr/koh/internal/git"
	"github.com/bshakr/koh/internal/signals"
	"github.com/bshakr/koh/internal/styles"
	"github.com/bshakr/koh/internal/tmux"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var (
	pruneDryRun       bool
	pruneAssumeYes    bool
	pruneDeleteBranch bool
	pruneNoFetch      bool
)

var pruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "Remove worktrees and branches that are safe to clean up",
	Long: `Prune worktrees that are no longer useful.

A worktree is considered prunable when one or more of these apply:
  • gone             — its directory has been removed on disk
  • merged           — its branch is fully merged into the default branch
  • gone-from-remote — its tracked upstream branch was deleted (e.g. after squash-merge)

The current worktree is never pruned, and worktrees with uncommitted changes
are skipped (use 'koh cleanup <name>' to remove those explicitly).

By default an interactive picker lets you confirm what gets removed. Use
--yes to skip the picker and remove everything classified as prunable, or
--dry-run to preview without changing anything.`,
	Args: cobra.NoArgs,
	RunE: runPrune,
}

func init() {
	pruneCmd.Flags().BoolVar(&pruneDryRun, "dry-run", false, "Show what would be pruned without removing anything")
	pruneCmd.Flags().BoolVar(&pruneAssumeYes, "yes", false, "Skip the interactive picker; prune everything classified as prunable")
	pruneCmd.Flags().BoolVar(&pruneDeleteBranch, "delete-branch", false, "Also delete the local branch for each pruned worktree")
	pruneCmd.Flags().BoolVar(&pruneNoFetch, "no-fetch", false, "Skip the implicit 'git fetch --prune' before classification")
	rootCmd.AddCommand(pruneCmd)
}

// pruneCandidate is a single row in the prune UI / batch.
type pruneCandidate struct {
	wt        git.WorktreeInfo
	name      string
	selected  bool
	isCurrent bool
}

func runPrune(_ *cobra.Command, _ []string) error {
	// Windows is not supported due to differences in process management
	// (same guard as the cleanup command, which does the same teardown).
	if runtime.GOOS == "windows" {
		return fmt.Errorf("prune command is not supported on Windows")
	}

	if !git.IsGitRepo() {
		return fmt.Errorf("not in a git repository")
	}

	ctx, cleanupSignals := signals.SetupCancellableContext()
	defer cleanupSignals()

	// Resolve via git-common-dir (not cwd) so prune works from any
	// subdirectory of the main repo, not just its root.
	mainRepoRoot, err := git.GetMainRepoRoot()
	if err != nil {
		return fmt.Errorf("failed to get repository root: %w", err)
	}
	kohDir := filepath.Join(mainRepoRoot, ".koh")

	// Cheap local listing first — skip the network fetch entirely when no
	// koh worktrees are registered at all. Deliberately no .koh stat check:
	// worktrees whose directories are gone (even a deleted .koh) still need
	// their administrative refs pruned.
	worktrees, err := git.ListWorktreesPorcelain(ctx)
	if err != nil {
		return err
	}
	kohWorktrees := filterKohWorktrees(worktrees, kohDir)
	if len(kohWorktrees) == 0 {
		fmt.Println(styles.SuccessMessage.Render(styles.IconCheck + " Nothing to prune"))
		return nil
	}

	if !pruneNoFetch {
		fmt.Println(styles.Muted.Render("Fetching from origin..."))
		if err := git.FetchPrune(ctx); err != nil {
			fmt.Println(styles.Muted.Render(fmt.Sprintf("Warning: %v (continuing with local state)", err)))
		}
	}

	candidates, err := buildPruneCandidates(ctx, kohWorktrees)
	if err != nil {
		return err
	}

	prunable := filterPrunable(candidates)
	if len(prunable) == 0 {
		fmt.Println(styles.SuccessMessage.Render(styles.IconCheck + " Nothing to prune"))
		return nil
	}

	if pruneDryRun {
		printDryRun(prunable)
		return nil
	}

	toPrune := prunable
	deleteBranch := pruneDeleteBranch

	if !pruneAssumeYes {
		selected, dbToggle, cancelled, err := runPruneTUI(ctx, candidates, pruneDeleteBranch)
		if err != nil {
			return fmt.Errorf("error running prune picker: %w", err)
		}
		if cancelled {
			fmt.Println(styles.Muted.Render("Prune cancelled"))
			return nil
		}
		toPrune = selected
		deleteBranch = dbToggle
	}

	if len(toPrune) == 0 {
		fmt.Println(styles.Muted.Render("Nothing selected"))
		return nil
	}

	return executePrune(ctx, toPrune, deleteBranch)
}

// filterKohWorktrees keeps only worktrees living inside this repo's .koh
// directory, so koh never touches the main repo or manual worktrees. Both
// list and prune use this same predicate so they agree on what a koh
// worktree is. Worktrees under the pre-rename .ko directory are included so
// they stay visible to cleanup and prune.
func filterKohWorktrees(worktrees []git.WorktreeInfo, kohDir string) []git.WorktreeInfo {
	legacyDir := filepath.Join(filepath.Dir(kohDir), ".ko")
	var out []git.WorktreeInfo
	for _, wt := range worktrees {
		if pathInside(wt.Path, kohDir) || pathInside(wt.Path, legacyDir) {
			out = append(out, wt)
		}
	}
	return out
}

// buildPruneCandidates classifies the given koh worktrees and converts each
// into a pruneCandidate. The current worktree is flagged (and never
// pre-selected) so no consumer — picker, --yes, or --dry-run — prunes it.
func buildPruneCandidates(ctx context.Context, worktrees []git.WorktreeInfo) ([]pruneCandidate, error) {
	classified, err := git.ClassifyWorktrees(ctx, worktrees)
	if err != nil {
		return nil, err
	}

	var currentPath string
	if git.IsInWorktree() {
		currentPath, err = git.GetCurrentWorktreePath()
		if err != nil {
			// The current-worktree guard depends on this path; without it we
			// could offer the user's own worktree for deletion.
			return nil, fmt.Errorf("failed to resolve current worktree path: %w", err)
		}
	}

	out := make([]pruneCandidate, 0, len(classified))
	for _, wt := range classified {
		isCurrent := currentPath != "" && samePath(wt.Path, currentPath)
		out = append(out, pruneCandidate{
			wt:        wt,
			name:      filepath.Base(wt.Path),
			selected:  wt.IsPrunable() && !isCurrent,
			isCurrent: isCurrent,
		})
	}
	return out, nil
}

// canonicalPath resolves symlinks and absolutizes a path so paths reported
// by different git commands (porcelain list vs rev-parse) compare equal even
// when one goes through a symlink (e.g. /tmp vs /private/tmp on macOS).
func canonicalPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	// The path itself may not exist (gone worktrees) — resolve its parent.
	if resolvedDir, err := filepath.EvalSymlinks(filepath.Dir(abs)); err == nil {
		return filepath.Join(resolvedDir, filepath.Base(abs))
	}
	return abs
}

func samePath(a, b string) bool {
	return canonicalPath(a) == canonicalPath(b)
}

// pathInside reports whether child is the same as parent or nested below it.
func pathInside(child, parent string) bool {
	rel, err := filepath.Rel(canonicalPath(parent), canonicalPath(child))
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	// A bare ".." prefix would also exclude legitimate names like "..archive".
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func filterPrunable(candidates []pruneCandidate) []pruneCandidate {
	var out []pruneCandidate
	for _, c := range candidates {
		// isCurrent is excluded here — not just in the picker — so --yes and
		// --dry-run honor the "current worktree is never pruned" invariant.
		if c.wt.IsPrunable() && !c.isCurrent {
			out = append(out, c)
		}
	}
	return out
}

func printDryRun(candidates []pruneCandidate) {
	fmt.Println(styles.RenderTitle(styles.IconTree + " Prune (dry run)"))
	for _, c := range candidates {
		fmt.Printf("  %s %s %s %s\n",
			styles.Muted.Render(styles.IconBullet),
			c.name,
			styles.Muted.Render(styles.IconBranch+" "+displayBranch(c.wt)),
			renderReasons(c.wt.Reasons))
	}
	fmt.Println()
	fmt.Println(styles.Muted.Render(fmt.Sprintf("%d worktree(s) would be removed. Run without --dry-run to proceed.", len(candidates))))
}

// executePrune removes the chosen worktrees, closes their tmux windows, and
// optionally deletes their branches. Failures on a single worktree are
// reported but do not stop the loop; an error is returned when any worktree
// failed so the process exits non-zero.
func executePrune(ctx context.Context, candidates []pruneCandidate, deleteBranch bool) error {
	inTmux := tmux.IsInTmux()

	var pruned, skipped, failed int
	var sawGone bool
	var branches []string

	for _, c := range candidates {
		if ctx.Err() != nil {
			fmt.Println(styles.Muted.Render("Stopping: operation cancelled"))
			break
		}

		fmt.Printf("%s %s\n", styles.Active.Render(styles.IconArrow), c.name)

		if c.wt.HasReason(git.ReasonGone) {
			// Directory already missing on disk — the PruneRefs call below
			// drops the stale gitdir reference. Calling "git worktree remove"
			// here would just error out.
			sawGone = true
			if _, err := os.Stat(c.wt.Path); err == nil {
				// git also flags worktrees with a broken gitdir file as
				// prunable while their directory still exists — leave the
				// files alone and say so rather than orphan them silently.
				fmt.Printf("  %s directory still exists, leaving files in place: %s\n",
					styles.Muted.Render("warn"), c.wt.Path)
			}
		} else {
			dirty, err := git.HasUncommittedChanges(ctx, c.wt.Path)
			if err != nil {
				fmt.Printf("  %s %v\n", styles.ErrorMessage.Render(styles.IconCross), err)
				failed++
				continue
			}
			if dirty {
				// Classification only looks at branch tips; never force-remove
				// a worktree that still holds uncommitted work.
				fmt.Printf("  %s uncommitted changes — skipped (use 'koh cleanup %s' to remove anyway)\n",
					styles.Muted.Render("skip"), c.name)
				skipped++
				continue
			}
			if err := git.RemoveWorktreeWithContext(ctx, c.wt.Path); err != nil {
				fmt.Printf("  %s %v\n", styles.ErrorMessage.Render(styles.IconCross), err)
				failed++
				continue
			}
			// git worktree remove can leave the directory behind — match cleanup.go.
			if err := os.RemoveAll(c.wt.Path); err != nil {
				fmt.Printf("  %s remove dir: %v\n", styles.Muted.Render("warn"), err)
			}
		}

		// Close the window only after the worktree is dealt with, so a failed
		// removal doesn't kill whatever is still running in it (matches cleanup.go).
		if inTmux {
			if err := tmux.CloseWindow("", c.name); err != nil {
				// Missing window is the common case (tmux already closed) — only log unexpected errors.
				if !strings.Contains(err.Error(), "no tmux window found") {
					fmt.Printf("  %s tmux: %v\n", styles.Muted.Render("warn"), err)
				}
			}
		}

		if deleteBranch && c.wt.Branch != "" {
			branches = append(branches, c.wt.Branch)
		}
		pruned++
	}

	// Drop administrative refs for gone worktrees before deleting branches —
	// git considers a branch checked out (and refuses to delete it) until its
	// stale worktree entry is pruned. Only run when needed: "git worktree
	// prune" acts repo-wide, not just on the selected candidates.
	if sawGone {
		if err := git.PruneRefs(ctx); err != nil {
			fmt.Printf("%s %v\n", styles.Muted.Render("warn"), err)
		}
	}

	for _, branch := range branches {
		if err := git.DeleteBranch(ctx, branch); err != nil {
			fmt.Printf("%s branch %s: %v\n", styles.Muted.Render("warn"), branch, err)
		} else {
			fmt.Printf("%s deleted branch %s\n", styles.SuccessMessage.Render(styles.IconCheck), branch)
		}
	}

	summary := fmt.Sprintf("Pruned %d worktree(s)", pruned)
	if skipped > 0 {
		summary += fmt.Sprintf(", %d skipped", skipped)
	}
	if failed > 0 {
		summary += fmt.Sprintf(", %d failed", failed)
	}
	fmt.Println()
	if failed > 0 {
		fmt.Println(styles.ErrorMessage.Render(styles.IconCross + " " + summary))
		return fmt.Errorf("%d worktree(s) failed to prune", failed)
	}
	fmt.Println(styles.SuccessMessage.Render(styles.IconCheck + " " + summary))
	return nil
}

func displayBranch(wt git.WorktreeInfo) string {
	if wt.Branch != "" {
		return wt.Branch
	}
	if wt.Detached {
		return "(detached)"
	}
	return "(unknown)"
}

var reasonTagStyle = lipgloss.NewStyle().Foreground(styles.Warning)

func renderReasons(reasons []git.PruneReason) string {
	if len(reasons) == 0 {
		return ""
	}
	parts := make([]string, len(reasons))
	for i, r := range reasons {
		parts[i] = reasonTagStyle.Render(string(r))
	}
	return styles.Muted.Render("(") + strings.Join(parts, styles.Muted.Render(", ")) + styles.Muted.Render(")")
}

// --- Bubbletea TUI ---

type pruneModel struct {
	candidates   []pruneCandidate
	cursor       int
	deleteBranch bool
	confirm      bool
	cancelled    bool
}

func runPruneTUI(ctx context.Context, candidates []pruneCandidate, deleteBranch bool) ([]pruneCandidate, bool, bool, error) {
	m := pruneModel{
		candidates:   candidates,
		deleteBranch: deleteBranch,
	}
	// Set cursor to the first pre-selected entry to give the user a useful starting point.
	for i, c := range candidates {
		if c.selected {
			m.cursor = i
			break
		}
	}
	// Wire the signal-cancellable context in so SIGTERM tears the picker
	// down instead of leaving it running over a dead context.
	p := tea.NewProgram(m, tea.WithContext(ctx))
	final, err := p.Run()
	if err != nil {
		return nil, deleteBranch, false, err
	}
	fm, ok := final.(pruneModel)
	if !ok {
		return nil, deleteBranch, false, fmt.Errorf("unexpected model type %T", final)
	}
	if fm.cancelled || !fm.confirm {
		return nil, fm.deleteBranch, true, nil
	}
	var selected []pruneCandidate
	for _, c := range fm.candidates {
		if c.selected && !c.isCurrent {
			selected = append(selected, c)
		}
	}
	return selected, fm.deleteBranch, false, nil
}

func (m pruneModel) Init() tea.Cmd { return nil }

func (m pruneModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch keyMsg.String() {
	case "q", "esc", "ctrl+c":
		m.cancelled = true
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.candidates)-1 {
			m.cursor++
		}
	case "g", "home":
		m.cursor = 0
	case "G", "end":
		if len(m.candidates) > 0 {
			m.cursor = len(m.candidates) - 1
		}
	case " ", "space", "x":
		if m.cursor < len(m.candidates) && !m.candidates[m.cursor].isCurrent {
			m.candidates[m.cursor].selected = !m.candidates[m.cursor].selected
		}
	case "a":
		for i := range m.candidates {
			if !m.candidates[i].isCurrent {
				m.candidates[i].selected = true
			}
		}
	case "n":
		for i := range m.candidates {
			m.candidates[i].selected = false
		}
	case "d":
		m.deleteBranch = !m.deleteBranch
	case "enter":
		m.confirm = true
		return m, tea.Quit
	}
	return m, nil
}

func (m pruneModel) View() string {
	if m.cancelled {
		return ""
	}

	var s strings.Builder
	s.WriteString("\n" + styles.RenderTitle(styles.IconTree+" Koh Prune") + "\n\n")

	if m.deleteBranch {
		s.WriteString(styles.Active.Render("Branch deletion: ON") + "  " +
			styles.Muted.Render("(local branches will be removed alongside their worktrees)") + "\n\n")
	}

	for i, c := range m.candidates {
		cursor := "  "
		if m.cursor == i {
			cursor = styles.Active.Render("▶ ")
		}

		box := "[ ]"
		if c.selected {
			box = styles.SuccessMessage.Render("[" + styles.IconCheck + "]")
		}

		nameStyled := c.name
		if c.isCurrent {
			nameStyled = styles.Muted.Render(c.name + " [current]")
			box = styles.Muted.Render("[-]")
		}

		branch := styles.Muted.Render(styles.IconBranch + " " + displayBranch(c.wt))
		fmt.Fprintf(&s, "%s%s %s %s %s\n", cursor, box, nameStyled, branch, renderReasons(c.wt.Reasons))
	}

	s.WriteString("\n")
	help := "space: toggle • a: all • n: none • d: toggle delete-branch • enter: prune • q: cancel"
	s.WriteString(styles.RenderHelp(help))
	s.WriteString("\n")
	return s.String()
}
