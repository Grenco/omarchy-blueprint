package themes

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

type runnerFunc func(context.Context, string, ...string) (string, error)

func (f runnerFunc) Run(ctx context.Context, name string, args ...string) (string, error) {
	return f(ctx, name, args...)
}

func TestDetectBuiltinTheme(t *testing.T) {
	builtin, user := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(builtin, "osaka-jade"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := Provider{Runner: runnerFunc(func(context.Context, string, ...string) (string, error) { return "Osaka Jade\n", nil }), BuiltinDir: builtin, UserDir: user}
	got, err := p.Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := profile.Themes{Current: "osaka-jade", Source: "builtin", Items: []profile.Theme{{ID: "osaka-jade", Type: "builtin", Enabled: true}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("theme = %#v", got)
	}
}

func TestCaptureCopiesUserOverride(t *testing.T) {
	builtin, user := t.TempDir(), t.TempDir()
	for _, root := range []string{builtin, user} {
		if err := os.Mkdir(filepath.Join(root, "osaka-jade"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(user, "osaka-jade", "colors.toml"), []byte("accent = '#fff'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	profileDir := t.TempDir()
	previous := filepath.Join(profileDir, "themes", "local", "osaka-jade", "colors.toml")
	if err := os.MkdirAll(filepath.Dir(previous), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(previous, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := Provider{Runner: runnerFunc(func(context.Context, string, ...string) (string, error) { return "Osaka Jade\n", nil }), BuiltinDir: builtin, UserDir: user, ProfileDir: profileDir}
	got, err := p.Capture(context.Background(), profile.Themes{}, func(string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != "overlay" || got.Items[0].Hash == "" {
		t.Fatalf("source = %q", got.Source)
	}
	if _, err := os.Stat(filepath.Join(profileDir, "themes", "local", "osaka-jade", "colors.toml")); err != nil {
		t.Fatal(err)
	}
	if err := p.RollbackCapture(); err != nil {
		t.Fatal(err)
	}
	if restored, err := os.ReadFile(previous); err != nil || string(restored) != "old\n" {
		t.Fatalf("restored snapshot=%q err=%v", restored, err)
	}
}

func TestCapturePreservesExistingArtifactWhenDisabled(t *testing.T) {
	builtin, user := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(user, "custom"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(user, "custom", "colors.toml"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	profileDir := t.TempDir()
	existing := filepath.Join(profileDir, "themes", "local", "custom", "colors.toml")
	if err := os.MkdirAll(filepath.Dir(existing), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(existing, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	saved := profile.Themes{Current: "custom", Items: []profile.Theme{{ID: "custom", Type: "local", Hash: "old-hash"}}}
	p := Provider{Runner: runnerFunc(func(context.Context, string, ...string) (string, error) { return "custom\n", nil }), BuiltinDir: builtin, UserDir: user, ProfileDir: profileDir}

	got, err := p.Capture(context.Background(), saved, func(string) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 1 || got.Items[0].Hash != "old-hash" {
		t.Fatalf("items = %#v, want the preserved saved metadata (old hash), not a fresh re-hash of the changed local content", got.Items)
	}
	content, err := os.ReadFile(filepath.Join(profileDir, "themes", "local", "custom", "colors.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "old\n" {
		t.Fatalf("staged artifact = %q, want the preserved prior content, not the changed local file", content)
	}
}

func TestCaptureTombstonesThemeRemovedLocally(t *testing.T) {
	builtin, user := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(builtin, "nord"), 0o755); err != nil {
		t.Fatal(err)
	}
	profileDir := t.TempDir()
	saved := profile.Themes{Current: "nord", Items: []profile.Theme{{ID: "custom", Type: "local", Hash: "abc"}}}
	p := Provider{Runner: runnerFunc(func(context.Context, string, ...string) (string, error) { return "nord\n", nil }), BuiltinDir: builtin, UserDir: user, ProfileDir: profileDir}

	got, err := p.Capture(context.Background(), saved, func(string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Absent) != 1 || got.Absent[0].ID != "custom" || got.Absent[0].Hash != "abc" {
		t.Fatalf("absent = %#v, want a tombstone for custom preserving its prior hash", got.Absent)
	}
	for _, item := range got.Items {
		if item.ID == "custom" {
			t.Fatalf("items = %#v, want custom removed from present items", got.Items)
		}
	}
}

// TestCaptureTwiceWhileStillAbsentPreservesTombstoneProvenance is a
// regression for a review finding on PR 3: re-capturing while a theme stays
// desired-absent must carry the EXISTING tombstone's provenance forward
// (Type/Hash/URL/Revision), not rebuild an ID-only Theme -- later Exact
// removal needs that provenance to prove ownership before deleting anything.
// TestPrepareStopManagingArtifactStagesRenameCommitOrRollback is a
// regression for a review finding on PR 3: Stop Managing must not delete a
// theme's local/overlay artifact immediately, since a caller that then fails
// to save the profile would leave it referencing a destroyed artifact.
// PrepareStopManagingArtifact stages the removal by rename; RollbackCapture
// must restore the original directory and its content byte-for-byte, and
// FinalizeCapture (only called once the caller knows the save succeeded)
// must permanently delete it.
func TestPrepareStopManagingArtifactStagesRenameCommitOrRollback(t *testing.T) {
	profileDir := t.TempDir()
	artifact := filepath.Join(profileDir, "themes", "local", "custom")
	if err := os.MkdirAll(artifact, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(artifact, "colors.toml"), []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := &Provider{ProfileDir: profileDir}
	if err := p.PrepareStopManagingArtifact("custom"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(artifact); !os.IsNotExist(err) {
		t.Fatal("artifact must be staged out of its original location, not left in place")
	}

	if err := p.RollbackCapture(); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(artifact, "colors.toml"))
	if err != nil || string(body) != "original" {
		t.Fatalf("rollback did not restore original content: body=%q err=%v", body, err)
	}

	if err := p.PrepareStopManagingArtifact("custom"); err != nil {
		t.Fatal(err)
	}
	if err := p.FinalizeCapture(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(artifact); !os.IsNotExist(err) {
		t.Fatal("finalize must permanently delete the staged artifact")
	}
	entries, err := os.ReadDir(filepath.Join(profileDir, "themes"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".stop-managing-") {
			t.Fatalf("finalize left a staging backup behind: %s", entry.Name())
		}
	}
}

func TestCaptureTwiceWhileStillAbsentPreservesTombstoneProvenance(t *testing.T) {
	builtin, user := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(builtin, "nord"), 0o755); err != nil {
		t.Fatal(err)
	}
	profileDir := t.TempDir()
	saved := profile.Themes{Current: "nord", Absent: []profile.Theme{{ID: "custom", Type: "local", Hash: "abc"}}}
	p := Provider{Runner: runnerFunc(func(context.Context, string, ...string) (string, error) { return "nord\n", nil }), BuiltinDir: builtin, UserDir: user, ProfileDir: profileDir}

	got, err := p.Capture(context.Background(), saved, func(string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Absent) != 1 || got.Absent[0].ID != "custom" || got.Absent[0].Type != "local" || got.Absent[0].Hash != "abc" {
		t.Fatalf("absent = %#v, want the existing tombstone's provenance preserved, not rebuilt ID-only", got.Absent)
	}
}

func TestCaptureNeverTombstonesABuiltinTheme(t *testing.T) {
	builtin, user := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(builtin, "nord"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(builtin, "catppuccin"), 0o755); err != nil {
		t.Fatal(err)
	}
	profileDir := t.TempDir()
	// "nord" was previously known present; the machine has since switched to
	// the (also builtin) "catppuccin", so a naive merge would see nord as
	// present -> absent and tombstone it were built-ins not excluded.
	saved := profile.Themes{Current: "nord", Items: []profile.Theme{{ID: "nord", Type: "builtin", Enabled: true}}}
	p := Provider{Runner: runnerFunc(func(context.Context, string, ...string) (string, error) { return "catppuccin\n", nil }), BuiltinDir: builtin, UserDir: user, ProfileDir: profileDir}

	got, err := p.Capture(context.Background(), saved, func(string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Absent) != 0 {
		t.Fatalf("absent = %#v, want no tombstones for a builtin theme no longer active/detected", got.Absent)
	}
	found := false
	for _, item := range got.Items {
		if item.ID == "catppuccin" && item.Type == "builtin" {
			found = true
		}
	}
	if !found {
		t.Fatalf("items = %#v, want the currently active builtin theme present", got.Items)
	}
}

func TestCaptureActiveThemeIndependentFromAvailabilityDecision(t *testing.T) {
	builtin, user := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(user, "custom"), 0o755); err != nil {
		t.Fatal(err)
	}
	profileDir := t.TempDir()
	saved := profile.Themes{Current: "old-theme"}
	p := Provider{Runner: runnerFunc(func(context.Context, string, ...string) (string, error) { return "custom\n", nil }), BuiltinDir: builtin, UserDir: user, ProfileDir: profileDir}

	got, err := p.Capture(context.Background(), saved, func(ref string) bool { return ref != "active" })
	if err != nil {
		t.Fatal(err)
	}
	if got.Current != "old-theme" {
		t.Fatalf("current = %q, want preserved: active selection is independent from theme:custom availability", got.Current)
	}
}

func TestDiffPlanAndVerify(t *testing.T) {
	saved := profile.Themes{Current: "osaka-jade", Source: "builtin"}
	current := profile.Themes{Current: "nord", Source: "builtin"}
	if got := Diff(saved, current); len(got) != 1 {
		t.Fatalf("changes = %#v", got)
	}
	p := Provider{BuiltinDir: t.TempDir()}
	if err := os.Mkdir(filepath.Join(p.BuiltinDir, "osaka-jade"), 0o755); err != nil {
		t.Fatal(err)
	}
	plan := p.Plan(saved, current, 1, "4.0", "4.0")
	if got := plan.Operations[0].Command; !reflect.DeepEqual(got, []string{"omarchy", "theme", "set", "osaka-jade"}) {
		t.Fatalf("command = %#v", got)
	}
	if Verify(saved, current).OK {
		t.Fatal("verification unexpectedly passed")
	}
}

func TestUserOverrideOfSavedBuiltinIsDriftButNotRemoved(t *testing.T) {
	saved := profile.Themes{Current: "osaka-jade", Source: "builtin"}
	current := profile.Themes{Current: "osaka-jade", Source: "overlay", Items: []profile.Theme{{ID: "osaka-jade", Type: "overlay", Hash: "different", Enabled: true}}}
	if got := Diff(saved, current); len(got) != 1 {
		t.Fatalf("changes = %#v", got)
	}
	plan := (Provider{}).Plan(saved, current, 1, "4.0", "4.0")
	if len(plan.Operations) != 0 || len(plan.Skipped) != 1 {
		t.Fatalf("plan = %#v", plan)
	}
	if Verify(saved, current).OK {
		t.Fatal("verification unexpectedly passed")
	}
}

// TestPlanAndVerifyIgnoreDesiredAbsenceTombstones is Task 23's PR 3 safety
// gate: a Capture-produced desired-absence tombstone is write-only today --
// Capture writes it, but Restore's Plan/Verify never read Absent -- so it
// cannot cause an unexpected removal, skip, or verification failure until
// PR 4 activates Restore-side policy.
func TestPlanAndVerifyIgnoreDesiredAbsenceTombstones(t *testing.T) {
	saved := profile.Themes{
		Current: "nord",
		Items:   []profile.Theme{{ID: "nord", Type: "builtin", Enabled: true}},
		Absent:  []profile.Theme{{ID: "gruvbox", Type: "local", Hash: "stale"}},
	}
	current := profile.Themes{Current: "nord", Items: []profile.Theme{{ID: "nord", Type: "builtin", Enabled: true}}}
	plan := (Provider{}).Plan(saved, current, 1, "4.0", "4.0")
	if len(plan.Operations) != 0 || len(plan.Skipped) != 0 {
		t.Fatalf("plan = %#v, want no operations or skips for the tombstoned theme", plan)
	}
	if !Verify(saved, current).OK {
		t.Fatal("verify unexpectedly failed because of a tombstoned theme")
	}
}

func TestPlanRestoresMissingGitAndLocalThemes(t *testing.T) {
	p := Provider{BuiltinDir: t.TempDir(), UserDir: t.TempDir(), ProfileDir: t.TempDir()}
	saved := profile.Themes{Current: "custom", Items: []profile.Theme{
		{ID: "remote", Type: "git", URL: "https://example.test/omarchy-remote-theme.git", Revision: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		{ID: "custom", Type: "local", Hash: "hash", Enabled: true},
	}}
	plan := p.Plan(saved, profile.Themes{Current: "nord", Items: []profile.Theme{{ID: "nord", Type: "builtin", Enabled: true}}}, 1, "4", "4")
	if len(plan.Operations) != 4 {
		t.Fatalf("operations = %#v", plan.Operations)
	}
	if plan.Operations[2].Copy == nil || plan.Operations[3].Action != "activate" {
		t.Fatalf("operations = %#v", plan.Operations)
	}
}

func TestDetectCleanGitThemeRecordsSanitizedProvenance(t *testing.T) {
	builtin, user := t.TempDir(), t.TempDir()
	path := filepath.Join(user, "remote")
	if err := os.MkdirAll(filepath.Join(path, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	runner := runnerFunc(func(_ context.Context, name string, args ...string) (string, error) {
		joined := name + " " + strings.Join(args, " ")
		switch {
		case joined == "omarchy theme current":
			return "Remote\n", nil
		case strings.HasSuffix(joined, "remote get-url origin"):
			return "https://secret@example.test/omarchy-remote-theme.git\n", nil
		case strings.HasSuffix(joined, "rev-parse HEAD"):
			return "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n", nil
		case strings.HasSuffix(joined, "status --porcelain --untracked-files=all"):
			return "", nil
		default:
			return "", fmt.Errorf("unexpected command: %s", joined)
		}
	})
	got, err := (Provider{Runner: runner, BuiltinDir: builtin, UserDir: user}).Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 1 || got.Items[0].Type != "git" || got.Items[0].URL != "https://example.test/omarchy-remote-theme.git" {
		t.Fatalf("themes = %#v", got.Items)
	}
}

// --- PR 5 Task 29: safe Exact removal for user themes ---

func removalOp(plan model.RestorePlan, id string) *model.Operation {
	for i := range plan.Operations {
		if plan.Operations[i].Action == "remove" && plan.Operations[i].Resource == "theme:"+id {
			return &plan.Operations[i]
		}
	}
	return nil
}

func TestPlanAdditiveNeverRemovesTombstonedTheme(t *testing.T) {
	saved := profile.Themes{
		Current: "nord",
		Items:   []profile.Theme{{ID: "nord", Type: "builtin", Enabled: true}},
		Absent:  []profile.Theme{{ID: "gruvbox", Type: "local", Hash: "hash"}},
	}
	current := profile.Themes{Current: "nord", Items: []profile.Theme{
		{ID: "nord", Type: "builtin", Enabled: true},
		{ID: "gruvbox", Type: "local", Hash: "hash"},
	}}
	// No PlanOptions at all: Additive is the zero value, matching every
	// existing caller that predates Exact removal.
	plan := (Provider{}).Plan(saved, current, 1, "4.0", "4.0")
	if op := removalOp(plan, "gruvbox"); op != nil {
		t.Fatalf("Operations = %#v, want no removal under Additive convergence", plan.Operations)
	}
}

func TestPlanExactNeverRemovesBuiltinTombstone(t *testing.T) {
	saved := profile.Themes{
		Current: "nord",
		Items:   []profile.Theme{{ID: "nord", Type: "builtin", Enabled: true}},
		Absent:  []profile.Theme{{ID: "osaka-jade", Type: "builtin"}},
	}
	current := profile.Themes{Current: "nord", Items: []profile.Theme{
		{ID: "nord", Type: "builtin", Enabled: true},
		{ID: "osaka-jade", Type: "builtin"},
	}}
	plan := (Provider{}).Plan(saved, current, 1, "4.0", "4.0", PlanOptions{Exact: true})
	if op := removalOp(plan, "osaka-jade"); op != nil {
		t.Fatalf("Operations = %#v, want a built-in theme never Exact-removed", plan.Operations)
	}
}

func TestPlanExactSkipsChangedLocalThemeProvenanceMismatch(t *testing.T) {
	saved := profile.Themes{
		Current: "nord",
		Items:   []profile.Theme{{ID: "nord", Type: "builtin", Enabled: true}},
		Absent:  []profile.Theme{{ID: "gruvbox", Type: "local", Hash: "stale-hash"}},
	}
	current := profile.Themes{Current: "nord", Items: []profile.Theme{
		{ID: "nord", Type: "builtin", Enabled: true},
		{ID: "gruvbox", Type: "local", Hash: "different-hash"},
	}}
	plan := (Provider{}).Plan(saved, current, 1, "4.0", "4.0", PlanOptions{Exact: true})
	if op := removalOp(plan, "gruvbox"); op != nil {
		t.Fatalf("Operations = %#v, want no removal for a changed/unowned local theme", plan.Operations)
	}
	var found bool
	for _, skipped := range plan.Skipped {
		if skipped.Resource == "theme:gruvbox" {
			found = true
			if !strings.Contains(skipped.Reason, "no longer matches") {
				t.Fatalf("skip reason = %q, want it to explain the provenance mismatch", skipped.Reason)
			}
		}
	}
	if !found {
		t.Fatalf("Skipped = %#v, want a visible skip for the mismatched theme", plan.Skipped)
	}
}

func TestPlanExactSkipsRemovingActiveThemeWithoutValidReplacement(t *testing.T) {
	saved := profile.Themes{
		// No desired active theme at all: there is nothing safe to fail
		// over to before removing the live active theme.
		Absent: []profile.Theme{{ID: "gruvbox", Type: "local", Hash: "hash"}},
	}
	current := profile.Themes{Current: "gruvbox", Items: []profile.Theme{
		{ID: "gruvbox", Type: "local", Hash: "hash"},
	}}
	plan := (Provider{}).Plan(saved, current, 1, "4.0", "4.0", PlanOptions{Exact: true})
	if op := removalOp(plan, "gruvbox"); op != nil {
		t.Fatalf("Operations = %#v, want no removal of the live active theme without a valid replacement", plan.Operations)
	}
	var found bool
	for _, skipped := range plan.Skipped {
		if skipped.Resource == "theme:gruvbox" {
			found = true
			if !strings.Contains(skipped.Reason, "active") {
				t.Fatalf("skip reason = %q, want it to explain the active-theme guard", skipped.Reason)
			}
		}
	}
	if !found {
		t.Fatalf("Skipped = %#v, want a visible skip for the blocked active-theme removal", plan.Skipped)
	}
}

func TestPlanExactSkipsWhenTombstonedThemeAlreadyGone(t *testing.T) {
	saved := profile.Themes{
		Current: "nord",
		Items:   []profile.Theme{{ID: "nord", Type: "builtin", Enabled: true}},
		Absent:  []profile.Theme{{ID: "gruvbox", Type: "local", Hash: "hash"}},
	}
	current := profile.Themes{Current: "nord", Items: []profile.Theme{{ID: "nord", Type: "builtin", Enabled: true}}}
	plan := (Provider{}).Plan(saved, current, 1, "4.0", "4.0", PlanOptions{Exact: true})
	if len(plan.Operations) != 0 || len(plan.Skipped) != 0 {
		t.Fatalf("plan = %#v, want no operation or skip: the tombstoned theme is already gone", plan)
	}
}

func TestPlanExactRemovesTombstonedUserTheme(t *testing.T) {
	saved := profile.Themes{
		Current: "nord",
		Items:   []profile.Theme{{ID: "nord", Type: "builtin", Enabled: true}},
		Absent:  []profile.Theme{{ID: "gruvbox", Type: "local", Hash: "hash"}},
	}
	current := profile.Themes{Current: "nord", Items: []profile.Theme{
		{ID: "nord", Type: "builtin", Enabled: true},
		{ID: "gruvbox", Type: "local", Hash: "hash"},
	}}
	plan := (Provider{}).Plan(saved, current, 1, "4.0", "4.0", PlanOptions{Exact: true})
	op := removalOp(plan, "gruvbox")
	if op == nil {
		t.Fatalf("Operations = %#v, want a removal operation for the tombstoned, provenance-matched theme", plan.Operations)
	}
	if got, want := op.Command, []string{"omarchy", "theme", "remove", "gruvbox"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("command = %#v, want %#v", got, want)
	}
	if op.Reversible {
		t.Fatalf("operation = %#v, want Reversible=false: omarchy theme remove takes no backup", op)
	}
	if op.Risk != model.RiskHigh {
		t.Fatalf("risk = %q, want high", op.Risk)
	}
	if len(op.DependsOn) != 0 {
		t.Fatalf("DependsOn = %#v, want none: nord is already active, so no ordering is required", op.DependsOn)
	}
}

func TestPlanExactRemovesActiveThemeAfterEstablishingReplacement(t *testing.T) {
	saved := profile.Themes{
		Current: "nord",
		Items:   []profile.Theme{{ID: "nord", Type: "builtin", Enabled: true}},
		Absent:  []profile.Theme{{ID: "gruvbox", Type: "local", Hash: "hash"}},
	}
	current := profile.Themes{Current: "gruvbox", Items: []profile.Theme{
		{ID: "nord", Type: "builtin", Enabled: true},
		{ID: "gruvbox", Type: "local", Hash: "hash"},
	}}
	plan := (Provider{}).Plan(saved, current, 1, "4.0", "4.0", PlanOptions{Exact: true})
	op := removalOp(plan, "gruvbox")
	if op == nil {
		t.Fatalf("Operations = %#v, want the active theme removed once a valid replacement is established", plan.Operations)
	}
	var activation *model.Operation
	for i := range plan.Operations {
		if plan.Operations[i].Action == "activate" {
			activation = &plan.Operations[i]
		}
	}
	if activation == nil {
		t.Fatalf("Operations = %#v, want an activation operation for the new desired active theme", plan.Operations)
	}
	if len(op.DependsOn) != 1 || op.DependsOn[0] != activation.ID {
		t.Fatalf("removal DependsOn = %#v, want it ordered after activation %q", op.DependsOn, activation.ID)
	}
}

func TestVerifyExactFailsWhenTombstonedThemeStillInstalled(t *testing.T) {
	saved := profile.Themes{
		Current: "nord",
		Items:   []profile.Theme{{ID: "nord", Type: "builtin", Enabled: true}},
		Absent:  []profile.Theme{{ID: "gruvbox", Type: "local", Hash: "hash"}},
	}
	current := profile.Themes{Current: "nord", Items: []profile.Theme{
		{ID: "nord", Type: "builtin", Enabled: true},
		{ID: "gruvbox", Type: "local", Hash: "hash"},
	}}
	if Verify(saved, current, VerifyOptions{Exact: true}).OK {
		t.Fatal("verification unexpectedly passed with the tombstoned theme still installed under Exact")
	}
	// Additive must not hold the same theme to the same bar.
	if !Verify(saved, current).OK {
		t.Fatal("Additive verification unexpectedly failed because of a tombstoned theme still present")
	}
}

func TestVerifyExactPassesWhenTombstonedThemeAlreadyRemoved(t *testing.T) {
	saved := profile.Themes{
		Current: "nord",
		Items:   []profile.Theme{{ID: "nord", Type: "builtin", Enabled: true}},
		Absent:  []profile.Theme{{ID: "gruvbox", Type: "local", Hash: "hash"}},
	}
	current := profile.Themes{Current: "nord", Items: []profile.Theme{{ID: "nord", Type: "builtin", Enabled: true}}}
	if !Verify(saved, current, VerifyOptions{Exact: true}).OK {
		t.Fatal("verification unexpectedly failed once the tombstoned theme is gone")
	}
}
