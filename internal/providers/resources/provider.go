package resources

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/command"
	"github.com/Grenco/omarchy-blueprint/internal/content"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/ownership"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/sensitive"
)

type Provider struct {
	Runner        command.Runner
	HomeDir       string
	ProfileDir    string
	LinkRoots     []LinkSearchRoot
	Ownership     ownership.Index
	ResourcePaths ResourcePaths
}

// GitWorkingSummary is runtime-only detail about local Git state.
type GitWorkingSummary struct {
	StagedTracked     int      `json:"staged_tracked"`
	UnstagedTracked   int      `json:"unstaged_tracked"`
	Untracked         []string `json:"untracked"`
	SelectedUntracked []string `json:"selected_untracked,omitempty"`
}

type Detection struct {
	Resources profile.Resources
	Links     []LinkCandidate
	Git       map[string]GitWorkingSummary
}

func (p Provider) Track(ctx context.Context, saved profile.Resources, path string, options TrackOptions) (profile.Resources, []model.Change, error) {
	prepared, err := p.PrepareTrack(ctx, saved, path, options)
	if err != nil {
		return saved, nil, err
	}
	if err := prepared.Install(); err != nil {
		return saved, nil, err
	}
	if err := prepared.Commit(); err != nil {
		_ = prepared.Rollback()
		return saved, nil, err
	}
	if err := prepared.Finalize(); err != nil {
		return saved, nil, err
	}
	return prepared.State, prepared.Changes, nil
}

type TrackOptions struct {
	ID               string
	Strategy         string
	IncludeUntracked []string
	ExcludeUntracked []string
}

// PrepareTrack stages a new tracked resource without making it visible.
func (p Provider) PrepareTrack(ctx context.Context, saved profile.Resources, path string, options TrackOptions) (*PreparedCapture, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	logical, err := LogicalHomePath(p.HomeDir, abs)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%s is a symlink; track its owning resource instead", logical)
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return nil, fmt.Errorf("unsupported resource type: %s", logical)
	}
	id := options.ID
	if id == "" {
		id = ResourceIDForPath(abs)
	}
	if err := ValidateResourceID(id); err != nil {
		return nil, err
	}
	existing := -1
	for i, item := range saved.Items {
		root, err := p.resourceRoot(item)
		if err != nil {
			return nil, err
		}
		if filepath.Clean(abs) == filepath.Clean(root) {
			if options.ID != "" && options.ID != item.ID {
				return nil, fmt.Errorf("resource %s is already tracked as %s", logical, item.ID)
			}
			existing = i
			id = item.ID
			continue
		}
		if item.ID == id || PathsOverlap(abs, root) {
			return nil, fmt.Errorf("resource %s conflicts with tracked resource %s", logical, item.ID)
		}
	}
	if p.ProfileDir != "" && PathsOverlap(abs, p.ProfileDir) {
		return nil, fmt.Errorf("resource %s overlaps active profile directory", logical)
	}
	if conflicts := p.Ownership.TrackConflict(abs); len(conflicts) != 0 {
		return nil, fmt.Errorf("resource %s is owned by %s", logical, conflicts[0].Provider)
	}

	item := profile.Resource{ID: id, Path: logical}
	if existing >= 0 {
		item = saved.Items[existing]
	}
	if info.IsDir() {
		isGit := p.isGitWorktreeRoot(ctx, abs)
		strategy := options.Strategy
		if strategy == "" {
			if existing >= 0 {
				strategy = item.Strategy
			} else {
				strategy = "copy"
			}
			if existing < 0 && isGit && p.Runner != nil {
				if _, ok, err := DetectGitResource(ctx, p.Runner, abs); err == nil && ok {
					strategy = "git"
				}
			}
		}
		if strategy != "copy" && strategy != "git" && strategy != "git+diff" {
			return nil, fmt.Errorf("invalid resource strategy %q", strategy)
		}
		if (strategy == "git" || strategy == "git+diff") && (!isGit || p.Runner == nil) {
			return nil, fmt.Errorf("resource %s is not a Git worktree", logical)
		}
		item.Kind, item.Strategy = "directory", strategy
		if strategy == "copy" {
			item.Remote, item.Branch, item.Revision = "", "", ""
			item.IndexPatchHash, item.WorktreePatchHash, item.Untracked = "", "", nil
		} else {
			item.Hash, item.Mode = "", ""
			git, ok, err := DetectGitResource(ctx, p.Runner, abs)
			if err != nil || !ok {
				return nil, fmt.Errorf("inspect Git resource %s: %w", logical, err)
			}
			item.Remote, item.Branch, item.Revision, item.Dirty = git.Remote, git.Branch, git.Revision, git.Dirty
			if strategy == "git" {
				item.IndexPatchHash, item.WorktreePatchHash, item.Untracked = "", "", nil
			}
		}
	} else {
		if options.Strategy != "" && options.Strategy != "copy" {
			return nil, fmt.Errorf("resource %s is a regular file and requires strategy copy", logical)
		}
		item.Kind, item.Strategy = "file", "copy"
	}
	if item.Strategy != "git+diff" && (len(options.IncludeUntracked) != 0 || len(options.ExcludeUntracked) != 0) {
		return nil, fmt.Errorf("--include-untracked and --exclude-untracked require strategy git+diff")
	}
	next := cloneResources(saved)
	if existing >= 0 {
		next.Items[existing] = item
	} else {
		next.Items = append(next.Items, item)
	}
	if item.Strategy == "git+diff" {
		selected := map[string]bool{}
		if existing >= 0 {
			for _, file := range saved.Items[existing].Untracked {
				selected[file.Path] = true
			}
		}
		for _, raw := range options.IncludeUntracked {
			path, err := normalizeUntrackedPath(raw)
			if err != nil {
				return nil, err
			}
			selected[path] = true
		}
		for _, raw := range options.ExcludeUntracked {
			path, err := normalizeUntrackedPath(raw)
			if err != nil {
				return nil, err
			}
			delete(selected, path)
		}
		tracked := &next.Items[len(next.Items)-1]
		if existing >= 0 {
			tracked = &next.Items[existing]
		}
		tracked.Untracked = make([]profile.GitUntrackedFile, 0, len(selected))
		for path := range selected {
			tracked.Untracked = append(tracked.Untracked, profile.GitUntrackedFile{Path: path})
		}
	}
	return p.PrepareCapture(ctx, next, CaptureOptions{})
}

func (p Provider) Capture(ctx context.Context, saved profile.Resources) (profile.Resources, []model.Change, error) {
	prepared, err := p.PrepareCapture(ctx, saved, CaptureOptions{})
	if err != nil {
		return saved, nil, err
	}
	if err := prepared.Install(); err != nil {
		return saved, nil, err
	}
	if err := prepared.Commit(); err != nil {
		_ = prepared.Rollback()
		return saved, nil, err
	}
	if err := prepared.Finalize(); err != nil {
		return saved, nil, err
	}
	return prepared.State, prepared.Changes, nil
}

// CaptureOptions is reserved for strategy-specific capture options.
type CaptureOptions struct{}

// PrepareCapture builds a complete Resources generation while retaining the
// current generation until the caller has persisted profile metadata.
func (p Provider) PrepareCapture(ctx context.Context, saved profile.Resources, options CaptureOptions) (_ *PreparedCapture, err error) {
	if p.ProfileDir == "" || p.HomeDir == "" {
		return nil, errors.New("home and profile directories are required")
	}
	next := cloneResources(saved)
	if err := p.validateMetadata(next); err != nil {
		return nil, err
	}
	parent := filepath.Join(p.ProfileDir, "resources")
	prepared, err := prepareCapture(parent, next, nil)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*PreparedCapture, error) { _ = prepared.Rollback(); return nil, err }
	staging := prepared.stage
	rawLinks := make(map[string][]RawLink)
	for i := range next.Items {
		item := &next.Items[i]
		root, err := p.resourceRoot(*item)
		if err != nil {
			return fail(err)
		}
		if item.Strategy == "git" {
			if p.Runner == nil {
				return fail(errors.New("command runner is required to capture Git resources"))
			}
			git, isGit, err := DetectGitResource(ctx, p.Runner, root)
			if err != nil || !isGit {
				return fail(fmt.Errorf("resource %s is not a Git worktree: %w", item.ID, err))
			}
			item.Remote, item.Branch, item.Revision = git.Remote, git.Branch, git.Revision
			item.Dirty = git.Dirty
			continue
		}
		if item.Strategy == "git+diff" {
			if p.Runner == nil {
				return fail(errors.New("command runner is required to capture Git resources"))
			}
			working, isGit, err := InspectGitWorkingState(ctx, p.Runner, root)
			if err != nil || !isGit {
				return fail(fmt.Errorf("resource %s is not a Git worktree: %w", item.ID, err))
			}
			selected := make([]string, 0, len(item.Untracked))
			for _, file := range item.Untracked {
				if containsString(working.Untracked, file.Path) {
					selected = append(selected, file.Path)
					continue
				}
				if _, err := p.Runner.Run(ctx, "git", "-C", root, "check-ignore", "-q", "--", file.Path); err == nil {
					return fail(fmt.Errorf("selected untracked path is now ignored: %s", file.Path))
				}
			}
			capture, err := CaptureGitWorkingState(ctx, p.Runner, root, selected)
			if err != nil {
				return fail(fmt.Errorf("capture Git state for %s: %w", item.ID, err))
			}
			item.Remote, item.Branch, item.Revision = working.Remote, working.Branch, working.Revision
			item.Dirty = working.StagedTracked > 0 || working.UnstagedTracked > 0 || len(working.Untracked) > 0
			item.Hash, item.Mode = "", ""
			item.IndexPatchHash, item.WorktreePatchHash, item.Untracked = capture.IndexPatchHash, capture.WorktreePatchHash, capture.Untracked
			gitState := filepath.Join(staging, "git-state", item.ID)
			if err := os.MkdirAll(gitState, 0o755); err != nil {
				return fail(fmt.Errorf("stage Git state for %s: %w", item.ID, err))
			}
			if len(capture.IndexPatch) != 0 {
				if err := os.WriteFile(filepath.Join(gitState, "index.patch"), capture.IndexPatch, 0o644); err != nil {
					return fail(fmt.Errorf("stage index patch for %s: %w", item.ID, err))
				}
			}
			if len(capture.WorktreePatch) != 0 {
				if err := os.WriteFile(filepath.Join(gitState, "worktree.patch"), capture.WorktreePatch, 0o644); err != nil {
					return fail(fmt.Errorf("stage worktree patch for %s: %w", item.ID, err))
				}
			}
			capture.TotalBytes = int64(len(capture.IndexPatch) + len(capture.WorktreePatch))
			for _, file := range capture.Untracked {
				source := filepath.Join(root, filepath.FromSlash(file.Path))
				destination := filepath.Join(gitState, "untracked", filepath.FromSlash(file.Path))
				scan, err := stageUntrackedFile(source, destination)
				if err != nil {
					return fail(fmt.Errorf("stage untracked file %s: %w", file.Path, err))
				}
				info, err := os.Stat(destination)
				if err != nil {
					return fail(fmt.Errorf("inspect staged untracked file %s: %w", file.Path, err))
				}
				capture.TotalBytes += info.Size()
				if capture.TotalBytes > MaxGitStateSize {
					return fail(fmt.Errorf("Git state exceeds %d bytes", MaxGitStateSize))
				}
				for j := range capture.Untracked {
					if capture.Untracked[j].Path == file.Path {
						capture.Untracked[j].Hash = scan.Hash
						capture.Untracked[j].Mode = fmt.Sprintf("%04o", scan.Mode.Perm())
					}
				}
			}
			sort.Slice(capture.Untracked, func(i, j int) bool { return capture.Untracked[i].Path < capture.Untracked[j].Path })
			item.Untracked = capture.Untracked
			continue
		}
		scan, err := StageCopyResourceWithOptions(root, resourceSnapshotPath(staging, *item), SnapshotOptions{ExcludeGitAdmin: p.isGitWorktreeRoot(ctx, root)})
		if err != nil {
			return fail(fmt.Errorf("capture resource %s: %w", item.ID, err))
		}
		item.Hash, item.Mode = scan.Hash, fmt.Sprintf("%04o", scan.Mode.Perm())
		rawLinks[item.ID] = scan.Links
	}
	next.Links = nil
	for _, item := range next.Items {
		if item.Strategy != "copy" {
			continue
		}
		roots, err := p.resourceRoots(next.Items)
		if err != nil {
			return fail(err)
		}
		links, err := classifyResourceLinks(p.HomeDir, item, rawLinks[item.ID], next.Items, roots)
		if err != nil {
			return fail(err)
		}
		for _, link := range links {
			if link.Classification != LinkManagedResource {
				return fail(fmt.Errorf("resource %s contains %s symlink at %s", item.ID, link.Classification, link.Source))
			}
			next.Links = append(next.Links, resourceLink(link, "resource"))
		}
	}
	roots, err := p.resourceRoots(next.Items)
	if err != nil {
		return fail(err)
	}
	links, err := discoverLinks(p.HomeDir, p.roots(), next.Items, roots, p.Ownership, next.IgnoredLinks)
	if err != nil {
		return fail(err)
	}
	for _, link := range links {
		if link.Classification == LinkManagedInbound {
			next.Links = append(next.Links, resourceLink(link, "inbound"))
		}
	}
	sortResources(&next)
	resourcesTOML, err := profile.MarshalResources(next)
	if err != nil {
		return fail(err)
	}
	if err := os.WriteFile(prepared.stagePath("resources.toml"), resourcesTOML, 0o644); err != nil {
		return fail(err)
	}
	prepared.State, prepared.Changes = next, Diff(saved, next)
	return prepared, nil
}

func (p Provider) Untrack(saved profile.Resources, ref string) (profile.Resources, []string, error) {
	prepared, changed, err := p.PrepareUntrack(saved, ref)
	if err != nil {
		return saved, nil, err
	}
	if prepared == nil {
		return cloneResources(saved), changed, nil
	}
	if err := prepared.Install(); err != nil {
		return saved, nil, err
	}
	if err := prepared.Commit(); err != nil {
		_ = prepared.Rollback()
		return saved, nil, err
	}
	if err := prepared.Finalize(); err != nil {
		return saved, nil, err
	}
	return prepared.State, changed, nil
}

// PrepareUntrack stages removal without changing the current generation.
func (p Provider) PrepareUntrack(saved profile.Resources, ref string) (*PreparedCapture, []string, error) {
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
			return nil, nil, fmt.Errorf("resource %s is not tracked", id)
		}
		next.Items = items
		links := next.Links[:0]
		for _, link := range next.Links {
			if link.TargetResource != id && link.SourceResource != id {
				links = append(links, link)
			}
		}
		next.Links = links
		prepared, err := p.prepareResourcesGeneration(saved, next)
		if err != nil {
			return nil, nil, err
		}
		if err := os.RemoveAll(filepath.Join(prepared.stage, "files", id)); err != nil {
			_ = prepared.Rollback()
			return nil, nil, err
		}
		if err := os.RemoveAll(filepath.Join(prepared.stage, "git-state", id)); err != nil {
			_ = prepared.Rollback()
			return nil, nil, err
		}
		return prepared, []string{"resource:" + id}, nil
	}
	if len(ref) > len("link:") && ref[:len("link:")] == "link:" {
		source := ref[len("link:"):]
		if _, err := ExpandHomePath(p.HomeDir, source); err != nil {
			return nil, nil, err
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
			return nil, nil, fmt.Errorf("link %s is not tracked", source)
		}
		next.Links = links
		next.IgnoredLinks = append(next.IgnoredLinks, source)
		sortResources(&next)
		prepared, err := p.prepareResourcesGeneration(saved, next)
		if err != nil {
			return nil, nil, err
		}
		return prepared, []string{"link:" + source}, nil
	}
	return nil, nil, fmt.Errorf("unknown resource reference %q", ref)
}

func (p Provider) prepareResourcesGeneration(saved, next profile.Resources) (*PreparedCapture, error) {
	prepared, err := prepareCapture(filepath.Join(p.ProfileDir, "resources"), next, Diff(saved, next))
	if err != nil {
		return nil, err
	}
	if err := copyGeneration(filepath.Join(p.ProfileDir, "resources"), prepared.stage); err != nil {
		_ = prepared.Rollback()
		return nil, err
	}
	body, err := profile.MarshalResources(next)
	if err != nil {
		_ = prepared.Rollback()
		return nil, err
	}
	if err := os.WriteFile(prepared.stagePath("resources.toml"), body, 0o644); err != nil {
		_ = prepared.Rollback()
		return nil, err
	}
	prepared.State = next
	return prepared, nil
}

func stageUntrackedFile(source, destination string) (SnapshotScan, error) {
	if _, err := StageCopyResource(source, destination); err != nil {
		return SnapshotScan{}, err
	}
	info, err := os.Stat(destination)
	if err != nil {
		return SnapshotScan{}, err
	}
	if info.Size() > MaxGitUntrackedFileSize {
		return SnapshotScan{}, fmt.Errorf("untracked file exceeds %d bytes", MaxGitUntrackedFileSize)
	}
	result, err := sensitive.ScanRegularFile(destination, MaxGitUntrackedFileSize)
	if err != nil {
		return SnapshotScan{}, err
	}
	if result.Sensitive {
		return SnapshotScan{}, fmt.Errorf("sensitive untracked content")
	}
	hash, err := content.HashRegularFile(destination)
	if err != nil {
		return SnapshotScan{}, err
	}
	return SnapshotScan{Hash: hash, Mode: info.Mode().Perm()}, nil
}

func copyGeneration(source, destination string) error {
	for _, name := range captureComponents {
		if err := copyGenerationPath(filepath.Join(source, name), filepath.Join(destination, name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func copyGenerationPath(source, destination string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err := os.MkdirAll(destination, info.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(source)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := copyGenerationPath(filepath.Join(source, entry.Name()), filepath.Join(destination, entry.Name())); err != nil {
				return err
			}
		}
		return os.Chmod(destination, info.Mode().Perm())
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("invalid resource generation entry: %s", source)
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func (p Provider) EnableLink(saved profile.Resources, source string) (profile.Resources, error) {
	abs, err := ExpandHomePath(p.HomeDir, source)
	if err != nil {
		return saved, err
	}
	roots, err := p.resourceRoots(saved.Items)
	if err != nil {
		return saved, err
	}
	candidate, err := classifyLink(p.HomeDir, abs, saved.Items, roots, "", p.Ownership)
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
	detection, err := p.DetectDetailed(ctx, saved)
	return detection.Resources, detection.Links, err
}

// DetectDetailed returns current state plus non-persisted Git working details.
func (p Provider) DetectDetailed(ctx context.Context, saved profile.Resources) (Detection, error) {
	current := cloneResources(saved)
	detection := Detection{Resources: current, Git: map[string]GitWorkingSummary{}}
	rawLinks := make(map[string][]RawLink)
	for i := range current.Items {
		item := &current.Items[i]
		root, err := p.resourceRoot(*item)
		if err != nil {
			return Detection{}, err
		}
		if item.Strategy == "copy" {
			scan, err := ScanCopyResourceWithOptions(root, SnapshotOptions{ExcludeGitAdmin: p.isGitWorktreeRoot(ctx, root)})
			if err != nil {
				item.Hash = ""
				continue
			}
			item.Hash, item.Mode = scan.Hash, fmt.Sprintf("%04o", scan.Mode.Perm())
			rawLinks[item.ID] = scan.Links
		} else {
			if p.Runner == nil {
				return Detection{}, errors.New("command runner is required to detect Git resources")
			}
			git, isGit, err := InspectGitWorkingState(ctx, p.Runner, root)
			if err != nil || !isGit {
				item.Revision = ""
				continue
			}
			item.Remote, item.Branch, item.Revision = git.Remote, git.Branch, git.Revision
			item.Dirty = git.StagedTracked > 0 || git.UnstagedTracked > 0 || len(git.Untracked) > 0
			summary := GitWorkingSummary{StagedTracked: git.StagedTracked, UnstagedTracked: git.UnstagedTracked, Untracked: append([]string(nil), git.Untracked...)}
			if item.Strategy == "git+diff" {
				selected := make([]string, 0, len(item.Untracked))
				for _, file := range item.Untracked {
					if containsString(git.Untracked, file.Path) {
						selected = append(selected, file.Path)
					}
				}
				capture, err := calculateGitWorkingStateOverlay(ctx, p.Runner, root, git, selected)
				if err != nil {
					return Detection{}, fmt.Errorf("inspect Git state for %s: %w", item.ID, err)
				}
				item.IndexPatchHash, item.WorktreePatchHash, item.Untracked = capture.IndexPatchHash, capture.WorktreePatchHash, capture.Untracked
				summary.SelectedUntracked = selected
			}
			detection.Git[item.ID] = summary
		}
	}
	roots, err := p.resourceRoots(current.Items)
	if err != nil {
		return Detection{}, err
	}
	links, err := discoverLinks(p.HomeDir, p.roots(), current.Items, roots, p.Ownership, current.IgnoredLinks)
	if err != nil {
		return Detection{}, err
	}
	current.Links = nil
	for _, item := range current.Items {
		if item.Strategy != "copy" || currentItemMissing(item) {
			continue
		}
		internal, err := classifyResourceLinks(p.HomeDir, item, rawLinks[item.ID], current.Items, roots)
		if err != nil {
			return Detection{}, err
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
	detection.Resources, detection.Links = current, links
	return detection, nil
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

// resourceRoot resolves a live Resource root without changing its portable
// profile path. An unset context retains ExpandHomePath's existing semantics.
func (p Provider) resourceRoot(item profile.Resource) (string, error) {
	paths := p.ResourcePaths
	if paths.Home == "" {
		paths.Home = p.HomeDir
	}
	if len(paths.Overrides) == 0 {
		return ExpandHomePath(p.HomeDir, item.Path)
	}
	return ResolveResourcePath(paths, item)
}

func (p Provider) resourceRoots(items []profile.Resource) (map[string]string, error) {
	roots := make(map[string]string, len(items))
	for _, item := range items {
		root, err := p.resourceRoot(item)
		if err != nil {
			return nil, err
		}
		roots[item.ID] = root
	}
	return roots, nil
}
func resourceLink(link LinkCandidate, origin string) profile.ResourceLink {
	return profile.ResourceLink{SourceResource: link.SourceResource, Source: link.Source, TargetResource: link.TargetResource, Target: link.TargetRelative, Origin: origin}
}
func cloneResources(in profile.Resources) profile.Resources {
	items := append([]profile.Resource(nil), in.Items...)
	for i := range items {
		items[i].Untracked = append([]profile.GitUntrackedFile(nil), items[i].Untracked...)
	}
	return profile.Resources{Items: items, Links: append([]profile.ResourceLink(nil), in.Links...), IgnoredLinks: append([]string(nil), in.IgnoredLinks...)}
}

func (p Provider) isGitWorktreeRoot(ctx context.Context, root string) bool {
	if p.Runner == nil {
		return false
	}
	top, err := p.Runner.Run(ctx, "git", "-C", root, "rev-parse", "--show-toplevel")
	return err == nil && filepath.Clean(strings.TrimSpace(top)) == filepath.Clean(root)
}

func normalizeUntrackedPath(raw string) (string, error) {
	clean := pathpkg.Clean(strings.ReplaceAll(raw, "\\", "/"))
	if raw == "" || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || pathpkg.IsAbs(clean) || strings.Contains("/"+clean+"/", "/.git/") {
		return "", fmt.Errorf("invalid untracked path %q", raw)
	}
	return clean, nil
}

func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
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
