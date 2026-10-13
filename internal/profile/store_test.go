package profile

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/policy"
)

// generations returns two distinct profiles touching every managed file:
// packages, themes, plugins, machines, policy and Resources.
func generations() (Data, Data) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	old := New("main", now)
	old.Manifest.Capture.Packages = true
	old.Packages.Official = []string{"git"}
	old.Themes = Themes{Current: "tokyo-night"}
	old.Plugins = Plugins{Items: []Plugin{{ID: "omarchy.clock", Enabled: true}}}
	old.Machines.Items = []Machine{{Name: "desktop"}, {Name: "laptop"}}
	old.Policy.Capture = []policy.Rule{{Category: "packages", Target: "official:git", Setting: policy.SettingEnabled}}
	next := New("main", now.Add(time.Hour))
	next.Manifest.Capture.Packages = true
	next.Packages.Official = []string{"git", "zoxide"}
	next.Themes = Themes{Current: "catppuccin"}
	next.Plugins = Plugins{Items: []Plugin{{ID: "omarchy.media", Enabled: true}}}
	next.Machines.Items = []Machine{{Name: "desktop"}}
	return old, next
}

func mustLoad(t *testing.T, dir string) Data {
	t.Helper()
	d, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// sameGeneration reports which saved generation d is, failing on a mix.
func sameGeneration(t *testing.T, d, old, next Data) string {
	t.Helper()
	matches := func(want Data) bool {
		return reflect.DeepEqual(d.Packages.Official, want.Packages.Official) &&
			d.Themes.Current == want.Themes.Current &&
			reflect.DeepEqual(d.Plugins, want.Plugins) &&
			len(d.Machines.Items) == len(want.Machines.Items) &&
			len(d.Policy.Capture) == len(want.Policy.Capture) &&
			d.Manifest.Profile.UpdatedAt.Equal(want.Manifest.Profile.UpdatedAt)
	}
	switch {
	case matches(old):
		return "old"
	case matches(next):
		return "new"
	}
	t.Fatalf("profile is a mix of two saves: packages %v, theme %q, plugins %v, %d machines, %d capture rules, updated %s",
		d.Packages.Official, d.Themes.Current, d.Plugins.Items, len(d.Machines.Items), len(d.Policy.Capture), d.Manifest.Profile.UpdatedAt)
	return ""
}

func injectSaveFault(t *testing.T, at string, after int) {
	t.Helper()
	previous := saveFault
	seen := 0
	saveFault = func(point string) error {
		if point != at {
			return nil
		}
		seen++
		if seen > after {
			return errors.New("simulated crash at " + point)
		}
		return nil
	}
	t.Cleanup(func() { saveFault = previous })
}

// STATE-01: an interrupted save resolves, on the next load, to the
// previous profile or the complete new one: never a mix.
func TestInterruptedSaveIsNeverObservedAsAMix(t *testing.T) {
	for _, tc := range []struct {
		name, point string
		after       int
		want        string
	}{
		{"before the commit point", "staged", 0, "old"},
		{"at the commit point", "committed", 0, "new"},
		{"after the first file moved", "applying", 0, "new"},
		{"after half the files moved", "applying", 6, "new"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			old, next := generations()
			if err := Save(dir, old); err != nil {
				t.Fatal(err)
			}
			injectSaveFault(t, tc.point, tc.after)
			err := Save(dir, next)
			if err == nil {
				t.Fatal("the simulated crash did not interrupt the save")
			}
			var committed *CommittedSaveError
			if errors.As(err, &committed) != (tc.want == "new") {
				t.Fatalf("err = %v; committed = %t, want %t", err, errors.As(err, &committed), tc.want == "new")
			}
			saveFault = func(string) error { return nil }
			if got := sameGeneration(t, mustLoad(t, dir), old, next); got != tc.want {
				t.Fatalf("after recovery the profile is the %s generation, want %s", got, tc.want)
			}
			if _, err := os.Lstat(filepath.Join(dir, transactionDir)); !os.IsNotExist(err) {
				t.Fatal("recovery left the transaction behind")
			}
			// The pruned laptop machine file goes only with the new generation.
			_, err = os.Lstat(filepath.Join(dir, "machines", "laptop.toml"))
			if (tc.want == "new") != os.IsNotExist(err) {
				t.Fatalf("machines/laptop.toml presence does not match the %s generation", tc.want)
			}
		})
	}
}

// A save that fails part-way under the old implementation left a mix: the
// failure here (an unwritable themes/ directory) used to strike after
// profile.toml and packages/ were already replaced. Now nothing live
// changes before the commit point, and a profile that cannot be completed
// is refused rather than shown half-saved.
func TestFailedSaveNeverLeavesAMixedProfile(t *testing.T) {
	dir := t.TempDir()
	old, next := generations()
	if err := Save(dir, old); err != nil {
		t.Fatal(err)
	}
	themes := filepath.Join(dir, "themes")
	if err := os.Chmod(themes, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(themes, 0o755) })
	if err := Save(dir, next); err == nil {
		t.Fatal("a save into a read-only themes/ succeeded")
	}
	if d, err := Load(dir); err == nil {
		sameGeneration(t, d, old, next)
	}
	if err := os.Chmod(themes, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := sameGeneration(t, mustLoad(t, dir), old, next); got != "new" {
		t.Fatalf("once writable, recovery produced the %s generation, want new", got)
	}
}

// SEC-01: a managed directory symlinked out of the profile (Git can commit
// symlinks) never makes Blueprint write, replace, read or delete outside it.
func TestSymlinkedManagedDirectoryCannotEscapeTheProfile(t *testing.T) {
	for _, managed := range []string{"machines", "config", "packages", "themes", "policy", "resources", "services"} {
		t.Run(managed, func(t *testing.T) {
			dir, outside := t.TempDir(), t.TempDir()
			old, next := generations()
			if err := Save(dir, old); err != nil {
				t.Fatal(err)
			}
			victims := map[string]string{"victim.toml": "do not delete", "config.toml": "secret = 'outside'", "machine-specific.txt": "keep", "official.txt": "keep", "themes.toml": "keep", "policy.toml": "keep", "resources.toml": "keep", "services.toml": "keep"}
			for name, body := range victims {
				if err := os.WriteFile(filepath.Join(outside, name), []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.RemoveAll(filepath.Join(dir, managed)); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(dir, managed)); err != nil {
				t.Fatal(err)
			}
			if err := Save(dir, next); err == nil {
				t.Fatalf("Save through a symlinked %s/ succeeded", managed)
			}
			if _, err := Load(dir); err == nil {
				t.Fatalf("Load through a symlinked %s/ succeeded", managed)
			}
			for name, body := range victims {
				got, err := os.ReadFile(filepath.Join(outside, name))
				if err != nil || string(got) != body {
					t.Fatalf("%s outside the profile was changed or removed (%q, %v)", name, got, err)
				}
			}
			entries, _ := os.ReadDir(outside)
			if len(entries) != len(victims) {
				t.Fatalf("files were created outside the profile: %d entries", len(entries))
			}
		})
	}
}

func TestSymlinkedMetadataFileIsNotReadOrFollowed(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	old, next := generations()
	if err := Save(dir, old); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(outside, "secret.toml")
	if err := os.WriteFile(secret, []byte("current = 'from-outside'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "themes", "themes.toml")
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, link); err != nil {
		t.Fatal(err)
	}
	if d, err := Load(dir); err == nil && d.Themes.Current == "from-outside" {
		t.Fatal("Load read metadata from outside the profile")
	}
	if err := Save(dir, next); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(secret); string(got) != "current = 'from-outside'\n" {
		t.Fatal("Save wrote through a symlinked metadata file")
	}
	if info, err := os.Lstat(link); err != nil || !info.Mode().IsRegular() {
		t.Fatal("Save did not replace the symlink with the profile's own file")
	}
}

func TestManagedFileHelpersAreConfined(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "shell.json"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "shell")); err != nil {
		t.Fatal(err)
	}
	if err := WriteManagedFile(dir, "shell/shell.json", []byte("x")); err == nil {
		t.Fatal("WriteManagedFile followed a symlinked shell/")
	}
	if err := RemoveManagedFile(dir, "shell/shell.json"); err == nil {
		t.Fatal("RemoveManagedFile followed a symlinked shell/")
	}
	if got, _ := os.ReadFile(filepath.Join(outside, "shell.json")); string(got) != "keep" {
		t.Fatal("a file outside the profile was changed")
	}
	for _, rel := range []string{"../escape", "/etc/passwd", "unmanaged/file", ".blueprint-save/files/x"} {
		if err := WriteManagedFile(dir, rel, nil); err == nil {
			t.Fatalf("WriteManagedFile accepted %q", rel)
		}
	}
	if err := CheckManagedDir(dir, "shell"); err == nil {
		t.Fatal("CheckManagedDir accepted a symlinked managed directory")
	}
}

// A Resources generation installed for a save resolves with it: rolled back
// when the save never committed, kept when it did.
func TestInstalledResourcesGenerationResolvesWithItsSave(t *testing.T) {
	for _, tc := range []struct {
		name, point string
		want        string
	}{
		{"save interrupted before committing", "staged", "previous"},
		{"save interrupted after committing", "committed", "installed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			old, next := generations()
			if err := Save(dir, old); err != nil {
				t.Fatal(err)
			}
			installResourcesGeneration(t, dir)
			injectSaveFault(t, tc.point, 0)
			if err := SaveWithOptions(dir, next, SaveOptions{CommitResourcesCapture: true}); err == nil {
				t.Fatal("the simulated crash did not interrupt the save")
			}
			saveFault = func(string) error { return nil }
			d := mustLoad(t, dir)
			generation := sameGeneration(t, d, old, next)
			files, _ := os.ReadFile(filepath.Join(dir, "resources", "files", "marker"))
			if got := strings.TrimSpace(string(files)); got != tc.want {
				t.Fatalf("Resources files are the %q generation, want %q", got, tc.want)
			}
			if (generation == "new") != (tc.want == "installed") {
				t.Fatalf("metadata is %s but Resources is %s", generation, tc.want)
			}
			for _, leftover := range []string{".capture-transaction", ".files-previous"} {
				if _, err := os.Lstat(filepath.Join(dir, "resources", leftover)); !os.IsNotExist(err) {
					t.Fatalf("recovery left resources/%s behind", leftover)
				}
			}
		})
	}
}

// Load never resolves a Resources capture that another process is in the
// middle of: that capture holds its lock and resolves its own generation.
func TestLoadLeavesAResourcesCaptureInProgressAlone(t *testing.T) {
	dir := t.TempDir()
	old, _ := generations()
	if err := Save(dir, old); err != nil {
		t.Fatal(err)
	}
	installResourcesGeneration(t, dir)
	lock, err := os.OpenFile(filepath.Join(dir, "resources", ".capture.lock"), os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	mustLoad(t, dir)
	if files, _ := os.ReadFile(filepath.Join(dir, "resources", "files", "marker")); strings.TrimSpace(string(files)) != "installed" {
		t.Fatal("Load rolled back a Resources capture still in progress")
	}
}

// installResourcesGeneration lays out what Resources' Install leaves before
// the profile save: the new generation in place, the previous one beside
// it, and a "rollback" marker.
func installResourcesGeneration(t *testing.T, dir string) {
	t.Helper()
	resources := filepath.Join(dir, "resources")
	for path, body := range map[string]string{".files-previous/marker": "previous", "files/marker": "installed"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(resources, path)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(resources, path), []byte(body+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(resources, ".capture-transaction"), []byte("rollback\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
