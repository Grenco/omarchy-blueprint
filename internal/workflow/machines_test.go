package workflow

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/machine"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func TestMachineWorkflowLifecycleAndUnmapDoesNotDuplicateMappings(t *testing.T) {
	profileDir, stateHome, home := t.TempDir(), t.TempDir(), t.TempDir()
	data := profile.New("test", time.Now())
	data.Resources.Items = []profile.Resource{
		{ID: "projects", Path: "~/Projects", Kind: "directory", Strategy: "copy"},
		{ID: "notes", Path: "~/Notes", Kind: "directory", Strategy: "copy"},
	}
	if err := profile.Save(profileDir, data); err != nil {
		t.Fatal(err)
	}
	session, err := Open(Dependencies{
		Now:       func() time.Time { return time.Unix(1, 0) },
		StateHome: func() (string, error) { return stateHome, nil },
		HomeDir:   func() (string, error) { return home, nil },
		Hostname:  func() (string, error) { return "Framework", nil },
	}, Options{ProfileDir: profileDir})
	if err != nil {
		t.Fatal(err)
	}
	if err := session.AddMachine(context.Background(), "laptop", true); err != nil {
		t.Fatal(err)
	}
	if session.Machine().Name != "laptop" {
		t.Fatalf("machine=%#v", session.Machine())
	}
	if err := session.MapResource(context.Background(), "laptop", "projects", filepath.Join(t.TempDir(), "projects")); err != nil {
		t.Fatal(err)
	}
	if err := session.MapResource(context.Background(), "laptop", "notes", filepath.Join(t.TempDir(), "notes")); err != nil {
		t.Fatal(err)
	}
	if err := session.UnmapResource(context.Background(), "laptop", "projects"); err != nil {
		t.Fatal(err)
	}
	got := session.Profile().Machines.Items[0].ResourcePaths
	if len(got) != 1 || got[0].Resource != "notes" {
		t.Fatalf("mappings after unmap=%#v", got)
	}
	if err := session.RenameMachine(context.Background(), "laptop", "desktop"); err != nil {
		t.Fatal(err)
	}
	if bound, err := (machine.BindingStore{StateHome: stateHome}).Load(profileDir); err != nil || bound != "desktop" {
		t.Fatalf("binding=%q err=%v", bound, err)
	}
	if err := session.ClearMachine(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := session.RemoveMachine(context.Background(), "desktop"); err != nil {
		t.Fatal(err)
	}
	if len(session.Profile().Machines.Items) != 0 {
		t.Fatalf("machines=%#v", session.Profile().Machines)
	}
}

func TestMachineMappingsRemainIndependent(t *testing.T) {
	profileDir, stateHome, home := t.TempDir(), t.TempDir(), t.TempDir()
	data := profile.New("test", time.Now())
	data.Resources.Items = []profile.Resource{{ID: "projects", Path: "~/Projects", Kind: "directory", Strategy: "copy"}}
	data.Machines.Items = []profile.Machine{{Name: "desktop"}, {Name: "laptop"}}
	if err := profile.Save(profileDir, data); err != nil {
		t.Fatal(err)
	}
	session, err := Open(Dependencies{Now: time.Now, StateHome: func() (string, error) { return stateHome, nil }, HomeDir: func() (string, error) { return home, nil }}, Options{ProfileDir: profileDir})
	if err != nil {
		t.Fatal(err)
	}
	if err := session.MapResource(context.Background(), "desktop", "projects", "~/DesktopProjects"); err != nil {
		t.Fatal(err)
	}
	if err := session.MapResource(context.Background(), "laptop", "projects", "~/LaptopProjects"); err != nil {
		t.Fatal(err)
	}
	for _, machine := range session.Profile().Machines.Items {
		if len(machine.ResourcePaths) != 1 || machine.ResourcePaths[0].Path != map[string]string{"desktop": "~/DesktopProjects", "laptop": "~/LaptopProjects"}[machine.Name] {
			t.Fatalf("machine mappings=%#v", session.Profile().Machines.Items)
		}
	}
}
