package profile

import (
	"os"
	pathpkg "path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// A save's manifest must not become durable before the directory entries
// of the files it names, or a power loss could keep the manifest and lose
// the files. Likewise a directory a save creates must be durable in its
// parent before the transaction is cleaned up.
func TestSaveMakesItsDirectoriesDurableBeforeEachStep(t *testing.T) {
	dir := t.TempDir()
	var events []string
	previousSync, previousFault := dirSynced, saveFault
	dirSynced = func(rel string) { events = append(events, "sync "+rel) }
	saveFault = func(point string) error { events = append(events, point); return nil }
	t.Cleanup(func() { dirSynced, saveFault = previousSync, previousFault })

	_, next := generations()
	if err := Save(dir, next); err != nil {
		t.Fatal(err)
	}
	staged, committed := slices.Index(events, "staged"), slices.Index(events, "committed")
	if staged < 0 || committed < 0 {
		t.Fatalf("save points missing: %v", events)
	}
	syncedBetween := func(from, to int, rel string) bool {
		return slices.Contains(events[from:to], "sync "+rel)
	}
	ancestors := func(rel string) []string {
		var dirs []string
		for d := pathpkg.Dir(rel); ; d = pathpkg.Dir(d) {
			dirs = append(dirs, d)
			if d == "." {
				return dirs
			}
		}
	}
	for _, rel := range []string{"profile.toml", "machines/desktop.toml", "themes/themes.toml"} {
		for _, d := range ancestors(pathpkg.Join(transactionFiles, rel)) {
			if !syncedBetween(0, staged, d) {
				t.Errorf("staged %s: %s was not synced before the commit point", rel, d)
			}
		}
		for _, d := range ancestors(rel) {
			if !syncedBetween(committed, len(events), d) {
				t.Errorf("applied %s: %s was not synced after applying", rel, d)
			}
		}
	}
}

// After the commit point, a staged file that is missing (as a power loss
// without directory barriers could leave it) must never be read as already
// applied: the save would complete with the previous content of that file,
// a mix of two generations.
func TestALostStagedFileIsRefusedNotTreatedAsApplied(t *testing.T) {
	dir := t.TempDir()
	old, next := generations()
	if err := Save(dir, old); err != nil {
		t.Fatal(err)
	}
	injectSaveFault(t, "committed", 0)
	if err := Save(dir, next); err == nil {
		t.Fatal("the simulated crash did not interrupt the save")
	}
	saveFault = func(string) error { return nil }
	if err := os.Remove(filepath.Join(dir, transactionFiles, "themes", "themes.toml")); err != nil {
		t.Fatal(err)
	}
	_, err := Load(dir)
	if err == nil {
		t.Fatal("recovery completed a save that lost one of its files")
	}
	if !strings.Contains(err.Error(), "cannot be completed") || !strings.Contains(err.Error(), transactionDir) {
		t.Fatalf("unclear refusal: %v", err)
	}
}
