package resources

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

var validResourceID = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

func ExpandHomePath(home, logical string) (string, error) {
	if logical == "~" || !strings.HasPrefix(logical, "~/") {
		return "", fmt.Errorf("resource path must use ~/...: %s", logical)
	}
	home = filepath.Clean(home)
	path := filepath.Clean(filepath.Join(home, filepath.FromSlash(strings.TrimPrefix(logical, "~/"))))
	if !within(home, path) || path == home {
		return "", fmt.Errorf("resource path escapes home: %s", logical)
	}
	return path, nil
}

func LogicalHomePath(home, absolute string) (string, error) {
	home, absolute = filepath.Clean(home), filepath.Clean(absolute)
	if !within(home, absolute) || absolute == home {
		return "", fmt.Errorf("resource path is outside home: %s", absolute)
	}
	relative, err := filepath.Rel(home, absolute)
	if err != nil {
		return "", err
	}
	return "~/" + filepath.ToSlash(relative), nil
}

func ValidateResourceID(id string) error {
	if id == "" || id == "." || id == ".." || !validResourceID.MatchString(id) {
		return fmt.Errorf("invalid resource id %q", id)
	}
	return nil
}

func ResourceIDForPath(path string) string { return filepath.Base(filepath.Clean(path)) }

func PathsOverlap(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	return within(a, b) || within(b, a)
}

func SafeRelativeResourcePath(path string) bool {
	if path == "" || filepath.IsAbs(path) {
		return false
	}
	clean := filepath.Clean(path)
	return clean == "." || (clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator)))
}

func within(root, path string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
