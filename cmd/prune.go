package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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

By default an interactive picker lets you confirm what gets removed. Use
--yes to skip the picker and remove everything classified as prunable, or
--dry-run to preview without changing anything.`,
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
	if !git.IsGitRepo() {
		return fmt.Errorf("not in a git repository")
	}

	ctx, cleanupSignals := signals.SetupCancellableContext()
	defer cleanupSignals()

	mainRepoRoot, err := git.GetMainRepoRootOrCwd()
	if err != nil {
		return fmt.Errorf("failed to get repository root: %w", err)
	}
	kohDir := filepath.Join(mainRepoRoot, ".koh")
	if _, err := os.Stat(kohDir); os.IsNotExist(err) {
		fmt.Println(styles.Muted.Render("No worktrees found (no .koh directory)"))
		return nil
	} else if err != nil {
		return fmt.Errorf("failed to check .koh directory: %w", err)
	}

	if !pruneNoFetch {
		fmt.Println(styles.Muted.Render("Fetching from origin..."))
		if err := git.FetchPrune(ctx); err != nil {
			fmt.Println(styles.Muted.Render(fmt.Sprintf("Warning: %v (continuing with local state)", err)))
		}
	}

	candidates, err := loadPruneCandidates(ctx, kohDir)
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

	var (
		toPrune       []pruneCandidate
		deleteBranch  = pruneDeleteBranch
		runtimeCancel bool
	)

	if pruneAssumeYes {
		toPrune = prunable
	} else {
		selected, dbToggle, cancel, err := runPruneTUI(candidates, pruneDeleteBranch)
		if err != nil {
			return fmt.Errorf("error running prune picker: %w", err)
		}
		runtimeCancel = cancel
		toPrune = selected
		deleteBranch = dbToggle
	}

	if runtimeCancel {
		fmt.Println(styles.Muted.Render("Prune cancelled"))
		return nil
	}
	if len(toPrune) == 0 {
		fmt.Println(styles.Muted.Render("Nothing selected"))
		return nil
	}

	return executePrune(ctx, toPrune, deleteBranch)
}

// loadPruneCandidates lists worktrees, classifies them, and converts each
// into a pruneCandidate. Worktrees outside the .koh directory (the main
// repo, manual worktrees) are filtered out so we never touch them.
func loadPruneCandidates(ctx context.Context, kohDir string) ([]pruneCandidate, error) {
	worktrees, err := git.ListWorktreesPorcelain(ctx)
	if err != nil {
		return nil, err
	}

	classified, err := git.ClassifyWorktrees(ctx, worktrees)
	if err != nil {
		return nil, err
	}

	var currentPath string
	if git.IsInWorktree() {
		currentPath, _ = git.GetCurrentWorktreePath()
	}

	absKoh, err := filepath.Abs(kohDir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve .koh path: %w", err)
	}

	var out []pruneCandidate
	for _, wt := range classified {
		if !pathInside(wt.Path, absKoh) {
			continue
		}
		out = append(out, pruneCandidate{
			wt:        wt,
			name:      filepath.Base(wt.Path),
			selected:  wt.IsPrunable(),
			isCurrent: currentPath != "" && samePath(wt.Path, currentPath),
		})
	}
	return out, nil
}

// pathInside reports whether child is the same as parent or nested below it.
func pathInside(child, parent string) bool {
	abs, err := filepath.Abs(child)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(parent, abs)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..") && rel != ".."
}

func samePath(a, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return false
	}
	return absA == absB
}

func filterPrunable(candidates []pruneCandidate) []pruneCandidate {
	var out []pruneCandidate
	for _, c := range candidates {
		if c.wt.IsPrunable() {
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
// reported but do not stop the loop.
func executePrune(ctx context.Context, candidates []pruneCandidate, deleteBranch bool) error {
	inTmux := tmux.IsInTmux()
	repoName, _ := git.GetRepoName()

	var pruned, failed int

	for _, c := range candidates {
		fmt.Printf("%s %s\n", styles.Active.Render(styles.IconArrow), c.name)

		if inTmux && repoName != "" {
			windowName := fmt.Sprintf("%s|%s", repoName, c.name)
			if err := tmux.CloseWindow(windowName, c.name); err != nil {
				// Missing window is the common case (tmux already closed) — only log unexpected errors.
				if !strings.Contains(err.Error(), "no tmux window found") {
					fmt.Printf("  %s tmux: %v\n", styles.Muted.Render("warn"), err)
				}
			}
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

		if deleteBranch && c.wt.Branch != "" {
			if err := git.DeleteBranch(ctx, c.wt.Branch); err != nil {
				fmt.Printf("  %s branch: %v\n", styles.Muted.Render("warn"), err)
			} else {
				fmt.Printf("  %s deleted branch %s\n", styles.SuccessMessage.Render(styles.IconCheck), c.wt.Branch)
			}
		}

		pruned++
	}

	// Mop up any administrative refs whose worktree directories were already gone.
	if err := git.PruneRefs(ctx); err != nil {
		fmt.Printf("%s %v\n", styles.Muted.Render("warn"), err)
	}

	summary := fmt.Sprintf("Pruned %d worktree(s)", pruned)
	if failed > 0 {
		summary += fmt.Sprintf(", %d failed", failed)
	}
	fmt.Println()
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

func renderReasons(reasons []git.PruneReason) string {
	if len(reasons) == 0 {
		return ""
	}
	tagStyle := lipgloss.NewStyle().Foreground(styles.Warning)
	parts := make([]string, len(reasons))
	for i, r := range reasons {
		parts[i] = tagStyle.Render(string(r))
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

func runPruneTUI(candidates []pruneCandidate, deleteBranch bool) ([]pruneCandidate, bool, bool, error) {
	m := pruneModel{
		candidates:   candidates,
		deleteBranch: deleteBranch,
	}
	// Set cursor to the first prunable entry to give the user a useful starting point.
	for i, c := range candidates {
		if c.wt.IsPrunable() {
			m.cursor = i
			break
		}
	}
	p := tea.NewProgram(m)
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
