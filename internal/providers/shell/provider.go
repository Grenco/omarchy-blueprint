package shell

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

// Status describes how the machine's Shell configuration relates to the
// Omarchy baseline.
type Status string

const (
	StatusDefault     Status = "default"
	StatusCustomized  Status = "customized"
	StatusUnsupported Status = "unsupported"
)

// State is the detected Shell state of the machine.
type State struct {
	Status       Status
	Version      int
	Hash         string
	BaselineHash string
	UserExists   bool
	References   []string
	Current      Document
	Baseline     Document
}

// Provider detects, captures, and validates Omarchy Shell state.
type Provider struct {
	BaselinePath string
	UserPath     string
	ProfileDir   string
}

// ShellIntent is the portable source change: the baseline shipped at capture
// time and the user's desired document captured from that baseline.
type ShellIntent struct {
	Baseline Document
	Desired  Document
}

var errUnsupportedShell = errors.New("unsupported Omarchy shell schema version")

var errMissingPluginProvenance = errors.New("shell references third-party plugin without captured plugin provenance")

// Detect inspects the baseline and user Shell documents without following
// symlinks. A missing user file is logically the default state; a malformed
// user file is an error, never default.
func (p Provider) Detect() (State, error) {
	baseline, err := ReadDocument(p.BaselinePath)
	if err != nil {
		return State{}, fmt.Errorf("read Omarchy shell baseline: %w", err)
	}
	if baseline.Version != SupportedVersion {
		return State{Status: StatusUnsupported, Version: baseline.Version, Baseline: baseline}, nil
	}
	state := State{
		Status:       StatusDefault,
		Version:      baseline.Version,
		BaselineHash: baseline.Hash,
		Baseline:     baseline,
	}
	current, err := ReadDocument(p.UserPath)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("read user shell config: %w", err)
	}
	state.UserExists = true
	state.Current = current
	state.Version = current.Version
	state.References = append([]string(nil), current.References...)
	if current.Version != SupportedVersion {
		state.Status = StatusUnsupported
		return state, nil
	}
	if current.Hash == baseline.Hash {
		return state, nil
	}
	state.Status = StatusCustomized
	state.Hash = current.Hash
	return state, nil
}

// Capture stores the already-detected Shell state into the profile. It writes
// the exact bytes Detect inspected and never re-reads the machine files, so
// the captured snapshot is internally coherent.
func (p Provider) Capture(state State) (profile.Shell, error) {
	if state.Status == StatusUnsupported {
		return profile.Shell{}, fmt.Errorf("%w %d; supported: %d", errUnsupportedShell, state.Version, SupportedVersion)
	}
	if p.ProfileDir == "" {
		return profile.Shell{}, fmt.Errorf("profile directory is required to capture shell state")
	}
	shellDir := filepath.Join(p.ProfileDir, "shell")
	if err := os.MkdirAll(shellDir, 0o755); err != nil {
		return profile.Shell{}, err
	}
	if state.Status == StatusCustomized {
		if err := atomicWrite(filepath.Join(shellDir, "shell.json"), state.Current.Raw); err != nil {
			return profile.Shell{}, err
		}
		if err := atomicWrite(filepath.Join(shellDir, "baseline.json"), state.Baseline.Raw); err != nil {
			return profile.Shell{}, err
		}
		return profile.Shell{
			Version:      state.Version,
			Hash:         state.Hash,
			BaselineHash: state.BaselineHash,
		}, nil
	}
	if err := removeStaleSnapshots(shellDir); err != nil {
		return profile.Shell{}, err
	}
	return profile.Shell{
		Version:      state.Baseline.Version,
		BaselineHash: state.Baseline.Hash,
	}, nil
}

func (p Provider) loadIntent(saved profile.Shell) (ShellIntent, error) {
	if saved.Hash == "" {
		return ShellIntent{}, fmt.Errorf("default Shell state has no captured intent")
	}
	if saved.Version != SupportedVersion {
		return ShellIntent{}, fmt.Errorf("unsupported recorded shell schema version %d; supported: %d", saved.Version, SupportedVersion)
	}
	desired, err := ReadDocument(filepath.Join(p.ProfileDir, "shell", "shell.json"))
	if err != nil {
		return ShellIntent{}, fmt.Errorf("desired shell snapshot: %w", err)
	}
	if desired.Hash != saved.Hash || desired.Version != saved.Version {
		return ShellIntent{}, fmt.Errorf("desired shell snapshot does not match recorded metadata")
	}
	baseline, err := ReadDocument(filepath.Join(p.ProfileDir, "shell", "baseline.json"))
	if err != nil {
		return ShellIntent{}, fmt.Errorf("baseline shell snapshot: %w", err)
	}
	if baseline.Hash != saved.BaselineHash || baseline.Version != saved.Version {
		return ShellIntent{}, fmt.Errorf("baseline shell snapshot does not match recorded metadata")
	}
	return ShellIntent{Baseline: baseline, Desired: desired}, nil
}

func effectiveTarget(current State) Document {
	if current.UserExists {
		return current.Current
	}
	return current.Baseline
}

// Analyze applies captured intent to the current target without mutating any
// documents. Empty captured state deliberately has no portable Shell intent.
func (p Provider) Analyze(saved profile.Shell, current State, options MergeOptions) (MergeResult, error) {
	if saved.Hash == "" {
		return MergeResult{Value: cloneValue(effectiveTarget(current).Value).(map[string]any)}, nil
	}
	if current.Status == StatusUnsupported || current.Version != SupportedVersion || current.Baseline.Version != SupportedVersion {
		return MergeResult{}, fmt.Errorf("Shell schema changed; migration required")
	}
	intent, err := p.loadIntent(saved)
	if err != nil {
		return MergeResult{}, err
	}
	target := effectiveTarget(current)
	if target.Version != SupportedVersion {
		return MergeResult{}, fmt.Errorf("Shell schema changed; migration required")
	}
	result, err := Merge(intent.Baseline.Value, intent.Desired.Value, current.Baseline.Value, target.Value, options)
	if err != nil {
		return MergeResult{}, err
	}
	return reconcileBarLayout(intent.Baseline.Value, intent.Desired.Value, target.Value, result, options), nil
}

func removeStaleSnapshots(dir string) error {
	for _, name := range []string{"shell.json", "baseline.json"} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove stale shell snapshot %s: %w", name, err)
		}
	}
	return nil
}

// ValidatePluginReferences requires every third-party Shell reference to have
// captured plugin provenance. First-party omarchy.* references are excluded.
func ValidatePluginReferences(refs []string, plugins profile.Plugins) error {
	have := map[string]profile.Plugin{}
	for _, item := range plugins.Items {
		have[item.ID] = item
	}
	for _, id := range refs {
		if strings.HasPrefix(id, "omarchy.") {
			continue
		}
		item, ok := have[id]
		if !ok || item.Source == "" || item.Source == "builtin" {
			return fmt.Errorf("%w: %q; capture plugins first", errMissingPluginProvenance, id)
		}
	}
	return nil
}

// RequiredThirdPartyPlugins returns only third-party references introduced by
// the proposed merge. Target-local references are preserved but never claimed
// as portable profile state.
func (p Provider) RequiredThirdPartyPlugins(saved profile.Shell, current State, options MergeOptions) ([]string, error) {
	if saved.Hash == "" {
		return []string{}, nil
	}
	analysis, err := p.Analyze(saved, current, options)
	if err != nil {
		return nil, err
	}
	proposed, err := EncodeDocument(analysis.Value)
	if err != nil {
		return nil, err
	}
	return introducedReferences(effectiveTarget(current), proposed), nil
}

func introducedReferences(before, after Document) []string {
	seen := make(map[string]bool, len(before.References))
	for _, id := range before.References {
		seen[id] = true
	}
	introduced := make([]string, 0, len(after.References))
	for _, id := range after.References {
		if !seen[id] {
			introduced = append(introduced, id)
		}
	}
	return introduced
}

// Check validates captured Shell state: snapshot existence/regularity,
// canonical hashes, versions, and third-party plugin provenance.
func (p Provider) Check(saved profile.Shell, plugins profile.Plugins) error {
	if saved.Hash == "" {
		if saved.Version != SupportedVersion {
			return fmt.Errorf("unsupported recorded shell schema version %d; supported: %d", saved.Version, SupportedVersion)
		}
		for _, name := range []string{"shell.json", "baseline.json"} {
			if _, err := os.Lstat(filepath.Join(p.ProfileDir, "shell", name)); !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("stale shell snapshot %s exists for default desired state", name)
			}
		}
		return nil
	}
	if saved.Version != SupportedVersion {
		return fmt.Errorf("unsupported recorded shell schema version %d; supported: %d", saved.Version, SupportedVersion)
	}
	desired, err := ReadDocument(filepath.Join(p.ProfileDir, "shell", "shell.json"))
	if err != nil {
		return fmt.Errorf("desired shell snapshot: %w", err)
	}
	if desired.Hash != saved.Hash {
		return fmt.Errorf("desired shell snapshot hash mismatch (%s != %s)", desired.Hash, saved.Hash)
	}
	if desired.Version != saved.Version {
		return fmt.Errorf("desired shell snapshot version %d does not match recorded %d", desired.Version, saved.Version)
	}
	baseline, err := ReadDocument(filepath.Join(p.ProfileDir, "shell", "baseline.json"))
	if err != nil {
		return fmt.Errorf("baseline shell snapshot: %w", err)
	}
	if baseline.Hash != saved.BaselineHash {
		return fmt.Errorf("baseline shell snapshot hash mismatch (%s != %s)", baseline.Hash, saved.BaselineHash)
	}
	if baseline.Version != saved.Version {
		return fmt.Errorf("baseline shell snapshot version %d does not match recorded %d", baseline.Version, saved.Version)
	}
	return ValidatePluginReferences(desired.References, plugins)
}

func atomicWrite(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".omarchy-blueprint-shell-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Chmod(0o644); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
