package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
	resourcesprovider "github.com/Grenco/omarchy-blueprint/internal/providers/resources"
)

// A profile save that commits a Resources generation but cannot finish
// applying must keep that generation: rolling it back would leave the
// committed metadata describing files that are gone.
func TestCommittedButUnfinishedSaveKeepsTheResourcesGeneration(t *testing.T) {
	home, profileDir := t.TempDir(), t.TempDir()
	source := filepath.Join(home, "deploy")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "content"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := profile.New("test", time.Now())
	if err := profile.Save(profileDir, d); err != nil {
		t.Fatal(err)
	}
	prepared, err := resourcesprovider.Provider{HomeDir: home, ProfileDir: profileDir}.PrepareTrack(context.Background(), d.Resources, source, resourcesprovider.TrackOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Install(); err != nil {
		t.Fatal(err)
	}
	d.Resources = prepared.State
	d.Manifest.Capture.Resources = true
	// Moving the staged hooks file into place fails after the commit point.
	blocker := filepath.Join(profileDir, "hooks", "hooks.toml")
	if err := os.RemoveAll(blocker); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(blocker, "x"), 0o755); err != nil {
		t.Fatal(err)
	}

	err = saveResourcesGeneration(profileDir, d, prepared)
	var committed *profile.CommittedSaveError
	if !errors.As(err, &committed) {
		t.Fatalf("save: %v, want a committed-but-unfinished save", err)
	}
	if err := os.RemoveAll(blocker); err != nil {
		t.Fatal(err)
	}
	loaded, err := profile.Load(profileDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Resources.Items) != 1 || loaded.Resources.Items[0].ID != "deploy" {
		t.Fatalf("committed metadata lost the resource: %#v", loaded.Resources.Items)
	}
	if b, err := os.ReadFile(filepath.Join(profileDir, "resources", "files", "deploy", "content")); err != nil || string(b) != "v1" {
		t.Fatalf("the committed generation's files are gone: %q %v", b, err)
	}
	// The capture lock was released: another capture can start.
	next, err := resourcesprovider.Provider{HomeDir: home, ProfileDir: profileDir}.PrepareTrack(context.Background(), loaded.Resources, source, resourcesprovider.TrackOptions{})
	if err != nil {
		t.Fatalf("Resources stayed locked: %v", err)
	}
	_ = next.Rollback()
}
