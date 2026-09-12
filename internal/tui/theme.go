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
	SelectionBackground string `toml:"selection_background"`
	Foreground          string `toml:"foreground"`
	DarkForeground      string `toml:"dark_foreground"`
	BrightForeground    string `toml:"bright_foreground"`
	Muted               string `toml:"muted"`
	Accent              string `toml:"accent"`
	Color1              string `toml:"color1"`
	Color2              string `toml:"color2"`
	Color3              string `toml:"color3"`
	Color8              string `toml:"color8"`
	Red                 string `toml:"red"`
	Yellow              string `toml:"yellow"`
	Green               string `toml:"green"`
	Cyan                string `toml:"cyan"`
	Blue                string `toml:"blue"`
	Magenta             string `toml:"magenta"`
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

	palette.Foreground = firstColor(colors.Foreground, palette.Foreground)
	palette.DarkForeground = firstColor(colors.DarkForeground, palette.DarkForeground)
	palette.BrightForeground = firstColor(colors.BrightForeground, palette.BrightForeground)
	palette.Muted = firstColor(colors.Muted, colors.Color8, palette.Muted)
	palette.Accent = firstColor(colors.Accent, palette.Accent)
	palette.Red = firstColor(colors.Red, colors.Color1, palette.Red)
	palette.Yellow = firstColor(colors.Yellow, colors.Color3, palette.Yellow)
	palette.Green = firstColor(colors.Green, colors.Color2, palette.Green)
	palette.Cyan = firstColor(colors.Cyan, palette.Cyan)
	palette.Blue = firstColor(colors.Blue, palette.Blue)
	palette.Magenta = firstColor(colors.Magenta, palette.Magenta)
	palette.SelectionBackground = firstColor(colors.SelectionBackground, palette.SelectionBackground)
	palette.SelectionForeground = selectionForeground(palette.SelectionBackground, palette.Foreground, palette.DarkForeground, palette.BrightForeground)
	palette.Selection = palette.SelectionBackground
	palette.Border, palette.BorderFocused = palette.Muted, palette.Accent
	palette.Error, palette.Removed = palette.Red, palette.Red
	palette.Success, palette.Added = palette.Green, palette.Green
	palette.Warning = palette.Yellow
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
		Foreground: "7", DarkForeground: "0", BrightForeground: "15",
		Muted: "8", Accent: "6", Red: "1", Yellow: "3", Green: "2", Cyan: "6", Blue: "4", Magenta: "5",
		SelectionBackground: "6", SelectionForeground: "0", Selection: "6",
		Success: "2", Warning: "3", Error: "1", Added: "2", Removed: "1", Border: "8", BorderFocused: "6", ColorEnabled: true,
	}
}

func selectionForeground(background string, candidates ...string) string {
	best, bestContrast := "", 0.0
	for _, candidate := range candidates {
		if contrast := contrastRatio(background, candidate); contrast > bestContrast {
			best, bestContrast = candidate, contrast
		}
	}
	if best != "" {
		return best
	}
	return ""
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
