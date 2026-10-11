package profilegit

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/command"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

// Archive extraction must not write through traversal, symlinks, duplicate entries, or special devices.
func TestArchiveRejectsHostileEntries(t *testing.T) {
	for _, tc := range []struct {
		name    string
		headers []*tar.Header
	}{
		{"traversal", []*tar.Header{{Name: "../outside", Typeflag: tar.TypeReg}}},
		{"absolute", []*tar.Header{{Name: "/outside", Typeflag: tar.TypeReg}}},
		{"device", []*tar.Header{{Name: "device", Typeflag: tar.TypeChar}}},
		{"hardlink", []*tar.Header{{Name: "hard", Typeflag: tar.TypeLink, Linkname: "outside"}}},
		{"external symlink", []*tar.Header{{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "../../outside"}}},
		{"absolute symlink", []*tar.Header{{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "/outside"}}},
		{"symlink write", []*tar.Header{{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "dir"}, {Name: "link/file", Typeflag: tar.TypeReg}}},
		{"symlink chain escape", []*tar.Header{{Name: "x/a", Typeflag: tar.TypeSymlink, Linkname: "."}, {Name: "x/b", Typeflag: tar.TypeSymlink, Linkname: "a/../../outside"}}},
		{"duplicate", []*tar.Header{{Name: "file", Typeflag: tar.TypeReg}, {Name: "file", Typeflag: tar.TypeReg}}},
		{"git metadata", []*tar.Header{{Name: ".git/config", Typeflag: tar.TypeReg}}},
		{"large", []*tar.Header{{Name: "file", Typeflag: tar.TypeReg, Size: maxArchiveBytes + 1}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			w := tar.NewWriter(&buf)
			for _, h := range tc.headers {
				h.Mode = 0644
				if err := w.WriteHeader(h); err != nil {
					t.Fatal(err)
				}
			}
			// Large/truncated payload intentionally has no body.
			_ = w.Close()
			if err := extractProfileArchive(context.Background(), bytes.NewReader(buf.Bytes()), t.TempDir()); err == nil {
				t.Fatal("unsafe archive accepted")
			}
		})
	}
}

type archiveSizeRunner struct{ size int64 }

func (r *archiveSizeRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	out, err := (command.SystemRunner{}).Run(ctx, name, args...)
	if err == nil {
		for _, arg := range args {
			if strings.HasPrefix(arg, "--output=") {
				info, statErr := os.Stat(strings.TrimPrefix(arg, "--output="))
				if statErr != nil {
					return "", statErr
				}
				r.size = info.Size()
			}
		}
	}
	return out, err
}

type deepTreeRunner struct{}

func (deepTreeRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	return "100644 blob " + strings.Repeat("a", 40) + "       0\t" + strings.Repeat("dir/", 20000) + "file\x00", nil
}
func TestArchiveTreeBudgetIncludesDirectoriesAndLongNames(t *testing.T) {
	if _, err := (Service{Runner: deepTreeRunner{}, Root: t.TempDir()}).archiveTree(context.Background(), strings.Repeat("a", 40)); err == nil {
		t.Fatal("deep tree metadata bypassed archive preflight bound")
	}
}

func TestMaterializeExportSubstCannotExpandArchiveBeforeLimitCheck(t *testing.T) {
	for _, source := range []string{"tree", "info", "configured"} {
		t.Run(source, func(t *testing.T) {
			root := t.TempDir()
			git(t, root, "init", "-b", "main")
			gitIdentity(t, root)
			content := strings.Repeat("$Format:%<(10000)%s$\n", 100)
			gitCommit(t, root, "config/expanded", content)
			attributes := "config/expanded export-subst\n"
			switch source {
			case "tree":
				gitCommit(t, root, ".gitattributes", attributes)
			case "info":
				mustWrite(t, filepath.Join(root, ".git/info/attributes"), attributes)
			case "configured":
				attrs := filepath.Join(t.TempDir(), "attributes")
				mustWrite(t, attrs, attributes)
				git(t, root, "config", "core.attributesFile", attrs)
			}
			sha := gitOutput(t, root, "rev-parse", "HEAD")
			runner := &archiveSizeRunner{}
			s, err := New(runner, root)
			if err != nil {
				t.Fatal(err)
			}
			dest := t.TempDir()
			if err := s.MaterializeCommit(context.Background(), sha, dest); err != nil {
				t.Error("literal commit content not materialized", err)
			}
			if runner.size > 16384 {
				t.Errorf("small literal tree expanded to %d bytes before size enforcement", runner.size)
			}
			if bytes, err := os.ReadFile(filepath.Join(dest, "config/expanded")); err != nil || string(bytes) != content {
				t.Fatal("commit literal content changed", err)
			}
		})
	}
}

func TestMaterializeRejectsExportAttributesChangingExactTree(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init", "-b", "main")
	gitIdentity(t, root)
	writeProfile(t, root)
	gitCommit(t, root, "config/ignored", "desired")
	gitCommit(t, root, ".gitattributes", "config/ignored export-ignore\n")
	sha := gitOutput(t, root, "rev-parse", "HEAD")
	dest := t.TempDir()
	if err := profileGitService(t, root).MaterializeCommit(context.Background(), sha, dest); err == nil {
		t.Fatal("export-ignore silently altered snapshot")
	}
	if entries, err := os.ReadDir(dest); err != nil || len(entries) != 0 {
		t.Fatal("failed snapshot retained", err)
	}
}

func TestArchiveRegularModesInternalSymlinkAndCancellation(t *testing.T) {
	var buf bytes.Buffer
	w := tar.NewWriter(&buf)
	if err := w.WriteHeader(&tar.Header{Name: "pax_global_header", Typeflag: tar.TypeXGlobalHeader, PAXRecords: map[string]string{"comment": strings.Repeat("a", 40)}}); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteHeader(&tar.Header{Name: "dir/script", Mode: 0751, Typeflag: tar.TypeReg, Size: 5}); err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("hello"))
	w.WriteHeader(&tar.Header{Name: "dir/link", Mode: 0777, Typeflag: tar.TypeSymlink, Linkname: "script"})
	w.Close()
	dest := t.TempDir()
	if err := extractProfileArchive(context.Background(), bytes.NewReader(buf.Bytes()), dest); err != nil {
		t.Fatal(err)
	}
	if got := mustRead(t, filepath.Join(dest, "dir/script")); got != "hello" {
		t.Fatal(got)
	}
	info, err := os.Stat(filepath.Join(dest, "dir/script"))
	if err != nil || info.Mode().Perm() != 0751 {
		t.Fatal(info, err)
	}
	if link, err := os.Readlink(filepath.Join(dest, "dir/link")); err != nil || link != "script" {
		t.Fatal(link, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := extractProfileArchive(ctx, bytes.NewReader(buf.Bytes()), t.TempDir()); !errors.Is(err, context.Canceled) {
		t.Fatal("cancel ignored", err)
	}
	truncated := buf.Bytes()[:515]
	if err := extractProfileArchive(context.Background(), bytes.NewReader(truncated), t.TempDir()); err == nil {
		t.Fatal("truncated archive accepted")
	}
}

func TestMaterializeExactCommitPreservesCheckoutIndexAndLoaderSemantics(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init", "-b", "main")
	gitIdentity(t, root)
	writeProfile(t, root)
	// Schema 1 remains supported: normal Load must upgrade in memory only.
	manifest := filepath.Join(root, "profile.toml")
	raw := mustRead(t, manifest)
	schemaLine := "schema = " + strings.TrimSpace(strings.Split(strings.Split(raw, "schema = ")[1], "\n")[0])
	legacy := strings.Replace(raw, schemaLine, "schema = 1", 1)
	mustWrite(t, manifest, legacy)
	mustWrite(t, filepath.Join(root, "config", "script"), "hello")
	if err := os.Chmod(filepath.Join(root, "config", "script"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("script", filepath.Join(root, "config", "link")); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "profile")
	git(t, root, "config", "tar.umask", "0077") // Repository tar preferences must not change snapshot semantics.
	sha := gitOutput(t, root, "rev-parse", "HEAD")
	s := profileGitService(t, root)
	// Dirty/staged/untracked contents must survive read-only materialization untouched.
	mustWrite(t, manifest, raw)
	git(t, root, "add", "profile.toml")
	mustWrite(t, filepath.Join(root, "notes.txt"), "untracked")
	status := gitOutput(t, root, "status", "--porcelain=v2", "-z")
	index := gitOutput(t, root, "write-tree")
	dest := t.TempDir()
	if err := s.MaterializeCommit(context.Background(), sha, dest); err != nil {
		t.Fatal(err)
	}
	loaded, err := profile.Load(dest)
	if err != nil || loaded.Manifest.Schema != profile.Schema {
		t.Fatal("legacy load failed", err)
	}
	if mustRead(t, filepath.Join(dest, "profile.toml")) != legacy {
		t.Fatal("loader wrote migration")
	}
	if gitOutput(t, root, "rev-parse", "HEAD") != sha || gitOutput(t, root, "write-tree") != index || gitOutput(t, root, "status", "--porcelain=v2", "-z") != status || mustRead(t, manifest) != raw {
		t.Fatal("materialization mutated checkout/index")
	}
	if link, err := os.Readlink(filepath.Join(dest, "config", "link")); err != nil || link != "script" {
		t.Fatal(link, err)
	}
	if _, err := os.Stat(filepath.Join(dest, ".git")); !os.IsNotExist(err) {
		t.Fatal("materialized Git metadata", err)
	}
	if err := s.MaterializeCommit(context.Background(), sha, dest); err == nil {
		t.Fatal("nonempty destination accepted")
	}
	if err := s.MaterializeCommit(context.Background(), "HEAD", t.TempDir()); err == nil {
		t.Fatal("unpinned reference accepted")
	}
}

func TestMaterializeRejectsSymlinkDestinationAndCleansFailedExtraction(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init", "-b", "main")
	gitIdentity(t, root)
	writeProfile(t, root)
	if err := os.Symlink("/outside", filepath.Join(root, "external")); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "unsafe tree")
	s := profileGitService(t, root)
	sha := gitOutput(t, root, "rev-parse", "HEAD")
	dest := t.TempDir()
	if err := s.MaterializeCommit(context.Background(), sha, dest); err == nil {
		t.Fatal("unsafe commit accepted")
	}
	if entries, err := os.ReadDir(dest); err != nil || len(entries) != 0 {
		t.Fatal("partial extraction retained", err)
	}
	link := filepath.Join(t.TempDir(), "dest")
	if err := os.Symlink(dest, link); err != nil {
		t.Fatal(err)
	}
	if err := s.MaterializeCommit(context.Background(), sha, link); err == nil {
		t.Fatal("symlink destination accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.MaterializeCommit(ctx, sha, dest); !errors.Is(err, context.Canceled) {
		t.Fatal("cancel lost", err)
	}
}
