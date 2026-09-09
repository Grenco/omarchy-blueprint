package content

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHashRegularTreeRejectsSymlinksAndSpecialFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := HashRegularTree(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("file", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := HashRegularTree(root); err == nil {
		t.Fatal("symlink tree unexpectedly accepted")
	}
}
