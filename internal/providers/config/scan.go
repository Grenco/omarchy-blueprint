package config

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/Grenco/omarchy-blueprint/internal/content"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

type Classification string

const (
	ConfigUnchangedBaseline Classification = "unchanged-baseline"
	ConfigModifiedBaseline  Classification = "modified-baseline"
	ConfigDeletedBaseline   Classification = "deleted-baseline"
	ConfigAdded             Classification = "added"
	ConfigExcluded          Classification = "excluded"
	ConfigVolatile          Classification = "volatile"
	ConfigUnmanagedSymlink  Classification = "unmanaged-symlink"
	ConfigUnsupported       Classification = "unsupported"
	ConfigOversized         Classification = "oversized"
)

type Candidate struct {
	Path           string         `json:"path"`
	Classification Classification `json:"classification"`
	UserHash       string         `json:"hash,omitempty"`
	BaselineHash   string         `json:"baseline_hash,omitempty"`
	Reason         string         `json:"reason,omitempty"`
}
type ScanSummary struct {
	Candidates []Candidate `json:"candidates"`
}

func (p Provider) Scan(saved profile.Configs) (ScanSummary, error) {
	paths := map[string]bool{}
	for _, root := range []string{p.UserRoot, p.BaselineRoot} {
		if root == "" {
			continue
		}
		_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if path != root && !entry.IsDir() {
				rel, _ := filepath.Rel(root, path)
				paths[filepath.ToSlash(rel)] = true
			}
			return nil
		})
	}
	keys := make([]string, 0, len(paths))
	for path := range paths {
		keys = append(keys, path)
	}
	sort.Strings(keys)
	result := ScanSummary{}
	for _, path := range keys {
		c, err := p.scanPath(path, saved.Excluded)
		if err != nil {
			return ScanSummary{}, err
		}
		result.Candidates = append(result.Candidates, c)
	}
	return result, nil
}
func (p Provider) scanPath(path string, excluded []string) (Candidate, error) {
	c := Candidate{Path: path}
	user, uerr := os.Lstat(filepath.Join(p.UserRoot, filepath.FromSlash(path)))
	_, berr := os.Lstat(filepath.Join(p.BaselineRoot, filepath.FromSlash(path)))
	if uerr == nil {
		d := ClassifyConfigPolicy(path, user, excluded)
		if d.Reason != PolicyAllowed {
			c.Classification, c.Reason = ConfigVolatile, string(d.Reason)
			return c, nil
		}
		if user.Mode()&os.ModeSymlink != 0 {
			c.Classification = ConfigUnmanagedSymlink
			return c, nil
		}
		c.UserHash, _ = content.HashRegularFile(filepath.Join(p.UserRoot, filepath.FromSlash(path)))
	}
	if berr == nil {
		c.BaselineHash, _ = content.HashRegularFile(filepath.Join(p.BaselineRoot, filepath.FromSlash(path)))
	}
	switch {
	case uerr == nil && berr == nil && c.UserHash == c.BaselineHash:
		c.Classification = ConfigUnchangedBaseline
	case uerr == nil && berr == nil:
		c.Classification = ConfigModifiedBaseline
	case uerr == nil:
		c.Classification = ConfigAdded
	case os.IsNotExist(uerr) && berr == nil:
		c.Classification = ConfigDeletedBaseline
	default:
		c.Classification = ConfigUnsupported
	}
	return c, nil
}
