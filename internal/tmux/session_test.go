package tmux

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/bshakr/koh/internal/config"
)

// TestMain points TMUX at a throwaway tmux server on a private socket so the
// live-server tests in this package can never touch the developer's real tmux
// session — they create and kill real windows, and previously left stray
// "test-repo|test-*" windows in whatever session `go test` ran from. When tmux
// isn't installed (or the private server fails to start), TMUX is left empty
// and those tests skip via their IsInTmux() guards, exactly as before.
func TestMain(m *testing.M) {
	os.Exit(runWithPrivateTmux(m))
}

func runWithPrivateTmux(m *testing.M) int {
	// Whatever happens below, never inherit the user's server.
	if err := os.Unsetenv("TMUX"); err != nil {
		fmt.Fprintf(os.Stderr, "failed to unset TMUX: %v\n", err)
		return 1
	}

	tmuxBin, err := exec.LookPath("tmux")
	if err != nil {
		return m.Run()
	}

	ctx := context.Background()
	socket := fmt.Sprintf("koh-test-%d", os.Getpid())
	// -f /dev/null keeps the user's tmux.conf out of the test server.
	start := exec.CommandContext(ctx, tmuxBin, "-L", socket, "-f", os.DevNull,
		"new-session", "-d", "-s", "koh-test", "-x", "200", "-y", "50")
	if out, err := start.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: private tmux server failed to start, live-server tests will skip: %v\n%s", err, out)
		return m.Run()
	}
	defer func() {
		_ = exec.CommandContext(ctx, tmuxBin, "-L", socket, "kill-server").Run()
	}()

	sockPath, err := exec.CommandContext(ctx, tmuxBin, "-L", socket,
		"display-message", "-p", "#{socket_path}").Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not resolve private tmux socket, live-server tests will skip: %v\n", err)
		return m.Run()
	}

	// TMUX is "socket_path,server_pid,session_id"; tmux clients only read the
	// socket path from it, so placeholder pid/session values are fine.
	if err := os.Setenv("TMUX", strings.TrimSpace(string(sockPath))+",0,0"); err != nil {
		fmt.Fprintf(os.Stderr, "failed to set TMUX: %v\n", err)
		return 1
	}
	return m.Run()
}

func TestIsInTmux(t *testing.T) {
	// This test just verifies the function runs without error
	result := IsInTmux()
	t.Logf("IsInTmux: %v", result)

	// Note: This will return false when running tests outside tmux
	// and true when running inside tmux
}

func TestRunTmuxCmdWithContext(t *testing.T) {
	if !IsInTmux() {
		t.Skip("Not in a tmux session, skipping test")
	}

	// Test a simple tmux command with context
	ctx := context.Background()
	err := runTmuxCmdWithContext(ctx, "display-message", "-p", "test")
	if err != nil {
		t.Errorf("runTmuxCmdWithContext() failed: %v", err)
	}
}

func TestRunTmuxCmdWithContextCancellation(t *testing.T) {
	if !IsInTmux() {
		t.Skip("Not in a tmux session, skipping test")
	}

	// Test cancellation
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := runTmuxCmdWithContext(ctx, "display-message", "-p", "test")
	if err == nil {
		t.Error("Expected error due to cancellation, got nil")
	}

	if err != nil && err.Error() != "operation cancelled" {
		// The operation might complete before cancellation is detected
		t.Logf("Got error: %v (might complete before cancellation)", err)
	}
}

func TestRunTmuxCmdWithContextTimeout(t *testing.T) {
	if !IsInTmux() {
		t.Skip("Not in a tmux session, skipping test")
	}

	// Test with a reasonable timeout
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := runTmuxCmdWithContext(ctx, "display-message", "-p", "test")
	if err != nil {
		t.Errorf("runTmuxCmdWithContext() with timeout failed: %v", err)
	}
}

func TestSendKeysWithContext(t *testing.T) {
	if !IsInTmux() {
		t.Skip("Not in a tmux session, skipping test")
	}

	// We can't easily test this without creating actual panes
	// So we'll just test that the function exists and has the right signature
	ctx := context.Background()

	// Try to send keys to pane 0 (should fail if we're not in the right window)
	err := sendKeysWithContext(ctx, 0, "echo test")
	// We expect this might fail depending on the tmux setup
	t.Logf("sendKeysWithContext result: %v", err)
}

func TestSendKeysWithContextCancellation(t *testing.T) {
	if !IsInTmux() {
		t.Skip("Not in a tmux session, skipping test")
	}

	// Test cancellation
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := sendKeysWithContext(ctx, 0, "echo test")
	if err == nil {
		t.Error("Expected error due to cancellation, got nil")
	}

	if err != nil && err.Error() != "operation cancelled" {
		// The operation might complete or fail for other reasons
		t.Logf("Got error: %v (might complete before cancellation or fail for other reasons)", err)
	}
}

func TestCloseWindow(t *testing.T) {
	if !IsInTmux() {
		t.Skip("Not in a tmux session, skipping test")
	}

	// We can't easily test this without creating actual windows
	// Just verify the function exists
	err := CloseWindow("test-window", "test-worktree")

	// We expect this to fail since the window doesn't exist
	if err == nil {
		t.Error("Expected error for non-existent window, got nil")
	}

	t.Logf("CloseWindow error (expected): %v", err)
}

// Test that the backwards-compatible functions still work
func TestBackwardsCompatibility(t *testing.T) {
	if !IsInTmux() {
		t.Skip("Not in a tmux session, skipping test")
	}

	// Test that the non-context versions still work by delegating to context versions
	err := runTmuxCmd("display-message", "-p", "test")
	if err != nil {
		t.Errorf("runTmuxCmd() (backwards compatible) failed: %v", err)
	}

	// Test sendKeys backwards compatibility
	err = sendKeys(0, "echo test")
	// Might fail depending on pane setup, but should not panic
	t.Logf("sendKeys result: %v", err)
}

// TestCreateSessionWithNoPaneCommands tests creating a session with only setup script
func TestCreateSessionWithNoPaneCommands(t *testing.T) {
	if !IsInTmux() {
		t.Skip("Not in a tmux session, skipping test")
	}

	cfg := &config.Config{
		SetupScript:  "",
		PaneCommands: []string{},
	}

	ctx := context.Background()
	err := CreateSessionWithContext(ctx, "test-repo", "test-worktree-0", "/tmp", cfg)
	if err != nil {
		t.Errorf("CreateSessionWithContext with no pane commands failed: %v", err)
	}

	// Cleanup
	if err := CloseWindow("test-repo", "test-worktree-0"); err != nil {
		t.Logf("Failed to close window: %v", err)
	}
}

// TestCreateSessionWithOnePaneCommand tests creating a session with setup + 1 command
func TestCreateSessionWithOnePaneCommand(t *testing.T) {
	if !IsInTmux() {
		t.Skip("Not in a tmux session, skipping test")
	}

	cfg := &config.Config{
		SetupScript:  "",
		PaneCommands: []string{"echo 'Command 1'"},
	}

	ctx := context.Background()
	err := CreateSessionWithContext(ctx, "test-repo", "test-worktree-1", "/tmp", cfg)
	if err != nil {
		t.Errorf("CreateSessionWithContext with 1 pane command failed: %v", err)
	}

	// Cleanup
	if err := CloseWindow("test-repo", "test-worktree-1"); err != nil {
		t.Logf("Failed to close window: %v", err)
	}
}

// TestCreateSessionWithTwoPaneCommands tests creating a session with setup + 2 commands
func TestCreateSessionWithTwoPaneCommands(t *testing.T) {
	if !IsInTmux() {
		t.Skip("Not in a tmux session, skipping test")
	}

	cfg := &config.Config{
		SetupScript:  "",
		PaneCommands: []string{"echo 'Command 1'", "echo 'Command 2'"},
	}

	ctx := context.Background()
	err := CreateSessionWithContext(ctx, "test-repo", "test-worktree-2", "/tmp", cfg)
	if err != nil {
		t.Errorf("CreateSessionWithContext with 2 pane commands failed: %v", err)
	}

	// Cleanup
	if err := CloseWindow("test-repo", "test-worktree-2"); err != nil {
		t.Logf("Failed to close window: %v", err)
	}
}

// TestCreateSessionWithThreePaneCommands tests creating a session with setup + 3 commands
func TestCreateSessionWithThreePaneCommands(t *testing.T) {
	if !IsInTmux() {
		t.Skip("Not in a tmux session, skipping test")
	}

	cfg := &config.Config{
		SetupScript:  "",
		PaneCommands: []string{"echo 'Command 1'", "echo 'Command 2'", "echo 'Command 3'"},
	}

	ctx := context.Background()
	err := CreateSessionWithContext(ctx, "test-repo", "test-worktree-3", "/tmp", cfg)
	if err != nil {
		t.Errorf("CreateSessionWithContext with 3 pane commands failed: %v", err)
	}

	// Cleanup
	if err := CloseWindow("test-repo", "test-worktree-3"); err != nil {
		t.Logf("Failed to close window: %v", err)
	}
}

// TestCreateSessionWithManyPaneCommands tests creating a session with setup + 5 commands
func TestCreateSessionWithManyPaneCommands(t *testing.T) {
	if !IsInTmux() {
		t.Skip("Not in a tmux session, skipping test")
	}

	cfg := &config.Config{
		SetupScript: "",
		PaneCommands: []string{
			"echo 'Command 1'",
			"echo 'Command 2'",
			"echo 'Command 3'",
			"echo 'Command 4'",
			"echo 'Command 5'",
		},
	}

	ctx := context.Background()
	err := CreateSessionWithContext(ctx, "test-repo", "test-worktree-many", "/tmp", cfg)
	if err != nil {
		t.Errorf("CreateSessionWithContext with many pane commands failed: %v", err)
	}

	// Cleanup
	if err := CloseWindow("test-repo", "test-worktree-many"); err != nil {
		t.Logf("Failed to close window: %v", err)
	}
}

// TestWindowExists tests checking if a window exists
func TestWindowExists(t *testing.T) {
	if !IsInTmux() {
		t.Skip("Not in a tmux session, skipping test")
	}

	// Test with non-existent window
	exists, err := WindowExists("nonexistent-worktree-12345")
	if err != nil {
		t.Errorf("WindowExists() error: %v", err)
	}
	if exists {
		t.Error("Expected WindowExists to return false for non-existent window")
	}
}

// TestWindowExistsWithContext tests checking if a window exists with context
func TestWindowExistsWithContext(t *testing.T) {
	if !IsInTmux() {
		t.Skip("Not in a tmux session, skipping test")
	}

	ctx := context.Background()

	// Test with non-existent window
	exists, err := WindowExistsWithContext(ctx, "nonexistent-worktree-ctx-12345")
	if err != nil {
		t.Errorf("WindowExistsWithContext() error: %v", err)
	}
	if exists {
		t.Error("Expected WindowExistsWithContext to return false for non-existent window")
	}

	// Test cancellation
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err = WindowExistsWithContext(ctx, "test-worktree")
	if err == nil {
		// Note: The command might complete before cancellation is detected
		t.Log("Command completed before cancellation (this is okay)")
	}
}

// TestSwitchToWindow tests switching to a window
func TestSwitchToWindow(t *testing.T) {
	if !IsInTmux() {
		t.Skip("Not in a tmux session, skipping test")
	}

	// Test with non-existent window should error
	err := SwitchToWindow("nonexistent-worktree-switch-12345")
	if err == nil {
		t.Error("Expected error when switching to non-existent window")
	}

	expectedErrorMsg := "no tmux window found for worktree:"
	if err != nil && !contains(err.Error(), expectedErrorMsg) {
		t.Errorf("Expected error to contain %q, got: %v", expectedErrorMsg, err)
	}
}

// TestSwitchToWindowWithContext tests switching to a window with context
func TestSwitchToWindowWithContext(t *testing.T) {
	if !IsInTmux() {
		t.Skip("Not in a tmux session, skipping test")
	}

	ctx := context.Background()

	// Test with non-existent window should error
	err := SwitchToWindowWithContext(ctx, "nonexistent-worktree-ctx-switch-12345")
	if err == nil {
		t.Error("Expected error when switching to non-existent window")
	}
}

// TestFindWindowByWorktree tests the helper function
func TestFindWindowByWorktree(t *testing.T) {
	if !IsInTmux() {
		t.Skip("Not in a tmux session, skipping test")
	}

	ctx := context.Background()

	// Test with non-existent window
	index, name, err := findWindowByWorktree(ctx, "nonexistent-worktree-find-12345")
	if err != nil {
		t.Errorf("findWindowByWorktree() error: %v", err)
	}
	if index != "" || name != "" {
		t.Errorf("Expected empty strings for non-existent window, got index=%q, name=%q", index, name)
	}
}

// TestWindowExistsAfterCreation tests that WindowExists returns true after creating a window
func TestWindowExistsAfterCreation(t *testing.T) {
	if !IsInTmux() {
		t.Skip("Not in a tmux session, skipping test")
	}

	worktreeName := "test-exists-window"
	cfg := &config.Config{
		SetupScript:  "",
		PaneCommands: []string{},
	}

	// Create the window
	ctx := context.Background()
	err := CreateSessionWithContext(ctx, "test-repo", worktreeName, "/tmp", cfg)
	if err != nil {
		t.Fatalf("Failed to create test window: %v", err)
	}

	// Check that it exists
	exists, err := WindowExists(worktreeName)
	if err != nil {
		t.Errorf("WindowExists() error: %v", err)
	}
	if !exists {
		t.Error("Expected WindowExists to return true for created window")
	}

	// Cleanup
	if err := CloseWindow("test-repo", worktreeName); err != nil {
		t.Logf("Failed to close window: %v", err)
	}

	// Verify it no longer exists
	exists, err = WindowExists(worktreeName)
	if err != nil {
		t.Errorf("WindowExists() error after cleanup: %v", err)
	}
	if exists {
		t.Error("Expected WindowExists to return false after closing window")
	}
}

// TestGetPanesForWindow tests getting pane IDs for a window
func TestGetPanesForWindow(t *testing.T) {
	if !IsInTmux() {
		t.Skip("Not in a tmux session, skipping test")
	}

	worktreeName := "test-get-panes"
	cfg := &config.Config{
		SetupScript:  "",
		PaneCommands: []string{"echo 'pane 1'", "echo 'pane 2'"},
	}

	// Create a window with multiple panes
	ctx := context.Background()
	err := CreateSessionWithContext(ctx, "test-repo", worktreeName, "/tmp", cfg)
	if err != nil {
		t.Fatalf("Failed to create test window: %v", err)
	}

	// Get the window index
	index, _, err := findWindowByWorktree(ctx, worktreeName)
	if err != nil {
		t.Fatalf("Failed to find window: %v", err)
	}

	// Get panes for the window
	panes, err := getPanesForWindow(ctx, index)
	if err != nil {
		t.Errorf("getPanesForWindow() error: %v", err)
	}

	// We should have 3 panes (setup + 2 commands)
	expectedPanes := 3
	if len(panes) != expectedPanes {
		t.Errorf("Expected %d panes, got %d", expectedPanes, len(panes))
	}

	// Verify each pane ID starts with % (tmux pane ID format)
	for i, paneID := range panes {
		if len(paneID) == 0 || paneID[0] != '%' {
			t.Errorf("Pane %d has invalid ID format: %q", i, paneID)
		}
	}

	// Cleanup
	if err := CloseWindow("test-repo", worktreeName); err != nil {
		t.Logf("Failed to close window: %v", err)
	}
}

// TestSendCtrlCToPane tests sending Ctrl-C to a pane
func TestSendCtrlCToPane(t *testing.T) {
	if !IsInTmux() {
		t.Skip("Not in a tmux session, skipping test")
	}

	worktreeName := "test-ctrl-c"
	cfg := &config.Config{
		SetupScript:  "",
		PaneCommands: []string{},
	}

	// Create a window with one pane
	ctx := context.Background()
	err := CreateSessionWithContext(ctx, "test-repo", worktreeName, "/tmp", cfg)
	if err != nil {
		t.Fatalf("Failed to create test window: %v", err)
	}

	// Get the window index and panes
	index, _, err := findWindowByWorktree(ctx, worktreeName)
	if err != nil {
		t.Fatalf("Failed to find window: %v", err)
	}

	panes, err := getPanesForWindow(ctx, index)
	if err != nil {
		t.Fatalf("Failed to get panes: %v", err)
	}

	if len(panes) == 0 {
		t.Fatal("Expected at least one pane")
	}

	// Send Ctrl-C to the first pane
	err = sendCtrlCToPane(ctx, panes[0])
	if err != nil {
		t.Errorf("sendCtrlCToPane() error: %v", err)
	}

	// Cleanup
	if err := CloseWindow("test-repo", worktreeName); err != nil {
		t.Logf("Failed to close window: %v", err)
	}
}

// TestGetPanesForWindowNonExistent tests error handling for non-existent window
func TestGetPanesForWindowNonExistent(t *testing.T) {
	if !IsInTmux() {
		t.Skip("Not in a tmux session, skipping test")
	}

	ctx := context.Background()
	// Use a very high window index that should not exist
	panes, err := getPanesForWindow(ctx, "999999")
	if err == nil {
		t.Error("Expected error for non-existent window, got nil")
	}
	if len(panes) != 0 {
		t.Errorf("Expected 0 panes for non-existent window, got %d", len(panes))
	}
}

// TestCloseWindowWithCtrlC tests that CloseWindow sends Ctrl-C before killing
func TestCloseWindowWithCtrlC(t *testing.T) {
	if !IsInTmux() {
		t.Skip("Not in a tmux session, skipping test")
	}

	worktreeName := "test-close-with-ctrl-c"
	cfg := &config.Config{
		SetupScript:  "",
		PaneCommands: []string{"sleep 10", "sleep 20"},
	}

	// Create a window with panes running sleep commands
	ctx := context.Background()
	err := CreateSessionWithContext(ctx, "test-repo", worktreeName, "/tmp", cfg)
	if err != nil {
		t.Fatalf("Failed to create test window: %v", err)
	}

	// Verify window exists
	exists, err := WindowExists(worktreeName)
	if err != nil {
		t.Fatalf("WindowExists() error: %v", err)
	}
	if !exists {
		t.Fatal("Window should exist after creation")
	}

	// Close the window (should send Ctrl-C to all panes before killing)
	err = CloseWindow("test-repo", worktreeName)
	if err != nil {
		t.Errorf("CloseWindow() error: %v", err)
	}

	// Verify window no longer exists
	exists, err = WindowExists(worktreeName)
	if err != nil {
		t.Errorf("WindowExists() error after close: %v", err)
	}
	if exists {
		t.Error("Window should not exist after closing")
	}
}

// TestParseWindowLine exercises the pure parsing of
// `tmux list-windows -F "#{window_index}:#{window_name}"` output, focusing on
// worktree names that themselves contain the ":" and "|" separators. This runs
// without a live tmux server.
func TestParseWindowLine(t *testing.T) {
	tests := []struct {
		name         string
		line         string
		wantIndex    string
		wantWindow   string
		wantWorktree string
		wantOK       bool
	}{
		{
			name:         "plain name",
			line:         "0:repo|feature",
			wantIndex:    "0",
			wantWindow:   "repo|feature",
			wantWorktree: "feature",
			wantOK:       true,
		},
		{
			name:         "worktree contains colon",
			line:         "3:repo|foo:bar",
			wantIndex:    "3",
			wantWindow:   "repo|foo:bar",
			wantWorktree: "foo:bar",
			wantOK:       true,
		},
		{
			name:         "worktree contains pipe",
			line:         "2:repo|foo|bar",
			wantIndex:    "2",
			wantWindow:   "repo|foo|bar",
			wantWorktree: "foo|bar",
			wantOK:       true,
		},
		{
			name:         "worktree contains both colon and pipe",
			line:         "10:repo|foo:bar|baz",
			wantIndex:    "10",
			wantWindow:   "repo|foo:bar|baz",
			wantWorktree: "foo:bar|baz",
			wantOK:       true,
		},
		{
			name:         "repo name irrelevant to worktree match",
			line:         "1:my-repo|task-42",
			wantIndex:    "1",
			wantWindow:   "my-repo|task-42",
			wantWorktree: "task-42",
			wantOK:       true,
		},
		{
			name:   "no pipe separator in window name",
			line:   "1:plainwindow",
			wantOK: false,
		},
		{
			name:   "no colon separator",
			line:   "notawindowline",
			wantOK: false,
		},
		{
			name:   "empty line",
			line:   "",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			index, window, worktree, ok := parseWindowLine(tt.line)
			if ok != tt.wantOK {
				t.Fatalf("parseWindowLine(%q) ok = %v, want %v", tt.line, ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if index != tt.wantIndex {
				t.Errorf("parseWindowLine(%q) index = %q, want %q", tt.line, index, tt.wantIndex)
			}
			if window != tt.wantWindow {
				t.Errorf("parseWindowLine(%q) window = %q, want %q", tt.line, window, tt.wantWindow)
			}
			if worktree != tt.wantWorktree {
				t.Errorf("parseWindowLine(%q) worktree = %q, want %q", tt.line, worktree, tt.wantWorktree)
			}
		})
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
