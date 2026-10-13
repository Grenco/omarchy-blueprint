package syncprofile

import (
	"context"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"os"
	"path/filepath"
	"testing"
)

// Builder must never write through hostile source or destination links, or
// escape its owned temporary root. The active source tree is never writable.
func TestBuilderOverlayArtifactIsolation(t *testing.T) {
	src := t.TempDir()
	outside := t.TempDir()
	path := filepath.Join(src, "config", "files", "a")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("chosen"), 0600); err != nil {
		t.Fatal(err)
	}
	b, err := NewBuilder(profile.Data{})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if err := b.ReplaceArtifact(context.Background(), src, "config/files/a"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(b.Root(), "config", "files", "a"))
	if err != nil || string(got) != "chosen" {
		t.Fatal(string(got), err)
	}
	for _, bad := range []string{"../escape", "/absolute", "config/../../escape", ".git/config"} {
		if err := b.ReplaceArtifact(context.Background(), src, bad); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
	if err := os.Symlink(outside, filepath.Join(src, "themes")); err != nil {
		t.Fatal(err)
	}
	if err := b.ReplaceArtifact(context.Background(), src, "themes"); err == nil {
		t.Fatal("followed source link")
	}
	if err := os.Symlink(outside, filepath.Join(b.Root(), "hooks")); err != nil {
		t.Fatal(err)
	}
	if err := b.RemoveArtifact("hooks/files/a"); err == nil {
		t.Fatal("followed destination ancestor")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := b.ReplaceArtifact(ctx, src, "config/files/a"); err != context.Canceled {
		t.Fatal(err)
	}
	root := b.Root()
	b.Close()
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("temporary tree retained", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "chosen" {
		t.Fatal("source modified")
	}
}
