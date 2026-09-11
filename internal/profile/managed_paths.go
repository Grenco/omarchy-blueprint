package profile

import (
	pathpkg "path"
	"path/filepath"
	"strings"
)

// ManagedTopLevelPaths are the profile paths Blueprint may stage and commit.
var ManagedTopLevelPaths = []string{
	"profile.toml",
	"packages",
	"themes",
	"plugins",
	"config",
	"defaults",
	"shell",
	"hooks",
	"resources",
	"machines",
}

// IsManagedRepositoryPath reports whether raw is a canonical, Blueprint-owned
// repository path. Repository paths are always slash-separated.
func IsManagedRepositoryPath(raw string) bool {
	if raw == "" || filepath.IsAbs(raw) || strings.Contains(raw, "\\") {
		return false
	}
	clean := pathpkg.Clean(raw)
	if clean != raw || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return false
	}
	for _, root := range ManagedTopLevelPaths {
		if clean == root || strings.HasPrefix(clean, root+"/") {
			return true
		}
	}
	return false
}
