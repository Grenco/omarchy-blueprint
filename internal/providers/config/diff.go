package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/content"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func DiffConfigs(previous, next profile.Configs) []model.Change {
	changes := []model.Change{}
	prevFiles, nextFiles := configFiles(previous), configFiles(next)
	for path, next := range nextFiles {
		if prior, ok := prevFiles[path]; !ok {
			changes = append(changes, configChange(model.ChangeAdd, path, "+ config "+path+" captured"))
		} else if prior.Hash != next.Hash || prior.BaselineHash != next.BaselineHash {
			changes = append(changes, configChange(model.ChangeModify, path, "~ config "+path+" differs"))
		}
	}
	for path := range prevFiles {
		if _, ok := nextFiles[path]; !ok {
			changes = append(changes, configChange(model.ChangeRemove, path, "- config "+path+" customization removed"))
		}
	}
	prevDeletes, nextDeletes := configDeletes(previous), configDeletes(next)
	for path := range nextDeletes {
		if _, ok := prevDeletes[path]; !ok {
			changes = append(changes, configChange(model.ChangeAdd, path, "+ config "+path+" deletion captured"))
		}
	}
	for path := range prevDeletes {
		if _, ok := nextDeletes[path]; !ok {
			changes = append(changes, configChange(model.ChangeRemove, path, "- config "+path+" deletion removed"))
		}
	}
	if !sameStrings(previous.Excluded, next.Excluded) {
		changes = append(changes, configChange(model.ChangeModify, "exclusions", "~ config exclusions changed"))
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Name < changes[j].Name })
	return changes
}

type PlanOptions struct{ Force bool }

// PlanOverlay restores sparse HOME-relative snapshots without replacing unknown
// target changes. When the shipped baseline changed, it applies the captured
// user delta with a three-way merge.
func (p Provider) PlanOverlay(saved profile.Configs, scan ScanSummary, schema int, from, to string, options ...PlanOptions) (model.RestorePlan, error) {
	force := len(options) > 0 && options[0].Force
	if err := p.Check(saved); err != nil {
		return model.RestorePlan{}, err
	}
	byPath := map[string]Candidate{}
	for _, c := range scan.Candidates {
		byPath[c.Path] = c
	}
	plan := model.RestorePlan{ProfileVersion: schema, OmarchyFrom: from, OmarchyTo: to}
	var reloadDependencies []string
	for _, file := range saved.Files {
		candidate, ok := byPath[file.Path]
		desired, err := p.resolveDesiredFile(file, candidate, ok)
		if err != nil {
			return model.RestorePlan{}, err
		}
		if desired.state == desiredSatisfied {
			continue
		}
		if desired.conflict {
			if force {
				op, err := p.forceFileWrite(file, "merge conflicted; captured user version will win")
				if err != nil {
					return model.RestorePlan{}, err
				}
				plan.Operations = append(plan.Operations, op)
				if isHyprConfigPath(file.Path) {
					reloadDependencies = append(reloadDependencies, op.ID)
				}
				continue
			}
			reason := "Omarchy baseline changed; merge conflict requires review"
			if desired.mergeErr {
				reason = "Omarchy baseline changed; configuration is not mergeable"
			}
			plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "config", Resource: "config:" + file.Path, Reason: reason})
			continue
		}
		if desired.state == desiredUnknown {
			if force {
				op, err := p.forceFileWrite(file, "replace unknown target")
				if err != nil {
					return model.RestorePlan{}, err
				}
				plan.Operations = append(plan.Operations, op)
				if isHyprConfigPath(file.Path) {
					reloadDependencies = append(reloadDependencies, op.ID)
				}
				continue
			}
			plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "config", Resource: "config:" + file.Path, Reason: "existing user configuration differs; overwrite disabled"})
			continue
		}
		destination, err := p.absoluteUserPath(file.Path)
		if err != nil {
			return model.RestorePlan{}, err
		}
		write := model.FileWrite{Destination: destination, RejectSymlinkParents: true}
		if mode, ok := configMode(file.Mode); ok {
			write.Mode = &mode
		}
		if desired.state == desiredMissing {
			write.ExpectedMissing = true
		} else {
			precondition, err := p.configCandidatePrecondition(candidate)
			if err != nil {
				return model.RestorePlan{}, err
			}
			write.ReplaceExisting, write.ExpectedExisting, write.Backup = true, &precondition, true
		}
		write.SourceHash = desired.hash
		if desired.content != nil {
			write.Generated, write.Content = true, desired.content
		} else {
			write.Source = p.overlaySnapshotPath("files", file.Path)
		}
		id := "config.write." + configOperationID(file.Path)
		risk := model.RiskMedium
		if file.BaselineHash == "" && desired.state == desiredMissing {
			risk = model.RiskLow
		}
		plan.Operations = append(plan.Operations, model.Operation{ID: id, Provider: "config", Action: "write", Resource: "config:" + file.Path, File: &write, Risk: risk, Reversible: true})
		if isHyprConfigPath(file.Path) {
			reloadDependencies = append(reloadDependencies, id)
		}
	}
	for _, deletion := range saved.Deletes {
		candidate, ok := byPath[deletion.Path]
		desired := resolveDesiredDelete(deletion, candidate, ok)
		if desired == desiredSatisfied {
			continue
		}
		if desired == desiredUnknown {
			if force {
				op, err := p.forceDelete(deletion)
				if err != nil {
					return model.RestorePlan{}, err
				}
				plan.Operations = append(plan.Operations, op)
				if isHyprConfigPath(deletion.Path) {
					reloadDependencies = append(reloadDependencies, op.ID)
				}
				continue
			}
			plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "config", Resource: "config:" + deletion.Path, Reason: "existing user configuration differs; delete disabled"})
			continue
		}
		destination, err := p.absoluteUserPath(deletion.Path)
		if err != nil {
			return model.RestorePlan{}, err
		}
		id := "config.delete." + configOperationID(deletion.Path)
		precondition, err := p.configCandidatePrecondition(candidate)
		if err != nil {
			return model.RestorePlan{}, err
		}
		plan.Operations = append(plan.Operations, model.Operation{ID: id, Provider: "config", Action: "delete", Resource: "config:" + deletion.Path, Delete: &model.FileDelete{Destination: destination, ExpectedExisting: &precondition, Backup: true, RejectSymlinkParents: true}, Risk: model.RiskHigh, Reversible: true})
		if isHyprConfigPath(deletion.Path) {
			reloadDependencies = append(reloadDependencies, id)
		}
	}
	if len(reloadDependencies) > 0 {
		plan.Operations = append(plan.Operations, model.Operation{ID: "config.reload", Provider: "config", Action: "reload", Resource: "config:hyprctl", Command: []string{"hyprctl", "reload"}, DependsOn: reloadDependencies, Risk: model.RiskLow})
	}
	return plan, nil
}

type desiredState uint8

const (
	desiredSatisfied desiredState = iota
	desiredMissing
	desiredDirect
	desiredBaseline
	desiredMerge
	desiredUnknown
)

// desiredCandidate carries the effective desired state: B directly, or M after
// applying A->B to C. Keeping it here makes Plan, Diff, and Verify agree.
type desiredCandidate struct {
	state    desiredState
	hash     string
	mode     string
	content  []byte
	conflict bool
	mergeErr bool
}

func (p Provider) resolveDesiredFile(a profile.ConfigFile, t Candidate, exists bool) (desiredCandidate, error) {
	b, err := readExpectedRegularFile(p.overlaySnapshotPath("files", a.Path), a.Hash)
	if err != nil {
		return desiredCandidate{}, fmt.Errorf("read captured config %s: %w", a.Path, err)
	}
	direct := desiredCandidate{state: desiredDirect, hash: a.Hash, mode: a.Mode}
	if !exists {
		direct.state = desiredMissing
		return direct, nil
	}
	if t.UserHash == "" {
		if a.BaselineHash != "" {
			return p.resolveChangedBaseline(a, b, t, direct)
		}
		direct.state = desiredMissing
		return direct, nil
	}
	if a.BaselineHash == "" {
		if matchesEffective(t.UserHash, t.UserMode, a.Hash, a.Mode) {
			return desiredCandidate{state: desiredSatisfied, hash: a.Hash, mode: a.Mode}, nil
		}
		return desiredCandidate{state: desiredUnknown}, nil
	}
	// The current baseline disappeared. A is still a known safe target, but any
	// other target is user-owned and must be preserved unless force is requested.
	if t.BaselineHash == "" {
		if matchesEffective(t.UserHash, t.UserMode, a.BaselineHash, a.BaselineMode) {
			return direct, nil
		}
		return desiredCandidate{state: desiredUnknown}, nil
	}
	if baselineIdentity(t.BaselineHash, t.BaselineMode, a.BaselineHash, a.BaselineMode) {
		if matchesEffective(t.UserHash, t.UserMode, a.Hash, a.Mode) {
			return desiredCandidate{state: desiredSatisfied, hash: a.Hash, mode: a.Mode}, nil
		}
		if matchesEffective(t.UserHash, t.UserMode, a.BaselineHash, a.BaselineMode) {
			return direct, nil
		}
		return desiredCandidate{state: desiredUnknown}, nil
	}
	return p.resolveChangedBaseline(a, b, t, direct)
}

// resolveChangedBaseline calculates M before classifying T. A clean merge is
// the desired result after an upstream baseline change, including for T=A/B.
func (p Provider) resolveChangedBaseline(a profile.ConfigFile, b []byte, t Candidate, direct desiredCandidate) (desiredCandidate, error) {
	if t.BaselineHash == "" {
		if matchesEffective(t.UserHash, t.UserMode, a.BaselineHash, a.BaselineMode) {
			return direct, nil
		}
		return desiredCandidate{state: desiredUnknown}, nil
	}
	base, err := readExpectedRegularFile(p.overlaySnapshotPath("baseline", a.Path), a.BaselineHash)
	if err != nil {
		return desiredCandidate{}, fmt.Errorf("read captured baseline %s: %w", a.Path, err)
	}
	currentPath, err := p.absoluteBaselinePath(a.Path)
	if err != nil {
		return desiredCandidate{}, fmt.Errorf("resolve current baseline %s: %w", a.Path, err)
	}
	current, err := readExpectedRegularFile(currentPath, t.BaselineHash)
	if err != nil {
		return desiredCandidate{}, fmt.Errorf("read current baseline %s: %w", a.Path, err)
	}
	merged, err := MergeText3(base, b, current)
	if err != nil || merged.Conflicts {
		return desiredCandidate{state: desiredUnknown, conflict: true, mergeErr: err != nil}, nil
	}
	m := desiredCandidate{state: desiredMerge, hash: hashContent(merged.Content), mode: a.Mode, content: merged.Content}
	if t.UserHash == "" {
		m.state = desiredMissing
		return m, nil
	}
	if matchesEffective(t.UserHash, t.UserMode, m.hash, m.mode) {
		m.state = desiredSatisfied
		return m, nil
	}
	if matchesEffective(t.UserHash, t.UserMode, t.BaselineHash, t.BaselineMode) {
		return m, nil
	}
	if matchesEffective(t.UserHash, t.UserMode, a.Hash, a.Mode) {
		return m, nil
	}
	if matchesEffective(t.UserHash, t.UserMode, a.BaselineHash, a.BaselineMode) {
		return m, nil
	}
	return desiredCandidate{state: desiredUnknown}, nil
}

func matchesEffective(hash, mode, wantHash, wantMode string) bool {
	return hash == wantHash && (wantMode == "" || mode == wantMode)
}

// baselineIdentity compares schema-8 baseline identities. Schema-7 records
// have no mode, so their hash remains the compatible identity fallback.
func baselineIdentity(hash, mode, wantHash, wantMode string) bool {
	return hash == wantHash && (wantMode == "" || mode == wantMode)
}
func resolveDesiredDelete(a profile.ConfigDelete, m Candidate, exists bool) desiredState {
	if !exists || m.Classification == ConfigDeletedBaseline {
		return desiredSatisfied
	}
	if !baselineIdentity(m.BaselineHash, m.BaselineMode, a.BaselineHash, a.BaselineMode) {
		return desiredUnknown
	}
	if matchesEffective(m.UserHash, m.UserMode, m.BaselineHash, m.BaselineMode) {
		return desiredDirect
	}
	return desiredUnknown
}
func configMode(value string) (uint32, bool) {
	var mode uint32
	if _, err := fmt.Sscanf(value, "%o", &mode); err != nil || mode > 0o777 {
		return 0, false
	}
	return mode, value != ""
}
func (p Provider) configCandidatePrecondition(c Candidate) (model.FilesystemPrecondition, error) {
	path, err := p.absoluteUserPath(c.Path)
	if err != nil {
		return model.FilesystemPrecondition{}, err
	}
	if precondition, err := configFilesystemPrecondition(path); err == nil {
		return precondition, nil
	} else if !os.IsNotExist(err) {
		return model.FilesystemPrecondition{}, err
	}
	// Some callers construct plans from a persisted scan after the target has
	// disappeared. The executor still rejects a changed target at execution.
	mode, ok := configMode(c.UserMode)
	if !ok {
		mode = 0o644
	}
	return model.FilesystemPrecondition{Type: "file", Hash: c.UserHash, Mode: mode}, nil
}
func readExpectedRegularFile(path, expected string) ([]byte, error) {
	f, _, err := content.OpenRegularFile(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	if hashContent(data) != expected {
		return nil, fmt.Errorf("snapshot hash mismatch")
	}
	return data, nil
}

func (p Provider) forceFileWrite(file profile.ConfigFile, action string) (model.Operation, error) {
	destination, err := p.absoluteUserPath(file.Path)
	if err != nil {
		return model.Operation{}, err
	}
	precondition, err := configFilesystemPrecondition(destination)
	if err != nil {
		return model.Operation{}, err
	}
	return model.Operation{ID: "config.write." + configOperationID(file.Path), Provider: "config", Action: action, Resource: "config:" + file.Path, File: &model.FileWrite{Source: p.overlaySnapshotPath("files", file.Path), Destination: destination, SourceHash: file.Hash, ReplaceExisting: true, ExpectedExisting: &precondition, Backup: true, RejectSymlinkParents: true}, Risk: model.RiskHigh, Reversible: true}, nil
}

func (p Provider) forceDelete(deletion profile.ConfigDelete) (model.Operation, error) {
	destination, err := p.absoluteUserPath(deletion.Path)
	if err != nil {
		return model.Operation{}, err
	}
	precondition, err := configFilesystemPrecondition(destination)
	if err != nil {
		return model.Operation{}, err
	}
	return model.Operation{ID: "config.delete." + configOperationID(deletion.Path), Provider: "config", Action: "delete unknown target", Resource: "config:" + deletion.Path, Delete: &model.FileDelete{Destination: destination, ExpectedExisting: &precondition, Backup: true, RejectSymlinkParents: true}, Risk: model.RiskHigh, Reversible: true}, nil
}

func configFilesystemPrecondition(path string) (model.FilesystemPrecondition, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return model.FilesystemPrecondition{}, err
	}
	precondition := model.FilesystemPrecondition{Mode: uint32(info.Mode().Perm())}
	if info.Mode()&os.ModeSymlink != 0 {
		precondition.Type = "symlink"
		precondition.Target, err = os.Readlink(path)
		return precondition, err
	}
	if info.IsDir() {
		precondition.Type = "directory"
	} else if info.Mode().IsRegular() {
		precondition.Type = "file"
	} else {
		return precondition, fmt.Errorf("unsupported config target: %s", path)
	}
	precondition.Hash, err = content.HashFilesystemObject(path)
	return precondition, err
}

func hashContent(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func isHyprConfigPath(path string) bool {
	return strings.HasPrefix(path, ".config/hypr/") || strings.HasPrefix(path, "hypr/")
}

func (p Provider) overlaySnapshotPath(tree, logical string) string {
	path := filepath.Join(p.ProfileDir, "config", tree, filepath.FromSlash(logical))
	if _, err := os.Lstat(path); err == nil {
		return path
	}
	// Schema-7 snapshots were stored relative to ~/.config.
	return filepath.Join(p.ProfileDir, "config", tree, filepath.FromSlash(strings.TrimPrefix(logical, ".config/")))
}

func configOperationID(path string) string {
	return strings.NewReplacer("/", ".", " ", "_").Replace(strings.TrimPrefix(path, ".config/"))
}

func Diff(saved profile.Configs, current any) []model.Change {
	if state, ok := current.(State); ok {
		return diffLegacy(saved, state)
	}
	scan, ok := current.(ScanSummary)
	if !ok {
		return nil
	}
	changes := []model.Change{}
	files, deletes := configFiles(saved), configDeletes(saved)
	for _, c := range scan.Candidates {
		if f, ok := files[c.Path]; ok {
			delete(files, c.Path)
			if !matchesEffective(c.UserHash, c.UserMode, f.Hash, f.Mode) {
				changes = append(changes, configChange(model.ChangeModify, c.Path, "~ config "+c.Path+" differs"))
			}
			continue
		}
		if _, ok := deletes[c.Path]; ok {
			d := deletes[c.Path]
			delete(deletes, c.Path)
			if resolveDesiredDelete(d, c, true) != desiredSatisfied {
				changes = append(changes, configChange(model.ChangeModify, c.Path, "~ config "+c.Path+" deletion differs"))
			}
			continue
		}
		if c.Classification == ConfigAdded || c.Classification == ConfigModifiedBaseline {
			changes = append(changes, configChange(model.ChangeAdd, c.Path, "+ config "+c.Path+" added"))
		}
	}
	for path := range files {
		changes = append(changes, configChange(model.ChangeRemove, path, "- config "+path+" missing"))
	}
	// A missing candidate is an already-applied tombstone.
	sort.Slice(changes, func(i, j int) bool { return changes[i].Name < changes[j].Name })
	return changes
}

func Verify(saved profile.Configs, current any) model.VerificationResult {
	if state, ok := current.(State); ok {
		return verifyLegacy(saved, state)
	}
	scan, ok := current.(ScanSummary)
	if !ok {
		return model.VerificationResult{OK: false}
	}
	byPath := map[string]Candidate{}
	for _, c := range scan.Candidates {
		byPath[c.Path] = c
	}
	missing := []string{}
	for _, f := range saved.Files {
		c, ok := byPath[f.Path]
		if !ok || !matchesEffective(c.UserHash, c.UserMode, f.Hash, f.Mode) {
			missing = append(missing, "config:"+f.Path)
		}
	}
	for _, d := range saved.Deletes {
		c, ok := byPath[d.Path]
		if ok && resolveDesiredDelete(d, c, true) != desiredSatisfied {
			missing = append(missing, "config:"+d.Path)
		}
	}
	sort.Strings(missing)
	return model.VerificationResult{OK: len(missing) == 0, Missing: missing}
}

// Diff evaluates overlay state using the same effective B/M candidate as Plan.
func (p Provider) Diff(saved profile.Configs, scan ScanSummary) ([]model.Change, error) {
	byPath := map[string]Candidate{}
	for _, c := range scan.Candidates {
		byPath[c.Path] = c
	}
	changes := []model.Change{}
	for _, f := range saved.Files {
		c, ok := byPath[f.Path]
		desired, err := p.resolveDesiredFile(f, c, ok)
		if err != nil {
			return nil, err
		}
		if desired.state != desiredSatisfied {
			changes = append(changes, configChange(model.ChangeModify, f.Path, "~ config "+f.Path+" differs"))
		}
		delete(byPath, f.Path)
	}
	for _, d := range saved.Deletes {
		c, ok := byPath[d.Path]
		if ok && resolveDesiredDelete(d, c, true) != desiredSatisfied {
			changes = append(changes, configChange(model.ChangeModify, d.Path, "~ config "+d.Path+" deletion differs"))
		}
		delete(byPath, d.Path)
	}
	for _, c := range byPath {
		if c.Classification == ConfigAdded || c.Classification == ConfigModifiedBaseline {
			changes = append(changes, configChange(model.ChangeAdd, c.Path, "+ config "+c.Path+" added"))
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Name < changes[j].Name })
	return changes, nil
}

// Verify evaluates overlay state using the same effective B/M candidate as Plan.
func (p Provider) Verify(saved profile.Configs, scan ScanSummary) (model.VerificationResult, error) {
	byPath := map[string]Candidate{}
	for _, c := range scan.Candidates {
		byPath[c.Path] = c
	}
	missing := []string{}
	for _, f := range saved.Files {
		c, ok := byPath[f.Path]
		desired, err := p.resolveDesiredFile(f, c, ok)
		if err != nil {
			return model.VerificationResult{}, err
		}
		if desired.state != desiredSatisfied {
			missing = append(missing, "config:"+f.Path)
		}
	}
	for _, d := range saved.Deletes {
		if c, ok := byPath[d.Path]; ok && resolveDesiredDelete(d, c, true) != desiredSatisfied {
			missing = append(missing, "config:"+d.Path)
		}
	}
	sort.Strings(missing)
	return model.VerificationResult{OK: len(missing) == 0, Missing: missing}, nil
}

func (p Provider) Check(saved profile.Configs) error {
	if err := validateOverlay(saved); err != nil {
		return err
	}
	for _, f := range saved.Files {
		if err := p.checkPolicy("files", f.Path, saved.Excluded); err != nil {
			return err
		}
		if f.BaselineHash != "" {
			if err := p.checkPolicy("baseline", f.Path, saved.Excluded); err != nil {
				return err
			}
		}
		if err := p.checkOwnership(f.Path); err != nil {
			return err
		}
		if err := p.checkOverlaySnapshot("files", f.Path, f.Hash); err != nil {
			return err
		}
		if sensitive, err := hasSensitiveContent(p.overlaySnapshotPath("files", f.Path)); err != nil {
			return fmt.Errorf("config %s desired snapshot: %w", f.Path, err)
		} else if sensitive {
			return fmt.Errorf("config %s desired snapshot contains sensitive content", f.Path)
		}
		if f.BaselineHash != "" {
			if err := p.checkOverlaySnapshot("baseline", f.Path, f.BaselineHash); err != nil {
				return err
			}
		}
	}
	for _, d := range saved.Deletes {
		if err := p.checkPolicy("baseline", d.Path, saved.Excluded); err != nil {
			return err
		}
		if err := p.checkOwnership(d.Path); err != nil {
			return err
		}
		if err := p.checkOverlaySnapshot("baseline", d.Path, d.BaselineHash); err != nil {
			return err
		}
	}
	return nil
}

func (p Provider) checkPolicy(tree, logical string, excluded []string) error {
	if p.hasHomeNamespace() && !isAllowedConfigPath(logical) {
		return fmt.Errorf("config path is outside managed surfaces: %s", logical)
	}
	info, err := os.Lstat(p.overlaySnapshotPath(tree, logical))
	if err != nil {
		return fmt.Errorf("config %s snapshot: %w", logical, err)
	}
	if decision := ClassifyConfigPolicy(logical, info, excluded); decision.Reason != PolicyAllowed {
		return fmt.Errorf("config path %s is %s by policy", logical, decision.Reason)
	}
	return nil
}

func isAllowedConfigPath(path string) bool {
	if strings.HasPrefix(path, ".config/") {
		return true
	}
	for _, spec := range DefaultHomeConfigSpecs() {
		if path == spec.Path {
			return true
		}
	}
	return false
}

func (p Provider) checkOwnership(logical string) error {
	path, err := p.absoluteUserPath(logical)
	if err != nil {
		return err
	}
	for _, claim := range p.Ownership.TrackConflict(path) {
		if claim.Provider != "config" {
			return fmt.Errorf("config path %s conflicts with %s ownership", logical, claim.Provider)
		}
	}
	return nil
}

func (p Provider) checkOverlaySnapshot(tree, logical, hash string) error {
	path := p.overlaySnapshotPath(tree, logical)
	got, err := content.HashRegularFile(path)
	if err != nil {
		return fmt.Errorf("config %s snapshot: %w", logical, err)
	}
	if got != hash {
		return fmt.Errorf("config %s snapshot hash mismatch", logical)
	}
	return nil
}
func validateOverlay(state profile.Configs) error {
	seen := map[string]bool{}
	for _, f := range state.Files {
		if err := profile.ValidateConfigPath(f.Path); err != nil {
			return err
		}
		if seen[f.Path] {
			return fmt.Errorf("duplicate config path %q", f.Path)
		}
		seen[f.Path] = true
	}
	for _, d := range state.Deletes {
		if err := profile.ValidateConfigPath(d.Path); err != nil {
			return err
		}
		if seen[d.Path] {
			return fmt.Errorf("duplicate config path %q", d.Path)
		}
		seen[d.Path] = true
	}
	for _, e := range state.Excluded {
		if err := profile.ValidateConfigPath(e); err != nil {
			return err
		}
	}
	return nil
}
func configFiles(s profile.Configs) map[string]profile.ConfigFile {
	m := map[string]profile.ConfigFile{}
	for _, f := range s.Files {
		m[f.Path] = f
	}
	return m
}
func configDeletes(s profile.Configs) map[string]profile.ConfigDelete {
	m := map[string]profile.ConfigDelete{}
	for _, d := range s.Deletes {
		m[d.Path] = d
	}
	return m
}
func configChange(kind model.ChangeType, path, summary string) model.Change {
	return model.Change{Type: kind, Provider: "config", Kind: "config", Name: path, Summary: summary}
}
func sameStrings(a, b []string) bool {
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
