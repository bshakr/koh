package styles

import (
	"os"
	"strings"
	"testing"

	"golang.org/x/term"
)

func TestGetTerminalWidth(t *testing.T) {
	width := GetTerminalWidth()
	if width <= 0 {
		t.Errorf("GetTerminalWidth() = %d, want a positive width", width)
	}

	// Under `go test`, stdout is a pipe rather than a TTY, so GetTerminalWidth
	// must fall back to its documented default of 80. Guard on IsTerminal so
	// this stays correct if the suite is ever run attached to a real terminal.
	if !term.IsTerminal(int(os.Stdout.Fd())) && width != 80 {
		t.Errorf("GetTerminalWidth() = %d with no TTY, want fallback of 80", width)
	}
}

func TestRenderHelpersIncludeContent(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"title", RenderTitle("Heading"), "Heading"},
		{"subtitle", RenderSubtitle("Sub"), "Sub"},
		{"success text", RenderSuccess("done"), "done"},
		{"success icon", RenderSuccess("done"), IconCheck},
		{"error text", RenderError("boom"), "boom"},
		{"error icon", RenderError("boom"), IconCross},
		{"key-value key", RenderKeyValue("name", "value"), "name"},
		{"key-value value", RenderKeyValue("name", "value"), "value"},
		{"box", RenderBox("contents"), "contents"},
		{"help", RenderHelp("hint"), "hint"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(tt.got, tt.want) {
				t.Errorf("render output %q does not contain %q", tt.got, tt.want)
			}
		})
	}
}
