package localstate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestInspectDirNeverCreates(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "omarchy-blueprint", "profiles", "x")
	if err := InspectDir(home, dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "omarchy-blueprint")); !os.IsNotExist(err) {
		t.Fatal("InspectDir created state")
	}
}

func TestPrivateDirTightensAndRefusesAliases(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "omarchy-blueprint", "profiles", "x")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := PrivateDir(home, dir); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(home, "omarchy-blueprint"), dir} {
		if info, _ := os.Stat(p); info.Mode().Perm() != 0o700 {
			t.Fatalf("%s mode %v", p, info.Mode())
		}
	}
	aliased := filepath.Join(home, "omarchy-blueprint", "profiles", "y")
	if err := os.Symlink(dir, aliased); err != nil {
		t.Fatal(err)
	}
	if err := PrivateDir(home, aliased); !errors.Is(err, ErrNotDirectory) {
		t.Fatalf("symlink alias accepted: %v", err)
	}
	if err := InspectDir(home, aliased); !errors.Is(err, ErrNotDirectory) {
		t.Fatalf("symlink alias inspected: %v", err)
	}
	if err := PrivateDir(home, filepath.Join(home, "..", "escape")); err == nil {
		t.Fatal("directory outside the state home accepted")
	}
}

func TestReadFileOnlyAcceptsBoundedRegularFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state")
	if _, err := ReadFile(path, 10); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing: %v", err)
	}
	if err := WriteFile(path, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", info.Mode())
	}
	if data, err := ReadFile(path, 10); err != nil || string(data) != "hello" {
		t.Fatalf("read %q %v", data, err)
	}
	if _, err := ReadFile(path, 4); err == nil || strings.Contains(err.Error(), "hello") {
		t.Fatalf("oversize read: %v", err)
	}
	link := filepath.Join(dir, "link")
	os.Symlink(path, link)
	if _, err := ReadFile(link, 10); err == nil {
		t.Fatal("followed a symlink")
	}
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err == nil {
		if _, err := ReadFile(fifo, 10); err == nil {
			t.Fatal("read a FIFO")
		}
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("temporary file left: %s", e.Name())
		}
	}
}

func TestRemoveFileIsIdempotent(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "s")
	path := filepath.Join(dir, "f")
	if err := RemoveFile(home, path); err != nil {
		t.Fatalf("missing dir: %v", err)
	}
	if err := PrivateDir(home, dir); err != nil {
		t.Fatal(err)
	}
	WriteFile(path, []byte("x"))
	if err := RemoveFile(home, path); err != nil {
		t.Fatal(err)
	}
	if err := RemoveFile(home, path); err != nil {
		t.Fatalf("second remove: %v", err)
	}
}
