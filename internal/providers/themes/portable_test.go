package themes

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func writePortableFixture(t *testing.T, root string, files map[string]string, modes map[string]os.FileMode) {
	t.Helper()
	for rel, content := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for rel, mode := range modes {
		if err := os.Chmod(filepath.Join(root, filepath.FromSlash(rel)), mode); err != nil {
			t.Fatal(err)
		}
	}
}

// legacyFingerprint is the pre-contract algorithm: the live tree, including
// every directory's and file's permission bits.
func legacyFingerprint(t *testing.T, root string) string {
	t.Helper()
	h := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if rel == "." {
			return nil
		}
		if rel == ".git" {
			return filepath.SkipDir
		}
		info, _ := entry.Info()
		fmt.Fprintf(h, "%s\x00%o\x00", filepath.ToSlash(rel), info.Mode().Perm())
		if info.Mode().IsRegular() {
			f, _ := os.Open(path)
			io.Copy(h, f)
			f.Close()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func TestPortableFingerprintMatchesWhatGitCanStore(t *testing.T) {
	files := map[string]string{"colors.toml": "accent = '#fff'\n", "backgrounds/dawn.jpg": "image", "empty.conf": "", "bin/apply.sh": "#!/bin/sh\n"}
	canonical := t.TempDir()
	writePortableFixture(t, canonical, files, map[string]os.FileMode{"bin/apply.sh": 0o755})
	want, err := portableFingerprint(canonical)
	if err != nil {
		t.Fatal(err)
	}
	// Recorded hashes of ordinarily-permissioned themes keep verifying.
	if legacy := legacyFingerprint(t, canonical); legacy != want {
		t.Fatalf("canonical theme changed fingerprint: legacy %s, contract %s", legacy, want)
	}

	unusual := t.TempDir()
	writePortableFixture(t, unusual, files, map[string]os.FileMode{"bin/apply.sh": 0o700, "colors.toml": 0o600, "backgrounds/dawn.jpg": 0o640, "backgrounds": 0o700, "bin": 0o750})
	if err := os.MkdirAll(filepath.Join(unusual, "unused", "deeper"), 0o700); err != nil {
		t.Fatal(err)
	}
	if got, err := portableFingerprint(unusual); err != nil || got != want {
		t.Fatalf("permissions or empty folders Git cannot store changed the fingerprint: %v", err)
	}

	notExecutable := t.TempDir()
	writePortableFixture(t, notExecutable, files, nil)
	if got, _ := portableFingerprint(notExecutable); got == want {
		t.Fatal("the executable bit Git stores did not affect the fingerprint")
	}
	emptied := t.TempDir()
	writePortableFixture(t, emptied, files, map[string]os.FileMode{"bin/apply.sh": 0o755})
	if err := os.WriteFile(filepath.Join(emptied, "colors.toml"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := portableFingerprint(emptied); err != nil || got == want {
		t.Fatalf("emptying a file was not a content change: %v", err)
	}
}

func TestPortableFingerprintRejectsUnrepresentableThemes(t *testing.T) {
	cases := map[string]func(t *testing.T, root string){
		"empty":              func(*testing.T, string) {},
		"empty-folders-only": func(t *testing.T, root string) { os.MkdirAll(filepath.Join(root, "a", "b"), 0o755) },
		"git-metadata-only": func(t *testing.T, root string) {
			writePortableFixture(t, root, map[string]string{".git/HEAD": "ref"}, nil)
		},
		"nested-git": func(t *testing.T, root string) {
			writePortableFixture(t, root, map[string]string{"colors.toml": "x", "vendor/.Git/HEAD": "ref"}, nil)
		},
		"symlink": func(t *testing.T, root string) {
			writePortableFixture(t, root, map[string]string{"colors.toml": "x"}, nil)
			if err := os.Symlink("colors.toml", filepath.Join(root, "link")); err != nil {
				t.Fatal(err)
			}
		},
		"special-file": func(t *testing.T, root string) {
			writePortableFixture(t, root, map[string]string{"colors.toml": "x"}, nil)
			if err := syscall.Mkfifo(filepath.Join(root, "pipe"), 0o644); err != nil {
				t.Skip("fifo unsupported:", err)
			}
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			setup(t, root)
			_, err := portableFingerprint(root)
			reason, unportable := unportableReason(err)
			if !unportable || reason == "" || strings.Contains(reason, root) {
				t.Fatalf("not reported as unportable with a safe reason: %v", err)
			}
			if err := copyPortable(root, filepath.Join(t.TempDir(), "out")); err == nil {
				t.Fatal("unportable theme copied")
			}
		})
	}
}

// Capture writes exactly the canonical snapshot, independent of live modes
// and the process umask, so the profile checkout equals a fresh clone.
func TestCopyPortableWritesCanonicalSnapshot(t *testing.T) {
	source := t.TempDir()
	writePortableFixture(t, source, map[string]string{"colors.toml": "x", "bin/apply.sh": "#!/bin/sh\n", "empty.conf": ""}, map[string]os.FileMode{"colors.toml": 0o600, "bin/apply.sh": 0o700, "bin": 0o700})
	os.MkdirAll(filepath.Join(source, "unused"), 0o700)
	destination := filepath.Join(t.TempDir(), "theme")
	previous := syscall.Umask(0o077)
	err := copyPortable(source, destination)
	syscall.Umask(previous)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]os.FileMode{".": 0o755, "colors.toml": 0o644, "bin": 0o755, "bin/apply.sh": 0o755, "empty.conf": 0o644}
	got := map[string]os.FileMode{}
	filepath.WalkDir(destination, func(path string, entry os.DirEntry, err error) error {
		info, _ := entry.Info()
		rel, _ := filepath.Rel(destination, path)
		got[filepath.ToSlash(rel)] = info.Mode().Perm()
		return nil
	})
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("snapshot %v, want %v", got, want)
	}
	a, _ := portableFingerprint(source)
	b, _ := portableFingerprint(destination)
	if a != b {
		t.Fatal("copied snapshot fingerprint differs from source")
	}
}

// Preserve carries an existing artifact forward; one Git could never have
// stored (missing, or with no files) stays absent instead of failing Capture.
func TestPreservePortableLeavesUnstorableArtifactAlone(t *testing.T) {
	for name, setup := range map[string]func(root string){
		"missing": func(root string) {},
		"empty":   func(root string) { os.Mkdir(root, 0o755) },
	} {
		t.Run(name, func(t *testing.T) {
			source := filepath.Join(t.TempDir(), "aether")
			setup(source)
			destination := filepath.Join(t.TempDir(), "aether")
			if err := preservePortable(source, destination); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Lstat(destination); !os.IsNotExist(err) {
				t.Fatalf("preserve invented an artifact: %v", err)
			}
		})
	}
	source := t.TempDir()
	writePortableFixture(t, source, map[string]string{"colors.toml": "x"}, nil)
	if err := os.Symlink("colors.toml", filepath.Join(source, "link")); err != nil {
		t.Fatal(err)
	}
	if err := preservePortable(source, filepath.Join(t.TempDir(), "out")); err == nil {
		t.Fatal("preserve hid an unsupported artifact")
	}
}
