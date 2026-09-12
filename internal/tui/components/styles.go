package components

import "charm.land/lipgloss/v2"

// Palette is the colour contract shared by the root and screen components.
// Colour-disabled styles deliberately render unchanged text so semantic markers
// remain the source of meaning under NO_COLOR.
type ThemePalette struct {
	Foreground, Muted, Accent        string
	Success, Warning, Error          string
	Added, Removed                   string
	Border, BorderFocused, Selection string
	ColorEnabled                     bool
}

type Styles struct{ Palette ThemePalette }

func NewStyles(palette ThemePalette) Styles { return Styles{Palette: palette} }

func (s Styles) Accent(value string) string  { return s.color(value, s.Palette.Accent) }
func (s Styles) Muted(value string) string   { return s.color(value, s.Palette.Muted) }
func (s Styles) Success(value string) string { return s.color(value, s.Palette.Success) }
func (s Styles) Warning(value string) string { return s.color(value, s.Palette.Warning) }
func (s Styles) Error(value string) string   { return s.color(value, s.Palette.Error) }
func (s Styles) Added(value string) string   { return s.color(value, s.Palette.Added) }
func (s Styles) Removed(value string) string { return s.color(value, s.Palette.Removed) }

func (s Styles) color(value, color string) string {
	if !s.Palette.ColorEnabled || color == "" {
		return value
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(value)
}
