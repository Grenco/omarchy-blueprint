package plugins

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/command"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

type Provider struct {
	Runner                         command.Runner
	UserDir, ProfileDir            string
	captureDestination, captureOld string
	capturePending                 bool
}
type catalogItem struct {
	ID         string `json:"id"`
	Enabled    bool   `json:"enabled"`
	FirstParty bool   `json:"firstParty"`
	CanDisable bool   `json:"canDisable"`
	ClonedFrom string `json:"clonedFrom"`
}

func (p Provider) Detect(ctx context.Context) (profile.Plugins, error) {
	out, err := p.Runner.Run(ctx, "omarchy", "plugin", "list", "--json")
	if err != nil {
		return profile.Plugins{}, fmt.Errorf("detect Omarchy plugins: %w", err)
	}
	var catalog []catalogItem
	if err := json.Unmarshal([]byte(out), &catalog); err != nil {
		return profile.Plugins{}, fmt.Errorf("parse Omarchy plugin catalog: %w", err)
	}
	state := profile.Plugins{}
	for _, item := range catalog {
		if item.FirstParty {
			if item.CanDisable {
				state.Items = append(state.Items, profile.Plugin{ID: item.ID, Source: "builtin", Enabled: item.Enabled})
			}
			continue
		}
		path := filepath.Join(p.UserDir, item.ID)
		plugin, err := p.detectUser(ctx, item, path)
		if err != nil {
			return state, err
		}
		state.Items = append(state.Items, plugin)
	}
	sort.Slice(state.Items, func(i, j int) bool { return state.Items[i].ID < state.Items[j].ID })
	return state, nil
}

// Capture merges live detection into desired state per the Capture merge
// transition table, driven by enabled's resolved per-target decision
// ("plugin:<id>" for each third-party plugin's source availability). A
// preserved (disabled) or freshly captured (enabled) target's local artifact
// is staged into the merged generation together -- disabled targets keep
// their existing artifact rather than the staged tree being wholesale
// replaced by only what this run freshly captured. First-party (builtin)
// plugins are never merged/tombstoned: Omarchy owns their availability, and
// plugin enablement itself remains Shell-owned, not Plugins' desired state.
func (p *Provider) Capture(ctx context.Context, saved profile.Plugins, enabled func(ref string) bool) (profile.Plugins, error) {
	current, err := p.Detect(ctx)
	if err != nil {
		return current, err
	}
	if p.ProfileDir == "" {
		return current, fmt.Errorf("profile directory is required to capture plugins")
	}
	parent := filepath.Join(p.ProfileDir, "plugins")
	if err := os.MkdirAll(parent, 0755); err != nil {
		return current, err
	}
	stage, err := os.MkdirTemp(parent, ".local-capture-*")
	if err != nil {
		return current, err
	}
	defer os.RemoveAll(stage)
	existing := filepath.Join(parent, "local")

	savedByID, currentByID := pluginMap(saved.Items), pluginMap(current.Items)
	savedAbsentByID := pluginMap(saved.Absent)
	ids := map[string]bool{}
	for id, item := range savedByID {
		if item.Source != "builtin" {
			ids[id] = true
		}
	}
	for id, item := range currentByID {
		if item.Source != "builtin" {
			ids[id] = true
		}
	}
	for id := range savedAbsentByID {
		ids[id] = true
	}

	var result profile.Plugins
	for id := range ids {
		savedItem, wasPresent := savedByID[id]
		currentItem, isPresent := currentByID[id]
		_, wasAbsent := savedAbsentByID[id]
		isEnabled := enabled("plugin:" + id)
		switch transition(wasPresent, wasAbsent, isPresent, isEnabled) {
		case transitionPresent:
			if isEnabled && isPresent {
				if currentItem.Source == "local" {
					source, err := filepath.EvalSymlinks(filepath.Join(p.UserDir, id))
					if err != nil {
						return current, err
					}
					if err := copyTree(source, filepath.Join(stage, id)); err != nil {
						return current, fmt.Errorf("capture plugin %q: %w", id, err)
					}
				}
				result.Items = append(result.Items, currentItem)
			} else {
				if savedItem.Source == "local" {
					if err := copyTree(filepath.Join(existing, id), filepath.Join(stage, id)); err != nil {
						return current, fmt.Errorf("preserve plugin %q: %w", id, err)
					}
				}
				result.Items = append(result.Items, savedItem)
			}
		case transitionAbsent:
			if wasAbsent {
				result.Absent = append(result.Absent, savedAbsentByID[id])
			} else {
				result.Absent = append(result.Absent, savedItem)
			}
		}
	}
	for _, item := range currentByID {
		if item.Source == "builtin" {
			result.Items = append(result.Items, item)
		}
	}
	sort.Slice(result.Items, func(i, j int) bool { return result.Items[i].ID < result.Items[j].ID })
	sort.Slice(result.Absent, func(i, j int) bool { return result.Absent[i].ID < result.Absent[j].ID })

	dest, old := filepath.Join(parent, "local"), filepath.Join(parent, ".local-previous")
	_ = os.RemoveAll(old)
	if _, err := os.Stat(dest); err == nil {
		if err := os.Rename(dest, old); err != nil {
			return current, err
		}
	}
	if err := os.Rename(stage, dest); err != nil {
		_ = os.Rename(old, dest)
		return current, err
	}
	p.captureDestination, p.captureOld, p.capturePending = dest, old, true
	return result, nil
}

// transitionResult is the Capture merge outcome for one target: whether it
// belongs in the new desired-present set, the new desired-absent (tombstone)
// set, or neither (still unmanaged). See internal/providers/packages'
// identical helper: each provider owns its own merge/preserve behavior, so
// this small, stable, pure table is duplicated rather than shared.
type transitionResult int

const (
	transitionNone transitionResult = iota
	transitionPresent
	transitionAbsent
)

// transition implements the Capture merge invariant table generically.
// wasPresent/wasAbsent describe the previous desired state (both false means
// "unknown": never captured); isPresent is the current live state; enabled
// is the resolved Capture decision for this target.
func transition(wasPresent, wasAbsent, isPresent, enabled bool) transitionResult {
	if !enabled {
		switch {
		case wasPresent:
			return transitionPresent
		case wasAbsent:
			return transitionAbsent
		default:
			return transitionNone
		}
	}
	if isPresent {
		return transitionPresent
	}
	if wasPresent || wasAbsent {
		return transitionAbsent
	}
	return transitionNone
}

// PrepareStopManagingArtifact stages the removal of one plugin's local clone
// artifact directory without deleting it yet: the directory is renamed out
// of the way, reusing the same pending-swap bookkeeping Capture uses, so the
// caller can defer the actual deletion until it knows the profile save that
// forgets the plugin's metadata has also succeeded (FinalizeCapture), or undo
// the rename if it has not (RollbackCapture). A plugin with no local artifact
// (git-remote, or already removed) is a no-op.
func (p *Provider) PrepareStopManagingArtifact(id string) error {
	source := filepath.Join(p.ProfileDir, "plugins", "local", id)
	if _, err := os.Lstat(source); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	backup := filepath.Join(p.ProfileDir, "plugins", ".stop-managing-"+id)
	if err := os.RemoveAll(backup); err != nil {
		return err
	}
	if err := os.Rename(source, backup); err != nil {
		return err
	}
	p.captureDestination, p.captureOld, p.capturePending = source, backup, true
	return nil
}

func (p *Provider) CommitCapture() error { return nil }
func (p *Provider) FinalizeCapture() error {
	if !p.capturePending {
		return nil
	}
	err := os.RemoveAll(p.captureOld)
	p.captureDestination, p.captureOld, p.capturePending = "", "", false
	return err
}
func (p *Provider) RollbackCapture() error {
	if !p.capturePending {
		return nil
	}
	if err := os.RemoveAll(p.captureDestination); err != nil {
		return err
	}
	if _, err := os.Stat(p.captureOld); err == nil {
		if err := os.Rename(p.captureOld, p.captureDestination); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	p.captureDestination, p.captureOld, p.capturePending = "", "", false
	return nil
}

func (p Provider) detectUser(ctx context.Context, item catalogItem, path string) (profile.Plugin, error) {
	result := profile.Plugin{ID: item.ID, ClonedFrom: item.ClonedFrom, Enabled: item.Enabled}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return result, fmt.Errorf("resolve plugin %q: %w", item.ID, err)
	}
	if isDir(filepath.Join(resolved, ".git")) {
		remote, re := p.Runner.Run(ctx, "git", "-C", resolved, "remote", "get-url", "origin")
		rev, ve := p.Runner.Run(ctx, "git", "-C", resolved, "rev-parse", "HEAD")
		dirty, se := p.Runner.Run(ctx, "git", "-C", resolved, "status", "--porcelain", "--untracked-files=all")
		if re == nil && ve == nil && se == nil && strings.TrimSpace(dirty) == "" {
			result.Source = "git"
			result.URL = sanitizeURL(strings.TrimSpace(remote))
			result.Revision = strings.TrimSpace(rev)
			return result, nil
		}
	}
	hash, err := hashTree(resolved)
	if err != nil {
		return result, fmt.Errorf("hash plugin %q: %w", item.ID, err)
	}
	result.Source = "local"
	result.Hash = hash
	return result, nil
}

// Semantics expresses plugin-provider ownership. When Shell state is captured,
// the Shell provider owns plugin enablement, so ManageEnabled is false and
// the plugins provider ignores Enabled differences entirely.
type Semantics struct {
	ManageEnabled bool
}

func Diff(saved, current profile.Plugins, semantics Semantics) []model.Change {
	have := pluginMap(current.Items)
	var out []model.Change
	for _, want := range saved.Items {
		got, ok := have[want.ID]
		if !ok {
			out = append(out, change(want.ID, "- plugin "+want.ID+" ("+want.Source+")"))
			continue
		}
		if !Equivalent(want, got) {
			out = append(out, change(want.ID, fmt.Sprintf("~ plugin %s differs (%s → %s)", want.ID, want.Source, got.Source)))
		} else if semantics.ManageEnabled && got.Enabled != want.Enabled {
			out = append(out, change(want.ID, fmt.Sprintf("~ plugin %s enabled: %t → %t", want.ID, got.Enabled, want.Enabled)))
		}
	}
	for _, got := range current.Items {
		if _, ok := pluginMap(saved.Items)[got.ID]; !ok && got.Source != "builtin" {
			out = append(out, change(got.ID, "+ plugin "+got.ID+" ("+got.Source+")"))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// PlanOptions controls Exact-only behavior; the zero value (Additive) never
// removes anything, matching every existing caller that predates it.
type PlanOptions struct{ Exact bool }

func (p Provider) Plan(saved, current profile.Plugins, schema int, from, to string, semantics Semantics, options ...PlanOptions) model.RestorePlan {
	var opts PlanOptions
	if len(options) > 0 {
		opts = options[0]
	}
	plan := model.RestorePlan{ProfileVersion: schema, OmarchyFrom: from, OmarchyTo: to}
	have := pluginMap(current.Items)
	wantMap := pluginMap(saved.Items)
	for _, want := range saved.Items {
		got, ok := have[want.ID]
		lastDependency := ""
		if !safeID(want.ID) {
			plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "plugins", Resource: "plugin:" + want.ID, Reason: "unsafe plugin identifier"})
			continue
		}
		if ok && !Equivalent(want, got) {
			plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "plugins", Resource: "plugin:" + want.ID, Reason: "existing plugin differs; overwrite disabled"})
			continue
		}
		if !ok {
			switch want.Source {
			case "builtin", "":
				plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "plugins", Resource: "plugin:" + want.ID, Reason: "first-party plugin unavailable in this Omarchy version"})
				continue
			case "git":
				if !validRevision(want.Revision) || want.URL == "" {
					plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "plugins", Resource: "plugin:" + want.ID, Reason: "invalid Git provenance"})
					continue
				}
				install := op("install", want.ID, []string{"omarchy", "plugin", "add", want.URL, "--yes"}, model.RiskHigh)
				pin := op("pin", want.ID, []string{"git", "-C", filepath.Join(p.UserDir, want.ID), "checkout", "--detach", want.Revision}, model.RiskLow)
				pin.DependsOn = []string{install.ID}
				validate := op("validate", want.ID, []string{"omarchy", "plugin", "validate", filepath.Join(p.UserDir, want.ID)}, model.RiskLow)
				validate.DependsOn = []string{pin.ID}
				rescan := op("rescan", want.ID, []string{"omarchy-shell", "shell", "rescanPlugins"}, model.RiskLow)
				rescan.DependsOn = []string{validate.ID}
				plan.Operations = append(plan.Operations, install, pin, validate, rescan)
				lastDependency = rescan.ID
			case "local":
				source := filepath.Join(p.ProfileDir, "plugins", "local", want.ID)
				validate := op("validate", want.ID, []string{"omarchy", "plugin", "validate", source}, model.RiskLow)
				copy := model.Operation{ID: "plugins.copy." + want.ID, Provider: "plugins", Action: "copy", Resource: "plugin:" + want.ID, Items: []string{want.ID}, Copy: &model.Copy{Source: source, Destination: filepath.Join(p.UserDir, want.ID)}, DependsOn: []string{validate.ID}, Risk: model.RiskHigh, Reversible: true}
				rescan := op("rescan", want.ID, []string{"omarchy-shell", "shell", "rescanPlugins"}, model.RiskLow)
				rescan.DependsOn = []string{copy.ID}
				plan.Operations = append(plan.Operations, validate, copy, rescan)
				lastDependency = rescan.ID
			default:
				continue
			}
		}
		if semantics.ManageEnabled {
			if want.Enabled && (!ok || !got.Enabled) {
				enable := op("enable", want.ID, []string{"omarchy", "plugin", "enable", want.ID}, model.RiskHigh)
				if lastDependency != "" {
					enable.DependsOn = []string{lastDependency}
				}
				plan.Operations = append(plan.Operations, enable)
			} else if ok && !want.Enabled && got.Enabled {
				plan.Operations = append(plan.Operations, op("disable", want.ID, []string{"omarchy", "plugin", "disable", want.ID}, model.RiskLow))
			}
		}
	}
	// A plugin tombstoned in saved.Absent is not "additional" -- it has its
	// own recorded desired state (desired-absent), just not Present state.
	// Under Additive it is silently left alone entirely (matching Packages'
	// and Themes' established invariant that Additive never reads Absent at
	// all); under Exact it gets its own, more specific skip/removal handling
	// below.
	wantAbsent := pluginMap(saved.Absent)
	for _, got := range current.Items {
		_, managed := wantMap[got.ID]
		_, tombstoned := wantAbsent[got.ID]
		if !managed && !tombstoned && got.Source != "builtin" {
			plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "plugins", Resource: "plugin:" + got.ID, Reason: "additional plugin left installed; removal disabled"})
		}
	}

	// Exact removal only ever considers saved.Absent -- desired-present
	// Items are never candidates -- and only third-party entries: Capture
	// never tombstones a first-party plugin (Omarchy owns its availability),
	// so this guard is defensive, not reachable through legitimate Capture
	// output. A candidate whose installed provenance no longer matches the
	// tombstone (Equivalent, the same check install/overwrite safety already
	// uses) is left alone rather than deleted. omarchy plugin remove --yes
	// disables the plugin itself before removing it, so no separate
	// disable/enablement ordering is required here.
	if opts.Exact {
		for _, desired := range saved.Absent {
			if desired.Source == "" || desired.Source == "builtin" {
				continue
			}
			if !safeID(desired.ID) {
				plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "plugins", Resource: "plugin:" + desired.ID, Reason: "unsafe plugin identifier"})
				continue
			}
			actual, present := have[desired.ID]
			if !present {
				continue
			}
			if !Equivalent(desired, actual) {
				plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "plugins", Resource: "plugin:" + desired.ID, Reason: "installed plugin no longer matches the removed profile entry; removal skipped"})
				continue
			}
			plan.Operations = append(plan.Operations, model.Operation{ID: "plugins.remove." + desired.ID, Provider: "plugins", Action: "remove", Resource: "plugin:" + desired.ID, Items: []string{desired.ID}, Command: []string{"omarchy", "plugin", "remove", desired.ID, "--yes"}, Risk: model.RiskHigh, Reversible: false})
		}
	}
	return plan
}

// VerifyOptions controls Exact-only verification; the zero value (Additive)
// never checks Absent, matching every existing caller that predates it.
type VerifyOptions struct{ Exact bool }

func Verify(saved, current profile.Plugins, semantics Semantics, options ...VerifyOptions) model.VerificationResult {
	var opts VerifyOptions
	if len(options) > 0 {
		opts = options[0]
	}
	have := pluginMap(current.Items)
	var missing []string
	for _, want := range saved.Items {
		got, ok := have[want.ID]
		if !ok || !Equivalent(want, got) {
			missing = append(missing, "plugin:"+want.ID)
			continue
		}
		if semantics.ManageEnabled && got.Enabled != want.Enabled {
			missing = append(missing, "plugin:"+want.ID)
		}
	}
	if opts.Exact {
		for _, desired := range saved.Absent {
			if desired.Source == "" || desired.Source == "builtin" {
				continue
			}
			if actual, present := have[desired.ID]; present && Equivalent(desired, actual) {
				missing = append(missing, "plugin:"+desired.ID)
			}
		}
	}
	sort.Strings(missing)
	return model.VerificationResult{OK: len(missing) == 0, Missing: missing}
}

// Equivalent reports whether a discovered plugin is the same captured source.
// Enabled state is intentionally excluded because Shell owns it after capture.
func Equivalent(a, b profile.Plugin) bool {
	as, bs := a.Source, b.Source
	if as == "" {
		as = "builtin"
	}
	if bs == "" {
		bs = "builtin"
	}
	if as != bs {
		return false
	}
	switch as {
	case "git":
		return a.URL == b.URL && a.Revision == b.Revision
	case "local":
		return a.Hash == b.Hash && a.ClonedFrom == b.ClonedFrom
	default:
		return true
	}
}
func op(action, id string, argv []string, risk model.Risk) model.Operation {
	return model.Operation{ID: "plugins." + action + "." + id, Provider: "plugins", Action: action, Resource: "plugin:" + id, Items: []string{id}, Command: argv, Risk: risk, Reversible: action != "install"}
}
func change(id, summary string) model.Change {
	return model.Change{Type: model.ChangeAdd, Provider: "plugins", Kind: "plugin", Name: id, Summary: summary}
}
func pluginMap(items []profile.Plugin) map[string]profile.Plugin {
	out := map[string]profile.Plugin{}
	for _, item := range items {
		out[item.ID] = item
	}
	return out
}
func sanitizeURL(raw string) string {
	u, err := url.Parse(raw)
	if err == nil && u.User != nil && u.Scheme != "" {
		u.User = nil
		return u.String()
	}
	return raw
}
func validRevision(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return true
}
func safeID(id string) bool {
	return id != "" && id != "." && id != ".." && !filepath.IsAbs(id) && filepath.Base(id) == id
}
func isDir(path string) bool { info, err := os.Stat(path); return err == nil && info.IsDir() }
func hashTree(root string) (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(root, func(path string, e os.DirEntry, we error) error {
		if we != nil {
			return we
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." {
			return err
		}
		if rel == ".git" || strings.HasPrefix(rel, ".git"+string(filepath.Separator)) {
			if e.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if e.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsupported symlink: %s", rel)
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		fmt.Fprintf(h, "%s\x00%o\x00", filepath.ToSlash(rel), info.Mode().Perm())
		if info.Mode().IsRegular() {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			_, ce := io.Copy(h, f)
			cl := f.Close()
			if ce != nil {
				return ce
			}
			return cl
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func copyTree(source, dest string) error {
	return filepath.WalkDir(source, func(path string, e os.DirEntry, we error) error {
		if we != nil {
			return we
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if rel == ".git" || strings.HasPrefix(rel, ".git"+string(filepath.Separator)) {
			if e.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if e.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsupported symlink: %s", rel)
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if e.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported file: %s", rel)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			in.Close()
			return err
		}
		_, ce := io.Copy(out, in)
		ie, oe := in.Close(), out.Close()
		if ce != nil {
			return ce
		}
		if ie != nil {
			return ie
		}
		return oe
	})
}
