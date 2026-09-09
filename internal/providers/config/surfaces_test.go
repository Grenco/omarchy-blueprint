package config

import (
	"path/filepath"
	"testing"
)

func TestDefaultHomeConfigSpecsAreUniqueAndSafe(t *testing.T) {
	seen := map[string]bool{}
	for _, spec := range DefaultHomeConfigSpecs() {
		if seen[spec.Path] {
			t.Fatalf("duplicate %s", spec.Path)
		}
		seen[spec.Path] = true
	}
	for _, path := range []string{".bashrc", ".zshrc", ".tmux.conf", ".gitconfig", ".cargo/config.toml"} {
		if !seen[path] {
			t.Fatalf("missing %s", path)
		}
	}
	for _, path := range []string{".netrc", ".ssh/config", ".bash_history"} {
		if seen[path] {
			t.Fatalf("unsafe %s", path)
		}
	}
}
func TestHomeConfigPathsRemainWithinHome(t *testing.T) {
	home := t.TempDir()
	absolute := filepath.Join(home, ".config", "ghostty", "config")
	logical, err := LogicalHomeConfigPath(home, absolute)
	if err != nil || logical != ".config/ghostty/config" {
		t.Fatalf("logical=%q err=%v", logical, err)
	}
	if _, err := LogicalHomeConfigPath(home, t.TempDir()); err == nil {
		t.Fatal("outside home accepted")
	}
}
