package components

import (
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// sgr matches the styling sequences an input may emit.
var sgr = regexp.MustCompile("\x1b\\[([0-9;]*)m")

// colourFree reports whether every styling sequence only sets attributes
// (reset, faint, reverse, …) and never a colour.
func colourFree(view string) bool {
	for _, match := range sgr.FindAllStringSubmatch(view, -1) {
		for _, param := range strings.Split(match[1], ";") {
			switch param {
			case "", "0", "1", "2", "7", "22", "27":
			default:
				return false
			}
		}
	}
	return true
}

func TestTextInputModalSanitizesViewWithoutChangingValue(t *testing.T) {
	modal := NewTextInputModal("origin\nvalue\x1b", "")
	if modal.Value() != "origin\nvalue\x1b" {
		t.Fatalf("value=%q", modal.Value())
	}
	view := modal.View()
	if plain := ansi.Strip(view); strings.Contains(plain, "\x1b") || !strings.Contains(plain, "origin value") {
		t.Fatalf("unsafe view=%q", view)
	}
	if leftover := sgr.ReplaceAllString(view, ""); strings.ContainsAny(leftover, "\x1b\x07\x00") {
		t.Fatalf("view carries escapes other than its own styling: %q", view)
	}
}

func TestFocusedInputShowsWhereToType(t *testing.T) {
	modal := NewTextInputModal("", "")
	modal.Focus()
	view := modal.View()
	if !strings.Contains(view, "\x1b[7m") {
		t.Fatalf("focused input has no visible cursor: %q", view)
	}
	if !colourFree(view) {
		t.Fatalf("input styling uses colour, which breaks NO_COLOR and themes: %q", view)
	}
}

// Without a width the underlying input shows only a placeholder's first
// character, which reads like a stray typed letter.
func TestPlaceholderIsShownWholeNotAsOneLetter(t *testing.T) {
	modal := NewTextInputModal("", "Machine name")
	modal.Focus()
	view := modal.View()
	if plain := ansi.Strip(view); !strings.Contains(plain, "Machine name") {
		t.Fatalf("placeholder collapsed: %q", plain)
	}
	if !strings.Contains(view, "\x1b[2m") || !colourFree(view) {
		t.Fatalf("placeholder is not faint, or uses colour: %q", view)
	}
	modal.Update(textKey('x'))
	if plain := ansi.Strip(modal.View()); strings.Contains(plain, "Machine name") || !strings.Contains(plain, "x") {
		t.Fatalf("placeholder stayed after typing: %q", plain)
	}
}

func TestFocusedCursorBlinks(t *testing.T) {
	modal := NewTextInputModal("", "")
	cmd := modal.Focus()
	if cmd == nil {
		t.Fatal("focus started no blink")
	}
	visible := strings.Contains(modal.View(), "\x1b[7m")
	next := modal.Update(cmd())
	if strings.Contains(modal.View(), "\x1b[7m") == visible {
		t.Fatal("a blink message did not toggle the cursor")
	}
	if next == nil {
		t.Fatal("the cursor stopped blinking after one blink")
	}
}

func textKey(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Text: string(r)} }
