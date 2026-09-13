package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// verticalViewport keeps independently scrollable root lists within their pane.
type verticalViewport struct{ offset int }

func (v *verticalViewport) move(delta, itemCount, height int) {
	if itemCount <= 0 || height <= 0 {
		v.offset = 0
		return
	}
	v.offset = max(0, min(v.offset+delta, max(0, itemCount-height)))
}

func (v *verticalViewport) ensure(index, itemCount, height int) {
	if index < v.offset {
		v.offset = index
	}
	if index >= v.offset+height {
		v.offset = index - height + 1
	}
	v.move(0, itemCount, height)
}

func (v *verticalViewport) render(lines []string, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	v.move(0, len(lines), height)
	end := min(len(lines), v.offset+height)
	return lipgloss.NewStyle().Width(width).Height(height).MaxWidth(width).MaxHeight(height).Render(strings.Join(lines[v.offset:end], "\n"))
}
