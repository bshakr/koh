// Package validation provides security-focused input validation for ko.
//
// This package validates user-supplied input to prevent security issues such as:
//   - Path traversal attacks (using .. or absolute paths)
//   - Special characters that could cause issues in shell commands
//   - Reserved system names that could cause conflicts
//   - Overly long input that could cause buffer issues
//
// All user-supplied worktree names must pass through ValidateWorktreeName
// before being used in file operations or shell commands.
//
// The validation is designed to be strict and cross-platform compatible,
// rejecting potentially dangerous input even on systems where it might be safe.
package validation

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ValidateWorktreeName validates that a worktree name is safe to use
func ValidateWorktreeName(name string) error {
	if name == "" {
		return fmt.Errorf("worktree name cannot be empty")
	}

	// Check length (reasonable limit)
	if len(name) > 255 {
		return fmt.Errorf("worktree name too long (max 255 characters)")
	}

	// Check for path separators
	if strings.ContainsAny(name, "/\\") {
		return fmt.Errorf("worktree name cannot contain path separators (/ or \\)")
	}

	// Reject characters that koh reserves for tmux window bookkeeping.
	// koh names each window "repo|worktree", so "|" is its own separator, and
	// ":" is tmux's target-syntax separator (as in "session:window"). A worktree
	// name containing either character corrupts window lookup, causing 'koh
	// switch' to open a duplicate window and 'koh cleanup' to miss the original.
	if strings.ContainsAny(name, ":|") {
		return fmt.Errorf("worktree name cannot contain ':' or '|'")
	}

	// Check for path traversal attempts
	if name == "." || name == ".." {
		return fmt.Errorf("worktree name cannot be '.' or '..'")
	}

	if strings.Contains(name, "..") {
		return fmt.Errorf("worktree name cannot contain '..'")
	}

	// Check for special characters that could cause issues
	if strings.ContainsAny(name, "\x00\n\r\t") {
		return fmt.Errorf("worktree name contains invalid characters")
	}

	// Ensure it's not trying to escape using filepath operations
	cleaned := filepath.Clean(name)
	if cleaned != name {
		return fmt.Errorf("worktree name contains invalid path components")
	}

	// Check for reserved names on Windows (even if we're not on Windows, be safe)
	reserved := []string{"CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4",
		"COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3",
		"LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9"}
	upperName := strings.ToUpper(name)
	for _, r := range reserved {
		if upperName == r {
			return fmt.Errorf("worktree name cannot be a reserved system name")
		}
	}

	return nil
}

// ValidatePathWithinRepository ensures that targetPath is within repoRoot.
// This prevents path traversal attacks where a user might try to access
// files outside the repository boundaries.
func ValidatePathWithinRepository(targetPath, repoRoot string) error {
	cleanTarget, err := filepath.Abs(targetPath)
	if err != nil {
		return fmt.Errorf("failed to resolve target path: %w", err)
	}

	cleanRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return fmt.Errorf("failed to resolve repository root: %w", err)
	}

	// Check if target is within root or equal to root
	if !strings.HasPrefix(cleanTarget, cleanRoot+string(filepath.Separator)) &&
		cleanTarget != cleanRoot {
		return fmt.Errorf("path must be within repository boundaries")
	}

	return nil
}
