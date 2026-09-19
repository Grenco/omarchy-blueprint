package workflow

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/Grenco/omarchy-blueprint/internal/machine"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/profilegit"
)

type Session struct {
	deps            Dependencies
	opts            Options
	profile         profile.Data
	machine         machine.Selection
	providers       []Provider
	profileGit      profilegit.Service
	finalizeRestore func(context.Context, profile.Data, []Provider, *model.RestorePlan, policy.RestoreOptions) error
}

func Open(deps Dependencies, opts Options) (*Session, error) {
	s := &Session{deps: deps, opts: opts}
	if err := s.Reload(); err != nil {
		return nil, err
	}
	service, err := profilegit.New(deps.Runner, s.opts.ProfileDir)
	if err != nil {
		return nil, err
	}
	s.profileGit = service
	return s, nil
}

// Reload resolves data again because external tools may have changed it.
func (s *Session) Reload() error {
	dir, err := machine.CanonicalProfileRoot(s.opts.ProfileDir)
	if err != nil {
		return err
	}
	s.opts.ProfileDir = dir
	data, err := profile.Load(dir)
	if err != nil {
		return err
	}
	state, err := s.deps.StateHome()
	if err != nil {
		return err
	}
	bound, err := (machine.BindingStore{StateHome: state}).Load(dir)
	if err != nil {
		return err
	}
	selection, err := machine.Select(s.opts.ExplicitMachine, bound, data.Machines.Items)
	if err != nil {
		return err
	}
	s.profile, s.machine = data, selection
	return nil
}

func (s *Session) Profile() profile.Data      { return s.profile }
func (s *Session) Machine() machine.Selection { return s.machine }
func (s *Session) ProfileDir() string         { return filepath.Clean(s.opts.ProfileDir) }
func (s *Session) HomeDir() string {
	if s.deps.HomeDir != nil {
		if home, err := s.deps.HomeDir(); err == nil && home != "" {
			return filepath.Clean(home)
		}
	}
	return ""
}

// SetProviderCaptured changes whether an optional provider participates in
// capture and restore without making a screen write profile files directly.
func (s *Session) SetProviderCaptured(_ context.Context, id string, captured bool) error {
	data := s.profile
	switch id {
	case "themes":
		data.Manifest.Capture.Themes = captured
	case "plugins":
		data.Manifest.Capture.Plugins = captured
	case "shell":
		data.Manifest.Capture.Shell = captured
	case "hooks":
		data.Manifest.Capture.Hooks = captured
	case "defaults":
		data.Manifest.Capture.Defaults = captured
	default:
		return fmt.Errorf("%s cannot be toggled as an optional provider", id)
	}
	if err := profile.Save(s.opts.ProfileDir, data); err != nil {
		return fmt.Errorf("save profile: %w", err)
	}
	s.profile = data
	return nil
}

// SetProviderItemEnabled toggles a saved package item's exclusion via
// SetPackageExcluded, which is also what the exclude/include CLI verbs use.
func (s *Session) SetProviderItemEnabled(_ context.Context, provider, section, key string) error {
	if provider != "packages" {
		return fmt.Errorf("%s does not support item selection", provider)
	}
	kind := map[string]string{"Official packages": "official", "AUR packages": "aur", "Mise tools": "mise"}[section]
	if kind == "" {
		return fmt.Errorf("package group %s cannot be toggled", section)
	}
	ref := kind + ":" + key
	return s.SetPackageExcluded(ref, !s.packageExcluded(ref))
}

// Profile Git operations remain owned by profilegit; workflow only exposes the
// active profile service to user interfaces.
func (s *Session) ProfileGitStatus(ctx context.Context) (profilegit.Status, error) {
	return s.profileGit.Status(ctx)
}
func (s *Session) ProfileGitDiff(ctx context.Context, path string) (profilegit.Diff, error) {
	return s.profileGit.Diff(ctx, path)
}
func (s *Session) ProfileGitInit(ctx context.Context) (profilegit.Result, error) {
	return s.profileGit.Init(ctx)
}
func (s *Session) ProfileGitRemote(ctx context.Context) (string, error) {
	return s.profileGit.Remote(ctx)
}
func (s *Session) ProfileGitSetRemote(ctx context.Context, rawURL string) (profilegit.Result, error) {
	return s.profileGit.SetRemote(ctx, rawURL)
}
func (s *Session) ProfileGitRemoveRemote(ctx context.Context) (profilegit.Result, error) {
	return s.profileGit.RemoveRemote(ctx)
}
func (s *Session) ProfileGitFetch(ctx context.Context) (profilegit.Result, error) {
	return s.profileGit.Fetch(ctx)
}
func (s *Session) ProfileGitCommit(ctx context.Context, message string) (profilegit.Result, error) {
	return s.profileGit.Commit(ctx, message)
}
func (s *Session) ProfileGitPull(ctx context.Context) (profilegit.Result, error) {
	result, err := s.profileGit.Pull(ctx)
	if err != nil {
		return result, err
	}
	if err := s.Reload(); err != nil {
		return result, fmt.Errorf("reload profile after pull: %w", err)
	}
	return result, nil
}
func (s *Session) ProfileGitPush(ctx context.Context) (profilegit.Result, error) {
	return s.profileGit.Push(ctx)
}

// SetProviders installs the application-specific adapters for this session.
func (s *Session) SetProviders(providers []Provider) { s.providers = providers }

// SetRestoreFinalizer installs application-specific aggregate restore rules.
// The finalizer receives the same policy.RestoreOptions that planned every
// selected provider, not the legacy RestoreMode, so it can never diverge
// from the effective Restore intent that produced the plan it is adjusting.
func (s *Session) SetRestoreFinalizer(finalize func(context.Context, profile.Data, []Provider, *model.RestorePlan, policy.RestoreOptions) error) {
	s.finalizeRestore = finalize
}
