package hooks

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

var hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ValidatePath accepts only an event file or an immediate child of event.d.
func ValidatePath(path string) error {
	if path == "" || strings.Contains(path, `\`) || filepath.IsAbs(path) {
		return fmt.Errorf("invalid hook path %q", path)
	}
	parts := strings.Split(path, "/")
	switch len(parts) {
	case 1:
		return validateComponent(parts[0], "event")
	case 2:
		dir, name := parts[0], parts[1]
		if err := validateComponent(dir, "event directory"); err != nil {
			return err
		}
		if !strings.HasSuffix(dir, ".d") || strings.TrimSuffix(dir, ".d") == "" {
			return fmt.Errorf("invalid hook event directory %q", dir)
		}
		if err := validateComponent(name, "hook filename"); err != nil {
			return err
		}
		if strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".sample") {
			return fmt.Errorf("hook is not runtime-relevant: %q", path)
		}
		return nil
	default:
		return fmt.Errorf("hook path must be flat or one .d child: %q", path)
	}
}

func validateComponent(component, kind string) error {
	if component == "" || component == "." || component == ".." || strings.Contains(component, "/") {
		return fmt.Errorf("invalid hook %s %q", kind, component)
	}
	return nil
}

func ParseMode(raw string) (uint32, error) {
	if len(raw) != 4 {
		return 0, fmt.Errorf("hook mode must contain four octal digits")
	}
	for _, r := range raw {
		if r < '0' || r > '7' {
			return 0, fmt.Errorf("invalid hook mode %q", raw)
		}
	}
	value, err := strconv.ParseUint(raw, 8, 32)
	if err != nil || value > 0o777 {
		return 0, fmt.Errorf("invalid hook mode %q", raw)
	}
	return uint32(value), nil
}

func FormatMode(mode os.FileMode) (string, error) {
	if mode&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return "", fmt.Errorf("hook mode contains special bits: %v", mode)
	}
	return fmt.Sprintf("%04o", mode.Perm()), nil
}

// ValidateMetadata keeps profile metadata constrained to safe, portable paths.
func ValidateMetadata(items []profile.Hook) error {
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		if err := ValidatePath(item.Path); err != nil {
			return err
		}
		if !hashPattern.MatchString(item.Hash) {
			return fmt.Errorf("invalid hook hash for %q", item.Path)
		}
		if _, err := ParseMode(item.Mode); err != nil {
			return fmt.Errorf("invalid hook mode for %q: %w", item.Path, err)
		}
		if seen[item.Path] {
			return fmt.Errorf("duplicate hook path %q", item.Path)
		}
		seen[item.Path] = true
	}
	return nil
}
