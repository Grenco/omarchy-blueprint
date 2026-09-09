package config

import (
	"fmt"
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

// PlanOverlay restores sparse HOME-relative snapshots only where the target is
// absent or still matches the captured Omarchy baseline. Tombstones remain
// persisted desired state, but Slice A does not mutate live files for them.
func (p Provider) PlanOverlay(saved profile.Configs, scan ScanSummary, schema int, from, to string) (model.RestorePlan, error) {
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
		if !ok || candidate.Classification == ConfigUnsupported {
			plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "config", Resource: "config:" + file.Path, Reason: "target config path unavailable"})
			continue
		}
		if candidate.UserHash == file.Hash {
			continue
		}
		if candidate.UserHash != "" && (file.BaselineHash == "" || candidate.UserHash != file.BaselineHash) {
			plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "config", Resource: "config:" + file.Path, Reason: "existing user configuration differs; overwrite disabled"})
			continue
		}
		if file.BaselineHash != "" && candidate.BaselineHash != file.BaselineHash {
			plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "config", Resource: "config:" + file.Path, Reason: "Omarchy baseline changed; migration required"})
			continue
		}
		destination, err := p.absoluteUserPath(file.Path)
		if err != nil {
			return model.RestorePlan{}, err
		}
		source := p.overlaySnapshotPath("files", file.Path)
		write := model.FileWrite{Source: source, Destination: destination, SourceHash: file.Hash, Backup: candidate.UserHash != "", RejectSymlinkParents: true}
		if candidate.UserHash == "" {
			write.ExpectedMissing = true
		} else {
			write.ExpectedHash = candidate.UserHash
		}
		id := "config.write." + configOperationID(file.Path)
		plan.Operations = append(plan.Operations, model.Operation{ID: id, Provider: "config", Action: "write", Resource: "config:" + file.Path, File: &write, Risk: model.RiskMedium, Reversible: true})
		if isHyprConfigPath(file.Path) {
			reloadDependencies = append(reloadDependencies, id)
		}
	}
	if len(reloadDependencies) > 0 {
		plan.Operations = append(plan.Operations, model.Operation{ID: "config.reload", Provider: "config", Action: "reload", Resource: "config:hyprctl", Command: []string{"hyprctl", "reload"}, DependsOn: reloadDependencies, Risk: model.RiskLow})
	}
	return plan, nil
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
			if c.UserHash != f.Hash {
				changes = append(changes, configChange(model.ChangeModify, c.Path, "~ config "+c.Path+" differs"))
			}
			continue
		}
		if _, ok := deletes[c.Path]; ok {
			delete(deletes, c.Path)
			if c.Classification != ConfigDeletedBaseline {
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
	for path := range deletes {
		changes = append(changes, configChange(model.ChangeRemove, path, "- config "+path+" tombstone missing"))
	}
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
		if !ok || c.UserHash != f.Hash {
			missing = append(missing, "config:"+f.Path)
		}
	}
	for _, d := range saved.Deletes {
		c, ok := byPath[d.Path]
		if !ok || c.Classification != ConfigDeletedBaseline {
			missing = append(missing, "config:"+d.Path)
		}
	}
	sort.Strings(missing)
	return model.VerificationResult{OK: len(missing) == 0, Missing: missing}
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
