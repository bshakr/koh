// Package cmd implements the CLI commands for koh.
//
// Koh is a tool for managing git worktrees with automatic tmux session setup.
// It provides commands to create, list, and clean up worktrees with pre-configured
// development environments.
//
// The main commands are:
//   - new: Create a new worktree with a tmux session
//   - switch: Switch to an existing worktree's tmux session
//   - cleanup: Remove a worktree and close its tmux session
//   - list: Display all koh-managed worktrees
//   - init: Interactive configuration wizard
//   - config: Display current configuration
//
// Each command is implemented in its own file (new.go, switch.go, cleanup.go, etc.).
package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/bshakr/koh/internal/config"
	"github.com/bshakr/koh/internal/git"
	"github.com/bshakr/koh/internal/styles"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "koh",
	Short: "Git Worktree tmux Automation",
	Long: `koh - Git Worktree tmux Automation

A tool for managing git worktrees with automatic tmux session setup.
Creates isolated development environments with pre-configured panes.`,
	Run: runRoot,
}

func runRoot(_ *cobra.Command, _ []string) {
	// Get actual terminal width
	terminalWidth := styles.GetTerminalWidth()

	// Print large ASCII title
	asciiTitle := `
██╗  ██╗ ██████╗ ██╗  ██╗
██║ ██╔╝██╔═══██╗██║  ██║
█████╔╝ ██║   ██║███████║
██╔═██╗ ██║   ██║██╔══██║
██║  ██╗╚██████╔╝██║  ██║
╚═╝  ╚═╝ ╚═════╝ ╚═╝  ╚═╝`

	koTitle := lipgloss.NewStyle().
		Bold(true).
		Foreground(styles.Primary).
		Align(lipgloss.Center).
		Width(terminalWidth).
		Render(asciiTitle)

	subtitle := lipgloss.NewStyle().
		Foreground(styles.Subtle).
		Align(lipgloss.Center).
		Width(terminalWidth).
		Render("Git Worktree Manager")

	// Top decorative border
	topBorder := lipgloss.NewStyle().
		Foreground(styles.Subtle).
		Align(lipgloss.Center).
		Width(terminalWidth).
		Render(strings.Repeat("─", 60))

	fmt.Println()
	fmt.Println(topBorder)
	fmt.Println(koTitle)
	fmt.Println(subtitle)
	fmt.Println(topBorder)
	fmt.Println()

	// Check if in git repo
	if !git.IsGitRepo() {
		errorMsg := lipgloss.NewStyle().
			Align(lipgloss.Center).
			Width(terminalWidth).
			Render(styles.ErrorMessage.Render("Not in a git repository"))

		helpMsg := lipgloss.NewStyle().
			Align(lipgloss.Center).
			Width(terminalWidth).
			Render(styles.Muted.Render("Please run koh from within a git repository"))

		fmt.Println(errorMsg)
		fmt.Println(helpMsg)
		fmt.Println()
		return
	}

	// Get repository info
	repoName, _ := git.GetRepoName()

	// Get worktree count and current worktree
	worktreeCount := 0
	currentWorktree := ""

	var mainRepoRoot string
	var currentWorktreePath string

	if git.IsInWorktree() {
		mainRepoRoot, _ = git.GetMainRepoRoot()
		currentWorktreePath, _ = git.GetCurrentWorktreePath()
		currentWorktree = filepath.Base(currentWorktreePath)
	} else {
		mainRepoRoot, _ = os.Getwd()
		currentWorktree = "main"
	}

	// Count worktrees
	if mainRepoRoot != "" {
		kohDir := filepath.Join(mainRepoRoot, ".koh")
		if _, err := os.Stat(kohDir); err == nil {
			ctx := context.Background()
			gitCmd := exec.CommandContext(ctx, "git", "worktree", "list")
			output, err := gitCmd.Output()
			if err == nil {
				lines := strings.Split(string(output), "\n")
				for _, line := range lines {
					if strings.Contains(line, "/.koh/") {
						worktreeCount++
					}
				}
			}
		}
	}

	// Check config status
	configExists, _ := config.ConfigExists()
	configStatus := styles.ErrorMessage.Render(styles.IconCross + " Not configured")
	if configExists {
		configStatus = styles.SuccessMessage.Render(styles.IconCheck + " Configured")
	}

	// Build status section with enhanced visual hierarchy
	statusHeader := lipgloss.NewStyle().
		Bold(true).
		Foreground(styles.Primary).
		Render("  STATUS  ")

	var statusContent strings.Builder
	statusContent.WriteString(styles.RenderKeyValue("Version", Version) + "\n")
	statusContent.WriteString(styles.RenderKeyValue("Repository", repoName) + "\n")
	statusContent.WriteString(styles.RenderKeyValue("Worktrees", fmt.Sprintf("%d active", worktreeCount)) + "\n")
	statusContent.WriteString(styles.RenderKeyValue("Current", currentWorktree) + "\n")
	statusContent.WriteString(styles.Key.Render("Config:") + " " + configStatus)

	statusBox := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(styles.Primary).
		Padding(0, 1).
		Render(statusContent.String())

	statusWithHeader := lipgloss.JoinVertical(lipgloss.Center, statusHeader, statusBox)

	// Center the status box
	centeredStatusBox := lipgloss.NewStyle().
		Align(lipgloss.Center).
		Width(terminalWidth).
		Render(statusWithHeader)

	fmt.Println(centeredStatusBox)
	fmt.Println()

	// Section divider
	sectionDivider := lipgloss.NewStyle().
		Foreground(styles.Subtle).
		Align(lipgloss.Center).
		Width(terminalWidth).
		Render("◆ ◆ ◆")
	fmt.Println(sectionDivider)
	fmt.Println()

	// Quick Start section with enhanced header
	quickStartIcon := "⚡"
	quickStartTitle := lipgloss.NewStyle().
		Align(lipgloss.Center).
		Width(terminalWidth).
		Render(styles.Subtitle.Render(quickStartIcon + " Quick Start"))
	fmt.Println(quickStartTitle)

	quickStart := []struct {
		icon    string
		command string
		desc    string
	}{
		{"➜", "koh new <name>", "Create a new worktree"},
		{"🔄", "koh switch <name>", "Switch to a worktree"},
		{"📋", "koh list", "View all worktrees"},
		{"⚙", "koh config", "Show configuration"},
	}

	// Find max command width for alignment
	maxCmdWidth := 0
	for _, qs := range quickStart {
		if len(qs.command) > maxCmdWidth {
			maxCmdWidth = len(qs.command)
		}
	}

	// Style for command column (colored but no background)
	cmdStyle := lipgloss.NewStyle().Foreground(styles.Warning)

	// Find max line length for this section
	maxLineLen := 0
	for _, qs := range quickStart {
		lineLen := 2 + maxCmdWidth + 3 + len(qs.desc) // icon + cmd + spacing + desc
		if lineLen > maxLineLen {
			maxLineLen = lineLen
		}
	}

	for _, qs := range quickStart {
		// Manually pad the command to max width
		paddedCmd := fmt.Sprintf("%-*s", maxCmdWidth, qs.command)

		// Apply styling to the padded command
		styledCmd := cmdStyle.Render(paddedCmd)

		// Build the line with icon, proper spacing
		line := qs.icon + " " + styledCmd + "   " + styles.Muted.Render(qs.desc)

		// Pad the entire line to max line length for consistent centering
		lineLenWithoutANSI := 2 + maxCmdWidth + 3 + len(qs.desc)
		paddingNeeded := maxLineLen - lineLenWithoutANSI
		if paddingNeeded > 0 {
			line = line + strings.Repeat(" ", paddingNeeded)
		}

		// Center the entire line
		centered := lipgloss.NewStyle().
			Align(lipgloss.Center).
			Width(terminalWidth).
			Render(line)
		fmt.Println(centered)
	}
	fmt.Println()

	// Section divider
	fmt.Println(sectionDivider)
	fmt.Println()

	// Common Workflows section with enhanced header
	workflowIcon := "🔄"
	workflowsTitle := lipgloss.NewStyle().
		Align(lipgloss.Center).
		Width(terminalWidth).
		Render(styles.Subtitle.Render(workflowIcon + " Common Workflows"))
	fmt.Println(workflowsTitle)

	workflows := []struct {
		icon    string
		name    string
		command string
	}{
		{"🚀", "Start new feature", "koh new feature-name"},
		{"📊", "List all worktrees", "koh list"},
		{"🧹", "Clean up old work", "koh cleanup <name>"},
	}

	// Find max workflow name width for alignment
	maxNameWidth := 0
	for _, wf := range workflows {
		if len(wf.name) > maxNameWidth {
			maxNameWidth = len(wf.name)
		}
	}

	// Find max line length for this section
	maxWorkflowLineLen := 0
	for _, wf := range workflows {
		lineLen := 2 + maxNameWidth + 3 + len(wf.command) // icon + name + spacing + command
		if lineLen > maxWorkflowLineLen {
			maxWorkflowLineLen = lineLen
		}
	}

	for _, wf := range workflows {
		// Manually pad the workflow name to max width
		paddedName := fmt.Sprintf("%-*s", maxNameWidth, wf.name)

		// Apply styling
		styledName := styles.Key.Render(paddedName)
		styledCommand := styles.Muted.Render(wf.command)

		// Build the line with icon and proper spacing
		line := wf.icon + " " + styledName + "   " + styledCommand

		// Pad the entire line to max line length for consistent centering
		lineLenWithoutANSI := 2 + maxNameWidth + 3 + len(wf.command)
		paddingNeeded := maxWorkflowLineLen - lineLenWithoutANSI
		if paddingNeeded > 0 {
			line = line + strings.Repeat(" ", paddingNeeded)
		}

		// Center the entire line
		centered := lipgloss.NewStyle().
			Align(lipgloss.Center).
			Width(terminalWidth).
			Render(line)
		fmt.Println(centered)
	}
	fmt.Println()

	// Section divider
	fmt.Println(sectionDivider)
	fmt.Println()

	// Commands section with enhanced header
	commandsIcon := "📦"
	commandsTitle := lipgloss.NewStyle().
		Align(lipgloss.Center).
		Width(terminalWidth).
		Render(styles.Subtitle.Render(commandsIcon + " Commands"))
	fmt.Println(commandsTitle)

	cmdGroups := []struct {
		icon  string
		title string
		cmds  []struct {
			name string
			desc string
		}
	}{
		{
			"⎇",
			"Worktree Management",
			[]struct {
				name string
				desc string
			}{
				{"new", "Create new worktree + tmux session"},
				{"switch", "Switch to existing worktree session"},
				{"list", "List all worktrees"},
				{"cleanup", "Remove worktree and close session"},
				{"prune", "Remove merged or stale worktrees in bulk"},
			},
		},
		{
			"⚙",
			"Configuration",
			[]struct {
				name string
				desc string
			}{
				{"init", "Interactive setup wizard"},
				{"config", "View current configuration"},
			},
		},
		{
			"❓",
			"Help",
			[]struct {
				name string
				desc string
			}{
				{"version", "Display koh version"},
				{"help", "Show help for any command"},
			},
		},
	}

	// Find max command name width across all groups for consistent alignment
	maxCmdNameWidth := 0
	for _, group := range cmdGroups {
		for _, cmd := range group.cmds {
			if len(cmd.name) > maxCmdNameWidth {
				maxCmdNameWidth = len(cmd.name)
			}
		}
	}

	// Find max line length using the padded command width
	maxCmdLineLen := 0
	for _, group := range cmdGroups {
		for _, cmd := range group.cmds {
			lineLen := maxCmdNameWidth + 3 + len(cmd.desc)
			if lineLen > maxCmdLineLen {
				maxCmdLineLen = lineLen
			}
		}
	}

	for i, group := range cmdGroups {
		if i > 0 {
			fmt.Println()
		}

		// Enhanced group title with icon and decorative border
		groupTitleText := group.icon + " " + group.title
		groupTitleStyled := lipgloss.NewStyle().
			Bold(true).
			Foreground(styles.Primary).
			Render(groupTitleText)

		groupTitleBox := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(styles.Subtle).
			Padding(0, 1).
			Render(groupTitleStyled)

		groupTitle := lipgloss.NewStyle().
			Align(lipgloss.Center).
			Width(terminalWidth).
			Render(groupTitleBox)
		fmt.Println(groupTitle)

		for _, cmd := range group.cmds {
			// Manually pad command name to max width
			paddedName := fmt.Sprintf("%-*s", maxCmdNameWidth, cmd.name)

			// Apply styling to command name (highlighted)
			styledCmdName := styles.Key.Render(paddedName)

			// Build the line with proper spacing
			line := "  " + styledCmdName + "   " + styles.Muted.Render(cmd.desc)

			// Pad the entire line to max line length for consistent centering
			lineLenWithoutANSI := 2 + maxCmdNameWidth + 3 + len(cmd.desc)
			paddingNeeded := maxCmdLineLen + 2 - lineLenWithoutANSI
			if paddingNeeded > 0 {
				line = line + strings.Repeat(" ", paddingNeeded)
			}

			// Center the line
			centered := lipgloss.NewStyle().
				Align(lipgloss.Center).
				Width(terminalWidth).
				Render(line)
			fmt.Println(centered)
		}
	}

	fmt.Println()

	// Section divider
	fmt.Println(sectionDivider)
	fmt.Println()

	// Context-aware tip with enhanced styling
	var tip string
	if !configExists {
		tip = "💡 Tip: Run 'koh init' to set up your configuration first"
	} else if worktreeCount == 0 {
		tip = "💡 Tip: Run 'koh new feature-name' to create your first worktree"
	} else {
		tip = "💡 Tip: Use 'koh list' to see all your worktrees"
	}

	tipBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.Warning).
		Padding(0, 1).
		Foreground(styles.Warning).
		Render(tip)

	centeredTip := lipgloss.NewStyle().
		Align(lipgloss.Center).
		Width(terminalWidth).
		Render(tipBox)
	fmt.Println(centeredTip)
	fmt.Println()

	// Bottom decorative border
	bottomBorder := lipgloss.NewStyle().
		Foreground(styles.Subtle).
		Align(lipgloss.Center).
		Width(terminalWidth).
		Render(strings.Repeat("─", 60))
	fmt.Println(bottomBorder)
	fmt.Println()
}

// Execute runs the root command and handles any errors.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	// Customize help template to use our custom usage function
	rootCmd.SetHelpTemplate(getCustomHelpTemplate())
	rootCmd.SetUsageFunc(customUsageFunc)
}

// getCustomHelpTemplate returns a custom help template with enhanced styling
func getCustomHelpTemplate() string {
	// Return simple template since we handle everything in the UsageFunc
	return `{{.UsageString}}`
}

// Helper functions for rendering

// fprintln is a wrapper around fmt.Fprintln that ignores errors
// This is safe because we're writing to cmd.OutOrStdout() which is typically stdout
func fprintln(w interface {
	Write(p []byte) (n int, err error)
}, a ...interface{},
) {
	_, _ = fmt.Fprintln(w, a...)
}

// renderBorder creates a centered decorative border
func renderBorder(terminalWidth int) string {
	return lipgloss.NewStyle().
		Foreground(styles.Subtle).
		Align(lipgloss.Center).
		Width(terminalWidth).
		Render(strings.Repeat("─", 60))
}

// renderDivider creates a centered section divider
func renderDivider(terminalWidth int) string {
	return lipgloss.NewStyle().
		Foreground(styles.Subtle).
		Align(lipgloss.Center).
		Width(terminalWidth).
		Render("◆ ◆ ◆")
}

// renderCentered centers text within the terminal width
func renderCentered(text string, terminalWidth int) string {
	return lipgloss.NewStyle().
		Align(lipgloss.Center).
		Width(terminalWidth).
		Render(text)
}

// renderGroupTitle creates a styled, centered group title box
func renderGroupTitle(icon, title string, terminalWidth int) string {
	titleText := icon + " " + title
	titleStyled := lipgloss.NewStyle().
		Bold(true).
		Foreground(styles.Primary).
		Render(titleText)

	titleBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.Subtle).
		Padding(0, 1).
		Render(titleStyled)

	return renderCentered(titleBox, terminalWidth)
}

// renderCommandLine creates a styled, centered command line with proper padding
func renderCommandLine(cmdName, cmdDesc string, cmdWidth, maxLineLen, terminalWidth int) string {
	paddedCmd := fmt.Sprintf("%-*s", cmdWidth, cmdName)
	styledName := styles.Key.Render(paddedCmd)
	styledDesc := styles.Muted.Render(cmdDesc)
	line := styledName + "   " + styledDesc

	// Pad the entire line to max line length for consistent centering
	lineLenWithoutANSI := cmdWidth + 3 + len(cmdDesc)
	paddingNeeded := maxLineLen - lineLenWithoutANSI
	if paddingNeeded > 0 {
		line = line + strings.Repeat(" ", paddingNeeded)
	}

	return renderCentered(line, terminalWidth)
}

// customUsageFunc provides custom help/usage formatting
func customUsageFunc(cmd *cobra.Command) error {
	// Get actual terminal width
	terminalWidth := styles.GetTerminalWidth()

	// Header with decorative border
	border := renderBorder(terminalWidth)
	title := renderCentered(
		lipgloss.NewStyle().
			Bold(true).
			Foreground(styles.Primary).
			Render("KOH - Git Worktree tmux Automation"),
		terminalWidth,
	)

	out := cmd.OutOrStdout()
	fprintln(out)
	fprintln(out, border)
	fprintln(out, title)
	fprintln(out, border)
	fprintln(out)

	// Usage section
	if cmd.Runnable() {
		usageHeader := renderCentered(
			lipgloss.NewStyle().
				Bold(true).
				Foreground(styles.Primary).
				Render("📖 USAGE"),
			terminalWidth,
		)

		usageText := renderCentered(
			lipgloss.NewStyle().
				Foreground(styles.Warning).
				Render(cmd.UseLine()),
			terminalWidth,
		)

		fprintln(out, usageHeader)
		fprintln(out, usageText)
		fprintln(out)
	}

	// Available Commands section
	if cmd.HasAvailableSubCommands() {
		// Section divider
		fprintln(out, renderDivider(terminalWidth))
		fprintln(out)

		commandsHeader := renderCentered(
			lipgloss.NewStyle().
				Bold(true).
				Foreground(styles.Primary).
				Render("📦 AVAILABLE COMMANDS"),
			terminalWidth,
		)

		fprintln(out, commandsHeader)
		fprintln(out)

		// Group commands by category
		worktreeCommands := []string{}
		configCommands := []string{}
		otherCommands := []string{}

		for _, c := range cmd.Commands() {
			if !c.IsAvailableCommand() {
				continue
			}

			switch c.Name() {
			case "new", "switch", "list", "cleanup", "prune":
				worktreeCommands = append(worktreeCommands, c.Name()+"§"+c.Short)
			case "init", "config":
				configCommands = append(configCommands, c.Name()+"§"+c.Short)
			default:
				otherCommands = append(otherCommands, c.Name()+"§"+c.Short)
			}
		}

		// Find max command width across ALL groups for consistent alignment
		maxCmdWidthGlobal := 0
		for _, cmdInfo := range worktreeCommands {
			parts := strings.Split(cmdInfo, "§")
			if len(parts[0]) > maxCmdWidthGlobal {
				maxCmdWidthGlobal = len(parts[0])
			}
		}
		for _, cmdInfo := range configCommands {
			parts := strings.Split(cmdInfo, "§")
			if len(parts[0]) > maxCmdWidthGlobal {
				maxCmdWidthGlobal = len(parts[0])
			}
		}
		for _, cmdInfo := range otherCommands {
			parts := strings.Split(cmdInfo, "§")
			if len(parts[0]) > maxCmdWidthGlobal {
				maxCmdWidthGlobal = len(parts[0])
			}
		}

		// Find max line length across ALL commands for consistent padding
		maxLineLen := 0
		allCommands := append(append(worktreeCommands, configCommands...), otherCommands...)
		for _, cmdInfo := range allCommands {
			parts := strings.Split(cmdInfo, "§")
			lineLen := maxCmdWidthGlobal + 3 + len(parts[1]) // cmd + spacing + desc
			if lineLen > maxLineLen {
				maxLineLen = lineLen
			}
		}

		// Print grouped commands
		if len(worktreeCommands) > 0 {
			fprintln(out, renderGroupTitle("⎇", "Worktree Management", terminalWidth))

			for _, cmdInfo := range worktreeCommands {
				parts := strings.Split(cmdInfo, "§")
				fprintln(out, renderCommandLine(parts[0], parts[1], maxCmdWidthGlobal, maxLineLen, terminalWidth))
			}
			fprintln(out)
		}

		if len(configCommands) > 0 {
			fprintln(out, renderGroupTitle("⚙", "Configuration", terminalWidth))

			for _, cmdInfo := range configCommands {
				parts := strings.Split(cmdInfo, "§")
				fprintln(out, renderCommandLine(parts[0], parts[1], maxCmdWidthGlobal, maxLineLen, terminalWidth))
			}
			fprintln(out)
		}

		if len(otherCommands) > 0 {
			fprintln(out, renderGroupTitle("❓", "Help & Other", terminalWidth))

			for _, cmdInfo := range otherCommands {
				parts := strings.Split(cmdInfo, "§")
				fprintln(out, renderCommandLine(parts[0], parts[1], maxCmdWidthGlobal, maxLineLen, terminalWidth))
			}
			fprintln(out)
		}
	}

	// Flags section
	if cmd.HasAvailableLocalFlags() {
		fprintln(out, renderDivider(terminalWidth))
		fprintln(out)

		flagsHeader := renderCentered(
			lipgloss.NewStyle().
				Bold(true).
				Foreground(styles.Primary).
				Render("⚑ FLAGS"),
			terminalWidth,
		)

		fprintln(out, flagsHeader)

		// Center flag usages
		flagUsages := cmd.LocalFlags().FlagUsages()
		fprintln(out, renderCentered(flagUsages, terminalWidth))
	}

	// Footer tip
	fprintln(out, renderDivider(terminalWidth))
	fprintln(out)

	tip := "💡 Use \"koh [command] --help\" for more information about a command"
	tipBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.Warning).
		Padding(0, 1).
		Foreground(styles.Warning).
		Render(tip)

	fprintln(out, renderCentered(tipBox, terminalWidth))
	fprintln(out)

	// Bottom border
	fprintln(out, renderBorder(terminalWidth))

	return nil
}
