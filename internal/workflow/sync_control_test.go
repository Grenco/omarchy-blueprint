package workflow

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func syncCommitProfile(t *testing.T, root string, data profile.Data) {
	t.Helper()
	if err := profile.Save(root, data); err != nil {
		t.Fatal(err)
	}
	syncGit(t, root, "add", ".")
	syncGit(t, root, "commit", "-m", "control fixture")
}

func TestSyncRemoteObservationScopeChangesRequireReinspection(t *testing.T) {
	for _, name := range []string{"include", "exclude", "ignored-links"} {
		t.Run(name, func(t *testing.T) {
			s, _, data, _, peer, binds, observes := syncTwoClones(t)
			switch name {
			case "include":
				data.Config.Included = []string{".config/writer/themes"}
			case "exclude":
				data.Config.Excluded = []string{".config/hypr"}
			case "ignored-links":
				data.Resources.IgnoredLinks = []string{"~/.config/test-link"}
			}
			syncCommitProfile(t, peer, data)
			syncGit(t, peer, "push")
			plan, err := s.InspectSync(context.Background(), SyncInspectOptions{Fetch: true})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, b := range plan.Blockers {
				found = found || b.Code == "control-reinspection"
			}
			if !found || len(plan.Items) != 0 {
				t.Fatal("old observation scope can certify settlement", plan)
			}
			if binds.Load() != 1 || observes.Load() != 0 {
				t.Fatal("reinspection reused/scanned invalid scope", binds.Load(), observes.Load())
			}
			if plan.Control == nil || plan.Control.History != "remote" || plan.Control.SelectedSource != "remote" {
				t.Fatal("scope blocker hid control intent", plan.Control)
			}
			encoded, err := json.Marshal(plan)
			if err != nil {
				t.Fatal(err)
			}
			for _, private := range append(append(data.Config.Included, data.Config.Excluded...), data.Resources.IgnoredLinks...) {
				if strings.Contains(string(encoded), private) {
					t.Fatal("control interpretation input leaked")
				}
			}
			bootstrap, err := s.InspectSyncBootstrap(context.Background(), SyncInspectOptions{})
			if err != nil {
				t.Fatal(err)
			}
			found = false
			for _, b := range bootstrap.Blockers {
				found = found || b.Code == "control-reinspection"
			}
			if !found || bootstrap.ConvergenceEligible || len(bootstrap.Items) != 0 {
				t.Fatal("bootstrap certified old scope", bootstrap)
			}
		})
	}
}

func TestSyncInspectionExposesControlHistorySeparately(t *testing.T) {
	for _, tc := range []struct {
		name, history, selected string
		local, remote           policy.Setting
	}{
		{"unchanged", "unchanged", "remote", "", ""},
		{"local-only", "local", "local", policy.SettingDisabled, ""},
		{"remote-only", "remote", "remote", "", policy.SettingDisabled},
		{"converged", "converged", "remote", policy.SettingDisabled, policy.SettingDisabled},
		{"conflicting", "conflict", "none", policy.SettingDisabled, policy.SettingEnabled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, data, _, peer, binds, observes := syncTwoClones(t)
			for _, side := range []struct {
				root    string
				setting policy.Setting
			}{{s.ProfileDir(), tc.local}, {peer, tc.remote}} {
				if side.setting == "" {
					continue
				}
				d := profile.CloneData(data)
				d.Policy.Restore = []policy.Rule{{Category: "packages", Target: "official:a", Setting: side.setting}}
				syncCommitProfile(t, side.root, d)
			}
			if tc.remote != "" {
				syncGit(t, peer, "push")
			}
			plan, err := s.InspectSync(context.Background(), SyncInspectOptions{Fetch: true})
			if err != nil {
				t.Fatal(err)
			}
			// Decode the public contract so the initial regression compiles before
			// the typed control model exists, and catches absent JSON evidence.
			encoded, err := json.Marshal(plan)
			if err != nil {
				t.Fatal(err)
			}
			var output struct {
				Control struct {
					History             string `json:"history"`
					SelectedSource      string `json:"selected_source"`
					Base, Local, Remote string
				} `json:"control"`
			}
			if err := json.Unmarshal(encoded, &output); err != nil {
				t.Fatal(err)
			}
			if output.Control.History != tc.history || output.Control.SelectedSource != tc.selected {
				t.Fatal("control change invisible or misclassified", string(encoded))
			}
			if tc.selected == "none" {
				if plan.Identity.ControlFingerprint != "" || len(plan.Items) != 0 {
					t.Fatal("conflict pretended to select authoritative interpretation", plan)
				}
			} else {
				selected := output.Control.Remote
				if tc.selected == "local" {
					selected = output.Control.Local
				}
				if plan.Identity.ControlFingerprint != selected {
					t.Fatal("selection disagrees with chosen control fingerprint")
				}
			}
			if len(output.Control.Base) != 64 || len(output.Control.Local) != 64 || len(output.Control.Remote) != 64 {
				t.Fatal("missing canonical B/L/R control fingerprints", output)
			}
			conflict := false
			for _, b := range plan.Blockers {
				conflict = conflict || b.Code == "control-conflict"
			}
			if conflict != (tc.history == "conflict") {
				t.Fatal("wrong conflict blocker", plan)
			}
			for _, item := range plan.Items {
				if item.History != "unchanged" {
					t.Fatal("control became runtime value", item)
				}
			}
			if binds.Load() != 1 || observes.Load() > 1 {
				t.Fatal("duplicate observation cycle", binds.Load(), observes.Load())
			}
			if tc.name == "unchanged" && !reflect.DeepEqual(output.Control.Base, output.Control.Local) {
				t.Fatal(output)
			}
		})
	}
}

func TestSyncObservationScopeUsesSetsNotSerializationOrder(t *testing.T) {
	local := profile.Data{}
	local.Config.Included = []string{".config/a", ".config/b"}
	local.Config.Excluded = []string{".config/c", ".config/d"}
	local.Resources.IgnoredLinks = []string{"~/x", "~/y"}
	remote := profile.CloneData(local)
	remote.Config.Included = []string{".config/b", ".config/a", ".config/a"}
	remote.Config.Excluded = []string{".config/d", ".config/c"}
	remote.Resources.IgnoredLinks = []string{"~/y", "~/x"}
	if syncObservationScopeChanged(local, remote) {
		t.Fatal("ordering-only change invalidated observation")
	}
	baseControl, err := syncControl(local, "test")
	if err != nil {
		t.Fatal(err)
	}
	remoteControl, err := syncControl(remote, "test")
	if err != nil {
		t.Fatal(err)
	}
	if got := classifySyncControl(baseControl, baseControl, remoteControl); got.History != "unchanged" {
		t.Fatal("ordering-only change became control drift", got)
	}
	if syncObservationScopeChanged(profile.Data{}, profile.Data{Config: profile.Configs{Included: []string{}, Excluded: []string{}}}) {
		t.Fatal("nil/empty list became observation drift")
	}
}
