package workflow

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/machine"
	"github.com/Grenco/omarchy-blueprint/internal/ownership"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	configprovider "github.com/Grenco/omarchy-blueprint/internal/providers/config"
	resourcesprovider "github.com/Grenco/omarchy-blueprint/internal/providers/resources"
)

// AddMachine creates a portable overlay and optionally selects it locally.
func (s *Session) AddMachine(_ context.Context, name string, use bool) error {
	if err := machine.ValidateName(name); err != nil {
		return err
	}
	if _, err := machine.Select(name, "", s.profile.Machines.Items); err == nil {
		return fmt.Errorf("machine %q already exists", name)
	}
	s.profile.Machines.Items = append(s.profile.Machines.Items, profile.Machine{Name: name})
	if err := s.saveMachines(); err != nil {
		return err
	}
	if use {
		store, err := s.bindingStore()
		if err != nil {
			return err
		}
		if err := store.Save(s.opts.ProfileDir, name); err != nil {
			return err
		}
	}
	return s.Reload()
}

func (s *Session) UseMachine(_ context.Context, name string) error {
	if _, err := machine.Select(name, "", s.profile.Machines.Items); err != nil {
		return err
	}
	store, err := s.bindingStore()
	if err != nil {
		return err
	}
	if err := store.Save(s.opts.ProfileDir, name); err != nil {
		return err
	}
	return s.Reload()
}

func (s *Session) ClearMachine(_ context.Context) error {
	store, err := s.bindingStore()
	if err != nil {
		return err
	}
	if err := store.Clear(s.opts.ProfileDir); err != nil {
		return err
	}
	return s.Reload()
}

// SuggestedMachineName returns an available name without deriving identity from
// the hostname; callers still explicitly confirm the returned value.
func (s *Session) SuggestedMachineName() (string, error) {
	hostname, err := s.deps.Hostname()
	if err != nil {
		return "", err
	}
	return machine.SuggestName(hostname, s.profile.Machines.Items), nil
}

func (s *Session) RenameMachine(_ context.Context, oldName, newName string) error {
	if err := machine.ValidateName(newName); err != nil {
		return err
	}
	old := -1
	for i, item := range s.profile.Machines.Items {
		if item.Name == oldName {
			old = i
		}
		if item.Name == newName {
			return fmt.Errorf("machine %q already exists", newName)
		}
	}
	if old < 0 {
		return fmt.Errorf("machine %q does not exist", oldName)
	}
	s.profile.Machines.Items[old].Name = newName
	if err := s.saveMachines(); err != nil {
		return err
	}
	store, err := s.bindingStore()
	if err != nil {
		return err
	}
	bound, err := store.Load(s.opts.ProfileDir)
	if err != nil {
		return err
	}
	if bound == oldName {
		if err := store.Save(s.opts.ProfileDir, newName); err != nil {
			return err
		}
	}
	return s.Reload()
}

func (s *Session) RemoveMachine(_ context.Context, name string) error {
	index := -1
	for i, item := range s.profile.Machines.Items {
		if item.Name == name {
			index = i
			break
		}
	}
	if index < 0 {
		return fmt.Errorf("machine %q does not exist", name)
	}
	s.profile.Machines.Items = append(s.profile.Machines.Items[:index], s.profile.Machines.Items[index+1:]...)
	if err := s.saveMachines(); err != nil {
		return err
	}
	store, err := s.bindingStore()
	if err != nil {
		return err
	}
	bound, err := store.Load(s.opts.ProfileDir)
	if err != nil {
		return err
	}
	if bound == name {
		if err := store.Clear(s.opts.ProfileDir); err != nil {
			return err
		}
	}
	return s.Reload()
}

func (s *Session) MapResource(_ context.Context, machineName, resourceID, path string) error {
	selected, err := s.machineByName(machineName)
	if err != nil {
		return err
	}
	resourceID = strings.TrimPrefix(resourceID, "resource:")
	if !hasResource(s.profile.Resources.Items, resourceID) {
		return fmt.Errorf("resource %q is not tracked", resourceID)
	}
	home, err := s.deps.HomeDir()
	if err != nil {
		return err
	}
	path, err = machine.NormalizeMappingPath(home, path)
	if err != nil {
		return err
	}
	candidate := *selected
	candidate.ResourcePaths = append([]profile.MachineResourcePath(nil), selected.ResourcePaths...)
	found := false
	for i := range candidate.ResourcePaths {
		if candidate.ResourcePaths[i].Resource == resourceID {
			candidate.ResourcePaths[i].Path, found = path, true
		}
	}
	if !found {
		candidate.ResourcePaths = append(candidate.ResourcePaths, profile.MachineResourcePath{Resource: resourceID, Path: path})
	}
	if err := s.validateMachineMappings(home, &candidate); err != nil {
		return err
	}
	for i := range s.profile.Machines.Items {
		if s.profile.Machines.Items[i].Name == candidate.Name {
			s.profile.Machines.Items[i] = candidate
		}
	}
	if err := s.saveMachines(); err != nil {
		return err
	}
	return s.Reload()
}

func (s *Session) UnmapResource(_ context.Context, machineName, resourceID string) error {
	selected, err := s.machineByName(machineName)
	if err != nil {
		return err
	}
	resourceID = strings.TrimPrefix(resourceID, "resource:")
	candidate := *selected
	candidate.ResourcePaths = nil
	for _, mapping := range selected.ResourcePaths {
		if mapping.Resource != resourceID {
			candidate.ResourcePaths = append(candidate.ResourcePaths, mapping)
		}
	}
	if len(candidate.ResourcePaths) == len(selected.ResourcePaths) {
		return nil
	}
	for i := range s.profile.Machines.Items {
		if s.profile.Machines.Items[i].Name == candidate.Name {
			s.profile.Machines.Items[i] = candidate
		}
	}
	if err := s.saveMachines(); err != nil {
		return err
	}
	return s.Reload()
}

func (s *Session) bindingStore() (machine.BindingStore, error) {
	state, err := s.deps.StateHome()
	if err != nil {
		return machine.BindingStore{}, err
	}
	return machine.BindingStore{StateHome: state}, nil
}

func (s *Session) saveMachines() error {
	if s.deps.Now == nil {
		return errors.New("workflow clock is unavailable")
	}
	s.profile.Manifest.Profile.UpdatedAt = s.deps.Now().UTC()
	if err := profile.Save(s.opts.ProfileDir, s.profile); err != nil {
		return fmt.Errorf("save profile: %w", err)
	}
	return nil
}

func (s *Session) machineByName(name string) (*profile.Machine, error) {
	if name == "" {
		return nil, errors.New("no machine selected; use `machine use <name>` or pass `--machine <name>`")
	}
	for i := range s.profile.Machines.Items {
		if s.profile.Machines.Items[i].Name == name {
			return &s.profile.Machines.Items[i], nil
		}
	}
	return nil, fmt.Errorf("machine %q does not exist", name)
}

func hasResource(items []profile.Resource, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func (s *Session) validateMachineMappings(home string, candidate *profile.Machine) error {
	state, err := s.deps.StateHome()
	if err != nil {
		return err
	}
	roots, _, err := machine.ResolveEffectiveRoots(home, s.opts.ProfileDir, filepath.Join(state, "omarchy-blueprint"), s.profile.Resources, candidate)
	if err != nil {
		return err
	}
	claims := ownership.Index{Claims: []ownership.Claim{{Provider: "profile", Path: s.opts.ProfileDir, Recursive: true}, {Provider: "state", Path: state, Recursive: true}}}
	if s.deps.ConfigDirs != nil {
		if _, user, err := s.deps.ConfigDirs(); err == nil {
			for _, spec := range configprovider.DefaultSpecs {
				claims.Claims = append(claims.Claims, ownership.Claim{Provider: "config", Path: filepath.Join(user, spec.Path)})
			}
		}
	}
	if s.deps.ShellPaths != nil {
		if _, user, err := s.deps.ShellPaths(); err == nil {
			claims.Claims = append(claims.Claims, ownership.Claim{Provider: "shell", Path: user})
		}
	}
	if s.deps.HooksDir != nil {
		if hooks, err := s.deps.HooksDir(); err == nil {
			claims.Claims = append(claims.Claims, ownership.Claim{Provider: "hooks", Path: hooks, Recursive: true, DelegateSymlinks: true})
		}
	}
	if s.deps.ThemeDirs != nil {
		if _, themes, err := s.deps.ThemeDirs(); err == nil {
			claims.Claims = append(claims.Claims, ownership.Claim{Provider: "themes", Path: themes, Recursive: true})
		}
	}
	if s.deps.PluginDir != nil {
		if plugins, err := s.deps.PluginDir(); err == nil {
			claims.Claims = append(claims.Claims, ownership.Claim{Provider: "plugins", Path: plugins, Recursive: true})
		}
	}
	return resourcesprovider.ValidateEffectiveOwnership(roots, claims)
}
