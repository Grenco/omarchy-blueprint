package content

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHashRegularFileRejectsSymlinksAndNonRegularFiles(t *testing.T) {
	dir := t.TempDir()
	regular := filepath.Join(dir, "regular")
	if err := os.WriteFile(regular, []byte("blueprint\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := HashRegularFile(regular); err != nil || got != "aa03fd02116afc393d06499ee0eecab8fa3df8fb557388354530cffe29750607" {
		t.Fatalf("HashRegularFile() = %q, %v", got, err)
	}

	link := filepath.Join(dir, "link")
	if err := os.Symlink(regular, link); err != nil {
		t.Fatal(err)
	}
	if _, err := HashRegularFile(link); err == nil {
		t.Fatal("HashRegularFile accepted a symlink")
	}
	if _, err := HashRegularFile(dir); err == nil {
		t.Fatal("HashRegularFile accepted a directory")
	}
}
