package config

import (
	"fmt"
	"path/filepath"
	"sort"

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
		if err := p.checkOwnership(f.Path); err != nil {
			return err
		}
		if err := p.checkOverlaySnapshot("files", f.Path, f.Hash); err != nil {
			return err
		}
		if f.BaselineHash != "" {
			if err := p.checkOverlaySnapshot("baseline", f.Path, f.BaselineHash); err != nil {
				return err
			}
		}
	}
	for _, d := range saved.Deletes {
		if err := p.checkOwnership(d.Path); err != nil {
			return err
		}
		if err := p.checkOverlaySnapshot("baseline", d.Path, d.BaselineHash); err != nil {
			return err
		}
	}
	return nil
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
	path := filepath.Join(p.ProfileDir, "config", tree, filepath.FromSlash(logical))
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
