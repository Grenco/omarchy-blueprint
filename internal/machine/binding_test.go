package machine

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestBindingSaveLoadPathAndPermissions(t *testing.T) {
	stateHome := t.TempDir()
	profile := filepath.Join(t.TempDir(), "profile")
	if err := os.Mkdir(profile, 0o755); err != nil {
		t.Fatal(err)
	}
	store := BindingStore{StateHome: stateHome}
	if err := store.Save(profile, "framework"); err != nil {
		t.Fatal(err)
	}

	canonical, err := canonicalProfileRoot(profile)
	if err != nil {
		t.Fatal(err)
	}
	key := sha256.Sum256([]byte(canonical))
	path := filepath.Join(stateHome, "omarchy-blueprint", "profiles", fmtHex(key), "machine.toml")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var stored binding
	if err := toml.Unmarshal(contents, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Profile != canonical || stored.Machine != "framework" {
		t.Errorf("binding = %+v, want profile %q and machine framework", stored, canonical)
	}
	info, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Errorf("parent mode = %o, want 700", got)
	}
	info, err = os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("file mode = %o, want 600", got)
	}
	got, err := store.Load(profile)
	if err != nil || got != "framework" {
		t.Errorf("Load() = %q, %v; want framework, nil", got, err)
	}
}

func TestBindingCanonicalProfileRoot(t *testing.T) {
	stateHome := t.TempDir()
	realProfile := filepath.Join(t.TempDir(), "profile")
	if err := os.Mkdir(realProfile, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "profile-link")
	if err := os.Symlink(realProfile, link); err != nil {
		t.Fatal(err)
	}
	store := BindingStore{StateHome: stateHome}
	if err := store.Save(link, "framework"); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load(realProfile)
	if err != nil || got != "framework" {
		t.Errorf("Load(real profile) = %q, %v; want framework, nil", got, err)
	}
}

func TestBindingClearAndInvalidBindings(t *testing.T) {
	stateHome := t.TempDir()
	profile := filepath.Join(t.TempDir(), "profile")
	otherProfile := filepath.Join(t.TempDir(), "other-profile")
	for _, dir := range []string{profile, otherProfile} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	store := BindingStore{StateHome: stateHome}
	if got, err := store.Load(profile); err != nil || got != "" {
		t.Errorf("Load(missing) = %q, %v; want empty, nil", got, err)
	}
	if err := store.Save(profile, "framework"); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(otherProfile, "desktop"); err != nil {
		t.Fatal(err)
	}
	if err := store.Clear(profile); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Load(profile); err != nil || got != "" {
		t.Errorf("Load(cleared) = %q, %v; want empty, nil", got, err)
	}
	if got, err := store.Load(otherProfile); err != nil || got != "desktop" {
		t.Errorf("Load(other profile) = %q, %v; want desktop, nil", got, err)
	}
	if err := store.Clear(profile); err != nil {
		t.Errorf("Clear(missing) = %v, want nil", err)
	}

	_, path, err := store.bindingPath(profile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("profile = \"/different\"\nmachine = \"framework\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(profile); err == nil {
		t.Error("Load accepted mismatched profile binding")
	}
	canonical, err := canonicalProfileRoot(profile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("profile = \""+canonical+"\"\nmachine = \"bad name\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(profile); err == nil {
		t.Error("Load accepted invalid machine name")
	}
	if err := os.WriteFile(path, []byte("profile = [\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(profile); err == nil {
		t.Error("Load accepted malformed binding")
	}
	if err := store.Save(profile, "bad name"); err == nil {
		t.Error("Save accepted invalid machine name")
	}
}

func fmtHex(sum [sha256.Size]byte) string {
	return fmt.Sprintf("%x", sum)
}
