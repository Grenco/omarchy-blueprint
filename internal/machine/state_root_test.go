package machine

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Changing the directory key or omitting symlink canonicalization would orphan existing bindings.
func TestStateRootPreservesBindingLocation(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	link := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(state, "omarchy-blueprint", "profiles", fmt.Sprintf("%x", sha256.Sum256([]byte(root))))
	for _, input := range []string{root, link, filepath.Join(root, ".")} {
		got, err := ProfileStateDir(state, input)
		if err != nil || got != want {
			t.Fatalf("state directory = %q, %v; want %q", got, err, want)
		}
		_, binding, err := (BindingStore{StateHome: state}).bindingPath(input)
		if err != nil || binding != filepath.Join(want, "machine.toml") {
			t.Fatalf("binding location = %q, %v", binding, err)
		}
	}
	other, err := ProfileStateDir(t.TempDir(), root)
	if err != nil || other == want {
		t.Fatal("state home override ignored", err)
	}
	if _, err := os.Stat(want); !os.IsNotExist(err) {
		t.Fatal("read helper created state", err)
	}
}
