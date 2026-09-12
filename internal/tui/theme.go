package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/restore"
	"github.com/pelletier/go-toml/v2"
)

type Palette struct {
	Foreground, Muted, Accent        string
	Success, Warning, Error          string
	Added, Removed                   string
	Border, BorderFocused, Selection string
	ColorEnabled                     bool
}

type ThemeLoader struct {
	StateHome func() (string, error)
	NoColor   bool
}

type omarchyColors struct {
	Foreground          string `toml:"foreground"`
	Accent              string `toml:"accent"`
	SelectionBackground string `toml:"selection_background"`
	Color1              string `toml:"color1"`
	Color2              string `toml:"color2"`
	Color3              string `toml:"color3"`
	Color8              string `toml:"color8"`
}

func (l ThemeLoader) Load() Palette {
	if l.NoColor {
		return Palette{}
	}

	palette := fallbackPalette()
	path := l.path()
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return palette
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return palette
	}

	var colors omarchyColors
	if toml.Unmarshal(contents, &colors) != nil {
		return palette
	}

	if color := usableColor(colors.Foreground); color != "" {
		palette.Foreground = color
	}
	if color := usableColor(colors.Accent); color != "" {
		palette.Accent = color
		palette.BorderFocused = color
	}
	if color := usableColor(colors.SelectionBackground); color != "" {
		palette.Selection = color
	}
	if color := usableColor(colors.Color1); color != "" {
		palette.Error = color
		palette.Removed = color
	}
	if color := usableColor(colors.Color2); color != "" {
		palette.Success = color
		palette.Added = color
	}
	if color := usableColor(colors.Color3); color != "" {
		palette.Warning = color
	}
	if color := usableColor(colors.Color8); color != "" {
		palette.Muted = color
		palette.Border = color
	}
	return palette
}

func (l ThemeLoader) Fingerprint() string {
	info, err := os.Stat(l.path())
	if err != nil || !info.Mode().IsRegular() {
		return "missing"
	}
	return fmt.Sprintf("%d:%d:%d:%s", info.ModTime().UnixNano(), info.Size(), info.Mode(), info.Name())
}

func (l ThemeLoader) path() string {
	stateHome := l.StateHome
	if stateHome == nil {
		stateHome = restore.StateHome
	}
	state, err := stateHome()
	if err != nil {
		return ""
	}
	return filepath.Join(state, "omarchy", "current", "theme", "colors.toml")
}

func fallbackPalette() Palette {
	return Palette{
		Accent:        "6",
		Muted:         "8",
		Success:       "2",
		Warning:       "3",
		Error:         "1",
		Added:         "2",
		Removed:       "1",
		Border:        "8",
		BorderFocused: "6",
		Selection:     "6",
		ColorEnabled:  true,
	}
}

func usableColor(color string) string {
	return strings.TrimSpace(color)
}
