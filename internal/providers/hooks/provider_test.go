package hooks

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func hookProvider(t *testing.T) (Provider, string, string) {
	t.Helper()
	root := t.TempDir()
	user, profileDir := filepath.Join(root, "user"), filepath.Join(root, "profile")
	return Provider{UserDir: user, ProfileDir: profileDir}, user, profileDir
}

func writeHook(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func TestDetectMissingRootIsEmpty(t *testing.T) {
	p, _, _ := hookProvider(t)
	state, err := p.Detect()
	if err != nil || len(state.Items) != 0 {
		t.Fatalf("state = %#v, err = %v", state, err)
	}
}

func TestDetectCapturesAndSortsRuntimeHooks(t *testing.T) {
	p, user, _ := hookProvider(t)
	writeHook(t, filepath.Join(user, "post-boot"), "boot\x00\n", 0o755)
	writeHook(t, filepath.Join(user, "future.d", "z"), "z", 0o644)
	writeHook(t, filepath.Join(user, "future.d", "a"), "a", 0o600)
	writeHook(t, filepath.Join(user, "future.d", ".hidden"), "hidden", 0o644)
	writeHook(t, filepath.Join(user, "future.d", "example.sample"), "sample", 0o644)
	writeHook(t, filepath.Join(user, "future.d", "nested", "ignored"), "ignored", 0o644)
	writeHook(t, filepath.Join(user, "not-runtime", "ignored"), "ignored", 0o644)
	state, err := p.Detect()
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{}
	for _, item := range state.Items {
		paths = append(paths, item.Path)
	}
	if !reflect.DeepEqual(paths, []string{"future.d/a", "future.d/z", "post-boot"}) {
		t.Fatalf("paths = %v", paths)
	}
	if state.Items[2].Mode != "0755" || string(state.Items[2].Raw) != "boot\x00\n" {
		t.Fatalf("hook = %#v", state.Items[2])
	}
}

func TestDetectLeavesRuntimeSymlinksUnmanaged(t *testing.T) {
	p, user, _ := hookProvider(t)
	if err := os.MkdirAll(user, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/tmp", user); err == nil {
		t.Fatal("replacing hooks root must fail")
	}
	root := t.TempDir()
	link := filepath.Join(root, "hooks")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	if _, err := (Provider{UserDir: link}).Detect(); err == nil {
		t.Fatal("root symlink must fail")
	}
	writeHook(t, filepath.Join(user, "real"), "x", 0o644)
	if err := os.Symlink(filepath.Join(user, "real"), filepath.Join(user, "post-boot")); err != nil {
		t.Fatal(err)
	}
	state, err := p.Detect()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Items) != 1 || state.Items[0].Path != "real" || len(state.Unmanaged) != 1 || state.Unmanaged[0].Path != "post-boot" || state.Unmanaged[0].Target == "" {
		t.Fatalf("state=%#v", state)
	}
	if err := os.Symlink(filepath.Join(user, "missing"), filepath.Join(user, "future")); err != nil {
		t.Fatal(err)
	}
	state, err = p.Detect()
	if err != nil || len(state.Unmanaged) != 2 || !state.Unmanaged[0].Broken && !state.Unmanaged[1].Broken {
		t.Fatalf("state=%#v err=%v", state, err)
	}
}

func TestCaptureStoresInertSnapshotsAndCheckValidatesTree(t *testing.T) {
	p, user, profileDir := hookProvider(t)
	writeHook(t, filepath.Join(user, "post-update.d", "update-rust"), "#!/bin/sh\nprintf x\n", 0o755)
	state, err := p.Detect()
	if err != nil {
		t.Fatal(err)
	}
	saved, err := p.Capture(state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Items[0].Mode != "0755" {
		t.Fatalf("metadata = %#v", saved)
	}
	snapshot := filepath.Join(profileDir, "hooks/files/post-update.d/update-rust")
	info, err := os.Stat(snapshot)
	if err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("snapshot mode = %v, err = %v", info.Mode(), err)
	}
	if err := p.Check(saved); err != nil {
		t.Fatalf("valid snapshots rejected: %v", err)
	}
	writeHook(t, filepath.Join(profileDir, "hooks/files", "orphan"), "x", 0o644)
	if err := p.Check(saved); err == nil {
		t.Fatal("orphan snapshot must fail")
	}
}

func TestCaptureRemovesStaleSnapshots(t *testing.T) {
	p, user, profileDir := hookProvider(t)
	writeHook(t, filepath.Join(user, "post-boot"), "x", 0o644)
	state, err := p.Detect()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Capture(state); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Capture(State{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(profileDir, "hooks/files/post-boot")); !os.IsNotExist(err) {
		t.Fatal("stale snapshot retained")
	}
}

func TestCheckRejectsTamperedAndSymlinkSnapshots(t *testing.T) {
	p, _, profileDir := hookProvider(t)
	body := "hook\n"
	sum := sha256.Sum256([]byte(body))
	saved := profile.Hooks{Items: []profile.Hook{{Path: "post-boot", Hash: hex.EncodeToString(sum[:]), Mode: "0644"}}}
	snapshot := filepath.Join(profileDir, "hooks/files/post-boot")
	writeHook(t, snapshot, "changed", 0o644)
	if err := p.Check(saved); err == nil {
		t.Fatal("tampered snapshot must fail")
	}
	if err := os.Remove(snapshot); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/tmp", snapshot); err != nil {
		t.Fatal(err)
	}
	if err := p.Check(saved); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("err = %v", err)
	}
}

func TestCheckRejectsDuplicateMetadata(t *testing.T) {
	p, _, _ := hookProvider(t)
	item := profile.Hook{Path: "post-boot", Hash: strings.Repeat("a", 64), Mode: "0644"}
	if err := p.Check(profile.Hooks{Items: []profile.Hook{item, item}}); err == nil {
		t.Fatal("duplicate metadata must fail")
	}
}
