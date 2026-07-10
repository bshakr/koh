package cmd

// Tests for the init wizard's error propagation. The bug these guard against:
// `koh init` stored a failed config Save on the model's err field but runInit
// never read it, so a failed save exited 0. setupRepo / t.Chdir come from
// integration_test.go (same package) and keep tests off any real tmux server.

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

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
