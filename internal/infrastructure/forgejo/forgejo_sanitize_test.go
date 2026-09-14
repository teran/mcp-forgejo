package forgejo

import "testing"

// TestStripControl verifies that ANSI escape sequences and C0 control
// characters are removed from free-text fields while structural whitespace
// (\n, \t, \r) is preserved (S9/N23).
func TestStripControl(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"plain text unchanged", "hello world", "hello world"},
		{"preserves structural whitespace", "line1\nline2\tcol\r\n", "line1\nline2\tcol\r\n"},
		{"strips other C0 controls", "a\x00b\x07c", "abc"},
		{"strips ANSI CSI color", "\x1b[31mred\x1b[0m", "red"},
		{"strips ANSI CSI with params", "\x1b[1;2;3mX", "X"},
		{"strips simple ESC sequence", "a\x1bcb", "ab"},
		{"drops trailing incomplete CSI", "x\x1b[", "x"},
		{"drops lone trailing ESC", "x\x1b", "x"},
		{"drops DEL", "a\x7fb", "ab"},
		{"mixed", "\x1b[1mB\x00old\x1b[0m", "Bold"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stripControl(tt.in); got != tt.want {
				t.Errorf("stripControl(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
