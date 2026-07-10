package cmd

// Tests for the init wizard. Two concerns live here:
//   - error propagation: `koh init` stored a failed config Save on the model's
//     err field but runInit never read it, so a failed save exited 0.
//   - config seeding: re-running init must pre-fill the wizard from the
//     existing .kohconfig instead of resetting to defaults.
// setupRepo / t.Chdir come from integration_test.go (same package) and keep
// tests off any real tmux server.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/bshakr/koh/internal/config"
	tea "github.com/charmbracelet/bubbletea"
)

// driveInitToSave runs the wizard key sequence that reaches the confirm step
// and triggers a config Save: Enter (setup script) -> Down (select "Finish
// setup") -> Enter (open confirm) -> Enter (save). It returns the final model.
func driveInitToSave(t *testing.T) initModel {
	t.Helper()
	m := initialModel()
	for _, kt := range []tea.KeyType{tea.KeyEnter, tea.KeyDown, tea.KeyEnter, tea.KeyEnter} {
		next, _ := m.Update(tea.KeyMsg{Type: kt})
		got, ok := next.(initModel)
		if !ok {
			t.Fatalf("Update returned %T, want initModel", next)
		}
		m = got
	}
	if m.step != stepDone {
		t.Fatalf("wizard did not reach stepDone, got step %d", m.step)
	}
	return m
}

// TestInitWizardSaveFailureRecordsErr verifies a failed Save is recorded on the
// model and surfaced by finalInitErr — the path that previously exited 0.
func TestInitWizardSaveFailureRecordsErr(t *testing.T) {
	repo := setupRepo(t)
	// A directory at the config path makes os.WriteFile (and so Save) fail
	// deterministically, independent of filesystem permissions.
	if err := os.Mkdir(filepath.Join(repo, ".kohconfig"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)

	m := driveInitToSave(t)
	if m.err == nil {
		t.Fatal("expected a save error to be recorded on the model, got nil")
	}
	if got := finalInitErr(m); got == nil {
		t.Error("finalInitErr must surface the save error so runInit exits non-zero")
	}
}

// TestInitWizardSaveSuccessNoErr verifies the happy path leaves no error and
// actually writes the config, proving the key sequence really reaches Save.
func TestInitWizardSaveSuccessNoErr(t *testing.T) {
	repo := setupRepo(t)
	t.Chdir(repo)

	m := driveInitToSave(t)
	if m.err != nil {
		t.Fatalf("expected no error on successful save, got %v", m.err)
	}
	if got := finalInitErr(m); got != nil {
		t.Errorf("finalInitErr should be nil on success, got %v", got)
	}
	if _, err := os.Stat(filepath.Join(repo, ".kohconfig")); err != nil {
		t.Errorf("expected .kohconfig to be written on success: %v", err)
	}
}

// TestInitWizardCancelRecordsNoErr verifies cancelling (esc / ctrl+c) quits
// without recording an error, so a cancelled wizard still exits 0.
func TestInitWizardCancelRecordsNoErr(t *testing.T) {
	for _, kt := range []tea.KeyType{tea.KeyEsc, tea.KeyCtrlC} {
		next, cmd := initialModel().Update(tea.KeyMsg{Type: kt})
		m, ok := next.(initModel)
		if !ok {
			t.Fatalf("Update returned %T, want initModel", next)
		}
		if m.err != nil {
			t.Errorf("cancel via %q recorded an error: %v", tea.KeyMsg{Type: kt}, m.err)
		}
		if finalInitErr(m) != nil {
			t.Errorf("finalInitErr must be nil for a cancelled wizard (%q)", tea.KeyMsg{Type: kt})
		}
		if cmd == nil {
			t.Fatalf("cancel via %q did not return a quit command", tea.KeyMsg{Type: kt})
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Errorf("cancel via %q should issue tea.Quit", tea.KeyMsg{Type: kt})
		}
	}
}

// TestFinalInitErr pins down the propagation helper in isolation.
func TestFinalInitErr(t *testing.T) {
	sentinel := errors.New("save failed")

	if got := finalInitErr(initModel{err: sentinel}); !errors.Is(got, sentinel) {
		t.Errorf("finalInitErr should return the model's err, got %v", got)
	}
	if got := finalInitErr(initModel{}); got != nil {
		t.Errorf("finalInitErr should be nil when the model has no err, got %v", got)
	}
	// A non-initModel (defensive: type assertion fails) must not panic.
	if got := finalInitErr(stubModel{}); got != nil {
		t.Errorf("finalInitErr should be nil for a foreign model type, got %v", got)
	}
}

// stubModel is a minimal tea.Model that is not initModel, exercising the
// failed type assertion branch of finalInitErr.
type stubModel struct{}

func (stubModel) Init() tea.Cmd                         { return nil }
func (m stubModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, nil }
func (stubModel) View() string                          { return "" }

// writeConfig writes a .kohconfig at the repo root, mirroring config.Save's format.
func writeConfig(t *testing.T, repo string, cfg *config.Config) {
	t.Helper()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".kohconfig"), data, 0600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}
}

// TestInitialModelSeedsFromExistingConfig verifies re-running init edits the
// existing config rather than resetting to defaults: the wizard pre-fills the
// setup script and every pane command from the on-disk .kohconfig.
func TestInitialModelSeedsFromExistingConfig(t *testing.T) {
	repo := setupRepo(t)
	t.Chdir(repo)

	existing := &config.Config{
		SetupScript:  "./bin/custom-setup",
		PaneCommands: []string{"nvim", "npm run dev", "lazygit", "htop", "tail -f log"},
	}
	writeConfig(t, repo, existing)

	m := initialModel()

	if !m.existingConfig {
		t.Error("existingConfig = false, want true when a .kohconfig is present")
	}
	if got := m.setupInput.Value(); got != existing.SetupScript {
		t.Errorf("setupInput = %q, want %q", got, existing.SetupScript)
	}
	if got := len(m.paneCommands); got != len(existing.PaneCommands) {
		t.Fatalf("paneCommands length = %d, want %d", got, len(existing.PaneCommands))
	}
	for i, want := range existing.PaneCommands {
		if m.paneCommands[i] != want {
			t.Errorf("paneCommands[%d] = %q, want %q", i, m.paneCommands[i], want)
		}
	}
}

// TestInitialModelPaneCommandsAreCopied ensures the pre-filled pane list does
// not alias the loaded config's slice, so wizard edits can't corrupt it.
func TestInitialModelPaneCommandsAreCopied(t *testing.T) {
	repo := setupRepo(t)
	t.Chdir(repo)

	writeConfig(t, repo, &config.Config{
		SetupScript:  "./bin/setup",
		PaneCommands: []string{"vim"},
	})

	m := initialModel()
	if len(m.paneCommands) != 1 {
		t.Fatalf("paneCommands length = %d, want 1", len(m.paneCommands))
	}
	// Mutating the model's slice must not reach back into m.config.
	m.paneCommands[0] = "mutated"
	if m.config.PaneCommands[0] != "vim" {
		t.Errorf("config.PaneCommands aliased model slice: got %q, want %q",
			m.config.PaneCommands[0], "vim")
	}
}

// TestInitialModelDefaultsWhenNoConfig verifies that with no .kohconfig on disk
// the wizard starts from defaults and does not claim an existing config.
func TestInitialModelDefaultsWhenNoConfig(t *testing.T) {
	repo := setupRepo(t)
	t.Chdir(repo)

	m := initialModel()

	if m.existingConfig {
		t.Error("existingConfig = true, want false when no .kohconfig is present")
	}
	if got, want := m.setupInput.Value(), config.DefaultConfig().SetupScript; got != want {
		t.Errorf("setupInput = %q, want default %q", got, want)
	}
	if len(m.paneCommands) != 0 {
		t.Errorf("paneCommands = %v, want empty", m.paneCommands)
	}
}
