package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

// blockMachineFile makes a save that adds machine name commit and then fail
// to apply: a non-empty directory sits where the machine's file goes, so
// moving the staged file into place fails after the commit point.
func blockMachineFile(t *testing.T, profileDir, name string) (unblock func()) {
	t.Helper()
	blocker := filepath.Join(profileDir, "machines", name+".toml")
	if err := os.MkdirAll(filepath.Join(blocker, "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	return func() {
		if err := os.RemoveAll(blocker); err != nil {
			t.Fatal(err)
		}
	}
}

func machineNames(data profile.Data) []string {
	var names []string
	for _, m := range data.Machines.Items {
		names = append(names, m.Name)
	}
	return names
}

// A save that committed but could not finish is durable: a later edit must
// start from it, never from the Session's older profile, which would
// recover the committed save and then overwrite it.
func TestEditAfterACommittedButUnfinishedSaveKeepsIt(t *testing.T) {
	ctx := context.Background()
	session := newCaptureSession(t, profile.New("test", time.Now()))
	unblock := blockMachineFile(t, session.ProfileDir(), "laptop")

	err := session.AddMachine(ctx, "laptop", false)
	var committed *profile.CommittedSaveError
	if !errors.As(err, &committed) {
		t.Fatalf("AddMachine: %v, want a committed-but-unfinished save", err)
	}
	if got := machineNames(session.Profile()); len(got) != 1 || got[0] != "laptop" {
		t.Fatalf("session machines after a committed save = %v", got)
	}

	// While the save still cannot finish, no edit may proceed.
	if err := session.SetProviderCaptured(ctx, "themes", true); err == nil || !strings.Contains(err.Error(), "cannot be reloaded") {
		t.Fatalf("an edit proceeded over an unfinished save: %v", err)
	}
	// The refused edit left the committed save pending on disk, untouched.
	manifest, err := os.ReadFile(filepath.Join(session.ProfileDir(), ".blueprint-save", "manifest.json"))
	if err != nil || !strings.Contains(string(manifest), "machines/laptop.toml") {
		t.Fatalf("the committed save is no longer pending: %v", err)
	}

	unblock()
	if err := session.SetProviderCaptured(ctx, "themes", true); err != nil {
		t.Fatalf("edit after the fault cleared: %v", err)
	}
	for where, data := range map[string]profile.Data{"session": session.Profile(), "disk": mustLoad(t, session.ProfileDir())} {
		if got := machineNames(data); len(got) != 1 || got[0] != "laptop" {
			t.Fatalf("%s lost the committed machine: %v", where, got)
		}
		if !data.Manifest.Capture.Themes {
			t.Fatalf("%s lost the second edit", where)
		}
	}
}

// Every Session mutation path is guarded, not only the one that failed.
func TestEveryMutationReloadsAfterACommittedButUnfinishedSave(t *testing.T) {
	ctx := context.Background()
	for name, edit := range map[string]func(*Session) error{
		"SetProviderCaptured": func(s *Session) error { return s.SetProviderCaptured(ctx, "themes", true) },
		"AddMachine":          func(s *Session) error { return s.AddMachine(ctx, "desk", false) },
		"SetConfigPolicy":     func(s *Session) error { return s.SetConfigPolicy(ctx, "~/.config/foo", "exclude") },
		"ClearPolicy": func(s *Session) error {
			return s.ClearPolicy(PolicyScope{}, "capture", "themes", "")
		},
	} {
		t.Run(name, func(t *testing.T) {
			session := newCaptureSession(t, profile.New("test", time.Now()))
			unblock := blockMachineFile(t, session.ProfileDir(), "laptop")
			if err := session.AddMachine(ctx, "laptop", false); err == nil {
				t.Fatal("blocked save succeeded")
			}
			unblock()
			if err := edit(session); err != nil {
				t.Fatal(err)
			}
			got := machineNames(mustLoad(t, session.ProfileDir()))
			if !slices.Contains(got, "laptop") {
				t.Fatalf("%s overwrote the committed machine: %v", name, got)
			}
		})
	}
}

func mustLoad(t *testing.T, dir string) profile.Data {
	t.Helper()
	data, err := profile.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
