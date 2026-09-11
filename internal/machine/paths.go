// Package machine owns portable machine-overlay policy.
package machine

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

var validName = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// ValidateName reports whether name is a safe, stable machine overlay ID.
func ValidateName(name string) error {
	if name == "" || name == "." || name == ".." || !validName.MatchString(name) {
		return fmt.Errorf("invalid machine name %q", name)
	}
	return nil
}

// ResolveEffectiveRoots resolves tracked Resources for machine and validates
// that their live roots cannot collide with each other or Blueprint state.
// Mapping IDs without a currently tracked Resource are returned as dormant.
func ResolveEffectiveRoots(home, profileDir, blueprintStateDir string, resources profile.Resources, machine *profile.Machine) (map[string]string, []string, error) {
	overrides := make(map[string]string)
	if machine != nil {
		for _, mapping := range machine.ResourcePaths {
			overrides[mapping.Resource] = mapping.Path
		}
	}
	paths := ResourcePaths{Home: home, Overrides: overrides}
	roots := make(map[string]string, len(resources.Items))
	tracked := make(map[string]bool, len(resources.Items))
	for _, item := range resources.Items {
		root, err := paths.Resolve(item)
		if err != nil {
			return nil, nil, fmt.Errorf("resolve resource %q: %w", item.ID, err)
		}
		if pathsOverlap(root, profileDir) {
			return nil, nil, fmt.Errorf("resource %q effective root %s overlaps active profile directory", item.ID, root)
		}
		if pathsOverlap(root, blueprintStateDir) {
			return nil, nil, fmt.Errorf("resource %q effective root %s overlaps Blueprint state directory", item.ID, root)
		}
		roots[item.ID] = root
		tracked[item.ID] = true
	}
	ids := make([]string, 0, len(roots))
	for id := range roots {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for index, id := range ids {
		for _, other := range ids[index+1:] {
			if pathsOverlap(roots[id], roots[other]) {
				return nil, nil, fmt.Errorf("resource %q effective root %s overlaps resource %q effective root %s", id, roots[id], other, roots[other])
			}
		}
	}
	var dormant []string
	if machine != nil {
		for _, mapping := range machine.ResourcePaths {
			if !tracked[mapping.Resource] {
				dormant = append(dormant, mapping.Resource)
			}
		}
	}
	sort.Strings(dormant)
	return roots, dormant, nil
}

// NormalizeMappingPath converts a valid mapping to its canonical stored form.
func NormalizeMappingPath(home, raw string) (string, error) {
	home = filepath.Clean(home)
	path, err := mappingPath(home, raw)
	if err != nil {
		return "", err
	}
	if isWithin(home, path) {
		relative, err := filepath.Rel(home, path)
		if err != nil {
			return "", err
		}
		return "~/" + filepath.ToSlash(relative), nil
	}
	return path, nil
}

// ExpandMappingPath expands a valid stored mapping to an absolute path.
func ExpandMappingPath(home, stored string) (string, error) {
	return mappingPath(filepath.Clean(home), stored)
}

// ResourcePaths resolves portable Resource paths with optional machine overrides.
type ResourcePaths struct {
	Home      string
	Overrides map[string]string
}

// Resolve returns item's effective live root without changing its portable path.
func (r ResourcePaths) Resolve(item profile.Resource) (string, error) {
	if override, ok := r.Overrides[item.ID]; ok {
		return ExpandMappingPath(r.Home, override)
	}
	return expandPortablePath(r.Home, item.Path)
}

func mappingPath(home, raw string) (string, error) {
	if strings.HasPrefix(raw, "~/") {
		path := filepath.Clean(filepath.Join(home, filepath.FromSlash(strings.TrimPrefix(raw, "~/"))))
		if !isWithin(home, path) {
			return "", fmt.Errorf("mapping path escapes home: %s", raw)
		}
		return path, nil
	}
	if !filepath.IsAbs(raw) {
		return "", fmt.Errorf("mapping path must use ~/... or an absolute path: %s", raw)
	}
	path := filepath.Clean(raw)
	if path == home || path == string(filepath.Separator) {
		return "", fmt.Errorf("mapping path must not be root: %s", raw)
	}
	return path, nil
}

func expandPortablePath(home, logical string) (string, error) {
	if logical == "~" || !strings.HasPrefix(logical, "~/") {
		return "", fmt.Errorf("resource path must use ~/...: %s", logical)
	}
	path := filepath.Clean(filepath.Join(filepath.Clean(home), filepath.FromSlash(strings.TrimPrefix(logical, "~/"))))
	if !isWithin(home, path) {
		return "", fmt.Errorf("resource path escapes home: %s", logical)
	}
	return path, nil
}

func isWithin(root, path string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func pathsOverlap(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	return contains(a, b) || contains(b, a)
}

func contains(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
