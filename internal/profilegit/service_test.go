package profilegit

import (
	"os"
	"path/filepath"
	"testing"
)

func TestServiceRootCanonicalization(t *testing.T) {
	realRoot := filepath.Join(t.TempDir(), "profile")
	if err := os.Mkdir(realRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "profile-link")
	if err := os.Symlink(realRoot, link); err != nil {
		t.Fatal(err)
	}
	service, err := New(nil, link)
	if err != nil {
		t.Fatal(err)
	}
	if service.RootPath() != realRoot {
		t.Fatalf("root = %q, want %q", service.RootPath(), realRoot)
	}
}

func TestServiceRejectsEmptyRoot(t *testing.T) {
	if _, err := New(nil, ""); err == nil {
		t.Fatal("New accepted empty root")
	}
}
