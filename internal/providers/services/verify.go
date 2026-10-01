package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/Grenco/omarchy-blueprint/internal/content"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func serviceFileMatches(path, hash, mode string, absent bool) bool {
	info, err := os.Lstat(path)
	if absent {
		return errors.Is(err, os.ErrNotExist)
	}
	if err != nil || !info.Mode().IsRegular() || hash == "" {
		return false
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || resolved != filepath.Clean(path) {
		return false
	}
	actualHash, err := content.HashRegularFile(path)
	if err != nil || actualHash != hash {
		return false
	}
	if mode != "" {
		want, err := strconv.ParseUint(mode, 8, 32)
		if err != nil || uint32(info.Mode().Perm()) != uint32(want) {
			return false
		}
	}
	return true
}

func (p *Provider) Verify(ctx context.Context, data profile.Data, rc workflow.RestoreContext) (model.VerificationResult, error) {
	result := model.VerificationResult{OK: true}
	if err := profile.Validate(data); err != nil {
		return result, err
	}
	selected, _, err := selectedServiceUnits(data, rc)
	if err != nil {
		return result, err
	}
	if len(selected) == 0 {
		return result, nil
	}
	if err := p.resolveSourceRoots(); err != nil {
		return result, err
	}
	if p.Systemd == nil {
		return result, fmt.Errorf("Services Verify needs user-manager inspection")
	}
	units, err := p.Systemd.InspectUserUnits(ctx)
	if err != nil {
		return result, err
	}
	current, err := observedByName(units)
	if err != nil {
		return result, err
	}
	exact := rc.Options.Convergence == policy.ConvergenceExact
	proposed := map[string][]byte{}
	for _, saved := range selected {
		actual, found := current[saved.Name]
		satisfied := true
		if saved.Presence == profile.ServicePresent && found && (actual.Generated || actual.Transient || actual.Runtime && !actual.Persistent || !actual.TopologyKnown) {
			satisfied = false
		}
		if saved.Presence == profile.ServicePresent && found && actual.LoadState != "" && actual.LoadState != "loaded" && saved.StartIntent != profile.ServiceStartMasked {
			satisfied = false
		}
		if saved.Management == profile.ServiceManagementDefinition && (saved.Presence == profile.ServicePresent || exact) {
			path := filepath.Join(p.Roots.UserConfigDir, saved.Name)
			satisfied = serviceFileMatches(path, saved.DefinitionHash, "", saved.Presence == profile.ServiceAbsent) && satisfied
			if saved.Presence == profile.ServicePresent && satisfied {
				if !found || !actual.TopologyKnown || actual.FragmentPath != path {
					satisfied = false
				} else if file, err := readServiceFile(path); err != nil {
					satisfied = false
				} else {
					proposed[saved.Name] = file.data
				}
			}
		} else if saved.Presence == profile.ServicePresent && (hasPresentDropIn(saved.DropIns) || saved.StartIntent != profile.ServiceStartMasked && saved.StartIntent != profile.ServiceStartNotManaged) {
			if !found || !actual.TopologyKnown || actual.FragmentPath == "" || actual.FragmentPath == "/dev/null" {
				satisfied = false
			} else {
				base, err := readServiceFile(actual.FragmentPath)
				if err != nil {
					satisfied = false
				} else {
					proposed[saved.Name] = base.data
				}
			}
		}
		if saved.LinkedSource != "" {
			target, err := os.Readlink(filepath.Join(p.Roots.UserConfigDir, saved.Name))
			_, sourceErr := os.Stat(saved.LinkedSource)
			if err != nil || target != saved.LinkedSource || sourceErr != nil {
				satisfied = false
			}
			if !found || !actual.TopologyKnown || actual.FragmentPath != saved.LinkedSource {
				satisfied = false
			}
			file, err := readServiceFile(saved.LinkedSource)
			if err != nil {
				satisfied = false
			} else {
				proposed[saved.Name] = file.data
			}
		}
		for _, dropIn := range saved.DropIns {
			if dropIn.Presence == profile.ServiceAbsent && !exact {
				continue
			}
			path := filepath.Join(p.Roots.UserConfigDir, saved.Name+".d", filepath.Base(dropIn.Path))
			if !serviceFileMatches(path, dropIn.Hash, dropIn.Mode, dropIn.Presence == profile.ServiceAbsent) {
				satisfied = false
				continue
			}
			if dropIn.Presence == profile.ServicePresent {
				file, err := readServiceFile(path)
				if err != nil {
					satisfied = false
				} else {
					proposed[saved.Name+".d/"+filepath.Base(dropIn.Path)] = file.data
				}
			}
		}
		if saved.Mask != nil && (saved.Mask.Presence == profile.ServicePresent || exact) {
			masked := userMask(saved.Name, p.Roots)
			if masked != (saved.Mask.Presence == profile.ServicePresent) {
				satisfied = false
			}
		}
		if saved.Presence == profile.ServicePresent {
			if !persistentStartMatches(saved.StartIntent, actual, found) {
				satisfied = false
			}
		}
		for _, instance := range saved.Instances {
			instanceActual, instanceFound := current[instance.Name]
			expectedSource := actual.FragmentPath
			if saved.Management == profile.ServiceManagementDefinition {
				expectedSource = filepath.Join(p.Roots.UserConfigDir, saved.Name)
			} else if saved.LinkedSource != "" {
				expectedSource = saved.LinkedSource
			}
			if hasPresentDropIn(instance.DropIns) && instanceFound && (!instanceActual.TopologyKnown || expectedSource == "" || instanceActual.FragmentPath != expectedSource) {
				satisfied = false
			}
			if instance.Presence == profile.ServiceAbsent {
				if exact && current[instance.Name].Name != "" && current[instance.Name].StartIntent != profile.ServiceStartDisabled {
					satisfied = false
				}
			} else {
				if saved.Presence != profile.ServicePresent {
					satisfied = false
				}
				if !persistentStartMatches(instance.StartIntent, current[instance.Name], current[instance.Name].Name != "") {
					satisfied = false
				}
				if instance.StartIntent != profile.ServiceStartMasked {
					if instanceActual.LoadState != "" && instanceActual.LoadState != "loaded" {
						satisfied = false
					}
					if instanceActual.TopologyKnown && instanceActual.FragmentPath != "" && instanceActual.FragmentPath != expectedSource {
						satisfied = false
					}
				}
			}
			if instance.Mask != nil && (instance.Mask.Presence == profile.ServicePresent || exact) && userMask(instance.Name, p.Roots) != (instance.Mask.Presence == profile.ServicePresent) {
				satisfied = false
			}
			if base, exists := proposed[saved.Name]; exists {
				proposed[instance.Name] = base
			}
			for _, dropIn := range instance.DropIns {
				if dropIn.Presence == profile.ServiceAbsent && !exact {
					continue
				}
				if !serviceFileMatches(filepath.Join(p.Roots.UserConfigDir, instance.Name+".d", filepath.Base(dropIn.Path)), dropIn.Hash, dropIn.Mode, dropIn.Presence == profile.ServiceAbsent) {
					satisfied = false
				} else if dropIn.Presence == profile.ServicePresent {
					if _, hasBase := proposed[instance.Name]; !hasBase {
						satisfied = false
						continue
					}
					file, err := readServiceFile(filepath.Join(p.Roots.UserConfigDir, instance.Name+".d", filepath.Base(dropIn.Path)))
					if err != nil {
						satisfied = false
					} else {
						proposed[instance.Name+".d/"+filepath.Base(dropIn.Path)] = file.data
					}
				}
			}
		}
		if !satisfied {
			result.Missing = append(result.Missing, saved.Name)
		}
	}
	if len(result.Missing) == 0 && len(proposed) > 0 {
		// Retain effective external overlays in the parser view too. The shared
		// context determines intent; no compatibility ranking is recalculated.
		for _, saved := range selected {
			if _, exists := proposed[saved.Name]; !exists {
				continue
			}
			for _, path := range current[saved.Name].DropInPaths {
				if filepath.Base(filepath.Dir(path)) != saved.Name+".d" {
					result.Missing = append(result.Missing, saved.Name)
					continue
				}
				file, err := readServiceFile(path)
				if err != nil {
					return result, err
				}
				proposed[saved.Name+".d/"+filepath.Base(path)] = file.data
			}
		}
		if err := p.validateProposedSet(ctx, proposed); err != nil {
			return model.VerificationResult{OK: false}, err
		}
	}
	sort.Strings(result.Missing)
	result.Missing = append(result.Missing, verifyActivation(current, rc)...)
	sort.Strings(result.Missing)
	result.OK = len(result.Missing) == 0
	return result, nil
}

func persistentStartMatches(desired profile.ServiceStartIntent, actual ObservedUnit, found bool) bool {
	if desired == "" || desired == profile.ServiceStartNotManaged {
		return true
	}
	return found && actual.StartIntent == desired
}
