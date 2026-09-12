package screens

import (
	"strings"
	"testing"
	"time"

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
