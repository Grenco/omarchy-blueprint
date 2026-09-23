package components

import "testing"

func TestSubtleAccentKeepsAccentHueWithoutBecomingMutedText(t *testing.T) {
	styles := NewStyles(ThemePalette{ColorEnabled: true, Accent: "#00aaff", Muted: "#222222"})
	got := styles.SubtleAccent("section")
	if got == "section" || got == styles.Muted("section") || got == styles.Accent("section") {
		t.Fatalf("subtle accent = %q; want a distinct accented structural style", got)
	}
	plain := NewStyles(ThemePalette{ColorEnabled: false, Accent: "#00aaff"})
	if got := plain.SubtleAccent("section"); got != "section" {
		t.Fatalf("no-colour subtle accent = %q", got)
	}
}
