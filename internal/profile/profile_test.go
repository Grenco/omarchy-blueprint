package profile

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSaveLoadRoundTripNormalizesPackages(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	d := New("main", now)
	if d.Manifest.Schema != 3 {
		t.Fatalf("new profile schema = %d, want 3", d.Manifest.Schema)
	}
	d.Manifest.Capture.Packages = true
	d.Packages = Packages{Official: []string{"zoxide", "git", "git", ""}, AUR: []string{"visual-studio-code-bin"}, MachineSpecific: []string{"official:nvidia-open"}, Excluded: []string{"aur:dislocker-git"}}
	d.Themes = Themes{Current: "custom", Items: []Theme{{ID: "custom", Type: "local", Hash: "abc", Enabled: true}, {ID: "remote", Type: "git", URL: "https://example.test/theme.git", Revision: "def"}}}
	d.Plugins = Plugins{Items: []Plugin{{ID: "omarchy.clock", Enabled: true}, {ID: "omarchy.media", Enabled: false}}}
	if err := Save(dir, d); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Packages.Official, []string{"git", "zoxide"}) {
		t.Fatalf("official = %#v", got.Packages.Official)
	}
	b, err := os.ReadFile(filepath.Join(dir, "packages", "official.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "git\nzoxide\n" {
		t.Fatalf("unexpected file: %q", b)
	}
	if !reflect.DeepEqual(got.Packages.MachineSpecific, []string{"official:nvidia-open"}) {
		t.Fatalf("machine-specific = %#v", got.Packages.MachineSpecific)
	}
	if !reflect.DeepEqual(got.Packages.Excluded, []string{"aur:dislocker-git"}) {
		t.Fatalf("excluded = %#v", got.Packages.Excluded)
	}
	if !reflect.DeepEqual(got.Themes, d.Themes) {
		t.Fatalf("themes = %#v", got.Themes)
	}
	if !reflect.DeepEqual(got.Plugins, d.Plugins) {
		t.Fatalf("plugins = %#v", got.Plugins)
	}
}

func TestLoadSchema1UpgradesWithoutConfigDefaultsAndPreservesExistingProviderState(t *testing.T) {
	dir := t.TempDir()
	profileTOML := `schema = 1

[profile]
name = "legacy"
created_at = 2026-09-02T12:00:00Z
updated_at = 2026-09-02T12:00:00Z

[capture]
packages = true
themes = true
plugins = true
`
	if err := os.WriteFile(filepath.Join(dir, "profile.toml"), []byte(profileTOML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "packages"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "packages", "official.txt"), []byte("git\nzoxide\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "themes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "themes", "themes.toml"), []byte("current = 'tokyo-night'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "plugins"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugins", "plugins.toml"), []byte("[[plugin]]\nid = 'omarchy.clock'\nenabled = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Manifest.Schema != 3 {
		t.Fatalf("schema = %d, want 3", got.Manifest.Schema)
	}
	if got.Manifest.Capture.Config || len(got.Config.Files) != 0 {
		t.Fatalf("config state = %#v, want empty uncaptured config", got.Config)
	}
	if got.Manifest.Capture.Defaults || (got.Defaults != Defaults{}) {
		t.Fatalf("defaults state = %#v, want empty uncaptured defaults", got.Defaults)
	}
	if !reflect.DeepEqual(got.Packages.Official, []string{"git", "zoxide"}) {
		t.Fatalf("official packages = %#v", got.Packages.Official)
	}
	if got.Themes.Current != "tokyo-night" || !reflect.DeepEqual(got.Plugins.Items, []Plugin{{ID: "omarchy.clock", Enabled: true}}) {
		t.Fatalf("existing provider state was not retained: themes=%#v plugins=%#v", got.Themes, got.Plugins)
	}
	if err := Save(dir, got); err != nil {
		t.Fatal(err)
	}
	savedManifest, err := os.ReadFile(filepath.Join(dir, "profile.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(savedManifest), "schema = 3\n") {
		t.Fatalf("saved profile.toml = %q, want schema 3", savedManifest)
	}
}

func TestSaveLoadRoundTripConfigMetadataInStableOrder(t *testing.T) {
	dir := t.TempDir()
	d := New("main", time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC))
	d.Manifest.Capture.Config = true
	d.Config = Configs{Files: []ConfigFile{
		{ID: "hypr.looknfeel", Path: "hypr/looknfeel.lua", Hash: "look", BaselineHash: "base-look"},
		{ID: "hypr.bindings", Path: "hypr/bindings.lua", Hash: "bind", BaselineHash: "base-bind"},
	}}
	d.Themes = Themes{Current: "tokyo-night"}
	d.Plugins = Plugins{Items: []Plugin{{ID: "omarchy.clock", Enabled: true}}}

	if err := Save(dir, d); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	wantFiles := []ConfigFile{
		{ID: "hypr.bindings", Path: "hypr/bindings.lua", Hash: "bind", BaselineHash: "base-bind"},
		{ID: "hypr.looknfeel", Path: "hypr/looknfeel.lua", Hash: "look", BaselineHash: "base-look"},
	}
	if !reflect.DeepEqual(got.Config.Files, wantFiles) {
		t.Fatalf("config files = %#v, want %#v", got.Config.Files, wantFiles)
	}
	if !got.Manifest.Capture.Config {
		t.Fatal("config capture metadata was not retained")
	}
	if got.Themes.Current != "tokyo-night" || !reflect.DeepEqual(got.Plugins.Items, d.Plugins.Items) {
		t.Fatalf("existing provider state was not retained: themes=%#v plugins=%#v", got.Themes, got.Plugins)
	}
	configTOML, err := os.ReadFile(filepath.Join(dir, "config", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	wantTOML := "[[file]]\nid = 'hypr.bindings'\npath = 'hypr/bindings.lua'\nhash = 'bind'\nbaseline_hash = 'base-bind'\n\n[[file]]\nid = 'hypr.looknfeel'\npath = 'hypr/looknfeel.lua'\nhash = 'look'\nbaseline_hash = 'base-look'\n"
	if string(configTOML) != wantTOML {
		t.Fatalf("config/config.toml = %q, want %q", configTOML, wantTOML)
	}
}

func TestLoadSchema2UpgradesWithoutDefaultsAndPreservesConfig(t *testing.T) {
	dir := t.TempDir()
	profileTOML := `schema = 2

[profile]
name = "schema2"
created_at = 2026-09-02T12:00:00Z
updated_at = 2026-09-02T12:00:00Z

[capture]
config = true
`
	if err := os.WriteFile(filepath.Join(dir, "profile.toml"), []byte(profileTOML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config", "config.toml"), []byte("[[file]]\nid = 'hypr.bindings'\npath = 'hypr/bindings.lua'\nhash = 'bind'\nbaseline_hash = 'base-bind'\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Manifest.Schema != 3 {
		t.Fatalf("schema = %d, want 3", got.Manifest.Schema)
	}
	if !got.Manifest.Capture.Config || len(got.Config.Files) != 1 {
		t.Fatalf("config state = %#v, want retained schema-2 config", got.Config)
	}
	if got.Manifest.Capture.Defaults || (got.Defaults != Defaults{}) {
		t.Fatalf("defaults state = %#v, want empty uncaptured defaults", got.Defaults)
	}
}

func TestSaveLoadRoundTripDefaultsOmitsUnsetValues(t *testing.T) {
	dir := t.TempDir()
	d := New("main", time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC))
	d.Manifest.Capture.Defaults = true
	d.Defaults = Defaults{Terminal: "ghostty", Browser: "firefox", Agent: "codex"}
	if err := Save(dir, d); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Defaults, d.Defaults) {
		t.Fatalf("defaults = %#v, want %#v", got.Defaults, d.Defaults)
	}
	if !got.Manifest.Capture.Defaults {
		t.Fatal("defaults capture metadata was not retained")
	}
	b, err := os.ReadFile(filepath.Join(dir, "defaults", "defaults.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "editor") {
		t.Fatalf("empty editor must be omitted: %q", b)
	}
	wantTOML := "terminal = 'ghostty'\nbrowser = 'firefox'\nagent = 'codex'\n"
	if string(b) != wantTOML {
		t.Fatalf("defaults/defaults.toml = %q, want %q", b, wantTOML)
	}
}

func TestValidateRejectsSchema1AfterMigration(t *testing.T) {
	d := New("main", time.Now())
	d.Manifest.Schema = 1
	if err := Validate(d); err == nil {
		t.Fatal("expected schema 1 to be rejected")
	}
}

func TestLoadRejectsUnsupportedSchema(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "profile.toml"), []byte("schema = 99\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("expected schema error")
	}
}
