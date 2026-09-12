package tui

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestThemeLoaderMapsOmarchyPalette(t *testing.T) {
	stateHome := t.TempDir()
	writeColors(t, stateHome, `foreground = "#f0f0f0"
accent = "#00aaff"
selection_background = "#224466"
color1 = "#ff0000"
color2 = "#00ff00"
color3 = "#ffff00"
color8 = "#888888"
`)

	palette := ThemeLoader{StateHome: func() (string, error) { return stateHome, nil }}.Load()
	if palette.Foreground != "#f0f0f0" || palette.Accent != "#00aaff" || palette.SelectionBackground != "#224466" {
		t.Errorf("base palette = %#v", palette)
	}
	if palette.Error != "#ff0000" || palette.Removed != "#ff0000" || palette.Success != "#00ff00" || palette.Added != "#00ff00" || palette.Warning != "#ffff00" || palette.Muted != "#888888" || palette.Border != "#888888" || palette.BorderFocused != "#00aaff" {
		t.Errorf("semantic palette = %#v", palette)
	}
}

func TestThemeLoaderPrefersCurrentSelectionField(t *testing.T) {
	stateHome := t.TempDir()
	writeColors(t, stateHome, `background = "#1a1b26"
foreground = "#c0caf5"
dark_foreground = "#15161e"
bright_foreground = "#ffffff"
selection = "#33467c"
selection_background = "#ff00ff"
`)
	palette := ThemeLoader{StateHome: func() (string, error) { return stateHome, nil }}.Load()
	if palette.SelectionBackground != "#33467c" || contrastRatio(palette.SelectionBackground, palette.SelectionForeground) < 4.5 {
		t.Fatalf("palette = %#v", palette)
	}
}

func TestThemeLoaderLightSelectionUsesDarkForeground(t *testing.T) {
	stateHome := t.TempDir()
	writeColors(t, stateHome, `background = "#ffffff"
foreground = "#202020"
dark_foreground = "#111111"
bright_foreground = "#ffffff"
selection = "#d8e2ff"
`)
	palette := ThemeLoader{StateHome: func() (string, error) { return stateHome, nil }}.Load()
	if palette.SelectionForeground != "#111111" {
		t.Fatalf("selection foreground = %q", palette.SelectionForeground)
	}
}

func TestThemeLoaderMissingOrMalformedUsesFallback(t *testing.T) {
	stateHome := t.TempDir()
	loader := ThemeLoader{StateHome: func() (string, error) { return stateHome, nil }}
	assertFallbackPalette(t, loader.Load())

	writeColors(t, stateHome, "foreground = [")
	assertFallbackPalette(t, loader.Load())

	loader.StateHome = func() (string, error) { return "", errors.New("unavailable") }
	assertFallbackPalette(t, loader.Load())
}

func TestNoColorDisablesPaletteColors(t *testing.T) {
	palette := ThemeLoader{NoColor: true}.Load()
	if palette.ColorEnabled {
		t.Error("ColorEnabled = true")
	}
	if palette.Foreground != "" || palette.Accent != "" || palette.Success != "" || palette.Selection != "" {
		t.Errorf("colours remain enabled: %#v", palette)
	}
}

func TestThemeLoaderUsesNamedSemanticFallbacks(t *testing.T) {
	stateHome := t.TempDir()
	writeColors(t, stateHome, `accent = "#00aaff"
muted = "#888888"
red = "#ff0000"
green = "#00ff00"
yellow = "#ffff00"
`)
	palette := ThemeLoader{StateHome: func() (string, error) { return stateHome, nil }}.Load()
	if palette.Accent != "#00aaff" || palette.Muted != "#888888" || palette.Error != "#ff0000" || palette.Success != "#00ff00" || palette.Warning != "#ffff00" {
		t.Fatalf("palette = %#v", palette)
	}
}

func TestThemeFingerprintChangesWhenPaletteChanges(t *testing.T) {
	stateHome := t.TempDir()
	writeColors(t, stateHome, `foreground = "#111111"`)
	loader := ThemeLoader{StateHome: func() (string, error) { return stateHome, nil }}
	before := loader.Fingerprint()
	time.Sleep(time.Millisecond)
	writeColors(t, stateHome, `foreground = "#222222"`)
	if after := loader.Fingerprint(); after == before {
		t.Errorf("fingerprint did not change: %q", after)
	}
}

func assertFallbackPalette(t *testing.T, palette Palette) {
	t.Helper()
	if !palette.ColorEnabled || palette.Foreground != "7" || palette.Accent != "6" || palette.Success != "2" || palette.Warning != "3" || palette.Error != "1" || palette.Muted != "8" || palette.SelectionBackground != "6" {
		t.Errorf("palette = %#v, want ANSI/default fallback", palette)
	}
}

func TestSelectionForegroundHasContrastForDarkAndLightThemes(t *testing.T) {
	for _, palette := range []Palette{
		{SelectionBackground: "#33467c", Foreground: "#c0caf5", DarkForeground: "#1a1b26", BrightForeground: "#ffffff"}, // Tokyo Night
		{SelectionBackground: "#d8e2ff", Foreground: "#202020", DarkForeground: "#111111", BrightForeground: "#ffffff"}, // White
	} {
		foreground := selectionForeground(palette.SelectionBackground, palette.Foreground, palette.DarkForeground, palette.BrightForeground)
		if contrastRatio(palette.SelectionBackground, foreground) < 4.5 {
			t.Fatalf("selection contrast is insufficient: background=%s foreground=%s", palette.SelectionBackground, foreground)
		}
	}
}

func writeColors(t *testing.T, stateHome, contents string) {
	t.Helper()
	path := filepath.Join(stateHome, "omarchy", "current", "theme", "colors.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
