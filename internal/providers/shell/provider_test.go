package shell

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

const defaultShellJSON = `{
  "version": 1,
  "idle": {"screensaver": 150, "lock": 300},
  "bar": {
    "id": "omarchy.bar",
    "position": "top",
    "transparent": false,
    "centerAnchor": "omarchy.clock",
    "layout": {
      "left": [{"id":"omarchy.menu"}],
      "center": [{"id":"omarchy.clock","format":"HH:mm"}],
      "right": [{"id":"omarchy.audio"}]
    }
  },
  "plugins": []
}`

const customizedShellJSON = `{
  "version": 1,
  "idle": {"screensaver": 150, "lock": 900},
  "bar": {
    "id": "omarchy.bar",
    "position": "bottom",
    "transparent": true,
    "centerAnchor": "omarchy.clock",
    "layout": {
      "left": [{"id":"omarchy.menu"}],
      "center": [{"id":"omarchy.clock","format":"HH:mm"}],
      "right": [{"id":"acme.weather","units":"celsius"}]
    }
  },
  "plugins": []
}`

type shellFixture struct {
	dir      string
	baseline string
	user     string
	profile  string
}

func newShellFixture(t *testing.T) shellFixture {
	t.Helper()
	dir := t.TempDir()
	f := shellFixture{
		dir:      dir,
		baseline: filepath.Join(dir, "baseline", "shell.json"),
		user:     filepath.Join(dir, "user", "shell.json"),
		profile:  filepath.Join(dir, "profile"),
	}
	if err := os.MkdirAll(filepath.Dir(f.baseline), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(f.user), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(f.profile, 0o755); err != nil {
		t.Fatal(err)
	}
	f.writeBaseline(defaultShellJSON)
	return f
}

func (f shellFixture) writeBaseline(body string) {
	if err := os.WriteFile(f.baseline, []byte(body), 0o644); err != nil {
		panic(err)
	}
}

func (f shellFixture) writeUser(body string) {
	if err := os.WriteFile(f.user, []byte(body), 0o644); err != nil {
		panic(err)
	}
}

func TestDetectMissingUserFileIsDefault(t *testing.T) {
	f := newShellFixture(t)
	p := Provider{BaselinePath: f.baseline, UserPath: f.user, ProfileDir: f.profile}
	state, err := p.Detect()
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != StatusDefault || state.UserExists {
		t.Fatalf("state = %#v", state)
	}
	if state.BaselineHash == "" || state.Version != SupportedVersion {
		t.Fatalf("baseline not recorded: %#v", state)
	}
}

func TestDetectFormattingOnlyDifferenceIsDefault(t *testing.T) {
	f := newShellFixture(t)
	// Re-serialize the default document with different whitespace and key
	// order: the canonical hash must treat this as the default state.
	var value map[string]any
	if err := json.Unmarshal([]byte(defaultShellJSON), &value); err != nil {
		t.Fatal(err)
	}
	compact, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	f.writeUser(string(compact))
	p := Provider{BaselinePath: f.baseline, UserPath: f.user, ProfileDir: f.profile}
	state, err := p.Detect()
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != StatusDefault {
		t.Fatalf("status = %q, want default for formatting-only difference", state.Status)
	}
}

func TestDetectCustomizedShell(t *testing.T) {
	f := newShellFixture(t)
	f.writeUser(customizedShellJSON)
	p := Provider{BaselinePath: f.baseline, UserPath: f.user, ProfileDir: f.profile}
	state, err := p.Detect()
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != StatusCustomized || !state.UserExists {
		t.Fatalf("state = %#v", state)
	}
	if state.Hash == "" || state.Hash == state.BaselineHash {
		t.Fatalf("hashes = %q vs %q", state.Hash, state.BaselineHash)
	}
	if !reflect.DeepEqual(state.References, []string{"acme.weather"}) {
		t.Fatalf("references = %v", state.References)
	}
}

func TestDetectUnsupportedShellVersion(t *testing.T) {
	f := newShellFixture(t)
	f.writeBaseline(`{"version":2}`)
	p := Provider{BaselinePath: f.baseline, UserPath: f.user, ProfileDir: f.profile}
	state, err := p.Detect()
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != StatusUnsupported || state.Version != 2 {
		t.Fatalf("state = %#v", state)
	}
	// A user file with an unsupported version is also unsupported, not an error.
	f.writeBaseline(defaultShellJSON)
	f.writeUser(`{"version":2}`)
	state, err = p.Detect()
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != StatusUnsupported {
		t.Fatalf("status = %q", state.Status)
	}
}

func TestDetectRejectsUserAndBaselineSymlinks(t *testing.T) {
	f := newShellFixture(t)
	p := Provider{BaselinePath: f.baseline, UserPath: f.user, ProfileDir: f.profile}
	if err := os.Symlink(f.baseline, f.user); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Detect(); err == nil {
		t.Fatal("user symlink must be rejected")
	}
	os.Remove(f.user)
	os.Remove(f.baseline)
	if err := os.WriteFile(filepath.Join(f.dir, "baseline-real.json"), []byte(defaultShellJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(f.dir, "baseline-real.json"), f.baseline); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Detect(); err == nil {
		t.Fatal("baseline symlink must be rejected")
	}
}

func TestCaptureCustomizedStoresExactDesiredAndBaselineBytes(t *testing.T) {
	f := newShellFixture(t)
	f.writeUser(customizedShellJSON)
	p := Provider{BaselinePath: f.baseline, UserPath: f.user, ProfileDir: f.profile}
	state, err := p.Detect()
	if err != nil {
		t.Fatal(err)
	}
	captured, err := p.Capture(state)
	if err != nil {
		t.Fatal(err)
	}
	if captured.Hash != state.Hash || captured.BaselineHash != state.BaselineHash || captured.Version != 1 {
		t.Fatalf("captured = %#v", captured)
	}
	userBytes, err := os.ReadFile(filepath.Join(f.profile, "shell", "shell.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(userBytes) != customizedShellJSON {
		t.Fatalf("captured user bytes must be exact, got %q", userBytes)
	}
	baselineBytes, err := os.ReadFile(filepath.Join(f.profile, "shell", "baseline.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(baselineBytes) != defaultShellJSON {
		t.Fatalf("baseline bytes must be exact, got %q", baselineBytes)
	}
}

func TestCaptureDefaultRemovesStaleSnapshots(t *testing.T) {
	f := newShellFixture(t)
	f.writeUser(customizedShellJSON)
	p := Provider{BaselinePath: f.baseline, UserPath: f.user, ProfileDir: f.profile}
	state, err := p.Detect()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Capture(state); err != nil {
		t.Fatal(err)
	}
	f.writeUser(defaultShellJSON)
	state, err = p.Detect()
	if err != nil {
		t.Fatal(err)
	}
	captured, err := p.Capture(state)
	if err != nil {
		t.Fatal(err)
	}
	if captured.Hash != "" {
		t.Fatalf("default capture must record no custom hash, got %q", captured.Hash)
	}
	if captured.Version != SupportedVersion || captured.BaselineHash == "" {
		t.Fatalf("captured = %#v", captured)
	}
	if _, err := os.Stat(filepath.Join(f.profile, "shell", "shell.json")); !os.IsNotExist(err) {
		t.Fatal("stale shell.json snapshot must be removed")
	}
	if _, err := os.Stat(filepath.Join(f.profile, "shell", "baseline.json")); !os.IsNotExist(err) {
		t.Fatal("stale baseline.json snapshot must be removed")
	}
}

func TestCaptureDefaultFailsWhenStaleSnapshotCannotBeRemoved(t *testing.T) {
	f := newShellFixture(t)
	p := Provider{BaselinePath: f.baseline, UserPath: f.user, ProfileDir: f.profile}
	state, err := p.Detect()
	if err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(f.profile, "shell", "shell.json")
	if err := os.MkdirAll(stale, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stale, "nested"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Capture(state); err == nil {
		t.Fatal("default capture must fail when stale snapshot cannot be removed")
	}
}

func TestCaptureUnsupportedStateFails(t *testing.T) {
	f := newShellFixture(t)
	f.writeBaseline(`{"version":2}`)
	p := Provider{BaselinePath: f.baseline, UserPath: f.user, ProfileDir: f.profile}
	state, err := p.Detect()
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Capture(state)
	if err == nil || !errors.Is(err, errUnsupportedShell) {
		t.Fatalf("err = %v, want unsupported-shell error", err)
	}
}

func TestValidatePluginReferencesRejectsMissingThirdPartyProvenance(t *testing.T) {
	plugins := profile.Plugins{Items: []profile.Plugin{
		{ID: "acme.weather", Source: "local"},
		{ID: "omarchy.clock", Source: "builtin"},
	}}
	if err := ValidatePluginReferences([]string{"acme.weather", "omarchy.menu"}, plugins); err != nil {
		t.Fatalf("first-party references must not require provenance: %v", err)
	}
	err := ValidatePluginReferences([]string{"acme.unknown"}, plugins)
	if err == nil {
		t.Fatal("missing provenance must fail")
	}
	if !errors.Is(err, errMissingPluginProvenance) {
		t.Fatalf("err = %v", err)
	}
}

func TestCheckValidatesSnapshotHashesVersionsAndPluginReferences(t *testing.T) {
	f := newShellFixture(t)
	f.writeUser(customizedShellJSON)
	p := Provider{BaselinePath: f.baseline, UserPath: f.user, ProfileDir: f.profile}
	state, err := p.Detect()
	if err != nil {
		t.Fatal(err)
	}
	saved, err := p.Capture(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Check(saved, profile.Plugins{Items: []profile.Plugin{{ID: "acme.weather", Source: "local"}}}); err != nil {
		t.Fatalf("valid shell profile rejected: %v", err)
	}
	// Missing plugin provenance must fail.
	if err := p.Check(saved, profile.Plugins{}); err == nil {
		t.Fatal("missing third-party provenance must fail check")
	}
	// Corrupted snapshot must fail.
	corruptPath := filepath.Join(f.profile, "shell", "shell.json")
	if err := os.WriteFile(corruptPath, []byte(`{"version":1,"tampered":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := p.Check(saved, profile.Plugins{Items: []profile.Plugin{{ID: "acme.weather", Source: "local"}}}); err == nil {
		t.Fatal("tampered desired snapshot must fail check")
	}
	// Default desired state with stale snapshots must fail.
	f.writeUser(defaultShellJSON)
	state, err = p.Detect()
	if err != nil {
		t.Fatal(err)
	}
	stale, err := p.Capture(state)
	if err != nil {
		t.Fatal(err)
	}
	_ = stale
	// Re-capture customized, then simulate a default capture with stale files.
	if err := os.WriteFile(filepath.Join(f.profile, "shell", "shell.json"), []byte(customizedShellJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	defaultSaved := profile.Shell{Version: 1, BaselineHash: hashFixture(t, f.baseline)}
	if err := p.Check(defaultSaved, profile.Plugins{}); err == nil {
		t.Fatal("default desired state must reject stale snapshots")
	}
}

func TestCheckRejectsDefaultStateWithoutSupportedVersion(t *testing.T) {
	f := newShellFixture(t)
	p := Provider{BaselinePath: f.baseline, UserPath: f.user, ProfileDir: f.profile}
	if err := p.Check(profile.Shell{}, profile.Plugins{}); err == nil {
		t.Fatal("captured default state without a supported version must fail")
	}
}

func TestRequiredThirdPartyPlugins(t *testing.T) {
	f := newShellFixture(t)
	f.writeUser(customizedShellJSON)
	p := Provider{BaselinePath: f.baseline, UserPath: f.user, ProfileDir: f.profile}
	state, err := p.Detect()
	if err != nil {
		t.Fatal(err)
	}
	saved, err := p.Capture(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(f.user); err != nil {
		t.Fatal(err)
	}
	current, err := p.Detect()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(mustRequired(t, p, saved, current), []string{"acme.weather"}) {
		t.Fatal("references mismatch")
	}
	if got, _ := p.RequiredThirdPartyPlugins(profile.Shell{Version: 1}, current, MergeOptions{}); len(got) != 0 {
		t.Fatalf("default desired state requires no plugins: %v", got)
	}
}

func mustRequired(t *testing.T, p Provider, saved profile.Shell, current State) []string {
	t.Helper()
	got, err := p.RequiredThirdPartyPlugins(saved, current, MergeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func hashFixture(t *testing.T, path string) string {
	t.Helper()
	doc, err := ReadDocument(path)
	if err != nil {
		t.Fatal(err)
	}
	return doc.Hash
}
