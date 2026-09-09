package config

import (
	"sort"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

// Association is an advisory relationship between an excluded package and
// captured configuration. It never changes either desired-state policy.
type Association struct {
	Package    string `json:"package"`
	ConfigPath string `json:"config_path"`
	Confidence string `json:"confidence"`
}

var curatedPackageConfig = map[string]string{
	"neovim": "nvim",
}

// RelatedConfig returns high-confidence package-to-config associations for
// saved .config state that remains included.
func RelatedConfig(excludedPackages []string, config profile.Configs) []Association {
	captured := map[string]bool{}
	for _, file := range config.Files {
		path := strings.TrimPrefix(file.Path, ".config/")
		if path == "" {
			continue
		}
		topLevel := strings.SplitN(path, "/", 2)[0]
		if !IsExcludedConfigPath(topLevel, config.Excluded) {
			captured[topLevel] = true
		}
	}

	seen := map[string]bool{}
	var associations []Association
	for _, pkg := range excludedPackages {
		name := pkg
		if _, value, ok := strings.Cut(pkg, ":"); ok {
			name = value
		}
		configPath := name
		if curated, ok := curatedPackageConfig[name]; ok {
			configPath = curated
		}
		key := pkg + "\x00" + configPath
		if captured[configPath] && !seen[key] {
			associations = append(associations, Association{Package: pkg, ConfigPath: configPath, Confidence: "high"})
			seen[key] = true
		}
	}
	sort.Slice(associations, func(i, j int) bool {
		if associations[i].Package == associations[j].Package {
			return associations[i].ConfigPath < associations[j].ConfigPath
		}
		return associations[i].Package < associations[j].Package
	})
	return associations
}
