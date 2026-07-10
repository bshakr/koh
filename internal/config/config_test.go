package config

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bshakr/koh/internal/git"
)

// newGitRepo creates a fresh git repository in a temp directory and changes
// the test's working directory into it. ConfigPath/Load/Save all resolve their
// path via git, so a real (isolated) repo is the simplest way to exercise them.
func newGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.CommandContext(t.Context(), "git", "init")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}
	t.Chdir(dir)
	// os.Getwd (used by ConfigPath for a non-worktree repo) reflects the
	// directory we just moved into; return it so callers compare against the
	// exact same string and sidestep any /tmp symlink normalisation.
	root, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd failed: %v", err)
	}
	return root
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.SetupScript == "" {
		t.Error("DefaultConfig() returned empty SetupScript")
	}

	if cfg.PaneCommands == nil {
		t.Error("DefaultConfig() returned nil PaneCommands")
	}

	t.Logf("Default config: %+v", cfg)
}

func TestConfigSaveAndLoad(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "ko-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	// Create a test config file path
	configPath := filepath.Join(tempDir, ".kohconfig")

	// Create a test config
	testConfig := &Config{
		SetupScript: "./test/setup",
		PaneCommands: []string{
			"nvim",
			"./test/setup",
			"./test/dev",
			"test-cli",
		},
	}

	// Marshal and save manually (since Save() uses ConfigPath which needs git)
	data, err := json.MarshalIndent(testConfig, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}

	//nolint:gosec // G306: Test file - 0644 is acceptable for temp test files
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	// Read it back
	//nolint:gosec // G304: Test file - reading test config is expected
	loadedData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Failed to read config: %v", err)
	}

	var loadedConfig Config
	if err := json.Unmarshal(loadedData, &loadedConfig); err != nil {
		t.Fatalf("Failed to unmarshal config: %v", err)
	}

	// Verify the loaded config matches
	if loadedConfig.SetupScript != testConfig.SetupScript {
		t.Errorf("SetupScript mismatch: got %s, want %s", loadedConfig.SetupScript, testConfig.SetupScript)
	}

	if len(loadedConfig.PaneCommands) != len(testConfig.PaneCommands) {
		t.Errorf("PaneCommands length mismatch: got %d, want %d",
			len(loadedConfig.PaneCommands), len(testConfig.PaneCommands))
	}

	for i, cmd := range testConfig.PaneCommands {
		if loadedConfig.PaneCommands[i] != cmd {
			t.Errorf("PaneCommands[%d] mismatch: got %s, want %s",
				i, loadedConfig.PaneCommands[i], cmd)
		}
	}
}

func TestConfigJSON(t *testing.T) {
	cfg := DefaultConfig()

	// Test marshaling
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}

	t.Logf("Config JSON:\n%s", string(data))

	// Test unmarshaling
	var loaded Config
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("Failed to unmarshal config: %v", err)
	}

	// Verify unmarshaled config matches original
	if loaded.SetupScript != cfg.SetupScript {
		t.Errorf("SetupScript mismatch after marshal/unmarshal")
	}
}

func TestConfigPathInGitRepo(t *testing.T) {
	root := newGitRepo(t)

	got, err := ConfigPath()
	if err != nil {
		t.Fatalf("ConfigPath() error: %v", err)
	}

	want := filepath.Join(root, ".kohconfig")
	if got != want {
		t.Errorf("ConfigPath() = %q, want %q", got, want)
	}
}

func TestConfigPathNotInGitRepo(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	// Defensive: a bare temp dir should not be inside a repo, but if the test
	// host nests $TMPDIR under one, skip rather than assert a false negative.
	if git.IsGitRepo() {
		t.Skip("temp dir is unexpectedly inside a git repository")
	}

	if _, err := ConfigPath(); err == nil {
		t.Fatal("ConfigPath() outside a git repo: expected error, got nil")
	} else if !strings.Contains(err.Error(), "not in a git repository") {
		t.Errorf("ConfigPath() error = %q, want it to contain %q", err.Error(), "not in a git repository")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	newGitRepo(t)

	want := &Config{
		SetupScript:  "./bin/bootstrap",
		PaneCommands: []string{"nvim", "npm run dev"},
	}
	if err := want.Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if got.SetupScript != want.SetupScript {
		t.Errorf("SetupScript = %q, want %q", got.SetupScript, want.SetupScript)
	}
	if len(got.PaneCommands) != len(want.PaneCommands) {
		t.Fatalf("PaneCommands length = %d, want %d", len(got.PaneCommands), len(want.PaneCommands))
	}
	for i := range want.PaneCommands {
		if got.PaneCommands[i] != want.PaneCommands[i] {
			t.Errorf("PaneCommands[%d] = %q, want %q", i, got.PaneCommands[i], want.PaneCommands[i])
		}
	}
}

func TestLoadMissingFile(t *testing.T) {
	newGitRepo(t)

	if _, err := Load(); err == nil {
		t.Fatal("Load() with no config file: expected error, got nil")
	} else if !strings.Contains(err.Error(), "no .kohconfig found") {
		t.Errorf("Load() error = %q, want it to contain %q", err.Error(), "no .kohconfig found")
	}
}

func TestLoadParseErrors(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{"non-json garbage", "this is not json {{{"},
		{"truncated object", `{"setup_script": `},
		{"json array", `["a", "b"]`},
		{"json string", `"just a string"`},
		{"json number", `42`},
		{"json bool", `true`},
		{"bare null", `null`},
		{"whitespace-padded null", "  null\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := newGitRepo(t)
			//nolint:gosec // G306: test fixture file, permissive mode is fine
			if err := os.WriteFile(filepath.Join(root, ".kohconfig"), []byte(tt.content), 0644); err != nil {
				t.Fatalf("failed to write fixture: %v", err)
			}

			_, err := Load()
			if err == nil {
				t.Fatalf("Load() with %q: expected error, got nil", tt.content)
			}
			if !strings.Contains(err.Error(), "failed to parse config file") {
				t.Errorf("Load() error = %q, want it to contain %q", err.Error(), "failed to parse config file")
			}
		})
	}
}

func TestLoadEmptyObjectIsValid(t *testing.T) {
	// An empty JSON object is a legitimate (if minimal) config, distinct from a
	// bare null: it must load without error and yield a zero-value config.
	root := newGitRepo(t)
	//nolint:gosec // G306: test fixture file, permissive mode is fine
	if err := os.WriteFile(filepath.Join(root, ".kohconfig"), []byte(`{}`), 0644); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() with {} error: %v", err)
	}
	if cfg.SetupScript != "" || len(cfg.PaneCommands) != 0 {
		t.Errorf("Load() with {} = %+v, want zero-value config", cfg)
	}
}

func TestConfigExists(t *testing.T) {
	root := newGitRepo(t)

	exists, err := ConfigExists()
	if err != nil {
		t.Fatalf("ConfigExists() error: %v", err)
	}
	if exists {
		t.Error("ConfigExists() = true before any config was written, want false")
	}

	//nolint:gosec // G306: test fixture file, permissive mode is fine
	if err := os.WriteFile(filepath.Join(root, ".kohconfig"), []byte(`{}`), 0644); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	exists, err = ConfigExists()
	if err != nil {
		t.Fatalf("ConfigExists() error: %v", err)
	}
	if !exists {
		t.Error("ConfigExists() = false after writing config, want true")
	}
}

func TestConfigExistsNotInGitRepo(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if git.IsGitRepo() {
		t.Skip("temp dir is unexpectedly inside a git repository")
	}

	if _, err := ConfigExists(); err == nil {
		t.Error("ConfigExists() outside a git repo: expected error, got nil")
	}
}
