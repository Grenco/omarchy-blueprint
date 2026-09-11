package resources

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/ownership"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

type LinkClassification string

const (
	LinkManagedInbound      LinkClassification = "managed-inbound"
	LinkManagedResource     LinkClassification = "managed-resource"
	LinkGitOwned            LinkClassification = "git-owned"
	LinkUntrackedHomeTarget LinkClassification = "untracked-home-target"
	LinkExternalTarget      LinkClassification = "external-target"
	LinkBroken              LinkClassification = "broken"
	LinkOwnershipConflict   LinkClassification = "ownership-conflict"
)

type LinkSearchRoot struct {
	Path      string
	Recursive bool
}
type LinkCandidate struct {
	Source, RawTarget, Resolved                    string
	Classification                                 LinkClassification
	SourceResource, TargetResource, TargetRelative string
}

func DefaultLinkSearchRoots(home string) []LinkSearchRoot {
	return []LinkSearchRoot{{Path: home}, {Path: filepath.Join(home, ".config"), Recursive: true}, {Path: filepath.Join(home, ".local", "bin"), Recursive: true}}
}

func DiscoverLinks(home string, roots []LinkSearchRoot, resources []profile.Resource, index ownership.Index, ignored []string) ([]LinkCandidate, error) {
	resourceRoots, err := defaultResourceRoots(home, resources)
	if err != nil {
		return nil, err
	}
	return discoverLinks(home, roots, resources, resourceRoots, index, ignored)
}

func discoverLinks(home string, roots []LinkSearchRoot, resources []profile.Resource, resourceRoots map[string]string, index ownership.Index, ignored []string) ([]LinkCandidate, error) {
	seen, ignoredSet := map[string]bool{}, map[string]bool{}
	for _, path := range ignored {
		ignoredSet[path] = true
	}
	var candidates []LinkCandidate
	for _, root := range roots {
		entries, err := linkEntries(root)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, source := range entries {
			if seen[source] || insideResource(source, resourceRoots) {
				continue
			}
			seen[source] = true
			candidate, err := classifyLink(home, source, resources, resourceRoots, "", index)
			if err != nil {
				return nil, err
			}
			if candidate.Classification == LinkManagedInbound && ignoredSet[candidate.Source] {
				continue
			}
			candidates = append(candidates, candidate)
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Source < candidates[j].Source })
	return candidates, nil
}

func ClassifyResourceLinks(home string, resource profile.Resource, raw []RawLink, all []profile.Resource) ([]LinkCandidate, error) {
	roots, err := defaultResourceRoots(home, all)
	if err != nil {
		return nil, err
	}
	return classifyResourceLinks(home, resource, raw, all, roots)
}

func classifyResourceLinks(home string, resource profile.Resource, raw []RawLink, all []profile.Resource, resourceRoots map[string]string) ([]LinkCandidate, error) {
	var candidates []LinkCandidate
	for _, link := range raw {
		candidate, err := classifyLink(home, link.SourceAbsolute, all, resourceRoots, resource.ID, ownership.Index{})
		if err != nil {
			return nil, err
		}
		if resource.Strategy == "git" {
			candidate.Classification = LinkGitOwned
			candidate.SourceResource = resource.ID
			candidates = append(candidates, candidate)
			continue
		}
		if candidate.Classification == LinkManagedInbound {
			candidate.Classification = LinkManagedResource
		}
		if candidate.Classification == LinkManagedResource {
			candidate.SourceResource = resource.ID
			root, ok := resourceRoots[resource.ID]
			if !ok {
				return nil, fmt.Errorf("unknown resource %s", resource.ID)
			}
			relative, err := filepath.Rel(root, link.SourceAbsolute)
			if err != nil {
				return nil, err
			}
			candidate.Source = filepath.ToSlash(relative)
		}
		candidates = append(candidates, candidate)
	}
	return candidates, nil
}

// IsTrackedInboundLink reports whether source is the exact live symlink
// recorded as an inbound link for a tracked resource.
func IsTrackedInboundLink(home, source string, saved profile.Resources) (bool, error) {
	if _, err := LogicalHomePath(home, source); err != nil {
		return false, nil
	}
	roots, err := defaultResourceRoots(home, saved.Items)
	if err != nil {
		return false, err
	}
	candidate, err := classifyLink(home, source, saved.Items, roots, "", ownership.Index{})
	if err != nil {
		return false, err
	}
	if candidate.Classification != LinkManagedInbound {
		return false, nil
	}
	for _, link := range saved.Links {
		if link.Origin == "inbound" && link.SourceResource == "" && link.Source == candidate.Source && link.TargetResource == candidate.TargetResource && link.Target == candidate.TargetRelative {
			return true, nil
		}
	}
	return false, nil
}

// IsTrackedInboundLinkPath reports whether source is reserved for an inbound
// resource link, including when that link still needs to be recreated.
func IsTrackedInboundLinkPath(home, source string, saved profile.Resources) (bool, error) {
	logical, err := LogicalHomePath(home, source)
	if err != nil {
		return false, nil
	}
	for _, link := range saved.Links {
		if link.Origin != "inbound" || link.SourceResource != "" || link.Source != logical {
			continue
		}
		for _, resource := range saved.Items {
			if resource.ID == link.TargetResource {
				return true, nil
			}
		}
	}
	return false, nil
}

func RelativeSymlinkTarget(sourcePath, targetPath string) (string, error) {
	target, err := filepath.Rel(filepath.Dir(sourcePath), targetPath)
	if err != nil {
		return "", fmt.Errorf("calculate relative symlink target: %w", err)
	}
	if target == "" || filepath.IsAbs(target) {
		return "", fmt.Errorf("invalid relative symlink target")
	}
	return target, nil
}

func linkEntries(root LinkSearchRoot) ([]string, error) {
	if !root.Recursive {
		entries, err := os.ReadDir(root.Path)
		if err != nil {
			return nil, err
		}
		var paths []string
		for _, entry := range entries {
			if isBlueprintBackupName(entry.Name()) {
				continue
			}
			if entry.Type()&os.ModeSymlink != 0 {
				paths = append(paths, filepath.Join(root.Path, entry.Name()))
			}
		}
		return paths, nil
	}
	var paths []string
	err := filepath.WalkDir(root.Path, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path != root.Path && isBlueprintBackupName(entry.Name()) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if path != root.Path && entry.Type()&os.ModeSymlink != 0 {
			paths = append(paths, path)
			if entry.IsDir() {
				return filepath.SkipDir
			}
		}
		return nil
	})
	return paths, err
}

func isBlueprintBackupName(name string) bool {
	return strings.HasPrefix(name, ".") && strings.Contains(name, ".omarchy-blueprint-backup-")
}

func classifyLink(home, source string, resources []profile.Resource, resourceRoots map[string]string, sourceResource string, index ownership.Index) (LinkCandidate, error) {
	raw, err := os.Readlink(source)
	if err != nil {
		return LinkCandidate{}, err
	}
	candidate := LinkCandidate{Source: source, RawTarget: raw, SourceResource: sourceResource}
	if sourceResource == "" {
		logical, err := LogicalHomePath(home, source)
		if err != nil {
			return LinkCandidate{}, err
		}
		candidate.Source = logical
	}
	target := raw
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(source), target)
	}
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		candidate.Classification = LinkBroken
		return candidate, nil
	}
	candidate.Resolved = resolved
	for _, resource := range resources {
		root, ok := resourceRoots[resource.ID]
		if !ok {
			return candidate, fmt.Errorf("unknown resource %s", resource.ID)
		}
		relative, err := filepath.Rel(root, resolved)
		if err == nil && SafeRelativeResourcePath(relative) {
			candidate.TargetResource, candidate.TargetRelative = resource.ID, filepath.ToSlash(relative)
			if sourceResource != "" {
				candidate.Classification = LinkManagedResource
				return candidate, nil
			}
			if len(index.LinkConflict(source)) > 0 {
				candidate.Classification = LinkOwnershipConflict
			} else {
				candidate.Classification = LinkManagedInbound
			}
			return candidate, nil
		}
	}
	if within(home, resolved) {
		candidate.Classification = LinkUntrackedHomeTarget
	} else {
		candidate.Classification = LinkExternalTarget
	}
	return candidate, nil
}

func defaultResourceRoots(home string, resources []profile.Resource) (map[string]string, error) {
	roots := make(map[string]string, len(resources))
	for _, resource := range resources {
		root, err := ExpandHomePath(home, resource.Path)
		if err != nil {
			return nil, err
		}
		roots[resource.ID] = root
	}
	return roots, nil
}

func insideResource(path string, resourceRoots map[string]string) bool {
	for _, root := range resourceRoots {
		if within(root, path) {
			return true
		}
	}
	return false
}
