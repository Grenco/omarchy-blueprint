package workflow

import (
	"context"
	"errors"
	"fmt"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/profilegit"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
	"os"
	"sort"
)

// SyncProfileView is private-lifetime historical evidence, never JSON output or
// an execution plan. Its Root is an isolated materialization, not the checkout.
type SyncProfileView struct {
	Revision string
	Root     string
	Profile  profile.Data
	Targets  []syncengine.DesiredTarget
	Control  SyncControlState
}

func (s *Session) loadSyncProfileView(ctx context.Context, git profilegit.Service, sha string) (SyncProfileView, func(), error) {
	s.configMu.Lock()
	providers := append([]Provider(nil), s.providers...)
	name := s.machine.Name
	s.configMu.Unlock()
	return loadSyncView(ctx, git, sha, providers, name)
}

// SyncViewStage names the step that rejected a pinned revision, so a blocker
// can say whether the snapshot, profile files, policy, provider artifacts,
// semantic projection or control state is at fault.
type SyncViewStage string

const (
	SyncViewSnapshot   SyncViewStage = "snapshot"
	SyncViewProfile    SyncViewStage = "profile"
	SyncViewPolicy     SyncViewStage = "policy"
	SyncViewArtifacts  SyncViewStage = "artifacts"
	SyncViewProjection SyncViewStage = "projection"
	SyncViewControl    SyncViewStage = "control"
)

// SyncViewError is privacy-safe by construction: a stage, a provider category
// and a provider-owned target identity, never a wrapped provider error, path
// or file content.
type SyncViewError struct {
	Stage    SyncViewStage
	Category string
	Target   string
	// Missing reports a committed artifact absent from the snapshot.
	Missing bool
}

func (e *SyncViewError) Error() string {
	return "sync: historical profile " + string(e.Stage) + " check failed"
}

func syncViewFailure(ctx context.Context, stage SyncViewStage, category string, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	result := &SyncViewError{Stage: stage, Category: category}
	var artifact *syncengine.ArtifactError
	if errors.As(err, &artifact) {
		result.Target, result.Missing = artifact.Target, artifact.Missing
	}
	return result
}

// syncViewBlocker turns a load failure into the historical-profile blocker
// with a stage-specific, actionable message. which names the revision.
func syncViewBlocker(which string, err error) syncengine.Blocker {
	message := which + " profile cannot be loaded safely"
	var view *SyncViewError
	if errors.As(err, &view) {
		category := view.Category
		switch view.Stage {
		case SyncViewSnapshot:
			message = which + " revision could not be read from local Git objects"
		case SyncViewProfile:
			message = which + " profile files are invalid or unsupported at that revision"
		case SyncViewPolicy:
			message = which + " profile policy names targets no provider recognizes"
		case SyncViewArtifacts:
			switch {
			case view.Missing:
				// Re-capturing would record the same uncommittable artifact again.
				message = fmt.Sprintf("%s profile records %s %s but its committed artifact is missing (Git cannot store an empty directory). Stop managing %s, set its %s Capture policy to preserve so Capture does not re-add it, then commit and publish", which, category, view.Target, view.Target, category)
			case view.Target != "":
				message = fmt.Sprintf("%s profile's committed %s artifact for %s does not match its recorded metadata. Re-capture %s on the machine that owns it, then commit", which, category, view.Target, category)
			default:
				message = fmt.Sprintf("%s profile's committed %s artifacts do not match their recorded metadata. Re-capture %s, then commit", which, category, category)
			}
		case SyncViewProjection:
			message = fmt.Sprintf("%s profile's %s targets cannot be projected for Sync", which, valueOrUnknown(category))
		case SyncViewControl:
			message = which + " profile's machine and policy control state cannot be read"
		}
	}
	return syncengine.Blocker{Code: "historical-profile", Message: message}
}

func valueOrUnknown(value string) string {
	if value == "" {
		return "provider"
	}
	return value
}

func loadSyncView(ctx context.Context, git profilegit.Service, sha string, providers []Provider, machine string) (SyncProfileView, func(), error) {
	if err := ctx.Err(); err != nil {
		return SyncProfileView{}, nil, err
	}
	root, err := os.MkdirTemp("", "blueprint-sync-revision-*")
	if err != nil {
		return SyncProfileView{}, nil, syncViewFailure(ctx, SyncViewSnapshot, "", err)
	}
	cleanup := func() { _ = os.RemoveAll(root) }
	success := false
	defer func() {
		if !success {
			cleanup()
		}
	}()
	if err := git.MaterializeCommit(ctx, sha, root); err != nil {
		return SyncProfileView{}, nil, syncViewFailure(ctx, SyncViewSnapshot, "", err)
	}
	data, err := profile.Load(root)
	if err != nil {
		return SyncProfileView{}, nil, syncViewFailure(ctx, SyncViewProfile, "", err)
	}
	if err := validateLoadedPolicyTargets(providers, data); err != nil {
		return SyncProfileView{}, nil, syncViewFailure(ctx, SyncViewPolicy, "", err)
	}
	if err := validateSyncArtifacts(ctx, root, data, providers); err != nil {
		return SyncProfileView{}, nil, err
	}
	targets, err := projectSyncProfile(data, providers)
	if err != nil {
		var view *SyncViewError
		if errors.As(err, &view) {
			return SyncProfileView{}, nil, syncViewFailure(ctx, SyncViewProjection, view.Category, err)
		}
		return SyncProfileView{}, nil, syncViewFailure(ctx, SyncViewProjection, "", err)
	}
	control, err := syncControl(data, machine)
	if err != nil {
		return SyncProfileView{}, nil, syncViewFailure(ctx, SyncViewControl, "", err)
	}
	if err := ctx.Err(); err != nil {
		return SyncProfileView{}, nil, err
	}
	success = true
	return SyncProfileView{Revision: sha, Root: root, Profile: data, Targets: targets, Control: control}, cleanup, nil
}

func validateSyncArtifacts(ctx context.Context, root string, data profile.Data, providers []Provider) error {
	for _, p := range providers {
		if err := ctx.Err(); err != nil {
			return err
		}
		if v, ok := p.(interface {
			ValidateSyncArtifacts(context.Context, string, profile.Data) error
		}); ok {
			if err := v.ValidateSyncArtifacts(ctx, root, profile.CloneData(data)); err != nil {
				return syncViewFailure(ctx, SyncViewArtifacts, p.ID(), err)
			}
		}
	}
	return ctx.Err()
}

func syncCapturedCategories(d profile.Data) map[string]bool {
	c := d.Manifest.Capture
	return map[string]bool{"packages": c.Packages, "themes": c.Themes, "plugins": c.Plugins, "config": c.Config, "defaults": c.Defaults, "shell": c.Shell, "hooks": c.Hooks, "resources": c.Resources, "services": c.Services}
}

func projectSyncProfile(data profile.Data, providers []Provider) ([]syncengine.DesiredTarget, error) {
	registered := map[string]bool{}
	seen := map[syncengine.TargetKey]bool{}
	out := []syncengine.DesiredTarget{}
	for _, p := range providers {
		id := p.ID()
		if registered[id] {
			return nil, fmt.Errorf("sync: duplicate provider")
		}
		registered[id] = true
		v, ok := p.(SyncDesiredProvider)
		if !ok {
			if p.Captured(data) {
				return nil, &SyncViewError{Stage: SyncViewProjection, Category: id}
			}
			continue
		}
		items, err := v.SyncDesired(profile.CloneData(data))
		if err != nil {
			return nil, &SyncViewError{Stage: SyncViewProjection, Category: id}
		}
		for _, x := range items {
			if x.Key.Category != id || x.Key.Target == "" || seen[x.Key] {
				return nil, &SyncViewError{Stage: SyncViewProjection, Category: id}
			}
			if validator, ok := p.(targetValidator); ok {
				key, err := validator.ValidateTarget(x.Key.Target)
				if err != nil || key != x.Key.Target {
					return nil, &SyncViewError{Stage: SyncViewProjection, Category: id}
				}
			}
			seen[x.Key] = true
			out = append(out, x)
		}
	}
	for id, captured := range syncCapturedCategories(data) {
		if captured && !registered[id] {
			return nil, &SyncViewError{Stage: SyncViewProjection, Category: id}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Key.Category != out[j].Key.Category {
			return out[i].Key.Category < out[j].Key.Category
		}
		return out[i].Key.Target < out[j].Key.Target
	})
	return out, nil
}
