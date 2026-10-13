package workflow

import (
	"context"
	"github.com/Grenco/omarchy-blueprint/internal/command"
	"github.com/Grenco/omarchy-blueprint/internal/gittest"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/profilegit"
	"github.com/Grenco/omarchy-blueprint/internal/providers/packages"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
	"github.com/Grenco/omarchy-blueprint/internal/syncprofile"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type syncPackageProvider struct{ statusTestProvider }

func (syncPackageProvider) SyncDesired(d profile.Data) ([]syncengine.DesiredTarget, error) {
	return (packages.Provider{}).SyncDesired(d)
}
func (syncPackageProvider) OverlaySyncTarget(ctx context.Context, b *syncprofile.Builder, v syncprofile.View, key string) error {
	return (packages.Provider{}).OverlaySyncTarget(ctx, b, v, key)
}

func syncGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	c := exec.Command("git", append([]string{"-C", root}, args...)...)
	c.Env = gittest.DisableAutomaticMaintenanceEnv(os.Environ())
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git test command failed: %v %s", err, out)
	}
	return strings.TrimSpace(string(out))
}
func syncViewRepo(t *testing.T) (*Session, profile.Data, string) {
	t.Helper()
	root := t.TempDir()
	data := profile.New("test", time.Time{})
	data.Manifest.Capture.Packages = true
	data.Packages.Official = []string{"z", "a"}
	data.Machines.Items = []profile.Machine{{Name: "test-machine"}}
	if err := profile.Save(root, data); err != nil {
		t.Fatal(err)
	}
	syncGit(t, root, "init", "-b", "main")
	syncGit(t, root, "config", "user.email", "test@example.invalid")
	syncGit(t, root, "config", "user.name", "Test")
	syncGit(t, root, "add", ".")
	syncGit(t, root, "commit", "-m", "base")
	s, err := Open(Dependencies{Runner: command.SystemRunner{}, StateHome: func() (string, error) { return t.TempDir(), nil }}, Options{ProfileDir: root, ExplicitMachine: "test-machine"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetProviders([]Provider{syncPackageProvider{statusTestProvider{id: "packages"}}}); err != nil {
		t.Fatal(err)
	}
	return s, data, syncGit(t, root, "rev-parse", "HEAD")
}

func TestSyncProfileViewExactIsolatedLifecycle(t *testing.T) {
	s, d, base := syncViewRepo(t)
	root := s.ProfileDir()
	d.Packages.Official = []string{"local"}
	if err := profile.Save(root, d); err != nil {
		t.Fatal(err)
	}
	syncGit(t, root, "add", ".")
	syncGit(t, root, "commit", "-m", "local")
	head := syncGit(t, root, "rev-parse", "HEAD")
	before := syncGit(t, root, "status", "--porcelain=v2")
	service, err := profilegit.New(command.SystemRunner{}, root)
	if err != nil {
		t.Fatal(err)
	}
	v, cleanup, err := s.loadSyncProfileView(context.Background(), service, base)
	if err != nil {
		t.Fatal(err)
	}
	if v.Revision != base || v.Root == root || !reflect.DeepEqual(v.Profile.Packages.Official, []string{"a", "z"}) {
		t.Fatal(v)
	}
	if len(v.Targets) != 2 || v.Targets[0].Key.Target != "official:a" {
		t.Fatal(v.Targets)
	}
	temp := v.Root
	cleanup()
	if _, err := os.Stat(temp); !os.IsNotExist(err) {
		t.Fatal("materialization leaked")
	}
	if syncGit(t, root, "rev-parse", "HEAD") != head || syncGit(t, root, "status", "--porcelain=v2") != before {
		t.Fatal("active checkout changed")
	}
	// Unsupported captured categories must not disappear from historical truth.
	d.Manifest.Capture.Defaults = true
	d.Defaults.Browser = "firefox"
	if err := profile.Save(root, d); err != nil {
		t.Fatal(err)
	}
	syncGit(t, root, "add", ".")
	syncGit(t, root, "commit", "-m", "unsupported")
	_, cleanup, err = s.loadSyncProfileView(context.Background(), service, syncGit(t, root, "rev-parse", "HEAD"))
	if cleanup != nil {
		cleanup()
	}
	if err == nil {
		t.Fatal("unsupported captured provider ignored")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err = s.loadSyncProfileView(ctx, service, base); err != context.Canceled {
		t.Fatal(err)
	}
}

func TestSyncControlNormalizationAndAuthority(t *testing.T) {
	d := profile.New("test", time.Time{})
	d.Machines.Items = []profile.Machine{{Name: "test-machine", ResourcePaths: []profile.MachineResourcePath{{Resource: "z", Path: "~/z"}, {Resource: "a", Path: "~/a"}}}}
	d.Policy.Capture = []policy.Rule{{Category: "services", Setting: policy.SettingDisabled}, {Category: "packages", Setting: policy.SettingEnabled}}
	a, err := syncControl(d, "test-machine")
	if err != nil {
		t.Fatal(err)
	}
	d.Manifest.Profile.UpdatedAt = time.Now()
	d.Policy.Capture[0], d.Policy.Capture[1] = d.Policy.Capture[1], d.Policy.Capture[0]
	d.Machines.Items[0].ResourcePaths[0], d.Machines.Items[0].ResourcePaths[1] = d.Machines.Items[0].ResourcePaths[1], d.Machines.Items[0].ResourcePaths[0]
	b, err := syncControl(d, "test-machine")
	if err != nil || a.Fingerprint != b.Fingerprint {
		t.Fatal("ordering/timestamp changed control")
	}
	d.Machines.Items[0].ResourcePaths[0].Path = "~/elsewhere"
	c, _ := syncControl(d, "test-machine")
	if a.Fingerprint == c.Fingerprint {
		t.Fatal("authority input omitted")
	}
	other, _ := syncControl(d, "other")
	if other.Fingerprint == c.Fingerprint {
		t.Fatal("selected identity omitted")
	}
	_ = filepath.Separator
}
