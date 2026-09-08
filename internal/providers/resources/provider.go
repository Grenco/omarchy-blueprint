package resources

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/Grenco/omarchy-blueprint/internal/command"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/ownership"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

type Provider struct {
	Runner     command.Runner
	HomeDir    string
	ProfileDir string
	LinkRoots  []LinkSearchRoot
	Ownership  ownership.Index
}

func (p Provider) Track(ctx context.Context, saved profile.Resources, path, requestedID string) (profile.Resources, []model.Change, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return saved, nil, err
	}
	logical, err := LogicalHomePath(p.HomeDir, abs)
	if err != nil {
		return saved, nil, err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return saved, nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return saved, nil, fmt.Errorf("%s is a symlink; track its owning resource instead", logical)
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return saved, nil, fmt.Errorf("unsupported resource type: %s", logical)
	}
	id := requestedID
	if id == "" {
		id = ResourceIDForPath(abs)
	}
	if err := ValidateResourceID(id); err != nil {
		return saved, nil, err
	}
	for _, item := range saved.Items {
		root, err := ExpandHomePath(p.HomeDir, item.Path)
		if err != nil {
			return saved, nil, err
		}
		if item.ID == id || PathsOverlap(abs, root) {
			return saved, nil, fmt.Errorf("resource %s conflicts with tracked resource %s", logical, item.ID)
		}
	}
	if p.ProfileDir != "" && PathsOverlap(abs, p.ProfileDir) {
		return saved, nil, fmt.Errorf("resource %s overlaps active profile directory", logical)
	}
	if conflicts := p.Ownership.TrackConflict(abs); len(conflicts) != 0 {
		return saved, nil, fmt.Errorf("resource %s is owned by %s", logical, conflicts[0].Provider)
	}

	item := profile.Resource{ID: id, Path: logical}
	if info.IsDir() {
		if p.Runner != nil {
			git, isGit, err := DetectGitResource(ctx, p.Runner, abs)
			if err != nil {
				return saved, nil, err
			}
			if isGit {
				if git.Dirty {
					return saved, nil, fmt.Errorf("resource %s has uncommitted or untracked Git state", id)
				}
				item.Kind, item.Strategy = "directory", "git"
				item.Remote, item.Branch, item.Revision = git.Remote, git.Branch, git.Revision
			}
		}
		if item.Strategy == "" {
			item.Kind, item.Strategy = "directory", "copy"
		}
	} else {
		item.Kind, item.Strategy = "file", "copy"
	}
	next := cloneResources(saved)
	next.Items = append(next.Items, item)
	next, err = p.capture(ctx, next)
	if err != nil {
		return saved, nil, err
	}
	return next, Diff(saved, next), nil
}

func (p Provider) Capture(ctx context.Context, saved profile.Resources) (profile.Resources, []model.Change, error) {
	next, err := p.capture(ctx, cloneResources(saved))
	if err != nil {
		return saved, nil, err
	}
	return next, Diff(saved, next), nil
}

func (p Provider) capture(ctx context.Context, next profile.Resources) (profile.Resources, error) {
	if p.ProfileDir == "" || p.HomeDir == "" {
		return profile.Resources{}, errors.New("home and profile directories are required")
	}
	if err := validateMetadata(p.HomeDir, next); err != nil {
		return profile.Resources{}, err
	}
	parent := filepath.Join(p.ProfileDir, "resources")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return profile.Resources{}, err
	}
	staging, err := os.MkdirTemp(parent, ".capture-*")
	if err != nil {
		return profile.Resources{}, err
	}
	defer os.RemoveAll(staging)
	rawLinks := make(map[string][]RawLink)
	for i := range next.Items {
		item := &next.Items[i]
		root, err := ExpandHomePath(p.HomeDir, item.Path)
		if err != nil {
			return profile.Resources{}, err
		}
		if item.Strategy == "git" {
			if p.Runner == nil {
				return profile.Resources{}, errors.New("command runner is required to capture Git resources")
			}
			git, isGit, err := DetectGitResource(ctx, p.Runner, root)
			if err != nil || !isGit || git.Dirty {
				return profile.Resources{}, fmt.Errorf("resource %s is not a clean Git worktree: %w", item.ID, err)
			}
			item.Remote, item.Branch, item.Revision = git.Remote, git.Branch, git.Revision
			continue
		}
		scan, err := StageCopyResource(root, resourceSnapshotPath(staging, *item))
		if err != nil {
			return profile.Resources{}, fmt.Errorf("capture resource %s: %w", item.ID, err)
		}
		item.Hash, item.Mode = scan.Hash, fmt.Sprintf("%04o", scan.Mode.Perm())
		rawLinks[item.ID] = scan.Links
	}
	next.Links = nil
	for _, item := range next.Items {
		if item.Strategy != "copy" {
			continue
		}
		links, err := ClassifyResourceLinks(p.HomeDir, item, rawLinks[item.ID], next.Items)
		if err != nil {
			return profile.Resources{}, err
		}
		for _, link := range links {
			if link.Classification != LinkManagedResource {
				return profile.Resources{}, fmt.Errorf("resource %s contains %s symlink at %s", item.ID, link.Classification, link.Source)
			}
			next.Links = append(next.Links, resourceLink(link, "resource"))
		}
	}
	links, err := DiscoverLinks(p.HomeDir, p.roots(), next.Items, p.Ownership, next.IgnoredLinks)
	if err != nil {
		return profile.Resources{}, err
	}
	for _, link := range links {
		if link.Classification == LinkManagedInbound {
			next.Links = append(next.Links, resourceLink(link, "inbound"))
		}
	}
	sortResources(&next)
	if err := swapResourceFiles(staging, filepath.Join(parent, "files")); err != nil {
		return profile.Resources{}, err
	}
	return next, nil
}

func (p Provider) Untrack(saved profile.Resources, ref string) (profile.Resources, []string, error) {
	next := cloneResources(saved)
	if len(ref) > len("resource:") && ref[:len("resource:")] == "resource:" {
		id := ref[len("resource:"):]
		found := false
		items := next.Items[:0]
		for _, item := range next.Items {
			if item.ID == id {
				found = true
				continue
			}
			items = append(items, item)
		}
		if !found {
			return saved, nil, fmt.Errorf("resource %s is not tracked", id)
		}
		next.Items = items
		links := next.Links[:0]
		for _, link := range next.Links {
			if link.TargetResource != id && link.SourceResource != id {
				links = append(links, link)
			}
		}
		next.Links = links
		if err := os.RemoveAll(filepath.Join(p.ProfileDir, "resources", "files", id)); err != nil {
			return saved, nil, err
		}
		return next, []string{"resource:" + id}, nil
	}
	if len(ref) > len("link:") && ref[:len("link:")] == "link:" {
		source := ref[len("link:"):]
		if _, err := ExpandHomePath(p.HomeDir, source); err != nil {
			return saved, nil, err
		}
		links := next.Links[:0]
		found := false
		for _, link := range next.Links {
			if link.SourceResource == "" && link.Source == source {
				found = true
				continue
			}
			links = append(links, link)
		}
		if !found {
			return saved, nil, fmt.Errorf("link %s is not tracked", source)
		}
		next.Links = links
		next.IgnoredLinks = append(next.IgnoredLinks, source)
		sortResources(&next)
		return next, []string{"link:" + source}, nil
	}
	return saved, nil, fmt.Errorf("unknown resource reference %q", ref)
}

func (p Provider) EnableLink(saved profile.Resources, source string) (profile.Resources, error) {
	abs, err := ExpandHomePath(p.HomeDir, source)
	if err != nil {
		return saved, err
	}
	candidate, err := classifyLink(p.HomeDir, abs, saved.Items, "", p.Ownership)
	if err != nil || candidate.Classification != LinkManagedInbound {
		return saved, fmt.Errorf("link %s does not resolve into a tracked resource", source)
	}
	next := cloneResources(saved)
	ignored := next.IgnoredLinks[:0]
	for _, item := range next.IgnoredLinks {
		if item != source {
			ignored = append(ignored, item)
		}
	}
	next.IgnoredLinks = ignored
	return next, nil
}

func (p Provider) Detect(ctx context.Context, saved profile.Resources) (profile.Resources, []LinkCandidate, error) {
	current := cloneResources(saved)
	rawLinks := make(map[string][]RawLink)
	for i := range current.Items {
		item := &current.Items[i]
		root, err := ExpandHomePath(p.HomeDir, item.Path)
		if err != nil {
			return profile.Resources{}, nil, err
		}
		if item.Strategy == "copy" {
			scan, err := ScanCopyResource(root)
			if err != nil {
				item.Hash = ""
				continue
			}
			item.Hash, item.Mode = scan.Hash, fmt.Sprintf("%04o", scan.Mode.Perm())
			rawLinks[item.ID] = scan.Links
		} else {
			if p.Runner == nil {
				return profile.Resources{}, nil, errors.New("command runner is required to detect Git resources")
			}
			git, isGit, err := DetectGitResource(ctx, p.Runner, root)
			if err != nil || !isGit || git.Dirty {
				item.Revision = ""
				continue
			}
			item.Remote, item.Branch, item.Revision = git.Remote, git.Branch, git.Revision
		}
	}
	links, err := DiscoverLinks(p.HomeDir, p.roots(), current.Items, p.Ownership, current.IgnoredLinks)
	if err != nil {
		return profile.Resources{}, nil, err
	}
	current.Links = nil
	for _, item := range current.Items {
		if item.Strategy != "copy" || currentItemMissing(item) {
			continue
		}
		internal, err := ClassifyResourceLinks(p.HomeDir, item, rawLinks[item.ID], current.Items)
		if err != nil {
			return profile.Resources{}, nil, err
		}
		for _, link := range internal {
			if link.Classification == LinkManagedResource {
				current.Links = append(current.Links, resourceLink(link, "resource"))
			}
		}
	}
	for _, link := range links {
		if link.Classification == LinkManagedInbound {
			current.Links = append(current.Links, resourceLink(link, "inbound"))
		}
	}
	return current, links, nil
}

func currentItemMissing(item profile.Resource) bool {
	return item.Strategy == "copy" && item.Hash == ""
}

func (p Provider) roots() []LinkSearchRoot {
	if p.LinkRoots != nil {
		return p.LinkRoots
	}
	return DefaultLinkSearchRoots(p.HomeDir)
}
func resourceLink(link LinkCandidate, origin string) profile.ResourceLink {
	return profile.ResourceLink{SourceResource: link.SourceResource, Source: link.Source, TargetResource: link.TargetResource, Target: link.TargetRelative, Origin: origin}
}
func cloneResources(in profile.Resources) profile.Resources {
	return profile.Resources{Items: append([]profile.Resource(nil), in.Items...), Links: append([]profile.ResourceLink(nil), in.Links...), IgnoredLinks: append([]string(nil), in.IgnoredLinks...)}
}
func sortResources(resources *profile.Resources) {
	sort.Slice(resources.Items, func(i, j int) bool { return resources.Items[i].ID < resources.Items[j].ID })
	sort.Slice(resources.Links, func(i, j int) bool { return linkKey(resources.Links[i]) < linkKey(resources.Links[j]) })
	sort.Strings(resources.IgnoredLinks)
}
func swapResourceFiles(staging, destination string) error {
	staged := filepath.Join(staging, "files")
	if err := os.MkdirAll(staged, 0o755); err != nil {
		return err
	}
	previous := filepath.Join(filepath.Dir(destination), ".files-previous")
	_ = os.RemoveAll(previous)
	if _, err := os.Lstat(destination); err == nil {
		if err := os.Rename(destination, previous); err != nil {
			return err
		}
	}
	if err := os.Rename(staged, destination); err != nil {
		_ = os.Rename(previous, destination)
		return err
	}
	return os.RemoveAll(previous)
}

func resourceSnapshotPath(root string, item profile.Resource) string {
	path := filepath.Join(root, "files", item.ID)
	if item.Kind == "file" {
		return filepath.Join(path, "content")
	}
	return path
}
