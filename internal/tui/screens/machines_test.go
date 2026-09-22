package screens

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func TestMachinesWithoutResourcesExplainsWhyScreenIsEmpty(t *testing.T) {
	profileDir, stateHome := t.TempDir(), t.TempDir()
	data := profile.New("test", time.Now())
	if err := profile.Save(profileDir, data); err != nil {
		t.Fatal(err)
	}
	session, err := workflow.Open(
		workflow.Dependencies{
			StateHome: func() (string, error) { return stateHome, nil },
			Hostname:  func() (string, error) { return "desktop", nil },
		},
		workflow.Options{ProfileDir: profileDir},
	)
	if err != nil {
		t.Fatal(err)
	}

	view := NewMachines(session).View()
	for _, want := range []string{
		"No machine-specific paths needed",
		"only matters when a Resource needs a different location",
		"There is nothing to configure on this screen.",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("empty Machines missing %q:\n%s", want, view)
		}
	}
}

func TestMachinesSummarizesOverridesByCategoryAndNavigates(t *testing.T) {
	profileDir, stateHome := t.TempDir(), t.TempDir()
	data := profile.New("test", time.Now())
	data.Resources.Items = []profile.Resource{{ID: "projects", Path: "~/Projects"}}
	data.Machines.Items = []profile.Machine{{Name: "desktop", Policy: policy.Rules{
		Capture: []policy.Rule{{Category: "packages", Target: "official:git", Setting: policy.SettingDisabled}, {Category: "config", Target: ".config/nvim", Setting: policy.SettingDisabled}},
		Restore: []policy.Rule{{Category: "packages", Target: "official:git", Setting: policy.SettingDisabled}},
	}}}
	if err := profile.Save(profileDir, data); err != nil {
		t.Fatal(err)
	}
	session, err := workflow.Open(workflow.Dependencies{StateHome: func() (string, error) { return stateHome, nil }}, workflow.Options{ProfileDir: profileDir, ExplicitMachine: "desktop"})
	if err != nil {
		t.Fatal(err)
	}
	screen := NewMachines(session)
	view := screen.View()
	for _, want := range []string{"CATEGORY", "OVERRIDES", "config", "packages", "\n\nPolicy overrides\n", "\n\nResource paths\n"} {
		if !strings.Contains(view, want) {
			t.Fatalf("machine policy summary missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "[/]") || strings.Contains(view, "open policy") {
		t.Fatalf("machine policy key hints duplicate the footer above the table:\n%s", view)
	}
	if got := strings.Count(view, "> "); got != 1 {
		t.Fatalf("Machines must show exactly one focused selection, got %d:\n%s", got, view)
	}
	screen.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	view = screen.View()
	if !strings.Contains(view, "> config") || strings.Contains(view, "> desktop") || strings.Contains(view, "> projects") {
		t.Fatalf("Tab did not focus the policy summary as its own region:\n%s", view)
	}
	if detail := screen.DetailView(); !strings.Contains(detail, "explicit Capture or Restore overrides") {
		t.Fatalf("policy summary purpose is unclear in Details:\n%s", detail)
	}
	cmd := screen.Update(tea.KeyPressMsg{Code: 'o'})
	if cmd == nil {
		t.Fatal("machine override summary did not offer navigation")
	}
	if msg, ok := cmd().(PolicyNavigation); !ok || msg.Category != "config" || msg.Machine != "desktop" {
		t.Fatalf("policy navigation = %#v", msg)
	}
	screen.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	view = screen.View()
	if !strings.Contains(view, "> projects") || strings.Contains(view, "> desktop") || strings.Contains(view, "> config") {
		t.Fatalf("second Tab did not focus Resource paths:\n%s", view)
	}

	screen = NewMachines(session)
	screen.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	view = screen.View()
	if !strings.Contains(view, "> desktop") || strings.Contains(view, "> projects") {
		t.Fatalf("right navigation still jumps between vertically stacked regions:\n%s", view)
	}
}

func TestMachinesTableKeepsDefaultsAndOverridesScannable(t *testing.T) {
	profileDir, stateHome := t.TempDir(), t.TempDir()
	data := profile.New("test", time.Now())
	data.Resources.Items = []profile.Resource{{ID: "projects", Path: "~/Projects"}}
	data.Machines.Items = []profile.Machine{
		{Name: "desktop", RestoreConflicts: policy.ConflictForce, RestoreConvergence: policy.ConvergenceExact, Policy: policy.Rules{Restore: []policy.Rule{{Category: "packages", Target: "official:git", Setting: policy.SettingDisabled}}}},
		{Name: "laptop"},
	}
	if err := profile.Save(profileDir, data); err != nil {
		t.Fatal(err)
	}
	session, err := workflow.Open(workflow.Dependencies{StateHome: func() (string, error) { return stateHome, nil }}, workflow.Options{ProfileDir: profileDir, ExplicitMachine: "desktop"})
	if err != nil {
		t.Fatal(err)
	}
	for _, width := range []int{140, 100, 80} {
		screen := NewMachines(session)
		screen.SetSize(width, 30)
		view := screen.View()
		for _, want := range []string{"MACHINE", "ACTIVE", "RESTORE DEFAULT", "OVERRIDES", "desktop", "Yes", "Force / Exact", "packages", "1", "Resource paths", "projects"} {
			if !strings.Contains(view, want) {
				t.Fatalf("%d-column Machines view missing %q:\n%s", width, want, view)
			}
		}
		if strings.Contains(view, "Restore defaults for desktop:") {
			t.Fatalf("%d-column Machines view retained prose summary:\n%s", width, view)
		}
	}
}

func TestMachinesWithPortableResourcesExplainsOverridesWithoutHidingResource(t *testing.T) {
	profileDir, stateHome := t.TempDir(), t.TempDir()
	data := profile.New("test", time.Now())
	data.Resources.Items = []profile.Resource{
		{ID: "projects", Path: "~/Projects"},
	}
	if err := profile.Save(profileDir, data); err != nil {
		t.Fatal(err)
	}
	session, err := workflow.Open(
		workflow.Dependencies{
			StateHome: func() (string, error) { return stateHome, nil },
			Hostname:  func() (string, error) { return "desktop", nil },
		},
		workflow.Options{ProfileDir: profileDir},
	)
	if err != nil {
		t.Fatal(err)
	}

	view := NewMachines(session).View()
	for _, want := range []string{
		"Portable paths are in use",
		"machine-specific mapping only when one computer needs a different location",
		"projects",
		"~/Projects",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("portable Machines missing %q:\n%s", want, view)
		}
	}
}

// TestMachinesZeroResourcesWithDormantMappingKeepsNormalPresentation
// reproduces a machine overlay whose Resource was later untracked: the
// Resources list is now empty, but the dormant mapping is still meaningful
// saved state that the full-screen "nothing to configure" guidance must not
// replace.
func TestMachinesZeroResourcesWithDormantMappingKeepsNormalPresentation(t *testing.T) {
	profileDir, stateHome := t.TempDir(), t.TempDir()
	data := profile.New("test", time.Now())
	data.Resources.Items = nil
	data.Machines.Items = []profile.Machine{{Name: "desktop", ResourcePaths: []profile.MachineResourcePath{{Resource: "retired", Path: "/mnt/retired"}}}}
	if err := profile.Save(profileDir, data); err != nil {
		t.Fatal(err)
	}
	session, err := workflow.Open(
		workflow.Dependencies{StateHome: func() (string, error) { return stateHome, nil }, Hostname: func() (string, error) { return "desktop", nil }},
		workflow.Options{ProfileDir: profileDir, ExplicitMachine: "desktop"},
	)
	if err != nil {
		t.Fatal(err)
	}

	view := NewMachines(session).View()
	for _, want := range []string{"desktop", "retired", "/mnt/retired", "dormant"} {
		if !strings.Contains(view, want) {
			t.Fatalf("dormant mapping missing %q with zero current Resources:\n%s", want, view)
		}
	}
	if strings.Contains(view, "There is nothing to configure on this screen.") {
		t.Fatalf("real saved overlay state was replaced with the empty-screen guidance:\n%s", view)
	}
}

// TestMachinesPortableGuidanceAtConstrainedHeightKeepsResourceRowsVisible
// reproduces the supported 80x18 terminal, where Machines receives roughly a
// 78x10 content budget. The "Portable paths are in use" note must not push
// the resource mapping table past the outer panel's height budget.
func TestMachinesPortableGuidanceAtConstrainedHeightKeepsResourceRowsVisible(t *testing.T) {
	profileDir, stateHome := t.TempDir(), t.TempDir()
	data := profile.New("test", time.Now())
	data.Resources.Items = []profile.Resource{
		{ID: "alpha", Path: "~/alpha"},
		{ID: "bravo", Path: "~/bravo"},
		{ID: "charlie", Path: "~/charlie"},
		{ID: "delta", Path: "~/delta"},
		{ID: "echo", Path: "~/echo"},
		{ID: "foxtrot", Path: "~/foxtrot"},
	}
	if err := profile.Save(profileDir, data); err != nil {
		t.Fatal(err)
	}
	session, err := workflow.Open(
		workflow.Dependencies{StateHome: func() (string, error) { return stateHome, nil }, Hostname: func() (string, error) { return "desktop", nil }},
		workflow.Options{ProfileDir: profileDir},
	)
	if err != nil {
		t.Fatal(err)
	}

	screen := NewMachines(session)
	screen.SetSize(78, 10)                           // the real width/height Machines receives at 80x18.
	screen.Update(tea.KeyPressMsg{Code: tea.KeyTab}) // focus the mapping table.
	for i := 0; i < len(data.Resources.Items)-1; i++ {
		screen.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}

	view := screen.View()
	lines := strings.Split(view, "\n")
	if len(lines) > 10 {
		t.Fatalf("Machines.View() returned %d lines for a 10-line budget; the outer panel would clip it instead of Machines scrolling itself:\n%s", len(lines), view)
	}
	if !strings.Contains(view, "Portable paths are in use") {
		t.Fatalf("portable guidance missing:\n%s", view)
	}
	last := data.Resources.Items[len(data.Resources.Items)-1].ID
	if !strings.Contains(view, last) {
		t.Fatalf("selected resource %q not visible at 80x18-equivalent size:\n%s", last, view)
	}
}

func TestMachinesAt80ColumnScreenKeepsAllThreeRegionsVisible(t *testing.T) {
	profileDir, stateHome := t.TempDir(), t.TempDir()
	data := profile.New("test", time.Now())
	data.Resources.Items = []profile.Resource{{ID: "projects", Path: "~/Projects"}}
	data.Machines.Items = []profile.Machine{{Name: "desktop", Policy: policy.Rules{Capture: []policy.Rule{{Category: "packages", Target: "official:git", Setting: policy.SettingDisabled}}}}}
	if err := profile.Save(profileDir, data); err != nil {
		t.Fatal(err)
	}
	session, err := workflow.Open(workflow.Dependencies{StateHome: func() (string, error) { return stateHome, nil }}, workflow.Options{ProfileDir: profileDir, ExplicitMachine: "desktop"})
	if err != nil {
		t.Fatal(err)
	}
	screen := NewMachines(session)
	screen.SetSize(78, 21)
	view := screen.View()
	for _, want := range []string{"Machines", "Policy overrides", "Resource paths", "projects"} {
		if !strings.Contains(view, want) {
			t.Fatalf("80-column Machines lost %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "\n\nPolicy overrides") || strings.Contains(view, "\n\nResource paths") {
		t.Fatalf("80-column Machines kept spacious region separators despite its reduced content budget:\n%s", view)
	}
	if lines := strings.Count(view, "\n") + 1; lines > 21 {
		t.Fatalf("80-column Machines emitted %d lines for a 21-line budget:\n%s", lines, view)
	}
}

func TestMachineScreenShowsSelectedMappingsAndDormantState(t *testing.T) {
	profileDir, stateHome := t.TempDir(), t.TempDir()
	data := profile.New("test", time.Now())
	data.Resources.Items = []profile.Resource{{ID: "projects", Path: "~/Projects"}}
	data.Machines.Items = []profile.Machine{{Name: "desktop", ResourcePaths: []profile.MachineResourcePath{{Resource: "projects", Path: "~/Code"}, {Resource: "retired", Path: "/mnt/retired"}}}}
	if err := profile.Save(profileDir, data); err != nil {
		t.Fatal(err)
	}
	session, err := workflow.Open(workflow.Dependencies{StateHome: func() (string, error) { return stateHome, nil }, Hostname: func() (string, error) { return "desktop", nil }}, workflow.Options{ProfileDir: profileDir, ExplicitMachine: "desktop"})
	if err != nil {
		t.Fatal(err)
	}
	screen := NewMachines(session)
	view := screen.View()
	for _, want := range []string{"MACHINE", "ACTIVE", "> desktop", "Yes", "Resource paths", "PORTABLE", "projects", "~/Projects", "~/Code", "override", "retired", "/mnt/retired", "dormant"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "> projects") {
		t.Fatalf("unfocused Resource paths table shows a competing selection:\n%s", view)
	}
	screen.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	view = screen.View()
	if !strings.Contains(view, "> projects") || strings.Contains(view, "> desktop") {
		t.Fatalf("Tab did not move the sole visible selection to Resource paths:\n%s", view)
	}
}

func TestMachineScreenSanitizesExternalValues(t *testing.T) {
	screen := &Machines{err: errors.New("bad\nerror\x1b")}
	if view := screen.View(); strings.Contains(view, "\x1b") || !strings.Contains(view, "bad?error?") {
		t.Fatalf("unsafe error view=%q", view)
	}
	screen = &Machines{name: "bad\nname\x1b", confirm: "rename"}
	request := screen.confirmModal()().(components.ModalRequest)
	if strings.Contains(request.Content, "\x1b") || !strings.Contains(request.Content, "bad?name?") {
		t.Fatalf("unsafe confirmation=%q", request.Content)
	}
}

func TestMachineScreenUnmapIsOnlyAvailableForOverrides(t *testing.T) {
	profileDir, stateHome := t.TempDir(), t.TempDir()
	data := profile.New("test", time.Now())
	data.Resources.Items = []profile.Resource{{ID: "projects", Path: "~/Projects"}}
	data.Machines.Items = []profile.Machine{{Name: "desktop"}}
	if err := profile.Save(profileDir, data); err != nil {
		t.Fatal(err)
	}
	session, err := workflow.Open(workflow.Dependencies{StateHome: func() (string, error) { return stateHome, nil }, Hostname: func() (string, error) { return "desktop", nil }}, workflow.Options{ProfileDir: profileDir, ExplicitMachine: "desktop"})
	if err != nil {
		t.Fatal(err)
	}
	screen := NewMachines(session)
	screen.region = machineRegionResources
	if row := screen.selectedMapping(); row.override {
		t.Fatalf("portable mapping reported as override: %#v", row)
	}
	if cmd := screen.Update(tea.KeyPressMsg{Code: 'u'}); cmd != nil {
		t.Fatal("portable mapping must not be unmapped")
	}
}

func TestMachineScreenDownSelectsMachineAndMapping(t *testing.T) {
	profileDir, stateHome := t.TempDir(), t.TempDir()
	data := profile.New("test", time.Now())
	data.Resources.Items = []profile.Resource{{ID: "projects", Path: "~/Projects"}, {ID: "notes", Path: "~/Notes"}}
	data.Machines.Items = []profile.Machine{{Name: "desktop"}, {Name: "laptop", ResourcePaths: []profile.MachineResourcePath{{Resource: "projects", Path: "~/LaptopProjects"}, {Resource: "notes", Path: "~/LaptopNotes"}}}}
	if err := profile.Save(profileDir, data); err != nil {
		t.Fatal(err)
	}
	session, err := workflow.Open(workflow.Dependencies{StateHome: func() (string, error) { return stateHome, nil }}, workflow.Options{ProfileDir: profileDir})
	if err != nil {
		t.Fatal(err)
	}
	screen := NewMachines(session)
	screen.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if machine := screen.selectedMachine(); machine.Name != "laptop" {
		t.Fatalf("selected machine=%#v", machine)
	}
	screen.region = machineRegionResources
	screen.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if mapping := screen.selectedMapping(); mapping.id != "projects" {
		t.Fatalf("selected mapping=%#v", mapping)
	}
}

func TestMachineScreenKeepsPaneNavigationUntilOuterEdge(t *testing.T) {
	screen := &Machines{}
	for _, key := range []string{"tab", "j", "down", "k", "up"} {
		if !screen.OwnsWorkspaceKey(key) {
			t.Errorf("Machines must own %q in its workspace", key)
		}
	}
	for _, key := range []string{"h", "left", "l", "right", ":"} {
		if screen.OwnsWorkspaceKey(key) {
			t.Fatalf("stacked Machines regions must not own horizontal key %q", key)
		}
	}
}
