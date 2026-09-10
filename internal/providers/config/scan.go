package config

import (
	"errors"
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
	ConfigUnchangedBaseline  Classification = "unchanged-baseline"
	ConfigModifiedBaseline   Classification = "modified-baseline"
	ConfigDeletedBaseline    Classification = "deleted-baseline"
	ConfigAdded              Classification = "added"
	ConfigDelegated          Classification = "delegated"
	ConfigExcluded           Classification = "excluded"
	ConfigVolatile           Classification = "volatile"
	ConfigSensitive          Classification = "sensitive"
	ConfigUnmanagedSymlink   Classification = "unmanaged-symlink"
	ConfigUnsupported        Classification = "unsupported"
	ConfigOversized          Classification = "oversized"
	ConfigHistoricalBaseline Classification = "historical-baseline"
	ConfigAmbiguousBaseline  Classification = "ambiguous-baseline"
	ConfigAmbiguousDeletion  Classification = "ambiguous-deletion"
)

var errAutomaticSurfaceBudget = errors.New("automatic Config surface budget exceeded")

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
	Candidates []Candidate      `json:"candidates"`
	Surfaces   []SurfaceSummary `json:"surfaces,omitempty"`
}

// Counts groups candidates by classification for concise capture output.
func (s ScanSummary) Counts() map[Classification]int {
	counts := make(map[Classification]int)
	for _, candidate := range s.Candidates {
		counts[candidate.Classification]++
	}
	return counts
}

type treeEntry struct {
	abs  string
	info os.FileInfo
}

const (
	maxAutomaticSurfaceFiles = 5000
	maxAutomaticSurfaceBytes = 128 << 20
)

// Scan unions the recursive .config roots with exact home paths. WalkDir only
// receives roots we control and always prunes symlinks, excluded, volatile, and
// delegated directories before their children are enumerated.
func (p Provider) Scan(saved profile.Configs) (ScanSummary, error) {
	return p.scan(saved, true)
}

// ScanForCapture intentionally excludes previous desired paths. A profile
// upgrade must not keep legacy automatic discoveries alive merely by recapture.
func (p Provider) ScanForCapture(saved profile.Configs) (ScanSummary, error) {
	return p.scan(saved, false)
}

func (p Provider) scan(saved profile.Configs, includeSaved bool) (ScanSummary, error) {
	entries := map[string]map[bool]treeEntry{}
	if p.BaselineRoot != "" {
		if err := p.walkRoot(p.BaselineRoot, p.configRootPrefix(), false, saved.Excluded, entries); err != nil {
			return ScanSummary{}, err
		}
	}
	var surfaces []SurfaceSummary
	if p.hasHomeNamespace() {
		var err error
		surfaces, err = p.scanUserSurfaces(saved, entries)
		if err != nil {
			return ScanSummary{}, err
		}
	} else if p.UserRoot != "" {
		if err := p.walkRoot(p.UserRoot, "", true, saved.Excluded, entries); err != nil {
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
	// Baseline provenance outranks surface classification: exact user
	// counterparts remain candidates even under a skipped application surface.
	for logical := range entries {
		if err := p.addExactUser(entries, logical); err != nil {
			return ScanSummary{}, err
		}
	}
	// Walks intentionally omit directories. Preserve that broad behavior, but
	// surface a directory that blocks an exact saved destination so planning can
	// safely report or replace it rather than treating the path as missing.
	if includeSaved {
		for _, path := range savedPaths(saved) {
			abs, err := p.absoluteUserPath(path)
			if err != nil {
				return ScanSummary{}, err
			}
			_, err = os.Lstat(abs)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return ScanSummary{}, err
			}
			if err := p.addExact(entries, true, abs, path); err != nil {
				return ScanSummary{}, err
			}
		}
	}
	paths := make([]string, 0, len(entries))
	for path := range entries {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	result := ScanSummary{Candidates: make([]Candidate, 0, len(paths)), Surfaces: surfaces}
	for _, path := range paths {
		c, err := p.classify(path, entries[path], saved.Excluded)
		if err != nil {
			return ScanSummary{}, err
		}
		if includeSaved && c.Classification == ConfigAmbiguousDeletion && (savedHasDelete(saved, path) || savedHasFile(saved, path)) {
			c.Classification, c.Reason = ConfigDeletedBaseline, "saved deletion"
		}
		for _, included := range saved.Included {
			if path == included || strings.HasPrefix(path, included+"/") {
				if c.Classification == ConfigAmbiguousBaseline {
					c.Classification, c.Reason = ConfigModifiedBaseline, "explicitly-included"
				}
				if c.Classification == ConfigAmbiguousDeletion {
					c.Classification, c.Reason = ConfigDeletedBaseline, "explicitly-included"
				}
			}
		}
		result.Candidates = append(result.Candidates, c)
	}
	return result, nil
}

func savedHasDelete(saved profile.Configs, path string) bool {
	for _, deletion := range saved.Deletes {
		if deletion.Path == path {
			return true
		}
	}
	return false
}

func savedHasFile(saved profile.Configs, path string) bool {
	for _, file := range saved.Files {
		if file.Path == path {
			return true
		}
	}
	return false
}

func (p Provider) scanUserSurfaces(saved profile.Configs, entries map[string]map[bool]treeEntry) ([]SurfaceSummary, error) {
	rootEntries, err := os.ReadDir(p.UserRoot)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sort.Slice(rootEntries, func(i, j int) bool { return rootEntries[i].Name() < rootEntries[j].Name() })
	summaries := make([]SurfaceSummary, 0, len(rootEntries))
	for _, entry := range rootEntries {
		path := filepath.Join(p.UserRoot, entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		logical := ".config/" + entry.Name()
		if IsBackupArtifactName(entry.Name(), info.IsDir()) || IsExcludedConfigPath(logical, saved.Excluded) || p.delegated(path) {
			continue
		}
		if !info.IsDir() {
			if err := p.addExact(entries, true, path, logical); err != nil {
				return nil, err
			}
			summaries = append(summaries, SurfaceSummary{Path: logical, Classification: SurfaceConfigLean})
			continue
		}
		probe, err := ProbeSurface(path)
		if err != nil {
			return nil, err
		}
		classification, reasons := ClassifySurface(probe)
		summary := SurfaceSummary{Path: logical, Classification: classification, Reasons: reasons, SampledEntries: probe.Entries}
		summaries = append(summaries, summary)
		if classification == SurfaceConfigLean {
			walked, exceeded, err := p.walkSurface(path, logical, saved.Excluded)
			if err != nil {
				return nil, err
			}
			if exceeded {
				summary.Classification = SurfaceMixed
				summary.Reasons = append(summary.Reasons, "automatic-scan-budget-exceeded")
				summaries[len(summaries)-1] = summary
				continue
			}
			mergeEntries(entries, walked)
		}
	}
	for _, included := range saved.Included {
		included, err := profile.NormalizeConfigPath(included)
		if err != nil {
			return nil, err
		}
		if IsExcludedConfigPath(included, saved.Excluded) {
			return nil, fmt.Errorf("config include %q is excluded", included)
		}
		path, err := p.absoluteUserPath(included)
		if err != nil {
			return nil, err
		}
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			decision := ClassifyConfigPolicy(included, info, saved.Excluded)
			if decision.Reason != PolicyAllowed || p.delegated(path) || info.Mode()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("config include %q is blocked by %s policy", included, decision.Reason)
			}
			walked, exceeded, err := p.walkSurface(path, included, saved.Excluded)
			if err != nil {
				return nil, err
			}
			if exceeded {
				return nil, fmt.Errorf("config include %q exceeds Config discovery budget; track large trees as Resources", included)
			}
			mergeEntries(entries, walked)
		} else if err := p.addExact(entries, true, path, included); err != nil {
			return nil, err
		}
		for i := range summaries {
			if included == summaries[i].Path || strings.HasPrefix(included, summaries[i].Path+"/") {
				summaries[i].Explicit = true
			}
		}
	}
	return summaries, nil
}

func (p Provider) walkSurface(root, logical string, excluded []string) (map[string]map[bool]treeEntry, bool, error) {
	result := map[string]map[bool]treeEntry{}
	if _, err := os.Lstat(root); os.IsNotExist(err) {
		return result, false, nil
	} else if err != nil {
		return nil, false, err
	}
	var files int
	var bytes int64
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
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
		entryLogical := filepath.ToSlash(rel)
		if IsBackupArtifactName(filepath.Base(entryLogical), d.IsDir()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		entryLogical = logical + "/" + entryLogical
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		decision := ClassifyConfigPolicy(entryLogical, info, excluded)
		if d.IsDir() && (decision.Reason != PolicyAllowed || p.delegated(path) || info.Mode()&os.ModeSymlink != 0) {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		if info.Mode().IsRegular() {
			files++
			bytes += info.Size()
			if files > maxAutomaticSurfaceFiles || bytes > maxAutomaticSurfaceBytes {
				return errAutomaticSurfaceBudget
			}
		}
		if result[entryLogical] == nil {
			result[entryLogical] = map[bool]treeEntry{}
		}
		result[entryLogical][true] = treeEntry{abs: path, info: info}
		return nil
	})
	if errors.Is(err, errAutomaticSurfaceBudget) {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	return result, false, nil
}

func mergeEntries(destination, source map[string]map[bool]treeEntry) {
	for logical, sides := range source {
		if destination[logical] == nil {
			destination[logical] = map[bool]treeEntry{}
		}
		for side, entry := range sides {
			destination[logical][side] = entry
		}
	}
}

func (p Provider) addExactUser(entries map[string]map[bool]treeEntry, logical string) error {
	path, err := p.absoluteUserPath(logical)
	if err != nil {
		return err
	}
	return p.addExact(entries, true, path, logical)
}

func savedPaths(saved profile.Configs) []string {
	paths := make([]string, 0, len(saved.Files)+len(saved.Deletes))
	for _, file := range saved.Files {
		paths = append(paths, file.Path)
	}
	for _, deletion := range saved.Deletes {
		paths = append(paths, deletion.Path)
	}
	return paths
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
		if IsBackupArtifactName(filepath.Base(logical), d.IsDir()) {
			// Restore keeps replacement backups as siblings for rollback and journaling.
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
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
		inspection, err := InspectRegularFile(user.abs, MaxAutomaticConfigFileSize)
		if err != nil {
			return c, err
		}
		if inspection.BytesRead > MaxAutomaticConfigFileSize {
			c.Classification = ConfigOversized
			c.Reason = string(PolicyOversized)
			return c, nil
		}
		if inspection.Sensitive {
			c.Classification = ConfigSensitive
			c.Reason = string(PolicySensitive)
			return c, nil
		}
		if !inspection.TextLike && !bok {
			c.Classification = ConfigVolatile
			c.Reason = "opaque application state"
			return c, nil
		}
		c.UserHash = inspection.Hash
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
	if uok && bok && !baselineIdentity(c.UserHash, c.UserMode, c.BaselineHash, c.BaselineMode) {
		if p.History == nil {
			c.Classification, c.Reason = ConfigAmbiguousBaseline, "baseline provenance unavailable"
			return c, nil
		}
		historical, err := p.History.Match(path, c.UserHash)
		if err != nil {
			return c, err
		}
		if historical {
			c.Classification, c.Reason = ConfigHistoricalBaseline, "matches trusted historical baseline"
			return c, nil
		}
	}
	switch {
	case uok && bok && baselineIdentity(c.UserHash, c.UserMode, c.BaselineHash, c.BaselineMode):
		c.Classification = ConfigUnchangedBaseline
	case uok && bok:
		c.Classification = ConfigModifiedBaseline
	case uok:
		c.Classification = ConfigAdded
	case bok:
		if history, ok := p.History.(interface{ Deleted(string) (bool, error) }); ok {
			deleted, err := history.Deleted(path)
			if err != nil {
				return c, err
			}
			if deleted {
				c.Classification = ConfigDeletedBaseline
			} else {
				c.Classification, c.Reason = ConfigAmbiguousDeletion, "deletion provenance unavailable"
			}
		} else {
			c.Classification, c.Reason = ConfigAmbiguousDeletion, "deletion provenance unavailable"
		}
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
