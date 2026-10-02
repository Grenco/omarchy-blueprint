package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/machine"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	resourcesprovider "github.com/Grenco/omarchy-blueprint/internal/providers/resources"
)

func TestResourcesReadCycleFreezesActualMachineBinding(t *testing.T) {
	for _, initial := range []string{"a", ""} {
		for _, next := range []string{"b", ""} {
			t.Run(initial+"-to-"+next, func(t *testing.T) {
				dir, deps := configSandbox(t)
				home, plugins := t.TempDir(), t.TempDir()
				deps.HomeDir = func() (string, error) { return home, nil }
				deps.PluginDir = func() (string, error) { return plugins, nil }
				deps.ResourceLinkRoots = func(string) []resourcesprovider.LinkSearchRoot {
					return []resourcesprovider.LinkSearchRoot{{Path: home}}
				}
				for _, name := range []string{"portable", "a", "b"} {
					root := filepath.Join(home, name)
					if err := os.MkdirAll(root, 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(root, "file"), []byte(name), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				d, err := profile.Load(dir)
				if err != nil {
					t.Fatal(err)
				}
				d.Manifest.Capture.Resources = true
				d.Resources.Items = []profile.Resource{{ID: "project", Path: "~/portable", Kind: "directory", Strategy: "copy"}}
				d.Machines.Items = []profile.Machine{
					{Name: "a", ResourcePaths: []profile.MachineResourcePath{{Resource: "project", Path: "~/a"}}},
					{Name: "b", ResourcePaths: []profile.MachineResourcePath{{Resource: "project", Path: "~/b"}}},
				}
				if err := profile.Save(dir, d); err != nil {
					t.Fatal(err)
				}
				state, _ := deps.StateHome()
				store := machine.BindingStore{StateHome: state}
				if initial != "" {
					if err := store.Save(dir, initial); err != nil {
						t.Fatal(err)
					}
				}
				session, err := openWorkflow(deps, &options{profileDir: dir})
				if err != nil {
					t.Fatal(err)
				}
				ctx := context.Background()
				cycle, err := session.BeginRead(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer cycle.Close()
				before, err := cycle.PolicyTargets(ctx, "resources")
				if err != nil {
					t.Fatal(err)
				}
				beforePlan, err := cycle.PreviewRestore(ctx, "resources", nil)
				if err != nil {
					t.Fatal(err)
				}
				if next == "" {
					err = store.Clear(dir)
				} else {
					err = store.Save(dir, next)
				}
				if err != nil {
					t.Fatal(err)
				}
				after, err := cycle.PolicyTargets(ctx, "resources")
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(before, after) {
					t.Fatal("Resources reread binding inside frozen cycle")
				}
				afterPlan, err := cycle.PreviewRestore(ctx, "resources", nil)
				if err != nil || !reflect.DeepEqual(beforePlan, afterPlan) {
					t.Fatal("Restore preview changed binding inside cycle", err)
				}
				fresh, err := session.PolicyTargets(ctx, "resources")
				if err != nil {
					t.Fatal(err)
				}
				if initial != next && reflect.DeepEqual(before, fresh) {
					t.Fatal("new cycle failed to observe binding change")
				}
			})
		}
	}
}

func TestStatusOutputUsesObservedMachineRatherThanEarlierBinding(t *testing.T) {
	dir, deps := configSandbox(t)
	home, plugins := t.TempDir(), t.TempDir()
	deps.HomeDir = func() (string, error) { return home, nil }
	deps.PluginDir = func() (string, error) { return plugins, nil }
	deps.ResourceLinkRoots = func(string) []resourcesprovider.LinkSearchRoot { return nil }
	d, err := profile.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	d.Manifest.Capture.Resources = true
	d.Resources.Items = []profile.Resource{{ID: "project", Path: "~/portable", Kind: "directory", Strategy: "copy"}}
	d.Machines.Items = []profile.Machine{{Name: "a", ResourcePaths: []profile.MachineResourcePath{{Resource: "project", Path: "~/a"}}}, {Name: "b", ResourcePaths: []profile.MachineResourcePath{{Resource: "project", Path: "~/b"}}}}
	if err := profile.Save(dir, d); err != nil {
		t.Fatal(err)
	}
	state, _ := deps.StateHome()
	store := machine.BindingStore{StateHome: state}
	if err := store.Save(dir, "a"); err != nil {
		t.Fatal(err)
	}
	calls := 0
	deps.StateHome = func() (string, error) {
		calls++
		if calls == 2 {
			if err := store.Save(dir, "b"); err != nil {
				return "", err
			}
		}
		return state, nil
	}
	var out, stderr bytes.Buffer
	deps.Out, deps.Err = &out, &stderr
	code := Execute(context.Background(), []string{"--profile", dir, "--json", "status", "resources"}, deps)
	if code != 0 && code != 2 {
		t.Fatal("status failed", stderr.String())
	}
	var envelope struct {
		Data struct {
			Machine   machineOutput
			Resources resourcesOutput
		}
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Machine.Name != "b" || len(envelope.Data.Resources.Items) != 1 || envelope.Data.Resources.Items[0].EffectivePath != filepath.Join(home, "b") {
		t.Fatal("CLI labelled fresh observations with earlier binding")
	}
}
