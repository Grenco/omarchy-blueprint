package profile

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/policy"
)

func TestSaveLoadRoundTripNormalizesPackages(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	d := New("main", now)
	if d.Manifest.Schema != Schema {
		t.Fatalf("new profile schema = %d, want %d", d.Manifest.Schema, Schema)
	}
	d.Manifest.Capture.Packages = true
	d.Packages = Packages{Official: []string{"zoxide", "git", "git", ""}, AUR: []string{"visual-studio-code-bin"}}
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
	if got.Manifest.Schema != Schema {
		t.Fatalf("schema = %d, want %d", got.Manifest.Schema, Schema)
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
	if !strings.Contains(string(savedManifest), "schema = 12\n") {
		t.Fatalf("saved profile.toml = %q, want schema 12", savedManifest)
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
		{Path: "hypr/bindings.lua", Hash: "bind", BaselineHash: "base-bind"},
		{Path: "hypr/looknfeel.lua", Hash: "look", BaselineHash: "base-look"},
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
	wantTOML := "[[file]]\npath = 'hypr/bindings.lua'\nhash = 'bind'\nbaseline_hash = 'base-bind'\n\n[[file]]\npath = 'hypr/looknfeel.lua'\nhash = 'look'\nbaseline_hash = 'base-look'\n"
	if string(configTOML) != wantTOML {
		t.Fatalf("config/config.toml = %q, want %q", configTOML, wantTOML)
	}
}

func TestSchema8ConfigOverlayRoundTrip(t *testing.T) {
	dir := t.TempDir()
	d := New("overlay", time.Unix(0, 0))
	d.Manifest.Capture.Config = true
	d.Config = Configs{Files: []ConfigFile{{ID: "legacy", Path: ".config/hypr/bindings.lua", Hash: strings.Repeat("a", 64), Mode: "0644", BaselineHash: strings.Repeat("b", 64), BaselineMode: "0644"}, {Path: ".config/ghostty/config", Hash: strings.Repeat("c", 64), Mode: "0600"}}, Deletes: []ConfigDelete{{Path: ".config/example/default.conf", BaselineHash: strings.Repeat("d", 64), BaselineMode: "0644"}}, Excluded: []string{".config/google-chrome", ".config/discord"}}
	if err := Save(dir, d); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := Configs{Files: []ConfigFile{{Path: ".config/ghostty/config", Hash: strings.Repeat("c", 64), Mode: "0600"}, {Path: ".config/hypr/bindings.lua", Hash: strings.Repeat("a", 64), Mode: "0644", BaselineHash: strings.Repeat("b", 64), BaselineMode: "0644"}}, Deletes: []ConfigDelete{{Path: ".config/example/default.conf", BaselineHash: strings.Repeat("d", 64), BaselineMode: "0644"}}, Excluded: []string{".config/discord", ".config/google-chrome"}}
	if got.Manifest.Schema != Schema || !reflect.DeepEqual(got.Config, want) {
		t.Fatalf("config=%#v want=%#v", got.Config, want)
	}
	contents, err := os.ReadFile(filepath.Join(dir, "config", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Index(string(contents), "ghostty/config") > strings.Index(string(contents), "hypr/bindings.lua") {
		t.Fatalf("config is not path sorted: %s", contents)
	}
}

func TestSaveRejectsNonCanonicalAndOverlappingConfigState(t *testing.T) {
	for _, configs := range []Configs{
		{Files: []ConfigFile{{Path: "a/../b", Hash: "x"}}},
		{Files: []ConfigFile{{Path: ".config/a", Hash: "x"}}, Deletes: []ConfigDelete{{Path: ".config/a", BaselineHash: "x"}}},
	} {
		d := New("test", time.Unix(0, 0))
		d.Config = configs
		if err := Save(t.TempDir(), d); err == nil {
			t.Fatalf("invalid configs accepted: %#v", configs)
		}
	}
}

func TestSavePrunesStaleMachineFiles(t *testing.T) {
	dir := t.TempDir()
	d := New("test", time.Now())
	d.Machines.Items = []Machine{{Name: "old"}}
	if err := Save(dir, d); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "machines", "README"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	d.Machines.Items = []Machine{{Name: "new"}}
	if err := Save(dir, d); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "machines", "old.toml")); !os.IsNotExist(err) {
		t.Fatalf("old=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "machines", "new.toml")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "machines", "README")); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(dir)
	if err != nil || !reflect.DeepEqual(loaded.Machines.Items, d.Machines.Items) {
		t.Fatalf("machines=%#v err=%v", loaded.Machines, err)
	}
}

func TestLoadRejectsInvalidMachineDefinitions(t *testing.T) {
	for _, machineTOML := range []string{
		"name = 'bad name'\n",
		"name = 'desktop'\n[[resource_path]]\nresource = 'projects'\npath = 'relative'\n",
		"name = 'desktop'\n[[resource_path]]\nresource = ''\npath = '~/Code'\n",
	} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "profile.toml"), []byte("schema = 11\n[profile]\nname = 'test'\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(dir, "machines"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "machines", "desktop.toml"), []byte(machineTOML), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(dir); err == nil {
			t.Fatalf("invalid machine accepted: %s", machineTOML)
		}
	}
}

func TestLoadSchema7MigratesLegacyConfigPaths(t *testing.T) {
	dir := t.TempDir()
	manifest := "schema = 7\n\n[profile]\nname = 'legacy'\ncreated_at = 2026-09-09T00:00:00Z\nupdated_at = 2026-09-09T00:00:00Z\n\n[capture]\nconfig = true\n"
	if err := os.WriteFile(filepath.Join(dir, "profile.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile(filepath.Join("testdata", "schema7-config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config", "config.toml"), fixture, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Config.Files[0].Path != ".config/hypr/bindings.lua" {
		t.Fatalf("config=%#v", got.Config)
	}
}

func TestLoadSchema3UpgradesWithoutShellAndPreservesDefaults(t *testing.T) {
	dir := t.TempDir()
	profileTOML := `schema = 3

[profile]
name = "schema3"
created_at = 2026-09-03T12:00:00Z
updated_at = 2026-09-03T12:00:00Z

[capture]
defaults = true
`
	if err := os.WriteFile(filepath.Join(dir, "profile.toml"), []byte(profileTOML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "defaults"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "defaults", "defaults.toml"), []byte("terminal = 'ghostty'\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Manifest.Schema != Schema {
		t.Fatalf("schema = %d, want %d", got.Manifest.Schema, Schema)
	}
	if got.Manifest.Capture.Shell {
		t.Fatal("schema-3 profile must upgrade with shell uncaptured")
	}
	if got.Shell != (Shell{}) {
		t.Fatalf("shell = %#v, want zero value", got.Shell)
	}
	if got.Defaults.Terminal != "ghostty" {
		t.Fatalf("defaults were not preserved: %#v", got.Defaults)
	}
}

func TestSaveLoadRoundTripShellMetadata(t *testing.T) {
	dir := t.TempDir()
	d := New("main", time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC))
	d.Manifest.Capture.Shell = true
	d.Shell = Shell{
		Version:      1,
		Hash:         "desired",
		BaselineHash: "baseline",
	}
	if err := Save(dir, d); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Shell != d.Shell {
		t.Fatalf("shell = %#v, want %#v", got.Shell, d.Shell)
	}
	if !got.Manifest.Capture.Shell {
		t.Fatal("shell capture metadata was not retained")
	}
	b, err := os.ReadFile(filepath.Join(dir, "shell", "shell.toml"))
	if err != nil {
		t.Fatal(err)
	}
	want := "version = 1\nhash = 'desired'\nbaseline_hash = 'baseline'\n"
	if string(b) != want {
		t.Fatalf("shell/shell.toml = %q, want %q", b, want)
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
	if got.Manifest.Schema != Schema {
		t.Fatalf("schema = %d, want %d", got.Manifest.Schema, Schema)
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

func TestLoaderThresholdsUseIntroductionVersions(t *testing.T) {
	// Loader thresholds must reference the schema version that introduced a
	// provider's state, never the latest Schema constant, so future schema
	// bumps do not silently drop existing provider state.
	if configSchema != 2 || defaultsSchema != 3 || shellSchema != 4 || hooksSchema != 5 || misePackagesSchema != 6 || resourcesSchema != 7 || configOverlaySchema != 8 {
		t.Fatalf(
			"introduction versions = config:%d defaults:%d shell:%d hooks:%d mise:%d resources:%d",
			configSchema, defaultsSchema, shellSchema, hooksSchema, misePackagesSchema, resourcesSchema,
		)
	}
	if configSchema > Schema || defaultsSchema > Schema || shellSchema > Schema || hooksSchema > Schema || misePackagesSchema > Schema || resourcesSchema > Schema || configOverlaySchema > Schema {
		t.Fatalf(
			"introduction versions must not exceed current schema %d",
			Schema,
		)
	}
}

func TestSchema4LoadsAsSchema5WithHooksUncaptured(t *testing.T) {
	dir := t.TempDir()
	profileTOML := `schema = 4

[profile]
name = "schema4"
created_at = 2026-09-03T12:00:00Z
updated_at = 2026-09-03T12:00:00Z
`
	if err := os.WriteFile(filepath.Join(dir, "profile.toml"), []byte(profileTOML), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Manifest.Schema != Schema || got.Manifest.Capture.Hooks || len(got.Hooks.Items) != 0 {
		t.Fatalf("schema-4 migration = %#v", got)
	}
}

func TestHooksRoundTripSchema5(t *testing.T) {
	dir := t.TempDir()
	d := New("test", time.Unix(0, 0))
	d.Manifest.Capture.Hooks = true
	d.Hooks.Items = []Hook{{Path: "post-update.d/update-rust", Hash: strings.Repeat("a", 64), Mode: "0755"}}
	if err := Save(dir, d); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.Hooks, d.Hooks) {
		t.Fatalf("hooks = %#v, want %#v", loaded.Hooks, d.Hooks)
	}
}

func TestCapturedHooksRequiresHooksToml(t *testing.T) {
	dir := t.TempDir()
	profileTOML := `schema = 5

[profile]
name = "broken"
created_at = 2026-09-03T12:00:00Z
updated_at = 2026-09-03T12:00:00Z

[capture]
hooks = true
`
	if err := os.WriteFile(filepath.Join(dir, "profile.toml"), []byte(profileTOML), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "hooks state marked captured") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadCapturedDefaultsRequiresDefaultsFile(t *testing.T) {
	dir := t.TempDir()
	profileTOML := `schema = 3

[profile]
name = "broken"
created_at = 2026-09-03T12:00:00Z
updated_at = 2026-09-03T12:00:00Z

[capture]
defaults = true
`
	if err := os.WriteFile(filepath.Join(dir, "profile.toml"), []byte(profileTOML), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("capture.defaults = true with missing defaults/defaults.toml must fail to load")
	} else if !strings.Contains(err.Error(), "defaults/defaults.toml is missing") {
		t.Fatalf("err = %v", err)
	}
	// A profile that never captured defaults loads fine without the file.
	ok := strings.Replace(profileTOML, "defaults = true", "defaults = false", 1)
	if err := os.WriteFile(filepath.Join(dir, "profile.toml"), []byte(ok), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err != nil {
		t.Fatalf("uncaptured defaults must load without the file: %v", err)
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

func TestSchema5LoadsAsSchema6WithMiseEmpty(t *testing.T) {
	dir := t.TempDir()
	profileTOML := "schema = 5\n\n[profile]\nname = 'schema5'\ncreated_at = 2026-09-08T00:00:00Z\nupdated_at = 2026-09-08T00:00:00Z\n"
	if err := os.WriteFile(filepath.Join(dir, "profile.toml"), []byte(profileTOML), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Manifest.Schema != Schema || len(got.Packages.Mise) != 0 {
		t.Fatalf("migration = %#v", got)
	}
}

func TestMisePackagesRoundTripSchema6(t *testing.T) {
	dir := t.TempDir()
	d := New("main", time.Unix(0, 0))
	d.Packages.Mise = MiseTools{
		"node":                          {"version": "24"},
		"python":                        {"version": []any{"3.12", "3.13"}},
		"npm:@anthropic-ai/claude-code": {"version": "latest"},
		"foo":                           {"version": "2", "postinstall": "foo setup", "install_env": map[string]any{"FOO_MODE": "portable"}},
	}
	if err := Save(dir, d); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(filepath.Join(dir, "packages", "mise.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(dir, d); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(filepath.Join(dir, "packages", "mise.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("mise.toml is not deterministic:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	loaded, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.Packages.Mise, d.Packages.Mise) {
		t.Fatalf("mise = %#v, want %#v", loaded.Packages.Mise, d.Packages.Mise)
	}
}

func TestSchema6LoadsAsSchema7WithResourcesEmpty(t *testing.T) {
	dir := t.TempDir()
	profileTOML := "schema = 6\n\n[profile]\nname = 'schema6'\ncreated_at = 2026-09-08T00:00:00Z\nupdated_at = 2026-09-08T00:00:00Z\n"
	if err := os.WriteFile(filepath.Join(dir, "profile.toml"), []byte(profileTOML), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Manifest.Schema != Schema {
		t.Fatalf("schema=%d want=%d", got.Manifest.Schema, Schema)
	}
	if got.Manifest.Capture.Resources {
		t.Fatal("schema-6 profile unexpectedly captured resources")
	}
	if len(got.Resources.Items) != 0 || len(got.Resources.Links) != 0 {
		t.Fatalf("resources=%#v want empty", got.Resources)
	}
}

func TestResourcesRoundTripSchema7(t *testing.T) {
	dir := t.TempDir()
	d := New("main", time.Unix(0, 0))
	d.Manifest.Capture.Resources = true
	d.Resources = Resources{
		Items: []Resource{
			{ID: "dotfiles", Path: "~/dotfiles", Kind: "directory", Strategy: "git", Remote: "git@github.com:example/dotfiles.git", Branch: "main", Revision: strings.Repeat("b", 40)},
			{ID: "deploy", Path: "~/.local/bin/deploy", Kind: "file", Strategy: "copy", Hash: strings.Repeat("a", 64), Mode: "0755"},
		},
		Links: []ResourceLink{
			{Source: "~/.config/nvim", TargetResource: "dotfiles", Target: "nvim", Origin: "inbound"},
			{SourceResource: "scripts", Source: "current", TargetResource: "dotfiles", Target: "bin/current", Origin: "resource"},
		},
		IgnoredLinks: []string{"~/.config/ignored", "~/.config/ignored"},
	}
	if err := Save(dir, d); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(filepath.Join(dir, "resources", "resources.toml"))
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := d.Resources
	sortResources(&want)
	if !reflect.DeepEqual(loaded.Resources, want) {
		t.Fatalf("resources=%#v want=%#v", loaded.Resources, want)
	}
	if err := Save(dir, loaded); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(filepath.Join(dir, "resources", "resources.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("resources.toml not deterministic:\n%s\n---\n%s", first, second)
	}
}

func TestSchema10ResourceGitDiffRoundTrip(t *testing.T) {
	dir := t.TempDir()
	d := New("test", time.Unix(0, 0))
	d.Manifest.Capture.Resources = true
	d.Resources.Items = []Resource{{
		ID: "dotfiles", Path: "~/dotfiles", Kind: "directory", Strategy: "git+diff",
		Remote: "github.com/example/dotfiles", Branch: "main", Revision: strings.Repeat("a", 40),
		IndexPatchHash: strings.Repeat("b", 64), WorktreePatchHash: strings.Repeat("c", 64),
		Untracked: []GitUntrackedFile{{Path: "notes.md", Hash: strings.Repeat("d", 64), Mode: "0644"}},
	}}
	d.Resources.Links = []ResourceLink{}
	if err := Save(dir, d); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Manifest.Schema != Schema || !reflect.DeepEqual(got.Resources, d.Resources) {
		t.Fatalf("round trip=%#v", got.Resources)
	}
}

func TestSchema9GitResourceLoadsWithoutInventedGitState(t *testing.T) {
	dir := t.TempDir()
	profileTOML := "schema = 9\n\n[profile]\nname = 'schema9'\ncreated_at = 2026-09-10T00:00:00Z\nupdated_at = 2026-09-10T00:00:00Z\n\n[capture]\nresources = true\n"
	if err := os.WriteFile(filepath.Join(dir, "profile.toml"), []byte(profileTOML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "resources"), 0o755); err != nil {
		t.Fatal(err)
	}
	resourcesTOML := "[[resource]]\nid = 'dotfiles'\npath = '~/dotfiles'\nkind = 'directory'\nstrategy = 'git'\nremote = 'github.com/example/dotfiles'\nrevision = '" + strings.Repeat("a", 40) + "'\n"
	if err := os.WriteFile(filepath.Join(dir, "resources", "resources.toml"), []byte(resourcesTOML), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	r := got.Resources.Items[0]
	if got.Manifest.Schema != Schema || r.Strategy != "git" || r.IndexPatchHash != "" || r.WorktreePatchHash != "" || len(r.Untracked) != 0 {
		t.Fatalf("migrated resource=%#v", r)
	}
}

func TestLoadSchema10MigratesWithNoMachines(t *testing.T) {
	dir := t.TempDir()
	manifest := "schema = 10\n\n[profile]\nname = 'legacy'\ncreated_at = 2026-09-11T00:00:00Z\nupdated_at = 2026-09-11T00:00:00Z\n"
	if err := os.WriteFile(filepath.Join(dir, "profile.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Manifest.Schema != Schema || len(got.Machines.Items) != 0 {
		t.Fatalf("migration = %#v", got)
	}
}

func TestLoadSchema11MachinesCanonicalizesAndValidates(t *testing.T) {
	dir := t.TempDir()
	manifest := "schema = 11\n\n[profile]\nname = 'main'\ncreated_at = 2026-09-11T00:00:00Z\nupdated_at = 2026-09-11T00:00:00Z\n"
	if err := os.WriteFile(filepath.Join(dir, "profile.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "machines"), 0o755); err != nil {
		t.Fatal(err)
	}
	framework := "name = \"framework\"\n\n[[resource_path]]\nresource = \"projects\"\npath = \"~/Code\"\n\n[[resource_path]]\nresource = \"dotfiles\"\npath = \"/mnt/dotfiles\"\n"
	if err := os.WriteFile(filepath.Join(dir, "machines", "framework.toml"), []byte(framework), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "machines", "ignored.txt"), []byte("ignored"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := Machines{Items: []Machine{{Name: "framework", ResourcePaths: []MachineResourcePath{{Resource: "dotfiles", Path: "/mnt/dotfiles"}, {Resource: "projects", Path: "~/Code"}}}}}
	if !reflect.DeepEqual(got.Machines, want) {
		t.Fatalf("machines = %#v, want %#v", got.Machines, want)
	}

	if err := os.WriteFile(filepath.Join(dir, "machines", "framework.toml"), []byte(strings.Replace(framework, "name = \"framework\"", "name = \"desktop\"", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("filename/name mismatch error = %v", err)
	}
}

func TestLoadSchema11RejectsDuplicateMachineResources(t *testing.T) {
	dir := t.TempDir()
	manifest := "schema = 11\n\n[profile]\nname = 'main'\ncreated_at = 2026-09-11T00:00:00Z\nupdated_at = 2026-09-11T00:00:00Z\n"
	if err := os.WriteFile(filepath.Join(dir, "profile.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "machines"), 0o755); err != nil {
		t.Fatal(err)
	}
	contents := "name = 'framework'\n[[resource_path]]\nresource = 'projects'\npath = '~/Code'\n[[resource_path]]\nresource = 'projects'\npath = '~/Work'\n"
	if err := os.WriteFile(filepath.Join(dir, "machines", "framework.toml"), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "duplicate resource path") {
		t.Fatalf("duplicate resource error = %v", err)
	}
}

func TestSaveLoadMachinesRoundTripInCanonicalOrder(t *testing.T) {
	dir := t.TempDir()
	d := New("main", time.Unix(0, 0))
	d.Machines = Machines{Items: []Machine{
		{Name: "framework", ResourcePaths: []MachineResourcePath{{Resource: "projects", Path: "~/Code"}, {Resource: "dotfiles", Path: "/mnt/dotfiles"}}},
		{Name: "desktop", ResourcePaths: []MachineResourcePath{{Resource: "music", Path: "/srv/music"}, {Resource: "photos", Path: "~/Pictures"}}},
	}}
	if err := Save(dir, d); err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join(dir, "profile.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(manifest), "schema = 12\n") {
		t.Fatalf("profile.toml = %q, want schema 12", manifest)
	}
	framework, err := os.ReadFile(filepath.Join(dir, "machines", "framework.toml"))
	if err != nil {
		t.Fatal(err)
	}
	wantFramework := "name = 'framework'\n\n[[resource_path]]\nresource = 'dotfiles'\npath = '/mnt/dotfiles'\n\n[[resource_path]]\nresource = 'projects'\npath = '~/Code'\n"
	if string(framework) != wantFramework {
		t.Fatalf("framework.toml = %q, want %q", framework, wantFramework)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := Machines{Items: []Machine{
		{Name: "desktop", ResourcePaths: []MachineResourcePath{{Resource: "music", Path: "/srv/music"}, {Resource: "photos", Path: "~/Pictures"}}},
		{Name: "framework", ResourcePaths: []MachineResourcePath{{Resource: "dotfiles", Path: "/mnt/dotfiles"}, {Resource: "projects", Path: "~/Code"}}},
	}}
	if !reflect.DeepEqual(got.Machines, want) {
		t.Fatalf("machines = %#v, want %#v", got.Machines, want)
	}
}

func TestSaveRejectsDuplicateMachineState(t *testing.T) {
	for _, machines := range []Machines{
		{Items: []Machine{{Name: "framework"}, {Name: "framework"}}},
		{Items: []Machine{{Name: "framework", ResourcePaths: []MachineResourcePath{{Resource: "projects", Path: "~/Code"}, {Resource: "projects", Path: "~/Work"}}}}},
	} {
		d := New("main", time.Unix(0, 0))
		d.Machines = machines
		if err := Save(t.TempDir(), d); err == nil {
			t.Fatalf("invalid machines accepted: %#v", machines)
		}
	}
}

func TestSavePolicyRoundTripsInSortedCanonicalOrder(t *testing.T) {
	dir := t.TempDir()
	d := New("main", time.Unix(0, 0))
	d.Policy = policy.Rules{
		Capture: []policy.Rule{
			{Category: "themes", Setting: policy.SettingDisabled},
			{Category: "packages", Target: "official:vim", Setting: policy.SettingDisabled},
			{Category: "packages", Target: "official:firefox", Setting: policy.SettingEnabled},
		},
		Restore: []policy.Rule{
			{Category: "config", Target: ".config/nvim", Setting: policy.SettingDisabled},
		},
	}
	if err := Save(dir, d); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "policy", "policy.toml"))
	if err != nil {
		t.Fatal(err)
	}
	want := "[[capture]]\ncategory = 'packages'\ntarget = 'official:firefox'\nsetting = 'enabled'\n\n[[capture]]\ncategory = 'packages'\ntarget = 'official:vim'\nsetting = 'disabled'\n\n[[capture]]\ncategory = 'themes'\nsetting = 'disabled'\n\n[[restore]]\ncategory = 'config'\ntarget = '.config/nvim'\nsetting = 'disabled'\n"
	if string(raw) != want {
		t.Fatalf("policy.toml = %q, want %q", raw, want)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	wantRules := policy.Rules{
		Capture: []policy.Rule{
			{Category: "packages", Target: "official:firefox", Setting: policy.SettingEnabled},
			{Category: "packages", Target: "official:vim", Setting: policy.SettingDisabled},
			{Category: "themes", Setting: policy.SettingDisabled},
		},
		Restore: []policy.Rule{
			{Category: "config", Target: ".config/nvim", Setting: policy.SettingDisabled},
		},
	}
	if !reflect.DeepEqual(got.Policy, wantRules) {
		t.Fatalf("policy = %#v, want %#v", got.Policy, wantRules)
	}
}

func TestSavePolicyRemovesFileWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	d := New("main", time.Unix(0, 0))
	d.Policy = policy.Rules{Capture: []policy.Rule{{Category: "packages", Setting: policy.SettingDisabled}}}
	if err := Save(dir, d); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "policy", "policy.toml")); err != nil {
		t.Fatalf("expected policy.toml to exist: %v", err)
	}
	d.Policy = policy.Rules{}
	if err := Save(dir, d); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "policy", "policy.toml")); !os.IsNotExist(err) {
		t.Fatalf("expected policy.toml to be removed once empty, stat err=%v", err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Policy, policy.Rules{}) {
		t.Fatalf("policy = %#v, want empty", got.Policy)
	}
}

func TestLoadPolicyMissingFileMeansNoOverrides(t *testing.T) {
	dir := t.TempDir()
	d := New("main", time.Unix(0, 0))
	if err := Save(dir, d); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Policy, policy.Rules{}) {
		t.Fatalf("policy = %#v, want empty", got.Policy)
	}
}

func TestSaveRejectsInvalidPolicySetting(t *testing.T) {
	d := New("main", time.Unix(0, 0))
	d.Policy = policy.Rules{Capture: []policy.Rule{{Category: "packages", Setting: "maybe"}}}
	if err := Save(t.TempDir(), d); err == nil {
		t.Fatal("invalid policy setting accepted")
	}
}

func TestSaveRejectsUnknownPolicyCategory(t *testing.T) {
	d := New("main", time.Unix(0, 0))
	d.Policy = policy.Rules{Capture: []policy.Rule{{Category: "bogus", Setting: policy.SettingDisabled}}}
	if err := Save(t.TempDir(), d); err == nil {
		t.Fatal("unknown policy category accepted")
	}
}

func TestSaveRejectsDuplicatePolicyRule(t *testing.T) {
	for _, rules := range []policy.Rules{
		{Capture: []policy.Rule{{Category: "packages", Target: "official:vim", Setting: policy.SettingDisabled}, {Category: "packages", Target: "official:vim", Setting: policy.SettingEnabled}}},
		{Restore: []policy.Rule{{Category: "shell", Setting: policy.SettingDisabled}, {Category: "shell", Setting: policy.SettingEnabled}}},
	} {
		d := New("main", time.Unix(0, 0))
		d.Policy = rules
		if err := Save(t.TempDir(), d); err == nil {
			t.Fatalf("duplicate policy rule accepted: %#v", rules)
		}
	}
}

func TestSaveMachineRestoreDefaultsAndPolicyRoundTrip(t *testing.T) {
	dir := t.TempDir()
	d := New("main", time.Unix(0, 0))
	d.Machines = Machines{Items: []Machine{{
		Name:               "desktop",
		RestoreConflicts:   policy.ConflictForce,
		RestoreConvergence: policy.ConvergenceExact,
		Policy:             policy.Rules{Capture: []policy.Rule{{Category: "config", Target: ".config/nvim", Setting: policy.SettingDisabled}}},
	}}}
	if err := Save(dir, d); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Machines.Items) != 1 {
		t.Fatalf("machines = %#v", got.Machines.Items)
	}
	machine := got.Machines.Items[0]
	if machine.RestoreConflicts != policy.ConflictForce || machine.RestoreConvergence != policy.ConvergenceExact {
		t.Fatalf("restore defaults = %+v, want Force/Exact", machine)
	}
	wantPolicy := policy.Rules{Capture: []policy.Rule{{Category: "config", Target: ".config/nvim", Setting: policy.SettingDisabled}}}
	if !reflect.DeepEqual(machine.Policy, wantPolicy) {
		t.Fatalf("machine policy = %#v, want %#v", machine.Policy, wantPolicy)
	}
}

func TestMachineEffectiveRestoreDefaultsNormalizesEmptyToSafeAdditive(t *testing.T) {
	var m Machine
	got := m.EffectiveRestoreDefaults()
	want := policy.DefaultRestoreOptions()
	if got != want {
		t.Fatalf("EffectiveRestoreDefaults() = %+v, want %+v", got, want)
	}
	m.RestoreConflicts = policy.ConflictForce
	if got := m.EffectiveRestoreDefaults(); got.Conflicts != policy.ConflictForce || got.Convergence != policy.ConvergenceAdditive {
		t.Fatalf("partial override = %+v, want Force/Additive", got)
	}
}

func TestSaveRejectsInvalidMachineRestoreDefaults(t *testing.T) {
	for _, machine := range []Machine{
		{Name: "desktop", RestoreConflicts: "sometimes"},
		{Name: "desktop", RestoreConvergence: "mostly"},
	} {
		d := New("main", time.Unix(0, 0))
		d.Machines = Machines{Items: []Machine{machine}}
		if err := Save(t.TempDir(), d); err == nil {
			t.Fatalf("invalid restore defaults accepted: %#v", machine)
		}
	}
}

func TestLoadSchema11MachineFileHasDefaultRestoreOptionsAndEmptyPolicy(t *testing.T) {
	dir := t.TempDir()
	manifest := "schema = 11\n\n[profile]\nname = 'legacy'\ncreated_at = 2026-09-18T00:00:00Z\nupdated_at = 2026-09-18T00:00:00Z\n"
	if err := os.WriteFile(filepath.Join(dir, "profile.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(dir, "profile.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "machines"), 0o755); err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile(filepath.Join("testdata", "schema11-machine.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "machines", "desktop.toml"), fixture, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Manifest.Schema != Schema {
		t.Fatalf("schema = %d, want %d", got.Manifest.Schema, Schema)
	}
	if !reflect.DeepEqual(got.Policy, policy.Rules{}) {
		t.Fatalf("policy = %#v, want empty", got.Policy)
	}
	if len(got.Machines.Items) != 1 {
		t.Fatalf("machines = %#v", got.Machines.Items)
	}
	machine := got.Machines.Items[0]
	if machine.RestoreConflicts != "" || machine.RestoreConvergence != "" || !reflect.DeepEqual(machine.Policy, policy.Rules{}) {
		t.Fatalf("machine = %#v, want empty restore/policy overrides", machine)
	}
	if got := machine.EffectiveRestoreDefaults(); got != policy.DefaultRestoreOptions() {
		t.Fatalf("EffectiveRestoreDefaults() = %+v, want Safe/Additive default", got)
	}

	// Load must never mutate the profile on disk.
	after, err := os.ReadFile(filepath.Join(dir, "profile.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("Load mutated profile.toml on disk: before=%q after=%q", before, after)
	}
}

func TestSavePackageAbsenceRoundTripsWithPriorMiseDeclaration(t *testing.T) {
	dir := t.TempDir()
	d := New("main", time.Unix(0, 0))
	d.Packages.Absent = []PackageAbsence{
		{Ref: "official:htop"},
		{Ref: "mise:node", Mise: MiseTool{"version": "24"}},
	}
	if err := Save(dir, d); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "packages", "absent.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "[[package]]") {
		t.Fatalf("absent.toml = %q, want [[package]] entries", raw)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Packages.Absent, d.Packages.Absent) {
		t.Fatalf("absent = %#v, want %#v", got.Packages.Absent, d.Packages.Absent)
	}
}

func TestSavePackageAbsenceRemovesFileWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	d := New("main", time.Unix(0, 0))
	d.Packages.Absent = []PackageAbsence{{Ref: "official:htop"}}
	if err := Save(dir, d); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "packages", "absent.toml")); err != nil {
		t.Fatalf("expected absent.toml to exist: %v", err)
	}
	d.Packages.Absent = nil
	if err := Save(dir, d); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "packages", "absent.toml")); !os.IsNotExist(err) {
		t.Fatalf("expected absent.toml to be removed once empty, stat err=%v", err)
	}
}

func TestSaveRejectsPackageBothPresentAndAbsent(t *testing.T) {
	for _, packages := range []Packages{
		{Official: []string{"htop"}, Absent: []PackageAbsence{{Ref: "official:htop"}}},
		{AUR: []string{"yay"}, Absent: []PackageAbsence{{Ref: "aur:yay"}}},
		{Mise: MiseTools{"node": {}}, Absent: []PackageAbsence{{Ref: "mise:node"}}},
	} {
		d := New("main", time.Unix(0, 0))
		d.Packages = packages
		if err := Save(t.TempDir(), d); err == nil {
			t.Fatalf("package both present and absent accepted: %#v", packages)
		}
	}
}

func TestSaveRejectsDuplicatePackageAbsence(t *testing.T) {
	d := New("main", time.Unix(0, 0))
	d.Packages.Absent = []PackageAbsence{{Ref: "official:htop"}, {Ref: "official:htop"}}
	if err := Save(t.TempDir(), d); err == nil {
		t.Fatal("duplicate package absence accepted")
	}
}

func TestSaveThemesPluginsHooksRejectBothPresentAndAbsent(t *testing.T) {
	themes := New("main", time.Unix(0, 0))
	themes.Themes = Themes{Items: []Theme{{ID: "catppuccin"}}, Absent: []Theme{{ID: "catppuccin"}}}
	if err := Save(t.TempDir(), themes); err == nil {
		t.Fatal("theme both present and absent accepted")
	}

	plugins := New("main", time.Unix(0, 0))
	plugins.Plugins = Plugins{Items: []Plugin{{ID: "acme.weather"}}, Absent: []Plugin{{ID: "acme.weather"}}}
	if err := Save(t.TempDir(), plugins); err == nil {
		t.Fatal("plugin both present and absent accepted")
	}

	hooks := New("main", time.Unix(0, 0))
	hooks.Hooks = Hooks{Items: []Hook{{Path: "post-update.d/refresh-icons"}}, Absent: []Hook{{Path: "post-update.d/refresh-icons"}}}
	if err := Save(t.TempDir(), hooks); err == nil {
		t.Fatal("hook both present and absent accepted")
	}
}

func TestSaveThemesPluginsHooksAbsenceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	d := New("main", time.Unix(0, 0))
	d.Themes = Themes{Current: "nord", Items: []Theme{{ID: "nord", Type: "builtin", Enabled: true}}, Absent: []Theme{{ID: "catppuccin", Type: "git", URL: "https://example.test/theme.git"}}}
	d.Plugins = Plugins{Items: []Plugin{{ID: "omarchy.clock", Enabled: true}}, Absent: []Plugin{{ID: "acme.weather", Source: "git"}}}
	d.Hooks = Hooks{Items: []Hook{{Path: "pre-restore.sh", Hash: "aaa", Mode: "0755"}}, Absent: []Hook{{Path: "post-update.d/refresh-icons", Hash: "bbb", Mode: "0644"}}}
	if err := Save(dir, d); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Themes, d.Themes) {
		t.Fatalf("themes = %#v, want %#v", got.Themes, d.Themes)
	}
	if !reflect.DeepEqual(got.Plugins, d.Plugins) {
		t.Fatalf("plugins = %#v, want %#v", got.Plugins, d.Plugins)
	}
	if !reflect.DeepEqual(got.Hooks, d.Hooks) {
		t.Fatalf("hooks = %#v, want %#v", got.Hooks, d.Hooks)
	}
}

func TestLoadSchema11MigratesLegacyPackageExclusionsToPolicy(t *testing.T) {
	dir := t.TempDir()
	manifest := "schema = 11\n\n[profile]\nname = 'legacy'\ncreated_at = 2026-09-18T00:00:00Z\nupdated_at = 2026-09-18T00:00:00Z\n\n[capture]\npackages = true\n"
	if err := os.WriteFile(filepath.Join(dir, "profile.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "packages"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "packages", "official.txt"), []byte("htop\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "packages", "excluded.txt"), []byte("aur:dislocker-git\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "packages", "machine-specific.txt"), []byte("official:nvidia-open\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	beforeExcluded, err := os.ReadFile(filepath.Join(dir, "packages", "excluded.txt"))
	if err != nil {
		t.Fatal(err)
	}

	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Packages.Excluded) != 0 {
		t.Fatalf("Excluded = %#v, want migrated away", got.Packages.Excluded)
	}
	if len(got.Packages.MachineSpecific) != 0 {
		t.Fatalf("MachineSpecific = %#v, want cleared: it is runtime inspection metadata now, not persisted state", got.Packages.MachineSpecific)
	}
	if !reflect.DeepEqual(got.Packages.Official, []string{"htop"}) {
		t.Fatalf("Official = %#v, want unchanged: migration never consults the current machine to infer desired absence", got.Packages.Official)
	}
	want := policy.Rules{
		Capture: []policy.Rule{{Category: "packages", Target: "aur:dislocker-git", Setting: policy.SettingDisabled}},
		Restore: []policy.Rule{{Category: "packages", Target: "aur:dislocker-git", Setting: policy.SettingDisabled}},
	}
	if !reflect.DeepEqual(got.Policy, want) {
		t.Fatalf("policy = %#v, want %#v", got.Policy, want)
	}

	// Loading a legacy profile must never mutate it on disk.
	afterExcluded, err := os.ReadFile(filepath.Join(dir, "packages", "excluded.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeExcluded, afterExcluded) {
		t.Fatalf("Load mutated packages/excluded.txt: before=%q after=%q", beforeExcluded, afterExcluded)
	}
	if _, err := os.Stat(filepath.Join(dir, "policy", "policy.toml")); !os.IsNotExist(err) {
		t.Fatalf("Load must not write policy/policy.toml, stat err=%v", err)
	}
}

// TestLoadSchema11MigrationRemovesTheExcludedMiseDeclaration is a
// regression for a review finding on PR 3: a legacy excluded Mise
// declaration stayed in Packages.Mise even after migration, since the old
// Excluded mechanism never stripped Mise (only Official/AUR). Migration
// must leave the excluded target with no desired state at all, including
// Mise.
func TestLoadSchema11MigrationRemovesTheExcludedMiseDeclaration(t *testing.T) {
	dir := t.TempDir()
	manifest := "schema = 11\n\n[profile]\nname = 'legacy'\ncreated_at = 2026-09-18T00:00:00Z\nupdated_at = 2026-09-18T00:00:00Z\n\n[capture]\npackages = true\n"
	if err := os.WriteFile(filepath.Join(dir, "profile.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "packages"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "packages", "excluded.txt"), []byte("mise:node\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "packages", "mise.toml"), []byte("[tools.node]\nversion = \"22\"\n\n[tools.python]\nversion = \"3.13\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Packages.Mise["node"]; ok {
		t.Fatalf("Mise = %#v, want the excluded node declaration removed", got.Packages.Mise)
	}
	if _, ok := got.Packages.Mise["python"]; !ok {
		t.Fatalf("Mise = %#v, want the unrelated python declaration left alone", got.Packages.Mise)
	}
	want := policy.Rules{
		Capture: []policy.Rule{{Category: "packages", Target: "mise:node", Setting: policy.SettingDisabled}},
		Restore: []policy.Rule{{Category: "packages", Target: "mise:node", Setting: policy.SettingDisabled}},
	}
	if !reflect.DeepEqual(got.Policy, want) {
		t.Fatalf("policy = %#v, want %#v", got.Policy, want)
	}
}

func TestLoadSchema11MigrationDoesNotDuplicateAnAlreadyPresentPolicyRule(t *testing.T) {
	dir := t.TempDir()
	manifest := "schema = 11\n\n[profile]\nname = 'legacy'\ncreated_at = 2026-09-18T00:00:00Z\nupdated_at = 2026-09-18T00:00:00Z\n"
	if err := os.WriteFile(filepath.Join(dir, "profile.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "packages"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "packages", "excluded.txt"), []byte("official:htop\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A hand-authored policy.toml already carries an equivalent rule.
	if err := os.MkdirAll(filepath.Join(dir, "policy"), 0o755); err != nil {
		t.Fatal(err)
	}
	policyTOML := "[[capture]]\ncategory = 'packages'\ntarget = 'official:htop'\nsetting = 'disabled'\n"
	if err := os.WriteFile(filepath.Join(dir, "policy", "policy.toml"), []byte(policyTOML), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Policy.Capture) != 1 {
		t.Fatalf("capture rules = %#v, want exactly one (no duplicate)", got.Policy.Capture)
	}
	if len(got.Policy.Restore) != 1 || got.Policy.Restore[0].Target != "official:htop" {
		t.Fatalf("restore rules = %#v, want the migrated restore-disabled rule", got.Policy.Restore)
	}
}

// TestLoadMigratesStalePackagesFilesEvenAtTheCurrentSchema is a regression
// for a review finding on PR 3: migration must not be gated to a legacy
// schema, since a stray packages/excluded.txt or machine-specific.txt left
// over from before this cutover (or written by code that had not yet been
// fixed) would otherwise never be cleaned up on an already-current-schema
// profile.
func TestLoadMigratesStalePackagesFilesEvenAtTheCurrentSchema(t *testing.T) {
	dir := t.TempDir()
	d := New("main", time.Now())
	d.Packages.Official = []string{"htop"}
	if err := Save(dir, d); err != nil {
		t.Fatal(err)
	}
	// Simulate a stray leftover from before the cutover on an otherwise
	// current-schema profile.
	if err := os.WriteFile(filepath.Join(dir, "packages", "excluded.txt"), []byte("aur:dislocker-git\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "packages", "machine-specific.txt"), []byte("official:nvidia-open\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Packages.Excluded) != 0 || len(got.Packages.MachineSpecific) != 0 {
		t.Fatalf("packages = %#v, want both cleared even at the current schema", got.Packages)
	}
	want := policy.Rules{
		Capture: []policy.Rule{{Category: "packages", Target: "aur:dislocker-git", Setting: policy.SettingDisabled}},
		Restore: []policy.Rule{{Category: "packages", Target: "aur:dislocker-git", Setting: policy.SettingDisabled}},
	}
	if !reflect.DeepEqual(got.Policy, want) {
		t.Fatalf("policy = %#v, want %#v", got.Policy, want)
	}
}

// TestSavePrunesObsoletePackagesFiles is a regression for a review finding
// on PR 3: Save must never write packages/excluded.txt or
// packages/machine-specific.txt, and must actively remove either file if it
// already exists, so a schema-12 save can never leave two authorities for
// the same state.
func TestSavePrunesObsoletePackagesFiles(t *testing.T) {
	dir := t.TempDir()
	d := New("main", time.Now())
	if err := Save(dir, d); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "packages", "excluded.txt"), []byte("aur:dislocker-git\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "packages", "machine-specific.txt"), []byte("official:nvidia-open\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Save(dir, d); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "packages", "excluded.txt")); !os.IsNotExist(err) {
		t.Fatal("packages/excluded.txt was not pruned")
	}
	if _, err := os.Stat(filepath.Join(dir, "packages", "machine-specific.txt")); !os.IsNotExist(err) {
		t.Fatal("packages/machine-specific.txt was not pruned")
	}
}
