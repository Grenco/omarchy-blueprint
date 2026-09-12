package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/command"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

type profileRunner struct{ ctx context.Context }

func (r *profileRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	r.ctx = ctx
	if name == "omarchy" && len(args) == 1 && args[0] == "version" {
		return "4.0.0\n", nil
	}
	if name == "omarchy" && len(args) == 2 && args[0] == "version" && args[1] == "channel" {
		return "stable\n", nil
	}
	return "", errors.New("unexpected command")
}

var _ command.Runner = (*profileRunner)(nil)

func TestCreateProfileMatchesCLIInitializationAndUsesContext(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "profile")
	runner := &profileRunner{}
	now := time.Date(2026, 9, 12, 1, 2, 3, 0, time.UTC)
	ctx := context.WithValue(context.Background(), "test", "create-profile")
	created, err := CreateProfile(ctx, Dependencies{Runner: runner, Now: func() time.Time { return now }}, dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	if created != dir || runner.ctx != ctx {
		t.Fatalf("created=%q runner context=%v", created, runner.ctx)
	}
	data, err := profile.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if data.Manifest.Profile.Name != "main" || data.Manifest.Profile.CreatedAt != now || data.Manifest.Omarchy.CapturedVersion != "4.0.0" || data.Manifest.Omarchy.Channel != "stable" {
		t.Fatalf("profile=%#v", data.Manifest)
	}
}

func TestCreateProfileRejectsPartialDirectory(t *testing.T) {
	dir := t.TempDir()
	partial := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(partial, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateProfile(context.Background(), Dependencies{Runner: &profileRunner{}, Now: time.Now}, dir, "main"); err == nil {
		t.Fatal("partial directory was accepted")
	}
	if _, err := os.Stat(partial); err != nil {
		t.Fatalf("partial file was changed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "profile.toml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("profile.toml error = %v", err)
	}
}

func TestIsMissingProfileOnlyAcceptsEmptyRoot(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	if !IsMissingProfile(missing, os.ErrNotExist) {
		t.Fatal("missing root was not accepted")
	}
	partial := t.TempDir()
	if err := os.WriteFile(filepath.Join(partial, "partial"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if IsMissingProfile(partial, os.ErrNotExist) {
		t.Fatal("partial root was accepted")
	}
}
