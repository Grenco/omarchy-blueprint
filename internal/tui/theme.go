package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/restore"
	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/pelletier/go-toml/v2"
)

type Palette = components.ThemePalette

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
	Muted               string `toml:"muted"`
	Red                 string `toml:"red"`
	Green               string `toml:"green"`
	Yellow              string `toml:"yellow"`
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
	if color := firstColor(colors.Color1, colors.Red); color != "" {
		palette.Error = color
		palette.Removed = color
	}
	if color := firstColor(colors.Color2, colors.Green); color != "" {
		palette.Success = color
		palette.Added = color
	}
	if color := firstColor(colors.Color3, colors.Yellow); color != "" {
		palette.Warning = color
	}
	if color := firstColor(colors.Color8, colors.Muted); color != "" {
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

func firstColor(colors ...string) string {
	for _, color := range colors {
		if color = usableColor(color); color != "" {
			return color
		}
	}
	return ""
}
