package workflow

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/Grenco/omarchy-blueprint/internal/machine"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/profilegit"
)

// Session is shared by the CLI and by the TUI, whose asynchronous commands
// call it from other goroutines while the interface reads it.
//
// Locking (STATE-02):
//   - stateMu guards profile and machine. It is a leaf lock, held only to
//     read or replace them; reads return deep copies (Profile, Machine), so
//     nothing outside the Session aliases its state.
//   - mutationMu serializes every read-modify-save (and Reload, and profile
//     Git operations that change the profile), so two mutations cannot both
//     start from the same profile and lose one update. It is taken once, at
//     a public entry point; helpers never take it. Reads never wait for it,
//     so a long Capture does not block the interface.
//   - configMu guards opts, providers and finalizers.
//   - needsReload is guarded by mutationMu.
//
// Lock order: mutationMu, then configMu, then stateMu.
type Session struct {
	mutationMu          sync.Mutex
	configMu            sync.Mutex
	stateMu             sync.RWMutex
	needsReload         bool
	deps                Dependencies
	opts                Options
	profile             profile.Data
	machine             machine.Selection
	providers           []Provider
	profileGit          profilegit.Service
	finalizeRestore     func(context.Context, profile.Data, []Provider, *model.RestorePlan, policy.RestoreOptions) error
	finalizeReadRestore func(context.Context, profile.Data, []string, *model.RestorePlan, policy.RestoreOptions) error
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

// Reload resolves data again because external tools may have changed it
// (including ProfileGitPull, which reloads after fetching remote changes).
// If providers were already registered (SetProviders already ran once, e.g.
// this is a running session, not the initial Open), the freshly loaded
// candidate data is revalidated against them before replacing s.profile:
// otherwise a pull of a profile with a malformed hand-edited policy target
// would silently replace an already-open, already-validated session's
// profile, even though opening that same profile fresh would fail in
// SetProviders. s.profile is left untouched on validation failure.
func (s *Session) Reload() error {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	return s.reloadLocked()
}

// reloadLocked is Reload for a caller that already holds mutationMu.
func (s *Session) reloadLocked() error {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	dir, err := machine.CanonicalProfileRoot(s.opts.ProfileDir)
	if err != nil {
		return err
	}
	s.opts.ProfileDir = dir
	data, selection, err := loadReadSnapshot(context.Background(), readConfig{deps: s.deps, opts: s.opts, providers: s.providers})
	if err != nil {
		return err
	}
	s.stateMu.Lock()
	s.profile, s.machine = data, selection
	s.stateMu.Unlock()
	s.needsReload = false
	return nil
}

// beginMutation serializes a read-modify-save; call the returned function
// to end it. If an earlier save committed but could not finish applying,
// the profile is reloaded first (loading completes that save), so no
// mutation can start from a profile older than what is on disk and
// overwrite the committed save with it. A mutation that cannot reload is
// refused.
//
//	end, err := s.beginMutation()
//	if err != nil {
//		return err
//	}
//	defer end()
func (s *Session) beginMutation() (func(), error) {
	s.mutationMu.Lock()
	if s.needsReload {
		if err := s.reloadLocked(); err != nil {
			s.mutationMu.Unlock()
			return nil, fmt.Errorf("an earlier profile save did not finish and the profile cannot be reloaded to complete it: %w", err)
		}
	}
	return s.mutationMu.Unlock, nil
}

// saveProfile saves data inside a mutation. A save that passed its commit
// point but could not finish applying is durable: data becomes the
// Session's profile, and the next mutation reloads (completing the save)
// before it starts. Its error is still returned.
func (s *Session) saveProfile(data profile.Data, options profile.SaveOptions) error {
	err := profile.SaveWithOptions(s.opts.ProfileDir, data, options)
	if committedSave(err) {
		s.setProfile(data)
	}
	s.noteSaveError(err)
	return err
}

// noteSaveError makes the next mutation reload when err says a profile save
// committed without finishing, including a save made by a provider.
func (s *Session) noteSaveError(err error) {
	if committedSave(err) {
		s.needsReload = true
	}
}

func committedSave(err error) bool {
	var committed *profile.CommittedSaveError
	return errors.As(err, &committed)
}

// workingProfile is a private deep copy of the loaded profile for a
// mutation to change; the Session itself changes only once it is saved.
func (s *Session) workingProfile() *profile.Data {
	data := s.Profile()
	return &data
}

// setProfile replaces the loaded profile with a saved one.
func (s *Session) setProfile(data profile.Data) {
	s.stateMu.Lock()
	s.profile = data
	s.stateMu.Unlock()
}

// view returns the loaded profile and machine for reading inside the
// Session. Mutations never change a loaded profile in place (they save a
// deep copy and replace it), so this shallow copy stays valid and
// unchanged however long the caller holds it. It must not be modified.
func (s *Session) view() (profile.Data, machine.Selection) {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.profile, s.machine
}

// Profile returns a snapshot of the loaded profile: changing it never
// changes the Session.
func (s *Session) Profile() profile.Data {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return profile.CloneData(s.profile)
}

// Machine returns a snapshot of the selected machine.
func (s *Session) Machine() machine.Selection {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return cloneMachine(s.machine)
}
func (s *Session) ProfileDir() string { return filepath.Clean(s.opts.ProfileDir) }
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
	end, err := s.beginMutation()
	if err != nil {
		return err
	}
	defer end()
	data := s.Profile()
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
	if err := s.saveProfile(data, profile.SaveOptions{}); err != nil {
		return fmt.Errorf("save profile: %w", err)
	}
	s.setProfile(data)
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
	// Read and toggle as one mutation, so two toggles cannot both read the
	// same state.
	end, err := s.beginMutation()
	if err != nil {
		return err
	}
	defer end()
	return s.setPackageExcluded(ref, !s.packageExcluded(ref))
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

// ProfileGitCommit commits the managed profile files. It waits for any
// mutation in progress, so a commit never stages half of a save.
func (s *Session) ProfileGitCommit(ctx context.Context, message string) (profilegit.Result, error) {
	end, err := s.beginMutation()
	if err != nil {
		return profilegit.Result{}, err
	}
	defer end()
	return s.profileGit.Commit(ctx, message)
}

// ProfileGitPull fast-forwards the profile and reloads it, as one mutation:
// no local mutation can interleave with the pull and be lost.
func (s *Session) ProfileGitPull(ctx context.Context) (profilegit.Result, error) {
	end, err := s.beginMutation()
	if err != nil {
		return profilegit.Result{}, err
	}
	defer end()
	result, err := s.profileGit.Pull(ctx)
	if err != nil {
		return result, err
	}
	if err := s.reloadLocked(); err != nil {
		return result, fmt.Errorf("reload profile after pull: %w", err)
	}
	return result, nil
}
func (s *Session) ProfileGitPush(ctx context.Context) (profilegit.Result, error) {
	return s.profileGit.Push(ctx)
}

// SetProviders installs the application-specific adapters for this session,
// then validates every already-loaded policy rule's target against its
// category's provider now that validators are actually available.
// profile.Load can only check structural shape (known category, valid
// setting, no duplicates -- see profile.validatePolicyRules): it has no
// reachable provider registry, so a hand-edited policy.toml or machine
// policy override with a provider-invalid target (e.g. "official:has
// space") would otherwise load silently and only fail much later, at the
// first explicit SetPolicy/ClearPolicy/StopManaging call that happens to
// touch the same target. Providers are left unset if validation fails, so a
// session that fails to open never ends up in a half-usable state.
func (s *Session) SetProviders(providers []Provider) error {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if err := validateLoadedPolicyTargets(providers, s.Profile()); err != nil {
		return err
	}
	s.providers = append([]Provider(nil), providers...)
	return nil
}

// SetRestoreFinalizer installs application-specific aggregate restore rules.
// The finalizer receives the same policy.RestoreOptions that planned every
// selected provider, so it cannot diverge from the effective Restore intent
// that produced the plan it is adjusting.
func (s *Session) SetRestoreFinalizer(finalize func(context.Context, profile.Data, []Provider, *model.RestorePlan, policy.RestoreOptions) error) {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	s.finalizeRestore = finalize
}

func (s *Session) SetReadRestoreFinalizer(finalize func(context.Context, profile.Data, []string, *model.RestorePlan, policy.RestoreOptions) error) {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	s.finalizeReadRestore = finalize
}
