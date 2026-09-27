package themes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func TestRestoreCompatibilityBuiltinAvailabilityAndSkip(t *testing.T) {
	builtin := t.TempDir()
	p := Provider{BuiltinDir: builtin}
	saved := profile.Themes{Current: "catppuccin", Items: []profile.Theme{{ID: "catppuccin", Type: "builtin"}}}
	apply := map[string]bool{"active": true, "theme:catppuccin": true}
	missing, err := p.RestoreCompatibility(saved, profile.Themes{}, apply, false)
	if err != nil || missing.State != model.CompatibilityIncompatible || missing.Authority != model.CompatibilityBlocked || missing.Findings[0].Code != "themes.builtin.unavailable" {
		t.Fatalf("unavailable builtin = %+v err=%v", missing, err)
	}
	skipped, err := p.RestoreCompatibility(saved, profile.Themes{}, map[string]bool{"active": false, "theme:catppuccin": false}, false)
	if err != nil || skipped.Applies || len(skipped.Findings) != 0 {
		t.Fatalf("skipped builtin blocked restore: %+v err=%v", skipped, err)
	}
	if err := os.Mkdir(filepath.Join(builtin, "catppuccin"), 0o755); err != nil {
		t.Fatal(err)
	}
	available, err := p.RestoreCompatibility(saved, profile.Themes{}, apply, false)
	if err != nil || available.State != model.CompatibilitySupported || available.Authority != model.CompatibilityUnchanged || len(available.Evidence) == 0 {
		t.Fatalf("available builtin = %+v err=%v", available, err)
	}
}

func TestRestoreCompatibilityThirdPartyProvenanceRemainsUnknown(t *testing.T) {
	p := Provider{BuiltinDir: t.TempDir()}
	for _, item := range []profile.Theme{
		{ID: "custom", Type: "local", Hash: strings.Repeat("a", 64)},
		{ID: "custom", Type: "git", URL: "https://example.invalid/custom-theme.git", Revision: strings.Repeat("b", 40)},
	} {
		got, err := p.RestoreCompatibility(profile.Themes{Items: []profile.Theme{item}}, profile.Themes{}, map[string]bool{"theme:custom": true}, false)
		if err != nil || got.State != model.CompatibilityUnknown || got.Authority != model.CompatibilityUnchanged || len(got.Evidence) == 0 || len(got.Findings) == 0 {
			t.Fatalf("third-party type=%s falsely supported: %+v err=%v", item.Type, got, err)
		}
		for _, e := range got.Evidence {
			if strings.Contains(e.Summary, "example.invalid") || strings.Contains(e.Summary, "/tmp/") {
				t.Fatalf("third-party evidence leaked provenance: %+v", e)
			}
		}
	}
}
