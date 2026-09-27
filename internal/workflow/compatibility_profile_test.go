package workflow

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func TestCompatibilityOlderProfileLoadsWithoutRewrite(t *testing.T) {
	dir, state := t.TempDir(), t.TempDir()
	manifest := "schema = 7\n\n[profile]\nname = 'legacy'\ncreated_at = 2026-09-09T00:00:00Z\nupdated_at = 2026-09-09T00:00:00Z\n\n[capture]\nconfig = true\n"
	if err := os.WriteFile(filepath.Join(dir, "profile.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile(filepath.Join("..", "profile", "testdata", "schema7-config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config", "config.toml"), fixture, 0o644); err != nil {
		t.Fatal(err)
	}
	session, err := Open(Dependencies{Runner: captureRunner{}, StateHome: func() (string, error) { return state, nil }}, Options{ProfileDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if err := session.SetProviders([]Provider{captureTestProvider{id: "config", order: &[]string{}, targets: []TargetInspection{{Key: ".config/hypr/bindings.lua", RestoreEligible: true}}}}); err != nil {
		t.Fatal(err)
	}
	plan, err := session.PlanRestore(context.Background(), "config", nil)
	if err != nil || plan.Compatibility.ProfileLastCapture.Known || len(plan.Compatibility.Categories) != 1 || plan.Compatibility.Categories[0].Category != "config" {
		t.Fatalf("older migrated profile = %+v err=%v", plan.Compatibility, err)
	}
	beforeManifest, err := os.ReadFile(filepath.Join(dir, "profile.toml"))
	if err != nil || string(beforeManifest) != manifest {
		t.Fatalf("planning rewrote legacy manifest: %v", err)
	}
	afterFixture, err := os.ReadFile(filepath.Join(dir, "config", "config.toml"))
	if err != nil || string(afterFixture) != string(fixture) {
		t.Fatalf("planning rewrote legacy config fixture: %v", err)
	}
}

type changingVersionRunner struct{ version *string }

func (r changingVersionRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	if name == "omarchy" && len(args) > 0 && args[0] == "version" {
		if len(args) == 2 {
			return "stable", nil
		}
		return *r.version, nil
	}
	return captureRunner{}.Run(ctx, name, args...)
}

type unrelatedCaptureProvider struct{ captureTestProvider }

func (unrelatedCaptureProvider) Capture(_ context.Context, _ *profile.Data, _ CaptureContext) (any, []model.Change, error) {
	return struct{}{}, nil, nil
}

func TestCompatibilityPartialCaptureUpdatesOnlyLastCaptureContext(t *testing.T) {
	data := profile.New("test", time.Unix(1, 0))
	data.Manifest.Omarchy.CapturedVersion = "4.0.0"
	data.Packages.Official = []string{"git"}
	session := newCaptureSession(t, data)
	version := "4.0.0"
	session.deps.Runner = changingVersionRunner{version: &version}
	if err := session.SetProviders([]Provider{
		captureTestProvider{id: "packages", order: &[]string{}, targets: []TargetInspection{{Key: "official:git", RestoreEligible: true}}},
		unrelatedCaptureProvider{captureTestProvider{id: "themes", order: &[]string{}}},
	}); err != nil {
		t.Fatal(err)
	}
	before, err := session.PlanRestore(context.Background(), "packages", nil)
	if err != nil {
		t.Fatal(err)
	}
	beforePackages := session.profile.Packages
	version = "4.2.0"
	if _, err := session.CaptureMany(context.Background(), []string{"themes"}); err != nil {
		t.Fatal(err)
	}
	after, err := session.PlanRestore(context.Background(), "packages", nil)
	if err != nil {
		t.Fatal(err)
	}
	if before.Compatibility.ProfileLastCapture.OmarchyVersion != "4.0.0" || after.Compatibility.ProfileLastCapture.OmarchyVersion != "4.2.0" || !reflect.DeepEqual(before.Compatibility.Categories, after.Compatibility.Categories) || !reflect.DeepEqual(beforePackages, session.profile.Packages) {
		t.Fatalf("partial Capture changed per-category authority: before=%+v after=%+v", before.Compatibility, after.Compatibility)
	}
}
