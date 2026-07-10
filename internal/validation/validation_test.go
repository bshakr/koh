package validation

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateWorktreeName(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		errMsg    string
		shouldErr bool
	}{
		{
			name:      "valid simple name",
			input:     "feature-branch",
			shouldErr: false,
		},
		{
			name:      "valid with numbers",
			input:     "feature-123",
			shouldErr: false,
		},
		{
			name:      "valid with underscores",
			input:     "feature_branch",
			shouldErr: false,
		},
		{
			name:      "empty name",
			input:     "",
			shouldErr: true,
			errMsg:    "cannot be empty",
		},
		{
			name:      "path traversal with ../",
			input:     "../etc/passwd",
			shouldErr: true,
			errMsg:    "path separators",
		},
		{
			name:      "path traversal with ..",
			input:     "..",
			shouldErr: true,
			errMsg:    "cannot be '.' or '..'",
		},
		{
			name:      "path traversal hidden",
			input:     "feature..branch",
			shouldErr: true,
			errMsg:    "cannot contain '..'",
		},
		{
			name:      "forward slash",
			input:     "feature/branch",
			shouldErr: true,
			errMsg:    "path separators",
		},
		{
			name:      "backslash",
			input:     "feature\\branch",
			shouldErr: true,
			errMsg:    "path separators",
		},
		{
			name:      "null byte",
			input:     "feature\x00branch",
			shouldErr: true,
			errMsg:    "invalid characters",
		},
		{
			name:      "newline",
			input:     "feature\nbranch",
			shouldErr: true,
			errMsg:    "invalid characters",
		},
		{
			name:      "too long name",
			input:     strings.Repeat("a", 256),
			shouldErr: true,
			errMsg:    "too long",
		},
		{
			name:      "reserved name CON",
			input:     "CON",
			shouldErr: true,
			errMsg:    "reserved system name",
		},
		{
			name:      "reserved name aux",
			input:     "aux",
			shouldErr: true,
			errMsg:    "reserved system name",
		},
		{
			name:      "just dot",
			input:     ".",
			shouldErr: true,
			errMsg:    "cannot be '.' or '..'",
		},
		{
			name:      "valid with hyphens",
			input:     "my-feature-branch",
			shouldErr: false,
		},
		{
			name:      "valid with mixed case",
			input:     "MyFeatureBranch",
			shouldErr: false,
		},
		{
			name:      "tab character",
			input:     "feature\tbranch",
			shouldErr: true,
			errMsg:    "invalid characters",
		},
		{
			name:      "carriage return",
			input:     "feature\rbranch",
			shouldErr: true,
			errMsg:    "invalid characters",
		},
		{
			name:      "exactly 255 chars (boundary)",
			input:     strings.Repeat("a", 255),
			shouldErr: false,
		},
		{
			name:      "reserved name lowercase prn",
			input:     "prn",
			shouldErr: true,
			errMsg:    "reserved system name",
		},
		{
			name:      "valid name with numbers at start",
			input:     "123-feature",
			shouldErr: false,
		},
		{
			name:      "path with absolute path attempt",
			input:     "/etc/passwd",
			shouldErr: true,
			errMsg:    "path separators",
		},
		{
			name:      "windows path",
			input:     "C:\\Windows\\System32",
			shouldErr: true,
			errMsg:    "path separators",
		},
		{
			name:      "hidden path traversal in middle",
			input:     "a/../etc",
			shouldErr: true,
			errMsg:    "path separators",
		},
		{
			name:      "colon (tmux target separator)",
			input:     "feature:branch",
			shouldErr: true,
			errMsg:    "cannot contain ':' or '|'",
		},
		{
			name:      "pipe (koh window separator)",
			input:     "feature|branch",
			shouldErr: true,
			errMsg:    "cannot contain ':' or '|'",
		},
		{
			name:      "both colon and pipe",
			input:     "foo:bar|baz",
			shouldErr: true,
			errMsg:    "cannot contain ':' or '|'",
		},
		// The following document CURRENT behavior: these inputs are accepted
		// today. They are pinned here so any future tightening is a deliberate,
		// visible change rather than a silent one.
		{
			name:      "leading dash currently allowed",
			input:     "-feature",
			shouldErr: false,
		},
		{
			name:      "trailing dash currently allowed",
			input:     "feature-",
			shouldErr: false,
		},
		{
			name:      "space currently allowed",
			input:     "feature branch",
			shouldErr: false,
		},
		{
			name:      "unicode letters currently allowed",
			input:     "café-branch",
			shouldErr: false,
		},
		{
			name:      "unicode CJK currently allowed",
			input:     "機能-branch",
			shouldErr: false,
		},
		{
			// Length is measured in bytes: 200 two-byte runes = 400 bytes > 255.
			name:      "unicode over byte limit",
			input:     strings.Repeat("é", 200),
			shouldErr: true,
			errMsg:    "too long",
		},
		{
			name:      "single dot-prefixed name allowed",
			input:     ".hidden",
			shouldErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateWorktreeName(tt.input)
			if tt.shouldErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errMsg)
				} else if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("expected no error, got %v", err)
				}
			}
		})
	}
}

func TestValidatePathWithinRepository(t *testing.T) {
	root := t.TempDir()

	tests := []struct {
		name      string
		target    string
		shouldErr bool
	}{
		{
			name:      "path inside repository",
			target:    filepath.Join(root, "worktrees", "feature"),
			shouldErr: false,
		},
		{
			name:      "repository root itself",
			target:    root,
			shouldErr: false,
		},
		{
			name:      "parent of repository",
			target:    filepath.Dir(root),
			shouldErr: true,
		},
		{
			name:      "sibling with shared prefix",
			target:    root + "-sibling",
			shouldErr: true,
		},
		{
			name:      "traversal back out of repository",
			target:    filepath.Join(root, "..", "escape"),
			shouldErr: true,
		},
		{
			name:      "unrelated absolute path",
			target:    "/completely/unrelated/path",
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePathWithinRepository(tt.target, root)
			if tt.shouldErr && err == nil {
				t.Errorf("ValidatePathWithinRepository(%q, %q) = nil, want error", tt.target, root)
			}
			if !tt.shouldErr && err != nil {
				t.Errorf("ValidatePathWithinRepository(%q, %q) = %v, want nil", tt.target, root, err)
			}
		})
	}
}
