package screens

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

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
	for _, want := range []string{"desktop *", "Resource       Portable", "projects       ~/Projects           ~/Code               override", "retired        -                    /mnt/retired         dormant"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
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
	screen.focusMappings = true
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
	screen.focusMappings = true
	screen.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if mapping := screen.selectedMapping(); mapping.id != "projects" {
		t.Fatalf("selected mapping=%#v", mapping)
	}
}
