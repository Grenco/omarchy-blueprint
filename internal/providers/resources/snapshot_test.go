package resources

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestStageCopyFilePreservesModeAndHash(t *testing.T) {
	source := filepath.Join(t.TempDir(), "deploy")
	if err := os.WriteFile(source, []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "deploy", "content")
	scan, err := StageCopyResource(source, destination)
	if err != nil || scan.Mode.Perm() != 0o755 || scan.Hash == "" || scan.FileCount != 1 {
		t.Fatalf("scan=%#v err=%v", scan, err)
	}
	content, err := os.ReadFile(destination)
	if err != nil || string(content) != "#!/bin/sh\necho ok\n" {
		t.Fatalf("content=%q err=%v", content, err)
	}
	if err := ValidateSnapshotTree(destination, scan.Hash); err != nil {
		t.Fatal(err)
	}
}

func TestStageCopyDirectoryOmitsSymlinks(t *testing.T) {
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "regular.txt"), []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("regular.txt", filepath.Join(source, "internal")); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "snapshot")
	scan, err := StageCopyResource(source, destination)
	if err != nil || len(scan.Links) != 1 || scan.Links[0].RawTarget != "regular.txt" {
		t.Fatalf("scan=%#v err=%v", scan, err)
	}
	if _, err := os.Lstat(filepath.Join(destination, "internal")); !os.IsNotExist(err) {
		t.Fatalf("snapshot contains symlink: %v", err)
	}
	if err := ValidateSnapshotTree(source, scan.Hash); err == nil || !strings.Contains(err.Error(), "snapshot contains symlink") {
		t.Fatalf("symlink snapshot validation error=%v", err)
	}
}

func TestCopyResourceRejectsSpecialAndSensitiveFiles(t *testing.T) {
	root := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(root, "pipe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ScanCopyResource(root); err == nil || !strings.Contains(err.Error(), "unsupported file") {
		t.Fatalf("FIFO error=%v", err)
	}
	for name, content := range map[string]string{".env.production": "KEY=value\n", "private.txt": "-----BEGIN OPENSSH PRIVATE KEY-----\n"} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := ScanCopyResource(dir); err == nil || !strings.Contains(err.Error(), name) {
			t.Fatalf("sensitive %s error=%v", name, err)
		}
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "credential-helper.go"), []byte("package helper\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ScanCopyResource(dir); err != nil {
		t.Fatalf("ordinary credential helper rejected: %v", err)
	}
}

func TestStageCopyResourcePreservesModesDespiteUmask(t *testing.T) {
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)
	source := t.TempDir()
	if err := os.Chmod(source, 0o775); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "file"), []byte("file"), 0o664); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(source, "file"), 0o664); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "script"), []byte("script"), 0o775); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(source, "script"), 0o775); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "snapshot")
	if _, err := StageCopyResource(source, destination); err != nil {
		t.Fatal(err)
	}
	for path, mode := range map[string]os.FileMode{destination: 0o775, filepath.Join(destination, "file"): 0o664, filepath.Join(destination, "script"): 0o775} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != mode {
			t.Fatalf("path=%s mode=%o err=%v", path, info.Mode().Perm(), err)
		}
	}
}

func TestStageCopyResourceWithOptionsExcludesGitAdministration(t *testing.T) {
	source := t.TempDir()
	if err := os.MkdirAll(filepath.Join(source, ".git", "objects"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, ".git", "objects", "object"), []byte("admin"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, ".gitignore"), []byte("ignored\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(source, "nested", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "nested", ".git", "config"), []byte("admin"), 0o644); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "snapshot")
	if _, err := StageCopyResourceWithOptions(source, destination, SnapshotOptions{ExcludeGitAdmin: true}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(destination, ".git"), filepath.Join(destination, "nested", ".git")} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("Git administration copied at %s: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(destination, ".gitignore")); err != nil {
		t.Fatalf(".gitignore was excluded: %v", err)
	}
}
