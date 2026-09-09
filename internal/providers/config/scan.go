package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/content"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

type Classification string

const (
	ConfigUnchangedBaseline Classification = "unchanged-baseline"
	ConfigModifiedBaseline  Classification = "modified-baseline"
	ConfigDeletedBaseline   Classification = "deleted-baseline"
	ConfigAdded             Classification = "added"
	ConfigDelegated         Classification = "delegated"
	ConfigExcluded          Classification = "excluded"
	ConfigVolatile          Classification = "volatile"
	ConfigSensitive         Classification = "sensitive"
	ConfigUnmanagedSymlink  Classification = "unmanaged-symlink"
	ConfigUnsupported       Classification = "unsupported"
	ConfigOversized         Classification = "oversized"
)

type Candidate struct {
	Path           string         `json:"path"`
	Classification Classification `json:"classification"`
	UserHash       string         `json:"hash,omitempty"`
	UserMode       string         `json:"mode,omitempty"`
	BaselineHash   string         `json:"baseline_hash,omitempty"`
	BaselineMode   string         `json:"baseline_mode,omitempty"`
	Reason         string         `json:"reason,omitempty"`
}
type ScanSummary struct {
	Candidates []Candidate `json:"candidates"`
}
type treeEntry struct {
	abs  string
	info os.FileInfo
}

// Scan unions the recursive .config roots with exact home paths. WalkDir only
// receives roots we control and always prunes symlinks, excluded, volatile, and
// delegated directories before their children are enumerated.
func (p Provider) Scan(saved profile.Configs) (ScanSummary, error) {
	entries := map[string]map[bool]treeEntry{}
	for side, root := range map[bool]string{true: p.UserRoot, false: p.BaselineRoot} {
		if root == "" {
			continue
		}
		if err := p.walkRoot(root, p.configRootPrefix(), side, saved.Excluded, entries); err != nil {
			return ScanSummary{}, err
		}
	}
	home := p.HomeDir
	if p.hasHomeNamespace() {
		for _, spec := range DefaultHomeConfigSpecs() {
			logical := spec.Path
			if err := p.addExact(entries, true, filepath.Join(home, filepath.FromSlash(logical)), logical); err != nil {
				return ScanSummary{}, err
			}
			base, ok, err := p.ResolveBaselineFor(logical)
			if err != nil {
				return ScanSummary{}, err
			}
			if ok {
				if err := p.addExact(entries, false, base, logical); err != nil {
					return ScanSummary{}, err
				}
			}
		}
	}
	paths := make([]string, 0, len(entries))
	for path := range entries {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	result := ScanSummary{Candidates: make([]Candidate, 0, len(paths))}
	for _, path := range paths {
		c, err := p.classify(path, entries[path], saved.Excluded)
		if err != nil {
			return ScanSummary{}, err
		}
		result.Candidates = append(result.Candidates, c)
	}
	return result, nil
}

func (p Provider) walkRoot(root, prefix string, user bool, excluded []string, entries map[string]map[bool]treeEntry) error {
	if _, err := os.Lstat(root); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		logical := filepath.ToSlash(rel)
		if prefix != "" {
			logical = prefix + "/" + logical
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		decision := ClassifyConfigPolicy(logical, info, excluded)
		delegated := p.delegated(path)
		if d.IsDir() && (decision.Reason != PolicyAllowed || delegated || info.Mode()&os.ModeSymlink != 0) {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		if entries[logical] == nil {
			entries[logical] = map[bool]treeEntry{}
		}
		entries[logical][user] = treeEntry{abs: path, info: info}
		return nil
	})
}
func (p Provider) addExact(entries map[string]map[bool]treeEntry, user bool, abs, logical string) error {
	info, err := os.Lstat(abs)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if entries[logical] == nil {
		entries[logical] = map[bool]treeEntry{}
	}
	entries[logical][user] = treeEntry{abs: abs, info: info}
	return nil
}
func (p Provider) classify(path string, entries map[bool]treeEntry, excluded []string) (Candidate, error) {
	c := Candidate{Path: path}
	user, uok := entries[true]
	base, bok := entries[false]
	if uok {
		if p.delegated(user.abs) {
			c.Classification = ConfigDelegated
			c.Reason = "owned by stronger provider"
			return c, nil
		}
		if user.info.Mode()&os.ModeSymlink != 0 {
			c.Classification = ConfigUnmanagedSymlink
			return c, nil
		}
		d := ClassifyConfigPolicy(path, user.info, excluded)
		if d.Reason != PolicyAllowed {
			c.Classification = policyClassification(d.Reason)
			c.Reason = string(d.Reason)
			return c, nil
		}
		if !user.info.Mode().IsRegular() {
			c.Classification = ConfigUnsupported
			return c, nil
		}
		sensitive, err := hasSensitiveContent(user.abs)
		if err != nil {
			return c, err
		}
		if sensitive {
			c.Classification = ConfigSensitive
			c.Reason = string(PolicySensitive)
			return c, nil
		}
		hash, err := content.HashRegularFile(user.abs)
		if err != nil {
			return c, err
		}
		c.UserHash = hash
		c.UserMode = fmt.Sprintf("%04o", user.info.Mode().Perm())
	}
	if bok {
		if d := ClassifyConfigPolicy(path, base.info, excluded); d.Reason != PolicyAllowed {
			c.Classification = policyClassification(d.Reason)
			c.Reason = string(d.Reason)
			return c, nil
		}
		if base.info.Mode()&os.ModeSymlink != 0 || !base.info.Mode().IsRegular() {
			bok = false
		} else {
			hash, err := content.HashRegularFile(base.abs)
			if err != nil {
				return c, err
			}
			c.BaselineHash = hash
			c.BaselineMode = fmt.Sprintf("%04o", base.info.Mode().Perm())
		}
	}
	switch {
	case uok && bok && c.UserHash == c.BaselineHash:
		c.Classification = ConfigUnchangedBaseline
	case uok && bok:
		c.Classification = ConfigModifiedBaseline
	case uok:
		c.Classification = ConfigAdded
	case bok:
		c.Classification = ConfigDeletedBaseline
	default:
		c.Classification = ConfigUnsupported
	}
	return c, nil
}
func policyClassification(reason PolicyReason) Classification {
	switch reason {
	case PolicyExcluded:
		return ConfigExcluded
	case PolicySensitive:
		return ConfigSensitive
	case PolicyOversized:
		return ConfigOversized
	default:
		return ConfigVolatile
	}
}
func (p Provider) homeDir() string {
	return p.HomeDir
}
func (p Provider) hasHomeNamespace() bool {
	return p.HomeDir != "" && filepath.Clean(filepath.Join(p.HomeDir, ".config")) == filepath.Clean(p.UserRoot)
}
func (p Provider) configRootPrefix() string {
	if p.hasHomeNamespace() {
		return ".config"
	}
	return ""
}
func (p Provider) absoluteUserPath(logical string) (string, error) {
	if logical == "" {
		return "", fmt.Errorf("empty config path")
	}
	logical, err := profile.NormalizeConfigPath(logical)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(logical, ".config/") {
		return filepath.Join(p.UserRoot, filepath.FromSlash(strings.TrimPrefix(logical, ".config/"))), nil
	}
	if !p.hasHomeNamespace() {
		return filepath.Join(p.UserRoot, filepath.FromSlash(logical)), nil
	}
	return ExpandHomeConfigPath(p.HomeDir, logical)
}
func (p Provider) absoluteBaselinePath(logical string) (string, error) {
	if strings.HasPrefix(logical, ".config/") {
		return filepath.Join(p.BaselineRoot, filepath.FromSlash(strings.TrimPrefix(logical, ".config/"))), nil
	}
	if strings.HasPrefix(logical, ".") {
		path, ok, err := p.ResolveBaselineFor(logical)
		if err != nil || !ok {
			return "", fmt.Errorf("baseline unavailable for %s", logical)
		}
		return path, nil
	}
	return filepath.Join(p.BaselineRoot, filepath.FromSlash(logical)), nil
}
func (p Provider) delegated(path string) bool {
	for _, claim := range p.Ownership.TrackConflict(path) {
		if claim.Provider != "config" {
			return true
		}
	}
	return false
}
