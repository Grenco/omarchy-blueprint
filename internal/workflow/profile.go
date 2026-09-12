package workflow

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Grenco/omarchy-blueprint/internal/machine"
	"github.com/Grenco/omarchy-blueprint/internal/omarchy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

// CreateProfile creates a new, empty Blueprint profile using the same
// environment metadata captured by the CLI init command.
func CreateProfile(ctx context.Context, deps Dependencies, dir, name string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	abs, err = machine.CanonicalProfileRoot(abs)
	if err != nil {
		return "", err
	}
	if err := ensureEmptyProfileRoot(abs); err != nil {
		return "", err
	}
	if name == "" {
		name = filepath.Base(abs)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	info, err := omarchy.Detect(ctx, deps.Runner)
	if err != nil {
		return "", err
	}
	data := profile.New(name, deps.Now())
	data.Manifest.Omarchy.CapturedVersion = info.Version
	data.Manifest.Omarchy.Channel = info.Channel
	if err := profile.Save(abs, data); err != nil {
		return "", fmt.Errorf("create profile: %w", err)
	}
	return abs, nil
}

// IsMissingProfile reports only a safely empty profile root. Existing partial
// directories are not onboarding candidates and must be surfaced as errors.
func IsMissingProfile(dir string, err error) bool {
	if !errors.Is(err, fs.ErrNotExist) {
		return false
	}
	entries, readErr := os.ReadDir(dir)
	return errors.Is(readErr, fs.ErrNotExist) || readErr == nil && len(entries) == 0
}

func ensureEmptyProfileRoot(dir string) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}
	if _, err := os.Stat(filepath.Join(dir, "profile.toml")); err == nil {
		return fmt.Errorf("profile already exists at %s", dir)
	}
	return fmt.Errorf("profile directory %s is not empty", dir)
}
