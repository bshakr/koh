package cmd

import (
	"fmt"
	"strings"

	"github.com/bshakr/koh/internal/config"
	"github.com/bshakr/koh/internal/styles"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Interactive configuration setup",
	Long:  `Run an interactive wizard to configure koh settings.`,
	RunE:  runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)
}

type step int

const (
	stepSetupScript step = iota
	stepAddPaneChoice
	stepPaneCommand
	stepConfirm
	stepDone
)

type initModel struct {
	setupInput   textinput.Model
	paneInput    textinput.Model
	paneCommands []string
	err          error
	config       *config.Config
	step         step
	choice       int // 0 = add pane, 1 = finish setup
}

func initialModel() initModel {
	cfg := config.DefaultConfig()

	// Setup script input
	setupInput := textinput.New()
	setupInput.Placeholder = "./bin/setup"
	setupInput.SetValue(cfg.SetupScript)
	setupInput.Focus()
	setupInput.CharLimit = 100
	setupInput.Width = 50
	setupInput.Prompt = "❯ "

	// Pane command input
	paneInput := textinput.New()
	paneInput.Placeholder = "vim"
	paneInput.CharLimit = 100
	paneInput.Width = 50
	paneInput.Prompt = "❯ "

	return initModel{
		step:         stepSetupScript,
		config:       cfg,
		setupInput:   setupInput,
		paneInput:    paneInput,
		paneCommands: []string{},
		choice:       0,
	}
}

func (m initModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m initModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			return m, tea.Quit

		case "enter":
			switch m.step {
			case stepSetupScript:
				// Save setup script and move to choice
				m.config.SetupScript = m.setupInput.Value()
				m.step = stepAddPaneChoice
				return m, nil

			case stepAddPaneChoice:
				if m.choice == 0 {
					// User chose "Add pane"
					m.paneInput.SetValue("")
					m.paneInput.Focus()
					m.step = stepPaneCommand
				} else {
					// User chose "Finish setup"
					m.step = stepConfirm
				}
				return m, nil

			case stepPaneCommand:
				// Save pane command and go back to choice
				if m.paneInput.Value() != "" {
					m.paneCommands = append(m.paneCommands, m.paneInput.Value())
				}
				m.paneInput.SetValue("")
				m.step = stepAddPaneChoice
				return m, nil

			case stepConfirm:
				// Save configuration (always overwrites existing config)
				m.config.SetupScript = m.setupInput.Value()
				m.config.PaneCommands = m.paneCommands

				if err := m.config.Save(); err != nil {
					m.err = err
				}
				m.step = stepDone
				return m, tea.Quit

			case stepDone:
				return m, tea.Quit
			}

		case "up", "down":
			// Toggle choice in stepAddPaneChoice
			if m.step == stepAddPaneChoice {
				m.choice = 1 - m.choice // Toggle between 0 and 1
				return m, nil
			}
		}
	}

	// Update text inputs
	var cmd tea.Cmd
	switch m.step {
	case stepSetupScript:
		m.setupInput, cmd = m.setupInput.Update(msg)
	case stepPaneCommand:
		m.paneInput, cmd = m.paneInput.Update(msg)
	}

	return m, cmd
}

func (m initModel) View() string {
	var b strings.Builder

	// Title
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(styles.Primary).
		MarginBottom(1).
		Render(styles.IconConfig + " Koh Configuration Setup")
	b.WriteString("\n" + title + "\n\n")

	switch m.step {
	case stepSetupScript:
		b.WriteString(styles.Subtitle.Render("Path to your setup script?"))
		b.WriteString("\n\n  ")
		b.WriteString(m.setupInput.View())
		b.WriteString("\n\n")
		b.WriteString(styles.Muted.Render("  This script runs once when creating a new worktree"))
		b.WriteString("\n")
		b.WriteString("\n")
		b.WriteString(styles.Help.Render("  Press Enter to continue, Ctrl+C to cancel"))
		b.WriteString("\n")

	case stepAddPaneChoice:
		b.WriteString(styles.Subtitle.Render("What would you like to do?"))
		b.WriteString("\n\n")

		// Show added panes
		if len(m.paneCommands) > 0 {
			b.WriteString(styles.Muted.Render("  Panes added so far:"))
			b.WriteString("\n")
			for i, cmd := range m.paneCommands {
				fmt.Fprintf(&b, "    %d. %s\n", i+1, styles.Key.Render(cmd))
			}
			b.WriteString("\n")
		}

		// Show choices
		addPaneStyle := lipgloss.NewStyle()
		finishStyle := lipgloss.NewStyle()

		if m.choice == 0 {
			addPaneStyle = addPaneStyle.Foreground(styles.Primary).Bold(true)
			b.WriteString("  " + addPaneStyle.Render("❯ Add a pane") + "\n")
			b.WriteString("  " + finishStyle.Render("  Finish setup") + "\n")
		} else {
			finishStyle = finishStyle.Foreground(styles.Primary).Bold(true)
			b.WriteString("  " + addPaneStyle.Render("  Add a pane") + "\n")
			b.WriteString("  " + finishStyle.Render("❯ Finish setup") + "\n")
		}

		b.WriteString("\n")
		b.WriteString(styles.Help.Render("  Use ↑/↓ to select, Enter to confirm, Ctrl+C to cancel"))
		b.WriteString("\n")

	case stepPaneCommand:
		b.WriteString(styles.Subtitle.Render(fmt.Sprintf("Command for pane %d?", len(m.paneCommands)+1)))
		b.WriteString("\n\n  ")
		b.WriteString(m.paneInput.View())
		b.WriteString("\n\n")
		b.WriteString(styles.Muted.Render("  Enter the command to run in this pane"))
		b.WriteString("\n")
		b.WriteString("\n")
		b.WriteString(styles.Help.Render("  Press Enter to add pane, Ctrl+C to cancel"))
		b.WriteString("\n")

	case stepConfirm:
		b.WriteString(styles.Subtitle.Render("Review your configuration:"))
		b.WriteString("\n\n")

		var content string
		content += styles.RenderKeyValue("Setup Script", m.setupInput.Value()) + "\n"
		content += "\n"
		if len(m.paneCommands) > 0 {
			content += styles.Key.Render("Pane Commands:") + "\n"
			for i, cmd := range m.paneCommands {
				content += fmt.Sprintf("  %d. %s\n", i+1, styles.Key.Render(cmd))
			}
		} else {
			content += styles.Muted.Render("No pane commands configured") + "\n"
		}

		box := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(styles.Subtle).
			Padding(1, 2).
			Render(content)

		b.WriteString(box)
		b.WriteString("\n\n")
		b.WriteString(styles.Help.Render("  Press Enter to save, Ctrl+C to cancel"))
		b.WriteString("\n")

	case stepDone:
		if m.err != nil {
			b.WriteString(styles.RenderError("Error saving configuration: " + m.err.Error()))
		} else {
			configPath, _ := config.ConfigPath()
			b.WriteString(styles.RenderSuccess("Configuration saved!"))
			b.WriteString("\n\n")
			b.WriteString(styles.Muted.Render("  Config location: " + configPath))
		}
		b.WriteString("\n")
	}

	return b.String()
}

func runInit(_ *cobra.Command, _ []string) error {
	p := tea.NewProgram(initialModel())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("error running interactive setup: %w", err)
	}
	return nil
}
