package machine

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// BindingStore persists the local machine selection for each portable profile.
type BindingStore struct {
	StateHome string
}

type binding struct {
	Profile string `toml:"profile"`
	Machine string `toml:"machine"`
}

// Load returns the locally selected machine for profileDir, if one exists.
func (s BindingStore) Load(profileDir string) (string, error) {
	profile, path, err := s.bindingPath(profileDir)
	if err != nil {
		return "", err
	}
	contents, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var stored binding
	if err := toml.Unmarshal(contents, &stored); err != nil {
		return "", fmt.Errorf("parse machine binding: %w", err)
	}
	if stored.Profile != profile {
		return "", fmt.Errorf("machine binding profile does not match requested profile")
	}
	if err := ValidateName(stored.Machine); err != nil {
		return "", fmt.Errorf("invalid machine binding: %w", err)
	}
	return stored.Machine, nil
}

// Save atomically records machine as the local selection for profileDir.
func (s BindingStore) Save(profileDir, machine string) error {
	if err := ValidateName(machine); err != nil {
		return err
	}
	profile, path, err := s.bindingPath(profileDir)
	if err != nil {
		return err
	}
	contents, err := toml.Marshal(binding{Profile: profile, Machine: machine})
	if err != nil {
		return err
	}
	return atomicBindingWrite(path, contents)
}

// Clear removes the local machine selection for profileDir.
func (s BindingStore) Clear(profileDir string) error {
	_, path, err := s.bindingPath(profileDir)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := os.Stat(filepath.Dir(path)); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}

func (s BindingStore) bindingPath(profileDir string) (string, string, error) {
	profile, err := canonicalProfileRoot(profileDir)
	if err != nil {
		return "", "", err
	}
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(profile)))
	return profile, filepath.Join(s.StateHome, "omarchy-blueprint", "profiles", key, "machine.toml"), nil
}

func canonicalProfileRoot(profileDir string) (string, error) {
	profile, err := filepath.Abs(profileDir)
	if err != nil {
		return "", err
	}
	profile = filepath.Clean(profile)
	if resolved, err := filepath.EvalSymlinks(profile); err == nil {
		return filepath.Clean(resolved), nil
	}
	return profile, nil
}

func atomicBindingWrite(path string, contents []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".omarchy-blueprint-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(contents); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	return syncDir(dir)
}

func syncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
