package components

import (
	"strings"
	"testing"
)

func TestTextInputModalSanitizesViewWithoutChangingValue(t *testing.T) {
	modal := NewTextInputModal("origin\nvalue\x1b", "")
	if modal.Value() != "origin\nvalue\x1b" {
		t.Fatalf("value=%q", modal.Value())
	}
	if view := modal.View(); strings.Contains(view, "\x1b") || !strings.Contains(view, "origin value") {
		t.Fatalf("unsafe view=%q", view)
	}
}
