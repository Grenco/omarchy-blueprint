package profile

import (
	"bytes"
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
	if d.Manifest.Schema != 9 {
		t.Fatalf("new profile schema = %d, want 9", d.Manifest.Schema)
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
	if got.Manifest.Schema != 9 {
		t.Fatalf("schema = %d, want 9", got.Manifest.Schema)
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
	if !strings.Contains(string(savedManifest), "schema = 9\n") {
		t.Fatalf("saved profile.toml = %q, want schema 9", savedManifest)
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
	if got.Manifest.Schema != 9 || !reflect.DeepEqual(got.Config, want) {
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
	if got.Manifest.Schema != 9 {
		t.Fatalf("schema = %d, want 9", got.Manifest.Schema)
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
	if got.Manifest.Schema != 9 {
		t.Fatalf("schema = %d, want 9", got.Manifest.Schema)
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
	if got.Manifest.Schema != 9 || got.Manifest.Capture.Hooks || len(got.Hooks.Items) != 0 {
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
	if got.Manifest.Schema != 9 || len(got.Packages.Mise) != 0 {
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
	if got.Manifest.Schema != 9 {
		t.Fatalf("schema=%d want=9", got.Manifest.Schema)
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
