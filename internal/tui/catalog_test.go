package tui

import (
	"slices"
	"strings"
	"testing"
)

func TestScreenCatalog(t *testing.T) {
	want := []ScreenID{
		ScreenOverview,
		ScreenCapture, ScreenRestore,
		ScreenPackages, ScreenThemes, ScreenPlugins, ScreenDefaults,
		ScreenConfig, ScreenShell, ScreenHooks, ScreenResources,
		ScreenMachines, ScreenSync,
	}
	if got := orderedScreenIDs(); !slices.Equal(got, want) {
		t.Fatalf("screen order = %v, want %v", got, want)
	}

	for _, id := range want {
		info := screenInfo(id)
		if info.Label == "" || info.Short == "" || info.Long == "" || info.Keywords == "" {
			t.Errorf("catalogue entry for %q is incomplete: %#v", id, info)
		}
	}

	config := screenInfo(ScreenConfig)
	if got, want := config.Short, "Review dotfiles and other system/app configuration Blueprint can remember and restore."; got != want {
		t.Errorf("Config short description = %q, want %q", got, want)
	}
	for _, keyword := range []string{"dotfiles", ".config"} {
		if !strings.Contains(config.Keywords, keyword) {
			t.Errorf("Config keywords %q do not contain %q", config.Keywords, keyword)
		}
	}
	restore := screenInfo(ScreenRestore)
	for _, want := range []string{"Safe", "Force", "Additive", "Exact"} {
		if !strings.Contains(restore.Long, want) {
			t.Errorf("Restore help omits %q: %s", want, restore.Long)
		}
	}
	if strings.Contains(restore.Long, "Normal mode") || strings.Contains(restore.Long, "Forced mode") {
		t.Errorf("Restore help retains legacy modes: %s", restore.Long)
	}

	sections := sidebarSections()
	labels := make([]string, len(sections)-1)
	for i, section := range sections[1:] {
		labels[i] = section.Label
	}
	if want := []string{"Workflow", "Software", "Customisation", "Profile"}; !slices.Equal(labels, want) {
		t.Errorf("section labels = %v, want %v", labels, want)
	}
}

func TestSidebarSectionMovement(t *testing.T) {
	for _, test := range []struct {
		current ScreenID
		delta   int
		want    ScreenID
	}{
		{ScreenOverview, 1, ScreenCapture},
		{ScreenCapture, 1, ScreenPackages},
		{ScreenPackages, -1, ScreenCapture},
		{ScreenCapture, -1, ScreenOverview},
		{ScreenSync, 1, ScreenSync},
	} {
		if got := moveSidebarSection(test.current, test.delta); got != test.want {
			t.Errorf("moveSidebarSection(%q, %d) = %q, want %q", test.current, test.delta, got, test.want)
		}
	}
}

func TestCatalogueDescribesPolicyAwareWorkflow(t *testing.T) {
	for _, test := range []struct {
		id   ScreenID
		want []string
	}{
		{ScreenOverview, []string{"Needs attention", "Changes available", "Intentional differences"}},
		{ScreenCapture, []string{"review", "Preserved by policy", "Include", "Preserve", "re-checks"}},
		{ScreenPackages, []string{"Capture", "Restore", "policy"}},
	} {
		long := screenInfo(test.id).Long
		for _, want := range test.want {
			if !strings.Contains(long, want) {
				t.Errorf("%s help omits %q: %s", test.id, want, long)
			}
		}
	}
}

// Displayed catalogue copy must use current vocabulary. Keywords are search
// aids and may keep older synonyms such as "drift".
func TestCatalogueCopyAvoidsStaleTerms(t *testing.T) {
	stale := []string{"Normal mode", "Forced mode", "excluded package", "machine-specific items", "drift", "provider", "git theme", "Git plugins"}
	for _, info := range screenCatalog {
		for _, text := range []string{info.Short, info.Long} {
			for _, term := range stale {
				if strings.Contains(strings.ToLower(text), strings.ToLower(term)) {
					t.Errorf("%s copy uses stale term %q: %s", info.ID, term, text)
				}
			}
		}
	}
}
