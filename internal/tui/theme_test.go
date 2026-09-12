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
	if palette.Foreground != "#f0f0f0" || palette.Accent != "#00aaff" || palette.Selection != "#224466" {
		t.Errorf("base palette = %#v", palette)
	}
	if palette.Error != "#ff0000" || palette.Removed != "#ff0000" || palette.Success != "#00ff00" || palette.Added != "#00ff00" || palette.Warning != "#ffff00" || palette.Muted != "#888888" || palette.Border != "#888888" || palette.BorderFocused != "#00aaff" {
		t.Errorf("semantic palette = %#v", palette)
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
	if !palette.ColorEnabled || palette.Foreground != "" || palette.Accent != "6" || palette.Success != "2" || palette.Warning != "3" || palette.Error != "1" || palette.Muted != "8" || palette.Selection != "6" {
		t.Errorf("palette = %#v, want ANSI/default fallback", palette)
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
