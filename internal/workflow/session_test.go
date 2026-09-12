package workflow

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func TestWorkflowOpenCanonicalizesProfileAndResolvesMachine(t *testing.T) {
	profileDir, stateHome := t.TempDir(), t.TempDir()
	data := profile.New("test", time.Now())
	data.Machines.Items = []profile.Machine{{Name: "desktop"}}
	if err := profile.Save(profileDir, data); err != nil {
		t.Fatal(err)
	}
	session, err := Open(Dependencies{StateHome: func() (string, error) { return stateHome, nil }}, Options{ProfileDir: filepath.Join(profileDir, "."), ExplicitMachine: "desktop"})
	if err != nil {
		t.Fatal(err)
	}
	if session.ProfileDir() != profileDir {
		t.Errorf("profile dir = %q, want %q", session.ProfileDir(), profileDir)
	}
	if session.Machine().Name != "desktop" || session.Machine().Source != "explicit" {
		t.Errorf("machine = %#v", session.Machine())
	}
}

func TestSessionSetConfigPolicySavesAndReloads(t *testing.T) {
	profileDir, stateHome := t.TempDir(), t.TempDir()
	data := profile.New("test", time.Now())
	data.Config.Included = []string{".config/nvim", ".config/nvim/lua"}
	data.Config.Excluded = []string{".config/nvim/cache"}
	if err := profile.Save(profileDir, data); err != nil {
		t.Fatal(err)
	}
	session, err := Open(Dependencies{StateHome: func() (string, error) { return stateHome, nil }, Now: func() time.Time { return time.Unix(1, 0) }}, Options{ProfileDir: profileDir})
	if err != nil {
		t.Fatal(err)
	}
	if err := session.SetConfigPolicy(context.Background(), ".config/nvim", "auto"); err != nil {
		t.Fatal(err)
	}
	if got := session.Profile().Config.Included; len(got) != 1 || got[0] != ".config/nvim/lua" {
		t.Fatalf("included=%v", got)
	}
	if err := session.SetConfigPolicy(context.Background(), ".config/nvim", "invalid"); err == nil {
		t.Fatal("invalid policy accepted")
	}
}
