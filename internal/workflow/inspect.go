package workflow

import (
	"context"
	"fmt"

	"github.com/Grenco/omarchy-blueprint/internal/inspection"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	configprovider "github.com/Grenco/omarchy-blueprint/internal/providers/config"
	resourcesprovider "github.com/Grenco/omarchy-blueprint/internal/providers/resources"
)

type GitInspection struct {
	Root           string   `json:"root"`
	Remote         string   `json:"remote,omitempty"`
	Revision       string   `json:"revision,omitempty"`
	Branch         string   `json:"branch,omitempty"`
	Staged         int      `json:"staged"`
	Unstaged       int      `json:"unstaged"`
	Untracked      int      `json:"untracked"`
	UntrackedPaths []string `json:"untracked_paths,omitempty"`
}

type PathInspection struct {
	Path              string         `json:"path"`
	Type              string         `json:"type"`
	Mode              string         `json:"mode,omitempty"`
	Size              int64          `json:"size,omitempty"`
	SymlinkTarget     string         `json:"symlink_target,omitempty"`
	OwnershipProvider string         `json:"ownership_provider,omitempty"`
	ResourceID        string         `json:"resource_id,omitempty"`
	ResourceDefault   string         `json:"resource_default_path,omitempty"`
	ResourceEffective string         `json:"resource_effective_path,omitempty"`
	Git               *GitInspection `json:"git,omitempty"`
	SuggestedStrategy string         `json:"suggested_strategy,omitempty"`
	StrategyReason    string         `json:"strategy_reason,omitempty"`
	BlockedReason     string         `json:"blocked_reason,omitempty"`
}

type ConfigInspection struct {
	Candidate      configprovider.Candidate `json:"candidate"`
	LivePath       string                   `json:"live_path,omitempty"`
	BaselinePath   string                   `json:"baseline_path,omitempty"`
	ProfilePath    string                   `json:"profile_path,omitempty"`
	Managed        bool                     `json:"managed"`
	BaselineToLive *inspection.DiffDocument `json:"baseline_to_live,omitempty"`
	ProfileToLive  *inspection.DiffDocument `json:"profile_to_live,omitempty"`
}

type ResourceInspection struct {
	Resource      profile.Resource                     `json:"resource"`
	DefaultPath   string                               `json:"default_path"`
	EffectivePath string                               `json:"effective_path"`
	Machine       string                               `json:"machine,omitempty"`
	Git           *resourcesprovider.GitWorkingSummary `json:"git,omitempty"`
}

type pathInspectionProvider interface {
	InspectPath(context.Context, profile.Data, string) (PathInspection, error)
}

type configInspectionProvider interface {
	InspectConfig(context.Context, profile.Data, string) (ConfigInspection, error)
}

type resourceInspectionProvider interface {
	InspectResource(context.Context, profile.Data, string) (ResourceInspection, error)
}

func (s *Session) InspectPath(ctx context.Context, path string) (PathInspection, error) {
	provider, ok := ProviderByID(s.providers, "resources")
	if !ok {
		return PathInspection{}, fmt.Errorf("resources inspection is unavailable")
	}
	inspector, ok := provider.(pathInspectionProvider)
	if !ok {
		return PathInspection{}, fmt.Errorf("resources inspection is unavailable")
	}
	return inspector.InspectPath(ctx, s.profile, path)
}

func (s *Session) InspectConfig(ctx context.Context, logical string) (ConfigInspection, error) {
	provider, ok := ProviderByID(s.providers, "config")
	if !ok {
		return ConfigInspection{}, fmt.Errorf("config inspection is unavailable")
	}
	inspector, ok := provider.(configInspectionProvider)
	if !ok {
		return ConfigInspection{}, fmt.Errorf("config inspection is unavailable")
	}
	return inspector.InspectConfig(ctx, s.profile, logical)
}

// SetConfigPolicy changes one explicit Config discovery policy, saves it, and
// reloads the session so subsequent scans always use the persisted state.
func (s *Session) SetConfigPolicy(ctx context.Context, logical string, policy string) error {
	_ = ctx
	var err error
	switch policy {
	case "include":
		s.profile.Config, _, err = configprovider.RemoveExclusion(s.profile.Config, logical)
		if err == nil {
			s.profile.Config, _, err = configprovider.AddInclusion(s.profile.Config, logical)
		}
	case "exclude":
		s.profile.Config, _, err = configprovider.AddExclusion(s.profile.Config, logical)
	case "auto":
		s.profile.Config, _, err = configprovider.ClearPolicy(s.profile.Config, logical)
	default:
		return fmt.Errorf("invalid config policy %q; want auto, include, or exclude", policy)
	}
	if err != nil {
		return err
	}
	if s.deps.Now != nil {
		s.profile.Manifest.Profile.UpdatedAt = s.deps.Now().UTC()
	}
	if err := profile.Save(s.opts.ProfileDir, s.profile); err != nil {
		return fmt.Errorf("save profile: %w", err)
	}
	return s.Reload()
}

func (s *Session) InspectResource(ctx context.Context, id string) (ResourceInspection, error) {
	provider, ok := ProviderByID(s.providers, "resources")
	if !ok {
		return ResourceInspection{}, fmt.Errorf("resources inspection is unavailable")
	}
	inspector, ok := provider.(resourceInspectionProvider)
	if !ok {
		return ResourceInspection{}, fmt.Errorf("resources inspection is unavailable")
	}
	return inspector.InspectResource(ctx, s.profile, id)
}
