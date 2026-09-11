package profilegit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/command"
	"github.com/Grenco/omarchy-blueprint/internal/inspection"
)

func TestDiffManagedFiles(t *testing.T) {
	root := newDiffRepository(t)
	mustWrite(t, filepath.Join(root, "profile.toml"), "changed\n")
	mustWrite(t, filepath.Join(root, "packages", "new.txt"), "new\n")
	mustWrite(t, filepath.Join(root, "themes", "binary"), "a\x00b")
	mustWrite(t, filepath.Join(root, "config", "large"), strings.Repeat("x", inspection.MaxPreviewBytes+1))
	mustWrite(t, filepath.Join(root, "shell", "secret"), "api_token = tokenvaluewithmorethan16chars\n")
	mustWrite(t, filepath.Join(root, "README.md"), "unmanaged\n")
	if err := os.Remove(filepath.Join(root, "hooks", "deleted")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(root, "defaults", "mode"), 0o755); err != nil {
		t.Fatal(err)
	}
	service, err := New(command.SystemRunner{}, root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.Diff(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	files := diffFiles(got.Files)
	if len(files) != 7 || files["README.md"].Path != "" {
		t.Fatalf("files = %#v", got.Files)
	}
	if files["profile.toml"].Document.Kind != inspection.DiffText || !files["packages/new.txt"].New || files["packages/new.txt"].Document.Kind != inspection.DiffText {
		t.Fatalf("text files = %#v", files)
	}
	if !files["hooks/deleted"].Deleted || files["themes/binary"].Document.Kind != inspection.DiffBinary || files["config/large"].Document.Kind != inspection.DiffTooLarge || files["shell/secret"].Document.Kind != inspection.DiffSensitive || files["defaults/mode"].Document.Kind != inspection.DiffMetadata || len(files["defaults/mode"].Document.Metadata) != 2 {
		t.Fatalf("classified files = %#v", files)
	}
	if _, err := service.Diff(context.Background(), "README.md"); err == nil {
		t.Fatal("unmanaged onlyPath accepted")
	}
	only, err := service.Diff(context.Background(), "profile.toml")
	if err != nil || len(only.Files) != 1 || only.Files[0].Path != "profile.toml" {
		t.Fatalf("only path = %#v, %v", only, err)
	}
}

func TestDiffUnbornHeadTreatsManagedFilesAsNew(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init", "-b", "main")
	mustWrite(t, filepath.Join(root, "profile.toml"), "new\n")
	service, err := New(command.SystemRunner{}, root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.Diff(context.Background(), "")
	if err != nil || len(got.Files) != 1 || !got.Files[0].New || got.Files[0].Document.Kind != inspection.DiffText {
		t.Fatalf("unborn diff = %#v, %v", got, err)
	}
}

func newDiffRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	git(t, root, "init", "-b", "main")
	git(t, root, "config", "user.name", "Blueprint Test")
	git(t, root, "config", "user.email", "blueprint@example.test")
	for path := range map[string]string{"profile.toml": "initial\n", "hooks/deleted": "gone\n", "defaults/mode": "same\n"} {
		mustWrite(t, filepath.Join(root, path), "initial\n")
	}
	mustWrite(t, filepath.Join(root, "defaults", "mode"), "same\n")
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "initial")
	return root
}

func diffFiles(files []DiffFile) map[string]DiffFile {
	result := make(map[string]DiffFile, len(files))
	for _, file := range files {
		result[file.Path] = file
	}
	return result
}
