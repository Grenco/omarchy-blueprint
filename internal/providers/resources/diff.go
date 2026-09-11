package resources

import (
	"context"
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/content"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func Diff(saved, current profile.Resources) []model.Change {
	changes := []model.Change{}
	savedItems, currentItems := resourceMap(saved.Items), resourceMap(current.Items)
	for id, item := range currentItems {
		desired, ok := savedItems[id]
		if !ok {
			summary := "+ resource " + id
			if item.Dirty {
				summary += "; local changes are not captured"
			}
			changes = append(changes, resourceChange(model.ChangeAdd, "resource", id, summary))
			continue
		}
		delete(savedItems, id)
		if !resourceSatisfied(desired, item) {
			changes = append(changes, resourceDiffChanges(desired, item)...)
		}
	}
	for id := range savedItems {
		changes = append(changes, resourceChange(model.ChangeRemove, "resource", id, "- resource "+id))
	}
	savedLinks, currentLinks := linkMap(saved.Links), linkMap(current.Links)
	for key, link := range currentLinks {
		desired, ok := savedLinks[key]
		if !ok {
			changes = append(changes, resourceChange(model.ChangeAdd, "link", key, "+ link "+link.Source))
			continue
		}
		delete(savedLinks, key)
		if desired.TargetResource != link.TargetResource || desired.Target != link.Target {
			changes = append(changes, resourceChange(model.ChangeModify, "link", key, "~ link "+link.Source+" differs"))
		}
	}
	for key, link := range savedLinks {
		changes = append(changes, resourceChange(model.ChangeRemove, "link", key, "- link "+link.Source))
	}
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].Kind == changes[j].Kind {
			return changes[i].Name < changes[j].Name
		}
		return changes[i].Kind < changes[j].Kind
	})
	return changes
}

func Verify(saved, current profile.Resources) model.VerificationResult {
	missing := []string{}
	items := resourceMap(current.Items)
	for _, item := range saved.Items {
		if current, ok := items[item.ID]; !ok || !resourceSatisfied(item, current) {
			missing = append(missing, "resource:"+item.ID)
		}
	}
	links := linkMap(current.Links)
	for _, link := range saved.Links {
		if current, ok := links[linkKey(link)]; !ok || current.TargetResource != link.TargetResource || current.Target != link.Target {
			missing = append(missing, "link:"+link.Source)
		}
	}
	sort.Strings(missing)
	return model.VerificationResult{OK: len(missing) == 0, Missing: missing}
}

func (p Provider) Check(ctx context.Context, saved profile.Resources) error {
	if err := validateMetadata(p.HomeDir, saved); err != nil {
		return err
	}
	for _, item := range saved.Items {
		if item.Strategy != "copy" {
			continue
		}
		if item.Hash == "" || item.Mode == "" {
			return fmt.Errorf("copy resource %s has incomplete metadata", item.ID)
		}
		snapshot := resourceSnapshotPath(filepath.Join(p.ProfileDir, "resources"), item)
		if err := ValidateSnapshotTree(snapshot, item.Hash); err != nil {
			return fmt.Errorf("resource snapshot %q: %w", item.ID, err)
		}
	}
	for _, item := range saved.Items {
		if item.Strategy != "git+diff" {
			continue
		}
		if err := validateGitStateMetadata(item, true); err != nil {
			return err
		}
		stateRoot := filepath.Join(p.ProfileDir, "resources", "git-state", item.ID)
		if err := validateGitStateArtifact(filepath.Join(stateRoot, "index.patch"), item.IndexPatchHash); err != nil {
			return fmt.Errorf("Git state index patch %q: %w", item.ID, err)
		}
		if err := validateGitStateArtifact(filepath.Join(stateRoot, "worktree.patch"), item.WorktreePatchHash); err != nil {
			return fmt.Errorf("Git state worktree patch %q: %w", item.ID, err)
		}
		for _, file := range item.Untracked {
			artifact := filepath.Join(stateRoot, "untracked", filepath.FromSlash(file.Path))
			if err := validateGitStateArtifact(artifact, file.Hash); err != nil {
				return fmt.Errorf("Git state untracked file %q (%s): %w", item.ID, file.Path, err)
			}
		}
	}
	for _, item := range saved.Items {
		if item.Strategy == "git" || item.Strategy == "git+diff" {
			if p.Runner == nil {
				return fmt.Errorf("Git resource %s requires a command runner", item.ID)
			}
			if _, err := p.Runner.Run(ctx, "git", "--version"); err != nil {
				return fmt.Errorf("Git is required for resource %s: %w", item.ID, err)
			}
			break
		}
	}
	for _, item := range saved.Items {
		if (item.Strategy == "git" || item.Strategy == "git+diff") && isGitHubRepo(item.Remote) {
			if _, err := p.Runner.Run(ctx, "gh", "--version"); err != nil {
				return fmt.Errorf("GitHub CLI is required for resource %s: %w", item.ID, err)
			}
			break
		}
	}
	return nil
}

func validateMetadata(home string, resources profile.Resources) error {
	if home == "" {
		return fmt.Errorf("home directory is required")
	}
	ids, roots, sources := map[string]bool{}, []string{}, map[string]bool{}
	for _, item := range resources.Items {
		if err := ValidateResourceID(item.ID); err != nil {
			return err
		}
		if ids[item.ID] {
			return fmt.Errorf("duplicate resource id %q", item.ID)
		}
		ids[item.ID] = true
		root, err := ExpandHomePath(home, item.Path)
		if err != nil {
			return err
		}
		for _, existing := range roots {
			if PathsOverlap(existing, root) {
				return fmt.Errorf("resource roots overlap: %s", item.Path)
			}
		}
		roots = append(roots, root)
		if item.Kind != "file" && item.Kind != "directory" {
			return fmt.Errorf("resource %s has invalid kind", item.ID)
		}
		switch item.Strategy {
		case "copy":
			if item.Remote != "" || item.Revision != "" {
				return fmt.Errorf("copy resource %s has invalid metadata", item.ID)
			}
		case "git":
			if item.Kind != "directory" || item.Hash != "" || item.Mode != "" || !validGitRevision(item.Revision) {
				return fmt.Errorf("Git resource %s has invalid metadata", item.ID)
			}
			if _, err := PortableGitRemote(item.Remote); err != nil {
				return err
			}
		case "git+diff":
			if item.Kind != "directory" || item.Hash != "" || item.Mode != "" || !validGitRevision(item.Revision) {
				return fmt.Errorf("Git resource %s has invalid metadata", item.ID)
			}
			if _, err := PortableGitRemote(item.Remote); err != nil {
				return err
			}
			if err := validateGitStateMetadata(item, false); err != nil {
				return err
			}
		default:
			return fmt.Errorf("resource %s has invalid strategy", item.ID)
		}
	}
	for _, link := range resources.Links {
		key := linkKey(link)
		if sources[key] {
			return fmt.Errorf("duplicate link source %q", link.Source)
		}
		sources[key] = true
		if link.SourceResource == "" {
			if _, err := ExpandHomePath(home, link.Source); err != nil {
				return err
			}
		} else if !SafeRelativeResourcePath(link.Source) || !ids[link.SourceResource] {
			return fmt.Errorf("invalid source resource for link %q", link.Source)
		}
		if !ids[link.TargetResource] || !SafeRelativeResourcePath(link.Target) {
			return fmt.Errorf("invalid target for link %q", link.Source)
		}
		if link.Origin != "inbound" && link.Origin != "resource" {
			return fmt.Errorf("invalid link origin %q", link.Origin)
		}
	}
	for _, ignored := range resources.IgnoredLinks {
		if _, err := ExpandHomePath(home, ignored); err != nil {
			return err
		}
	}
	return nil
}

func validateGitStateMetadata(item profile.Resource, requireContent bool) error {
	for _, hash := range []string{item.IndexPatchHash, item.WorktreePatchHash} {
		if hash != "" && !validSHA256(hash) {
			return fmt.Errorf("Git resource %s has invalid patch hash", item.ID)
		}
	}
	seen := map[string]bool{}
	for _, file := range item.Untracked {
		if !safeGitStatePath(file.Path) || seen[file.Path] || (requireContent && !validSHA256(file.Hash)) || (!requireContent && file.Hash != "" && !validSHA256(file.Hash)) {
			return fmt.Errorf("Git resource %s has invalid untracked metadata", item.ID)
		}
		seen[file.Path] = true
		if !requireContent && file.Mode == "" {
			continue
		}
		mode, err := strconv.ParseUint(file.Mode, 8, 16)
		if err != nil || mode > 0o777 {
			return fmt.Errorf("Git resource %s has invalid untracked mode", item.ID)
		}
	}
	return nil
}

func validateGitStateArtifact(filename, expected string) error {
	_, err := os.Lstat(filename)
	if expected == "" {
		if err == nil {
			return fmt.Errorf("unexpected artifact")
		}
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err != nil {
		return err
	}
	got, err := content.HashRegularFile(filename)
	if err != nil {
		return err
	}
	if got != expected {
		return fmt.Errorf("hash mismatch")
	}
	return nil
}

func safeGitStatePath(value string) bool {
	clean := pathpkg.Clean(value)
	return value != "" && clean == value && clean != "." && clean != ".." && !strings.HasPrefix(clean, "../") && !pathpkg.IsAbs(clean) && !strings.Contains("/"+clean+"/", "/.git/")
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, c := range value {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

func resourceMap(items []profile.Resource) map[string]profile.Resource {
	result := make(map[string]profile.Resource, len(items))
	for _, item := range items {
		result[item.ID] = item
	}
	return result
}

func resourceSatisfied(saved, current profile.Resource) bool {
	if saved.ID != current.ID || saved.Path != current.Path || saved.Kind != current.Kind || saved.Strategy != current.Strategy {
		return false
	}
	switch saved.Strategy {
	case "git":
		return saved.Remote == current.Remote && saved.Revision == current.Revision
	case "git+diff":
		return saved.Remote == current.Remote && saved.Revision == current.Revision &&
			saved.IndexPatchHash == current.IndexPatchHash && saved.WorktreePatchHash == current.WorktreePatchHash &&
			equalUntracked(saved.Untracked, current.Untracked)
	case "copy":
		return saved.Hash == current.Hash && saved.Mode == current.Mode
	default:
		return false
	}
}

func equalUntracked(a, b []profile.GitUntrackedFile) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func resourceDiffChanges(saved, current profile.Resource) []model.Change {
	if saved.Strategy != "git+diff" {
		return []model.Change{resourceChange(model.ChangeModify, "resource", current.ID, "~ resource "+current.ID+" differs")}
	}
	changes := []model.Change{}
	if saved.Remote != current.Remote || saved.Revision != current.Revision {
		changes = append(changes, resourceChange(model.ChangeModify, "resource", current.ID, "~ resource "+current.ID+" differs"))
	}
	if saved.IndexPatchHash != current.IndexPatchHash {
		changes = append(changes, resourceChange(model.ChangeModify, "resource", current.ID, "~ resource "+current.ID+" staged Git state differs"))
	}
	if saved.WorktreePatchHash != current.WorktreePatchHash {
		changes = append(changes, resourceChange(model.ChangeModify, "resource", current.ID, "~ resource "+current.ID+" unstaged Git state differs"))
	}
	currentFiles := map[string]profile.GitUntrackedFile{}
	for _, file := range current.Untracked {
		currentFiles[file.Path] = file
	}
	for _, file := range saved.Untracked {
		got, ok := currentFiles[file.Path]
		if !ok {
			changes = append(changes, resourceChange(model.ChangeRemove, "resource", current.ID, "- resource "+current.ID+" untracked:"+file.Path+" missing"))
		} else if got != file {
			changes = append(changes, resourceChange(model.ChangeModify, "resource", current.ID, "~ resource "+current.ID+" untracked:"+file.Path+" differs"))
		}
	}
	return changes
}
func linkKey(link profile.ResourceLink) string {
	if link.SourceResource != "" {
		return "resource:" + link.SourceResource + ":" + link.Source
	}
	return "home:" + link.Source
}
func linkMap(links []profile.ResourceLink) map[string]profile.ResourceLink {
	result := make(map[string]profile.ResourceLink, len(links))
	for _, link := range links {
		result[linkKey(link)] = link
	}
	return result
}
func resourceChange(kind model.ChangeType, resourceKind, name, summary string) model.Change {
	return model.Change{Type: kind, Provider: "resources", Kind: resourceKind, Name: name, Summary: summary}
}
