package config

import (
	"os"
	"testing"
	"time"
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
