package profilegit

import (
	"bytes"
	"context"
	"github.com/Grenco/omarchy-blueprint/internal/command"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadOnlyStatusDoesNotRefreshIndex(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init", "-b", "main")
	git(t, root, "config", "user.email", "test@example.invalid")
	git(t, root, "config", "user.name", "Test")
	file := filepath.Join(root, "profile.toml")
	if err := os.WriteFile(file, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "fixture")
	index := filepath.Join(root, ".git", "index")
	before, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Minute)
	if err := os.Chtimes(file, future, future); err != nil {
		t.Fatal(err)
	}
	s, err := New(command.SystemRunner{}, root)
	if err != nil {
		t.Fatal(err)
	}
	status, err := s.ReadOnlyStatus(context.Background())
	if err != nil || len(status.Changes) != 0 {
		t.Fatal(status, err)
	}
	after, err := os.ReadFile(index)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("status rewrote index stat cache", err)
	}
}
