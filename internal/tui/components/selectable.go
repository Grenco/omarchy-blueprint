package components

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// Selectable is a bounded, independently scrollable list with one selection.
type Selectable struct {
	Selected int
	offset   int
	pendingG bool
}

func (s *Selectable) SetSelected(index, count, height int) {
	if count == 0 {
		s.Selected, s.offset = 0, 0
		return
	}
	s.Selected = max(0, min(index, count-1))
	s.ensure(count, height)
}

func (s *Selectable) Move(delta, count, height int) bool {
	before := s.Selected
	s.SetSelected(s.Selected+delta, count, height)
	return before != s.Selected
}

// Vim handles common whole-list and half-page movement keys.
func (s *Selectable) Vim(key string, count, height int) bool {
	switch key {
	case "g":
		if s.pendingG {
			s.pendingG = false
			s.SetSelected(0, count, height)
		} else {
			s.pendingG = true
		}
		return true
	case "G", "shift+g":
		s.pendingG = false
		s.SetSelected(count-1, count, height)
		return true
	case "ctrl+u":
		s.pendingG = false
		s.Move(-max(1, height/2), count, height)
		return true
	case "ctrl+d":
		s.pendingG = false
		s.Move(max(1, height/2), count, height)
		return true
	default:
		s.pendingG = false
		return false
	}
}

func (s *Selectable) View(lines []string, width, height int) string {
	if width <= 0 || height <= 0 || len(lines) == 0 {
		return ""
	}
	s.SetSelected(s.Selected, len(lines), height)
	end := min(len(lines), s.offset+height)
	return lipgloss.NewStyle().Width(width).Height(height).MaxWidth(width).MaxHeight(height).Render(strings.Join(lines[s.offset:end], "\n"))
}

func (s *Selectable) ensure(count, height int) {
	if height <= 0 {
		s.offset = 0
		return
	}
	if s.Selected < s.offset {
		s.offset = s.Selected
	}
	if s.Selected >= s.offset+height {
		s.offset = s.Selected - height + 1
	}
	s.offset = max(0, min(s.offset, max(0, count-height)))
}
