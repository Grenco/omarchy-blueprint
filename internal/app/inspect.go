package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Grenco/omarchy-blueprint/internal/content"
	"github.com/Grenco/omarchy-blueprint/internal/inspection"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	resourcesprovider "github.com/Grenco/omarchy-blueprint/internal/providers/resources"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func inspectCommand(deps Dependencies, opt *options) *cobra.Command {
	cmd := &cobra.Command{Use: "inspect", Short: "Inspect a path, config entry, or tracked resource"}
	cmd.AddCommand(inspectPathCommand(deps, opt), inspectConfigCommand(deps, opt), inspectResourceCommand(deps, opt))
	return cmd
}

func inspectPathCommand(deps Dependencies, opt *options) *cobra.Command {
	return &cobra.Command{Use: "path <path>", Args: cobra.ExactArgs(1), Short: "Inspect a filesystem path", RunE: func(cmd *cobra.Command, args []string) error {
		session, err := openWorkflow(deps, opt)
		if err != nil {
			return profileError(opt.profileDir, err)
		}
		result, err := session.InspectPath(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		return emit(deps.Out, opt.json, "inspect path", true, map[string]any{"inspection": result}, renderPathInspection(result))
	}}
}

func inspectConfigCommand(deps Dependencies, opt *options) *cobra.Command {
	return &cobra.Command{Use: "config <logical-path>", Args: cobra.ExactArgs(1), Short: "Inspect a configuration path", RunE: func(cmd *cobra.Command, args []string) error {
		session, err := openWorkflow(deps, opt)
		if err != nil {
			return profileError(opt.profileDir, err)
		}
		result, err := session.InspectConfig(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		return emit(deps.Out, opt.json, "inspect config", true, map[string]any{"inspection": result}, fmt.Sprintf("Config: %s\nClassification: %s\n", result.Candidate.Path, result.Candidate.Classification))
	}}
}

func inspectResourceCommand(deps Dependencies, opt *options) *cobra.Command {
	return &cobra.Command{Use: "resource <id>", Args: cobra.ExactArgs(1), Short: "Inspect a tracked resource", RunE: func(cmd *cobra.Command, args []string) error {
		session, err := openWorkflow(deps, opt)
		if err != nil {
			return profileError(opt.profileDir, err)
		}
		result, err := session.InspectResource(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		return emit(deps.Out, opt.json, "inspect resource", true, map[string]any{"inspection": result}, fmt.Sprintf("Resource: %s\nPath: %s\n", result.Resource.ID, result.EffectivePath))
	}}
}

func (p resourcesStateProvider) InspectPath(ctx context.Context, d profile.Data, raw string) (workflow.PathInspection, error) {
	path, err := filepath.Abs(raw)
	if err != nil {
		return workflow.PathInspection{}, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return workflow.PathInspection{}, err
	}
	result := workflow.PathInspection{Path: path, Mode: fmt.Sprintf("%04o", info.Mode().Perm()), Size: info.Size()}
	if info.Mode()&os.ModeSymlink != 0 {
		result.Type = "symlink"
		result.SymlinkTarget, _ = os.Readlink(path)
		result.BlockedReason = "symlinks must be tracked through their owning resource"
		return result, nil
	}
	if info.IsDir() {
		result.Type = "directory"
	} else if info.Mode().IsRegular() {
		result.Type = "file"
	} else {
		result.Type = "special"
		result.BlockedReason = "unsupported resource type"
		return result, nil
	}
	provider, err := p.provider(d)
	if err != nil {
		return workflow.PathInspection{}, err
	}
	for _, item := range d.Resources.Items {
		root, err := provider.ResourcePaths.Resolve(item)
		if err != nil {
			return workflow.PathInspection{}, err
		}
		if resourcesprovider.PathsOverlap(path, root) {
			result.ResourceID, result.ResourceDefault, result.ResourceEffective = item.ID, item.Path, root
			result.OwnershipProvider = "resources"
			return result, nil
		}
	}
	if conflicts := provider.Ownership.TrackConflict(path); len(conflicts) > 0 {
		result.OwnershipProvider = conflicts[0].Provider
		result.BlockedReason = "path is owned by " + conflicts[0].Provider
		return result, nil
	}
	if info.IsDir() && provider.Runner != nil {
		state, git, err := resourcesprovider.InspectGitWorkingState(ctx, provider.Runner, path)
		if err != nil {
			result.SuggestedStrategy, result.StrategyReason = "copy", "Git worktree does not meet portable Git policy"
			return result, nil
		}
		if git {
			result.Git = &workflow.GitInspection{Root: path, Remote: state.Remote, Revision: state.Revision, Branch: state.Branch, Staged: state.StagedTracked, Unstaged: state.UnstagedTracked, Untracked: len(state.Untracked)}
			dirty := state.StagedTracked > 0 || state.UnstagedTracked > 0 || len(state.Untracked) > 0 || state.Conflicted || state.DirtySubmodule
			if dirty {
				result.SuggestedStrategy, result.StrategyReason = "git+diff", "portable Git worktree has local changes"
			} else {
				result.SuggestedStrategy, result.StrategyReason = "git", "portable Git worktree is clean"
			}
			return result, nil
		}
	}
	result.SuggestedStrategy, result.StrategyReason = "copy", "path is not a portable Git worktree"
	return result, nil
}

func (p resourcesStateProvider) InspectResource(ctx context.Context, d profile.Data, id string) (workflow.ResourceInspection, error) {
	item, ok := resourceByID(d.Resources.Items, strings.TrimPrefix(id, "resource:"))
	if !ok {
		return workflow.ResourceInspection{}, fmt.Errorf("resource %q is not tracked", id)
	}
	provider, err := p.provider(d)
	if err != nil {
		return workflow.ResourceInspection{}, err
	}
	effective, err := provider.ResourcePaths.Resolve(item)
	if err != nil {
		return workflow.ResourceInspection{}, err
	}
	result := workflow.ResourceInspection{Resource: item, DefaultPath: item.Path, EffectivePath: effective, Machine: p.machineName(d)}
	if (item.Strategy == "git" || item.Strategy == "git+diff") && provider.Runner != nil {
		state, ok, err := resourcesprovider.InspectGitWorkingState(ctx, provider.Runner, effective)
		if err != nil {
			return workflow.ResourceInspection{}, err
		}
		if ok {
			result.Git = &resourcesprovider.GitWorkingSummary{StagedTracked: state.StagedTracked, UnstagedTracked: state.UnstagedTracked, Untracked: state.Untracked}
		}
	}
	return result, nil
}

func (p resourcesStateProvider) machineName(d profile.Data) string {
	context, err := resolveMachineContext(p.deps, p.opt, d)
	if err != nil {
		return ""
	}
	return context.Selection.Name
}

func (p configStateProvider) InspectConfig(_ context.Context, d profile.Data, logical string) (workflow.ConfigInspection, error) {
	provider, err := p.provider(d)
	if err != nil {
		return workflow.ConfigInspection{}, err
	}
	logical = filepath.ToSlash(filepath.Clean(logical))
	if !strings.HasPrefix(logical, ".config/") && logical != ".config" {
		return workflow.ConfigInspection{}, fmt.Errorf("config path must start with .config/: %s", logical)
	}
	scan, err := provider.Scan(d.Config)
	if err != nil {
		return workflow.ConfigInspection{}, err
	}
	for _, candidate := range scan.Candidates {
		if candidate.Path == logical {
			livePath := filepath.Join(filepath.Dir(provider.UserRoot), filepath.FromSlash(logical))
			baselinePath := filepath.Join(provider.BaselineRoot, filepath.FromSlash(strings.TrimPrefix(logical, ".config/")))
			profilePath := filepath.Join(provider.ProfileDir, "config", "files", filepath.FromSlash(logical))
			managed := configManaged(d.Config, logical)
			result := workflow.ConfigInspection{Candidate: candidate, LivePath: livePath, BaselinePath: baselinePath, ProfilePath: profilePath, Managed: managed}
			result.BaselineToLive = configDiff("baseline/"+logical, baselinePath, "live/"+logical, livePath)
			if managed {
				result.ProfileToLive = configDiff("profile/"+logical, profilePath, "live/"+logical, livePath)
			}
			return result, nil
		}
	}
	return workflow.ConfigInspection{}, fmt.Errorf("config path %q was not found", logical)
}

// configDiff reads only regular files and caps each side before delegating
// classification, credential detection, and rendering safety to BuildTextDiff.
func configDiff(oldLabel, oldPath, newLabel, newPath string) *inspection.DiffDocument {
	old, oldExists, err := readConfigPreview(oldPath)
	if err != nil {
		return unavailableConfigDiff(oldLabel, newLabel, "old content is unavailable")
	}
	new, newExists, err := readConfigPreview(newPath)
	if err != nil {
		return unavailableConfigDiff(oldLabel, newLabel, "new content is unavailable")
	}
	if !oldExists {
		old = nil
	}
	if !newExists {
		new = nil
	}
	doc := inspection.BuildTextDiff(oldLabel, old, newLabel, new)
	return &doc
}

func readConfigPreview(path string) ([]byte, bool, error) {
	f, _, err := content.OpenRegularFile(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	preview, err := io.ReadAll(io.LimitReader(f, inspection.MaxPreviewBytes+1))
	return preview, true, err
}

func unavailableConfigDiff(oldLabel, newLabel, reason string) *inspection.DiffDocument {
	return &inspection.DiffDocument{Kind: inspection.DiffUnavailable, OldLabel: oldLabel, NewLabel: newLabel, Metadata: []inspection.DiffFact{{Key: "reason", Value: reason}}}
}

func configManaged(config profile.Configs, logical string) bool {
	for _, file := range config.Files {
		if file.Path == logical {
			return true
		}
	}
	for _, deletion := range config.Deletes {
		if deletion.Path == logical {
			return true
		}
	}
	return false
}

func renderPathInspection(result workflow.PathInspection) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Path: %s\nType: %s\n", result.Path, result.Type)
	if result.OwnershipProvider != "" {
		fmt.Fprintf(&b, "Owner: %s\n", result.OwnershipProvider)
	}
	if result.SuggestedStrategy != "" {
		fmt.Fprintf(&b, "Suggested strategy: %s\n", result.SuggestedStrategy)
	}
	if result.BlockedReason != "" {
		fmt.Fprintf(&b, "Blocked: %s\n", result.BlockedReason)
	}
	return b.String()
}
