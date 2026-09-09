package resources

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/content"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

type resourcePlanState struct {
	Satisfied           bool
	ReadyOpID, Conflict string
}
type PlanOptions struct{ Force bool }

func (p Provider) Plan(_ context.Context, saved, current profile.Resources, schema int, from, to string, options ...PlanOptions) (model.RestorePlan, error) {
	force := len(options) > 0 && options[0].Force
	plan := model.RestorePlan{ProfileVersion: schema, OmarchyFrom: from, OmarchyTo: to}
	currentItems := resourceMap(current.Items)
	states := map[string]resourcePlanState{}
	items := append([]profile.Resource(nil), saved.Items...)
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	for _, item := range items {
		state, ops, skip, err := p.planResource(item, currentItems[item.ID])
		if err != nil {
			return model.RestorePlan{}, err
		}
		states[item.ID] = state
		plan.Operations = append(plan.Operations, ops...)
		if skip != "" {
			plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "resources", Resource: "resource:" + item.ID, Reason: skip})
		}
	}
	links := append([]profile.ResourceLink(nil), saved.Links...)
	sort.Slice(links, func(i, j int) bool { return linkKey(links[i]) < linkKey(links[j]) })
	currentLinks := linkMap(current.Links)
	for _, link := range links {
		if existing, ok := currentLinks[linkKey(link)]; ok && existing.TargetResource == link.TargetResource && existing.Target == link.Target {
			continue
		}
		targetState := states[link.TargetResource]
		if !targetState.Satisfied {
			plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "resources", Resource: "link:" + link.Source, Reason: "target resource " + link.TargetResource + " is not satisfied"})
			continue
		}
		var sourcePath string
		var deps []string
		if link.SourceResource == "" {
			var err error
			sourcePath, err = ExpandHomePath(p.HomeDir, link.Source)
			if err != nil {
				return model.RestorePlan{}, err
			}
		} else {
			source := states[link.SourceResource]
			if !source.Satisfied {
				plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "resources", Resource: "link:" + link.Source, Reason: "source resource " + link.SourceResource + " is not satisfied"})
				continue
			}
			root, err := p.resourcePath(saved, link.SourceResource)
			if err != nil {
				return model.RestorePlan{}, err
			}
			sourcePath = filepath.Join(root, filepath.FromSlash(link.Source))
			if source.ReadyOpID != "" {
				deps = append(deps, source.ReadyOpID)
			}
		}
		targetRoot, err := p.resourcePath(saved, link.TargetResource)
		if err != nil {
			return model.RestorePlan{}, err
		}
		target := filepath.Join(targetRoot, filepath.FromSlash(link.Target))
		if info, err := os.Lstat(sourcePath); err == nil {
			if info.Mode()&os.ModeSymlink != 0 && equivalentLink(sourcePath, target) {
				continue
			}
			if force {
				precondition, err := filesystemPrecondition(sourcePath, info)
				if err != nil {
					return model.RestorePlan{}, err
				}
				raw, err := RelativeSymlinkTarget(sourcePath, target)
				if err != nil {
					return model.RestorePlan{}, err
				}
				if targetState.ReadyOpID != "" {
					deps = append(deps, targetState.ReadyOpID)
				}
				sort.Strings(deps)
				plan.Operations = append(plan.Operations, model.Operation{ID: "resources.link." + safeOperationID(linkKey(link)), Provider: "resources", Action: "replace resource link", Resource: "link:" + link.Source, Symlink: &model.SymlinkWrite{Destination: sourcePath, Target: raw, ReplaceExisting: true, ExpectedExisting: &precondition, Backup: true, RejectSymlinkParents: true}, DependsOn: deps, Risk: model.RiskHigh, Reversible: true})
				continue
			}
			plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "resources", Resource: "link:" + link.Source, Reason: "existing link destination differs; overwrite disabled"})
			continue
		} else if !os.IsNotExist(err) {
			return model.RestorePlan{}, err
		}
		raw, err := RelativeSymlinkTarget(sourcePath, target)
		if err != nil {
			return model.RestorePlan{}, err
		}
		if targetState.ReadyOpID != "" {
			deps = append(deps, targetState.ReadyOpID)
		}
		sort.Strings(deps)
		plan.Operations = append(plan.Operations, model.Operation{ID: "resources.link." + safeOperationID(linkKey(link)), Provider: "resources", Action: "symlink", Resource: "link:" + link.Source, Symlink: &model.SymlinkWrite{Destination: sourcePath, Target: raw, ExpectedMissing: true, RejectSymlinkParents: true}, DependsOn: deps, Risk: model.RiskLow})
	}
	return plan, nil
}

func filesystemPrecondition(path string, info os.FileInfo) (model.FilesystemPrecondition, error) {
	p := model.FilesystemPrecondition{Mode: uint32(info.Mode().Perm())}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err != nil {
			return p, err
		}
		p.Type, p.Target = "symlink", target
		return p, nil
	}
	if info.IsDir() {
		p.Type = "directory"
	} else if info.Mode().IsRegular() {
		p.Type = "file"
	} else {
		return p, fmt.Errorf("unsupported link destination: %s", path)
	}
	hash, err := content.HashFilesystemObject(path)
	p.Hash = hash
	return p, err
}

func (p Provider) planResource(saved, current profile.Resource) (resourcePlanState, []model.Operation, string, error) {
	path, err := ExpandHomePath(p.HomeDir, saved.Path)
	if err != nil {
		return resourcePlanState{}, nil, "", err
	}
	if resourceSatisfied(saved, current) {
		return resourcePlanState{Satisfied: true}, nil, "", nil
	}
	missing := current.ID == "" || (saved.Strategy == "copy" && current.Hash == "") || (saved.Strategy == "git" && current.Revision == "")
	if !missing {
		return resourcePlanState{Conflict: "existing resource differs"}, nil, "existing resource differs; overwrite disabled", nil
	}
	if _, err := os.Lstat(path); err == nil {
		return resourcePlanState{Conflict: "existing resource differs"}, nil, "existing resource differs; overwrite disabled", nil
	} else if !os.IsNotExist(err) {
		return resourcePlanState{}, nil, "", err
	}
	if saved.Strategy == "copy" {
		source := resourceSnapshotPath(filepath.Join(p.ProfileDir, "resources"), saved)
		if saved.Kind == "file" {
			mode, err := parseResourceMode(saved.Mode)
			if err != nil {
				return resourcePlanState{}, nil, "", err
			}
			id := "resources.file." + saved.ID
			return resourcePlanState{Satisfied: true, ReadyOpID: id}, []model.Operation{{ID: id, Provider: "resources", Action: "file", Resource: "resource:" + saved.ID, File: &model.FileWrite{Source: source, Destination: path, SourceHash: saved.Hash, ExpectedMissing: true, Mode: &mode, RejectSymlinkParents: true}, Risk: model.RiskLow}}, "", nil
		}
		id := "resources.copy." + saved.ID
		return resourcePlanState{Satisfied: true, ReadyOpID: id}, []model.Operation{{ID: id, Provider: "resources", Action: "copy", Resource: "resource:" + saved.ID, Copy: &model.Copy{Source: source, Destination: path, SourceHash: saved.Hash, RejectSymlinkParents: true}, Risk: model.RiskLow}}, "", nil
	}
	parent := filepath.Dir(path)
	var ops []model.Operation
	mkdir := ""
	if _, err := os.Lstat(parent); os.IsNotExist(err) {
		mkdir = "resources.mkdir." + saved.ID
		ops = append(ops, model.Operation{ID: mkdir, Provider: "resources", Action: "directory", Resource: "resource:" + saved.ID, Directory: &model.DirectoryCreate{Path: parent, Mode: 0o755, RejectSymlinkParents: true}, Risk: model.RiskLow})
	} else if err != nil {
		return resourcePlanState{}, nil, "", err
	}
	clone := "resources.git.clone." + saved.ID
	command := []string{"git", "clone", "--no-checkout", saved.Remote, path}
	if repo, ok := githubRepo(saved.Remote); ok {
		command = []string{"gh", "repo", "clone", strings.TrimPrefix(repo, "github.com/"), path, "--", "--no-checkout"}
	}
	ops = append(ops, model.Operation{ID: clone, Provider: "resources", Action: "git clone", Resource: "resource:" + saved.ID, Command: command, DependsOn: nonEmpty(mkdir), Risk: model.RiskLow})
	checkout := "resources.git.checkout." + saved.ID
	ops = append(ops, model.Operation{ID: checkout, Provider: "resources", Action: "git checkout", Resource: "resource:" + saved.ID, Command: []string{"git", "-C", path, "checkout", "--detach", saved.Revision}, DependsOn: []string{clone}, Risk: model.RiskLow})
	return resourcePlanState{Satisfied: true, ReadyOpID: checkout}, ops, "", nil
}
func (p Provider) resourcePath(resources profile.Resources, id string) (string, error) {
	for _, item := range resources.Items {
		if item.ID == id {
			return ExpandHomePath(p.HomeDir, item.Path)
		}
	}
	return "", fmt.Errorf("unknown resource %s", id)
}
func parseResourceMode(value string) (uint32, error) {
	parsed, err := strconv.ParseUint(value, 8, 32)
	if err != nil || parsed > 0o777 {
		return 0, fmt.Errorf("invalid resource mode %q", value)
	}
	return uint32(parsed), nil
}
func nonEmpty(value string) []string {
	if value == "" {
		return nil
	}
	return []string{value}
}
func safeOperationID(value string) string {
	out := make([]rune, 0, len(value))
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			out = append(out, r)
		} else {
			out = append(out, '-')
		}
	}
	return string(out)
}

func equivalentLink(source, expected string) bool {
	raw, err := os.Readlink(source)
	if err != nil {
		return false
	}
	actual := raw
	if !filepath.IsAbs(actual) {
		actual = filepath.Join(filepath.Dir(source), actual)
	}
	actual, err = filepath.EvalSymlinks(actual)
	if err != nil {
		return false
	}
	expected, err = filepath.EvalSymlinks(expected)
	return err == nil && filepath.Clean(actual) == filepath.Clean(expected)
}
