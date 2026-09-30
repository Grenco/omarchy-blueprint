package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"

	"github.com/Grenco/omarchy-blueprint/internal/content"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/sensitive"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

const maxAuthoredUnitBytes = 1 << 20

type preparedCapture struct {
	stage, old string
	committed  bool
}

type capturedFile struct {
	data []byte
	hash string
	mode string
}

func readServiceFile(path string) (capturedFile, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || resolved != filepath.Clean(path) {
		return capturedFile{}, fmt.Errorf("service source %q crosses a link or is unavailable", path)
	}
	f, info, err := content.OpenRegularFile(path)
	if err != nil {
		return capturedFile{}, err
	}
	defer f.Close()
	if info.Size() > maxAuthoredUnitBytes {
		return capturedFile{}, fmt.Errorf("service source %q exceeds the capture size limit", path)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxAuthoredUnitBytes+1))
	if err != nil || len(data) > maxAuthoredUnitBytes {
		return capturedFile{}, fmt.Errorf("read bounded service source %q: %v", path, err)
	}
	result, err := sensitive.ScanReader(bytes.NewReader(data), maxAuthoredUnitBytes)
	if err != nil || result.Sensitive {
		return capturedFile{}, fmt.Errorf("service source %q contains sensitive content or cannot be inspected: %v", path, err)
	}
	sum := sha256.Sum256(data)
	return capturedFile{data: data, hash: hex.EncodeToString(sum[:]), mode: fmt.Sprintf("%04o", info.Mode().Perm())}, nil
}

func (p *Provider) Capture(ctx context.Context, data *profile.Data, capCtx workflow.CaptureContext) (any, []model.Change, error) {
	if p.prepared != nil {
		return nil, nil, errors.New("Services Capture already has a staged generation")
	}
	candidates, err := p.discovered(ctx, data.Services)
	if err != nil {
		return nil, nil, err
	}
	prior := map[string]profile.ServiceUnit{}
	for _, unit := range data.Services.Units {
		prior[unit.Name] = unit
	}
	fresh := map[string]capturedFile{}
	var next profile.Services
	var changes []model.Change
	for _, candidate := range candidates {
		name := candidate.Unit.Name
		decision, err := capCtx.Require(name)
		if err != nil {
			return nil, nil, err
		}
		old, managed := prior[name]
		if !managed && (!candidate.Eligible || !decision.Capture || !decision.Selected) {
			continue
		}
		if managed && (!decision.Capture || !candidate.Eligible || (managedServiceArtifactMissing(old, candidate, p.Roots) || candidate.OwnershipExpansion || candidate.NewOverlayArtifact) && !decision.Selected) {
			next.Units = append(next.Units, old)
			continue
		}
		if !candidate.Eligible || !decision.Capture {
			return nil, nil, fmt.Errorf("service %q is not eligible for Capture", name)
		}
		if candidate.Missing {
			absent := old
			if absent.Management == profile.ServiceManagementDefinition {
				absent.Presence = profile.ServiceAbsent
			}
			absent.StartIntent, absent.ActivationPreference, absent.ObservedActive = profile.ServiceStartNotManaged, profile.ServiceActivationPersistentOnly, false
			absent.DropIns = append([]profile.ServiceArtifact(nil), old.DropIns...)
			for n := range absent.DropIns {
				absent.DropIns[n].Presence = profile.ServiceAbsent
			}
			if absent.Mask != nil {
				absent.Mask = &profile.ServiceMask{Presence: profile.ServiceAbsent}
			}
			absent.Instances = append([]profile.ServiceInstance(nil), old.Instances...)
			for n := range absent.Instances {
				absent.Instances[n].Presence, absent.Instances[n].StartIntent = profile.ServiceAbsent, profile.ServiceStartNotManaged
			}
			next.Units = append(next.Units, absent)
			changes = append(changes, serviceChange(model.ChangeRemove, name, "Remember reviewed service removal"))
			continue
		}
		unit, artifacts, err := p.projectServiceUnit(candidate, old, managed, decision.Selected)
		if err != nil {
			return nil, nil, err
		}
		for path, file := range artifacts {
			fresh[path] = file
		}
		next.Units = append(next.Units, unit)
		kind := model.ChangeAdd
		if managed {
			kind = model.ChangeModify
		}
		changes = append(changes, serviceChange(kind, name, "Remember selected user-service intent"))
	}
	if len(next.Units) == 0 && !data.Manifest.Capture.Services {
		return nil, nil, nil
	}
	if p.ProfileDir == "" {
		return nil, nil, fmt.Errorf("profile directory is required to capture Services")
	}
	stage, err := p.stageServiceFiles(next, fresh)
	if err != nil {
		return nil, nil, err
	}
	p.prepared = &preparedCapture{stage: stage}
	data.Services = next
	data.Manifest.Capture.Services = true
	return next, changes, nil
}

func serviceChange(kind model.ChangeType, name, summary string) model.Change {
	return model.Change{Type: kind, Provider: "services", Kind: "user-service", Name: name, Summary: summary + ": " + name}
}

// projectServiceUnit is the exact semantic metadata Capture would persist for
// an available unit. Diff uses this same projection instead of guessing which
// fields might change, so approved previews include every captured effect.
func (p Provider) projectServiceUnit(candidate Candidate, old profile.ServiceUnit, managed, selected bool) (profile.ServiceUnit, map[string]capturedFile, error) {
	name := candidate.Unit.Name
	artifacts := map[string]capturedFile{}
	unit := profile.ServiceUnit{
		Name: name, Kind: candidate.Unit.Kind, Management: candidate.Management, Presence: profile.ServicePresent,
		StartIntent: candidate.Unit.StartIntent, ActivationPreference: profile.ServiceActivationPersistentOnly,
		ObservedActive: candidate.Unit.ObservedActive, LinkedSource: candidate.Unit.LinkedSource,
	}
	if managed {
		unit.ActivationPreference = old.ActivationPreference
	}
	if unit.StartIntent == "" {
		unit.StartIntent = profile.ServiceStartNotManaged
	}
	if unit.Management == profile.ServiceManagementCustomization {
		unit.StartIntent = profile.ServiceStartNotManaged
		if candidate.Unit.RawUnitFileState == "masked" && userMask(name, p.Roots) {
			unit.StartIntent, unit.Mask = profile.ServiceStartMasked, &profile.ServiceMask{Presence: profile.ServicePresent}
		}
	}
	if unit.Management == profile.ServiceManagementDefinition {
		unit.Definition = "units/" + name
		file, err := readServiceFile(candidate.Unit.FragmentPath)
		if err != nil {
			return profile.ServiceUnit{}, nil, fmt.Errorf("capture service %q definition: %w", name, err)
		}
		unit.DefinitionHash, artifacts[unit.Definition] = file.hash, file
	}
	managedDropIns := map[string]bool{}
	for _, item := range old.DropIns {
		managedDropIns[item.Path] = true
	}
	for _, path := range candidate.Unit.DropInPaths {
		if !withinUserRoot(path, p.Roots.UserConfigDir) {
			continue
		}
		rel := "units/" + name + ".d/" + filepath.Base(path)
		if managed && !managedDropIns[rel] && old.Management == profile.ServiceManagementCustomization && !selected {
			continue // A new overlay artifact needs reviewed ownership.
		}
		file, err := readServiceFile(path)
		if err != nil {
			return profile.ServiceUnit{}, nil, fmt.Errorf("capture service %q drop-in: %w", name, err)
		}
		unit.DropIns = append(unit.DropIns, profile.ServiceArtifact{Path: rel, Presence: profile.ServicePresent, Hash: file.hash, Mode: file.mode})
		artifacts[rel] = file
	}
	for _, item := range old.DropIns {
		if _, ok := artifacts[item.Path]; !ok && managed {
			if selected && item.Presence == profile.ServicePresent && !managedDropInPresent(name, item, candidate.Unit, p.Roots) {
				item.Presence = profile.ServiceAbsent
			}
			unit.DropIns = append(unit.DropIns, item)
		}
	}
	if managed && old.Mask != nil && unit.Mask == nil {
		unit.Mask = old.Mask
		if selected && old.Mask.Presence == profile.ServicePresent && !userMask(name, p.Roots) {
			unit.Mask = &profile.ServiceMask{Presence: profile.ServiceAbsent}
		} else if old.Mask.Presence == profile.ServicePresent {
			unit.StartIntent = profile.ServiceStartMasked
		}
	}
	if managed {
		unit.Instances = old.Instances
	}
	sort.Slice(unit.DropIns, func(i, j int) bool { return unit.DropIns[i].Path < unit.DropIns[j].Path })
	return unit, artifacts, nil
}

func (p *Provider) stageServiceFiles(next profile.Services, fresh map[string]capturedFile) (string, error) {
	parent := filepath.Join(p.ProfileDir, "services")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", err
	}
	stage, err := os.MkdirTemp(parent, ".capture-*")
	if err != nil {
		return "", err
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(stage)
		}
	}()
	files := map[string]string{}
	for _, unit := range next.Units {
		if unit.Presence == profile.ServicePresent && unit.Definition != "" {
			files[unit.Definition] = unit.DefinitionHash
		}
		for _, dropIn := range unit.DropIns {
			if dropIn.Presence == profile.ServicePresent {
				files[dropIn.Path] = dropIn.Hash
			}
		}
	}
	for rel, wantHash := range files {
		if wantHash == "" {
			return "", fmt.Errorf("service artifact %q has no captured content hash", rel)
		}
		file, ok := fresh[rel]
		if !ok {
			file, err = readServiceFile(filepath.Join(parent, filepath.FromSlash(rel)))
			if err != nil {
				return "", fmt.Errorf("preserve service artifact %q: %w", rel, err)
			}
		}
		if file.hash != wantHash {
			return "", fmt.Errorf("service artifact %q changed since Capture", rel)
		}
		target := filepath.Join(stage, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(target, file.data, 0o644); err != nil {
			return "", err
		}
	}
	keep = true
	return stage, nil
}

func (p *Provider) CommitCapture() error {
	if p.prepared == nil {
		return nil
	}
	parent := filepath.Join(p.ProfileDir, "services")
	destination, old := filepath.Join(parent, "units"), filepath.Join(parent, ".units-previous")
	if _, err := os.Lstat(old); err == nil {
		return fmt.Errorf("Services previous-generation path already exists")
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Join(p.prepared.stage, "units"), 0o755); err != nil {
		return err
	}
	if _, err := os.Lstat(destination); err == nil {
		if err := os.Rename(destination, old); err != nil {
			return err
		}
		p.prepared.old = old
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(filepath.Join(p.prepared.stage, "units"), destination); err != nil {
		if p.prepared.old != "" {
			_ = os.Rename(old, destination)
			p.prepared.old = ""
		}
		return err
	}
	p.prepared.committed = true
	return nil
}

func (p *Provider) FinalizeCapture() error {
	if p.prepared == nil {
		return nil
	}
	prepared := p.prepared
	p.prepared = nil
	return errors.Join(os.RemoveAll(prepared.stage), removeIfSet(prepared.old))
}

func removeIfSet(path string) error {
	if path == "" {
		return nil
	}
	return os.RemoveAll(path)
}

func (p *Provider) RollbackCapture() error {
	if p.prepared == nil {
		return nil
	}
	prepared := p.prepared
	p.prepared = nil
	var err error
	if prepared.committed {
		destination := filepath.Join(p.ProfileDir, "services", "units")
		err = os.RemoveAll(destination)
		if prepared.old != "" && err == nil {
			err = os.Rename(prepared.old, destination)
		}
	}
	return errors.Join(err, os.RemoveAll(prepared.stage))
}

func (p Provider) Diff(ctx context.Context, d profile.Data) ([]model.Change, error) {
	candidates, err := p.discovered(ctx, d.Services)
	if err != nil {
		return nil, err
	}
	savedByName := map[string]profile.ServiceUnit{}
	for _, unit := range d.Services.Units {
		savedByName[unit.Name] = unit
	}
	var changes []model.Change
	for _, candidate := range candidates {
		if !candidate.Managed {
			continue
		}
		saved := savedByName[candidate.Unit.Name]
		if candidate.Missing {
			if saved.Presence == profile.ServicePresent {
				changes = append(changes, serviceChange(model.ChangeModify, saved.Name, "Managed service is missing"))
			}
			continue
		}
		if saved.Management != candidate.Management || managedServiceArtifactMissing(saved, candidate, p.Roots) || candidate.NewOverlayArtifact || !candidate.Eligible {
			changes = append(changes, serviceChange(model.ChangeModify, saved.Name, "Managed service state differs"))
			continue
		}
		projected, _, err := p.projectServiceUnit(candidate, saved, true, false)
		if err != nil {
			changes = append(changes, serviceChange(model.ChangeWarn, saved.Name, "Managed service cannot be safely inspected"))
			continue
		}
		prior := saved
		prior.DropIns = append([]profile.ServiceArtifact(nil), saved.DropIns...)
		sort.Slice(prior.DropIns, func(i, j int) bool { return prior.DropIns[i].Path < prior.DropIns[j].Path })
		if !reflect.DeepEqual(prior, projected) {
			changes = append(changes, serviceChange(model.ChangeModify, saved.Name, "Managed service state differs"))
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Name < changes[j].Name })
	return changes, nil
}

func (p Provider) Check(_ context.Context, d profile.Data) error {
	if err := profile.Validate(d); err != nil {
		return err
	}
	for _, unit := range d.Services.Units {
		if unit.Presence == profile.ServicePresent && unit.Definition != "" {
			if err := p.checkServiceArtifact(unit.Definition, unit.DefinitionHash); err != nil {
				return err
			}
		}
		for _, dropIn := range unit.DropIns {
			if dropIn.Presence == profile.ServicePresent {
				if err := p.checkServiceArtifact(dropIn.Path, dropIn.Hash); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (p Provider) checkServiceArtifact(path, expected string) error {
	if expected == "" {
		return fmt.Errorf("service artifact %q has no content provenance", path)
	}
	file, err := readServiceFile(filepath.Join(p.ProfileDir, "services", filepath.FromSlash(path)))
	if err != nil {
		return err
	}
	if file.hash != expected {
		return fmt.Errorf("service artifact %q hash mismatch", path)
	}
	return nil
}
