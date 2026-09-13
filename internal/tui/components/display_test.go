package components

import "testing"

func TestDisplayTextReplacesTerminalControls(t *testing.T) {
	if got := DisplayText("name\n\t\x1b[31m"); got != "name???[31m" {
		t.Fatalf("display text = %q", got)
	}
}
