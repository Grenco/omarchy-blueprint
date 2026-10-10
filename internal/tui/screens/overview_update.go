package screens

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/Grenco/omarchy-blueprint/internal/updates"
)

// SetUpdateNotice shows that a newer Blueprint release exists. The notice
// is presentation only: Overview's data, selection and refreshes are
// unaffected.
func (s *Overview) SetUpdateNotice(notice *updates.Notice) {
	s.update = notice
	s.list.SetSelected(s.selected, len(s.rows()), s.listHeight())
	s.selected = s.list.Selected
}

// HasUpdateNotice reports whether a newer release has been found.
func (s *Overview) HasUpdateNotice() bool { return s.update != nil }

func (s *Overview) updateLine(width int) string {
	line := "↑ Update available · " + components.DisplayText(s.update.Current) + " → " + components.DisplayText(s.update.Latest) + " · u details"
	return s.styles.SubtleAccent(components.WrapText(line, width)[0])
}

func (s *Overview) updateDetailView() string {
	notice := s.update
	lines := []string{
		"Update available",
		"Blueprint " + components.DisplayText(notice.Latest) + " is out; this is " + components.DisplayText(notice.Current) + ".",
	}
	if notice.Instructions != "" {
		lines = append(lines, "", components.DisplayText(notice.Instructions))
	}
	if notice.DetailsURL != "" {
		lines = append(lines, "", "Release: "+components.DisplayText(notice.DetailsURL), "Press b to open it in your browser.")
	}
	lines = append(lines, "", "Blueprint only tells you about updates; it never installs them. Press u to go back.")
	return strings.Join(lines, "\n")
}

// updateKey handles the notice's keys: u shows or hides its details, b
// opens the release page while they are shown.
func (s *Overview) updateKey(key string) (tea.Cmd, bool) {
	if s.update == nil {
		return nil, false
	}
	switch key {
	case "u":
		s.showUpdate = !s.showUpdate
		return nil, true
	case "b":
		if !s.showUpdate || s.update.DetailsURL == "" {
			return nil, false
		}
		url := s.update.DetailsURL
		return func() tea.Msg { return HandoffRequest{Source: "overview", Kind: "browser", Path: url} }, true
	}
	return nil, false
}
