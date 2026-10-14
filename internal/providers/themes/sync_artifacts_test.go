package themes

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/command"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/profilegit"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
)

// Capture hashes the live theme directory; Sync verifies the committed copy
// from an isolated Git snapshot. A theme with a subdirectory must verify.
func TestValidateSyncArtifactsAcceptsCommittedLocalThemeWithSubdirectory(t *testing.T) {
	writeTheme := func(dir string) {
		for name, content := range map[string]string{"colors.toml": "accent = \"#fff\"\n", "backgrounds/garden.jpg": "image"} {
			path := filepath.Join(dir, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		for _, sub := range []string{dir, filepath.Join(dir, "backgrounds")} {
			if err := os.Chmod(sub, 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	live := filepath.Join(t.TempDir(), "garden")
	writeTheme(live)
	hash, err := hashTree(live)
	if err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		out, err := (command.SystemRunner{}).Run(context.Background(), "git", append([]string{"-C", repo, "-c", "user.name=Test", "-c", "user.email=test@example.test"}, args...)...)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	run("init", "-b", "main")
	writeTheme(filepath.Join(repo, "themes", "local", "garden"))
	run("add", ".")
	run("commit", "-m", "themes")
	service, err := profilegit.New(command.SystemRunner{}, repo)
	if err != nil {
		t.Fatal(err)
	}
	sha := run("rev-parse", "HEAD")
	snapshot := t.TempDir()
	if err := service.MaterializeCommit(context.Background(), sha[:40], snapshot); err != nil {
		t.Fatal(err)
	}
	data := func(items ...profile.Theme) profile.Data {
		var d profile.Data
		d.Manifest.Capture.Themes = true
		d.Themes.Items = items
		return d
	}
	if err := (Provider{}).ValidateSyncArtifacts(context.Background(), snapshot, data(profile.Theme{ID: "garden", Type: "local", Hash: hash})); err != nil {
		t.Fatalf("valid committed theme rejected: %v", err)
	}

	// A committed artifact that does not match its metadata still fails closed
	// and names only the provider-owned target.
	var artifact *syncengine.ArtifactError
	err = (Provider{}).ValidateSyncArtifacts(context.Background(), snapshot, data(profile.Theme{ID: "garden", Type: "local", Hash: "0" + hash[1:]}))
	if !errors.As(err, &artifact) || artifact.Target != "theme:garden" {
		t.Fatalf("mismatched theme accepted or unnamed: %v", err)
	}
	// An empty captured theme cannot be carried by Git, so its committed
	// artifact is absent: a clone could not Restore it either.
	empty := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	err = (Provider{}).ValidateSyncArtifacts(context.Background(), snapshot, data(profile.Theme{ID: "garden", Type: "local", Hash: hash}, profile.Theme{ID: "aether", Type: "local", Hash: empty}))
	if !errors.As(err, &artifact) || artifact.Target != "theme:aether" || !artifact.Missing {
		t.Fatalf("absent theme artifact accepted or misreported: %v", err)
	}
}
