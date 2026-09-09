package config

import (
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func TestConfigBackupAndExclusionPolicy(t *testing.T) {
	for name, want := range map[string]bool{"bindings.lua.bak.20260909": true, "x.bak.y": true, ".bak.y": false, "x.bak.": false, ".zshrc.backup-now": true, ".backup-now": false, ".config.omarchy-blueprint-backup-1": true, ".anything.with.dots.omarchy-blueprint-backup-42": true, ".omarchy-blueprint-backup-1": false, ".config.omarchy-blueprint-backup-x": false} {
		if got := IsOmarchyUpdateBackupName(name) || IsOmarchySetupBackupName(name) || IsBlueprintBackupName(name); got != want {
			t.Fatalf("%s=%v", name, got)
		}
	}
	if !IsExcludedConfigPath(".config/google-chrome/Default", []string{".config/google-chrome"}) || IsExcludedConfigPath(".config/google-chromium", []string{".config/google-chrome"}) {
		t.Fatal("exclusion containment")
	}
}

func TestConfigExclusionHelpersNormalizeAndPruneMetadata(t *testing.T) {
	saved := profile.Configs{
		Files:   []profile.ConfigFile{{Path: "nvim/init.lua"}, {Path: "ghostty/config"}},
		Deletes: []profile.ConfigDelete{{Path: "nvim/plugin.lua"}, {Path: "hypr/bindings.lua"}},
	}
	updated, removed, err := AddExclusion(saved, "~/.config/nvim")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(updated.Excluded, []string{"nvim"}) || !reflect.DeepEqual(removed, []string{"nvim/init.lua", "nvim/plugin.lua"}) {
		t.Fatalf("excluded=%v removed=%v", updated.Excluded, removed)
	}
	if len(updated.Files) != 1 || updated.Files[0].Path != "ghostty/config" || len(updated.Deletes) != 1 || updated.Deletes[0].Path != "hypr/bindings.lua" {
		t.Fatalf("state=%#v", updated)
	}
	updated, changed, err := RemoveExclusion(updated, ".config/nvim")
	if err != nil || !changed || len(updated.Excluded) != 0 {
		t.Fatalf("state=%#v changed=%v err=%v", updated, changed, err)
	}
}

func TestNormalizeConfigExclusionPathRejectsUnsafeInput(t *testing.T) {
	for _, input := range []string{"", "../ssh", "/etc", "~/.ssh", ".ssh/id_ed25519", "~/.config"} {
		if _, err := NormalizeExclusionPath(input); err == nil {
			t.Fatalf("%q accepted", input)
		}
	}
}
func TestConfigPolicyVolatileAndSize(t *testing.T) {
	info := fakeInfo{size: MaxAutomaticConfigFileSize}
	if ClassifyConfigPolicy(".config/foo/app.lock", info, nil).Reason != PolicyVolatile || ClassifyConfigPolicy(".config/myapp/config.json", info, nil).Reason != PolicyAllowed {
		t.Fatal("volatile policy")
	}
	if ClassifyConfigPolicy(".config/big", fakeInfo{size: MaxAutomaticConfigFileSize + 1}, nil).Reason != PolicyOversized {
		t.Fatal("size policy")
	}
}

func TestConfigPolicyDeniesKnownSensitivePaths(t *testing.T) {
	for _, path := range []string{".config/gh/hosts.yml", ".config/gcloud/configurations/config_default", ".config/rclone/rclone.conf", ".config/sops/age/keys.txt", ".config/containers/auth.json"} {
		if got := ClassifyConfigPolicy(path, fakeInfo{}, nil).Reason; got != PolicySensitive {
			t.Fatalf("%s policy=%s", path, got)
		}
	}
}

func TestSensitiveContentDetector(t *testing.T) {
	path := t.TempDir() + "/innocuous.conf"
	if err := os.WriteFile(path, []byte("-----BEGIN OPENSSH PRIVATE KEY-----\nsecret\n-----END OPENSSH PRIVATE KEY-----\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sensitive, err := hasSensitiveContent(path)
	if err != nil || !sensitive {
		t.Fatalf("sensitive=%v err=%v", sensitive, err)
	}
}

type fakeInfo struct{ size int64 }

func (f fakeInfo) Name() string       { return "x" }
func (f fakeInfo) Size() int64        { return f.size }
func (f fakeInfo) Mode() os.FileMode  { return 0o644 }
func (f fakeInfo) ModTime() time.Time { return time.Time{} }
func (f fakeInfo) IsDir() bool        { return false }
func (f fakeInfo) Sys() any           { return nil }
