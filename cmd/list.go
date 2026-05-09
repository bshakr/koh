package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bshakr/koh/internal/git"
	"github.com/bshakr/koh/internal/styles"
	"github.com/bshakr/koh/internal/tmux"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all koh worktrees",
	Long:  `List all git worktrees in the .koh directory. Use arrow keys or j/k to navigate, g/G to jump, Enter to switch, q to quit.`,
	RunE:  runList,
}

func init() {
	rootCmd.AddCommand(listCmd)
}

// worktreeItem represents a single worktree in the list
type worktreeItem struct {
	name      string
	branch    string
	path      string
	isCurrent bool
	reasons   []git.PruneReason
}

// listModel is the bubbletea model for the interactive worktree list
type listModel struct {
	worktrees     []worktreeItem
	cursor        int
	selected      string
	quitting      bool
	inTmux        bool
	switchSuccess bool
}

func runList(_ *cobra.Command, _ []string) error {
	// Check if we're in a git repository
	if !git.IsGitRepo() {
		return fmt.Errorf("not in a git repository")
	}

	// Determine the main repo root (handle being inside a worktree)
	mainRepoRoot, err := git.GetMainRepoRootOrCwd()
	if err != nil {
		return fmt.Errorf("failed to get repository root: %w", err)
	}

	// Get current worktree path if we're in a worktree
	var currentWorktreePath string
	if git.IsInWorktree() {
		currentWorktreePath, err = git.GetCurrentWorktreePath()
		if err != nil {
			return fmt.Errorf("failed to get current worktree path: %w", err)
		}
	}

	// Check if .koh directory exists
	koDir := filepath.Join(mainRepoRoot, ".koh")
	if _, err := os.Stat(koDir); err != nil {
		if os.IsNotExist(err) {
			fmt.Println(styles.Muted.Render("No worktrees found (no .koh directory)"))
			return nil
		}
		return fmt.Errorf("failed to check .koh directory: %w", err)
	}

	// List git worktrees via porcelain so we can also surface prune reasons.
	ctx := context.Background()
	rawWorktrees, err := git.ListWorktreesPorcelain(ctx)
	if err != nil {
		return fmt.Errorf("failed to list worktrees: %w", err)
	}

	classified, err := git.ClassifyWorktrees(ctx, rawWorktrees)
	if err != nil {
		// Fall back to unclassified data — reason tags will be empty but the list still works.
		classified = rawWorktrees
	}

	var worktrees []worktreeItem
	for _, wt := range classified {
		if !strings.Contains(wt.Path, "/.koh/") {
			continue
		}
		isCurrent := currentWorktreePath != "" && wt.Path == currentWorktreePath
		branch := wt.Branch
		if branch == "" {
			if wt.Detached {
				branch = "(detached)"
			} else {
				branch = "(unknown)"
			}
		}
		worktrees = append(worktrees, worktreeItem{
			name:      filepath.Base(wt.Path),
			branch:    branch,
			path:      wt.Path,
			isCurrent: isCurrent,
			reasons:   wt.Reasons,
		})
	}

	if len(worktrees) == 0 {
		fmt.Println(styles.Muted.Render("No koh worktrees found"))
		return nil
	}

	// Check if in tmux for switching functionality
	inTmux := tmux.IsInTmux()

	// Create and run the interactive list
	m := listModel{
		worktrees: worktrees,
		cursor:    0,
		inTmux:    inTmux,
	}

	// Set cursor to current worktree if found
	for i, wt := range worktrees {
		if wt.isCurrent {
			m.cursor = i
			break
		}
	}

	p := tea.NewProgram(m)
	finalModel, err := p.Run()
	if err != nil {
		return fmt.Errorf("error running interactive list: %w", err)
	}

	// Check if user selected a worktree to switch to
	if finalModel, ok := finalModel.(listModel); ok {
		if finalModel.selected != "" && inTmux {
			// Switch to the selected worktree using the extracted function
			return switchToWorktree(finalModel.selected, true)
		}
	}

	return nil
}

// Init initializes the bubbletea model
func (m listModel) Init() tea.Cmd {
	return nil
}

// Update handles keyboard input and updates the model
func (m listModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		// Quit keys
		case "q", "esc", "ctrl+c":
			m.quitting = true
			return m, tea.Quit

		// Navigation: arrow keys
		case "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down":
			if m.cursor < len(m.worktrees)-1 {
				m.cursor++
			}

		// Navigation: vim keys
		case "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "j":
			if m.cursor < len(m.worktrees)-1 {
				m.cursor++
			}

		// Navigation: jump to start/end
		case "g", "home":
			m.cursor = 0
		case "G", "end":
			if len(m.worktrees) > 0 {
				m.cursor = len(m.worktrees) - 1
			}

		// Select and switch
		case "enter":
			// Defensive check (should always be true due to navigation bounds and empty list early return)
			if m.inTmux && m.cursor >= 0 && m.cursor < len(m.worktrees) {
				m.selected = m.worktrees[m.cursor].name
				m.switchSuccess = true
				return m, tea.Quit
			}
		}
	}

	return m, nil
}

// View renders the UI
func (m listModel) View() string {
	if m.quitting && !m.switchSuccess {
		return ""
	}

	var s strings.Builder

	// Title
	title := styles.RenderTitle(styles.IconTree + " Koh Worktrees")
	s.WriteString("\n" + title + "\n\n")

	// Worktrees list
	for i, wt := range m.worktrees {
		cursor := "  "
		if m.cursor == i {
			cursor = styles.Active.Render("▶ ")
		}

		var line string
		reasonTag := renderReasons(wt.reasons)
		if wt.isCurrent {
			// Current session in green text (no background)
			greenStyle := lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("2")) // Green

			icon := greenStyle.Render(styles.IconCurrent)
			nameStyled := greenStyle.Render(wt.name)
			branchStyled := greenStyle.Render(styles.IconBranch + " " + wt.branch)
			currentLabel := styles.Muted.Render("[current]")
			line = fmt.Sprintf("%s%s %s %s %s %s", cursor, icon, nameStyled, branchStyled, currentLabel, reasonTag)
		} else {
			icon := styles.Muted.Render(styles.IconBullet)
			nameStyled := wt.name
			branchStyled := styles.Muted.Render(styles.IconBranch + " " + wt.branch)
			line = fmt.Sprintf("%s%s %s %s %s", cursor, icon, nameStyled, branchStyled, reasonTag)
		}
		line = strings.TrimRight(line, " ")

		s.WriteString(line + "\n")
	}

	// Help text
	s.WriteString("\n")
	if m.inTmux {
		help := styles.RenderHelp("↑/↓ or j/k: navigate • g/G: jump to top/bottom • enter: switch • q: quit")
		s.WriteString(help)
	} else {
		help := styles.RenderHelp("↑/↓ or j/k: navigate • g/G: jump to top/bottom • q: quit (not in tmux)")
		s.WriteString(help)
	}
	s.WriteString("\n")

	return s.String()
}
