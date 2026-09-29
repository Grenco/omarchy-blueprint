package profile

import (
	"fmt"
	"path"
	"sort"
	"strings"
	"unicode"
)

type ServiceManagementMode string
type ServicePresence string
type ServiceStartIntent string
type ServiceActivationPreference string

const (
	ServiceManagementDefinition    ServiceManagementMode = "definition"
	ServiceManagementCustomization ServiceManagementMode = "customization"

	ServicePresent ServicePresence = "present"
	ServiceAbsent  ServicePresence = "absent"

	ServiceStartEnabled    ServiceStartIntent = "enabled"
	ServiceStartDisabled   ServiceStartIntent = "disabled"
	ServiceStartMasked     ServiceStartIntent = "masked"
	ServiceStartIndirect   ServiceStartIntent = "indirect"
	ServiceStartNotManaged ServiceStartIntent = "not-managed"

	ServiceActivationPersistentOnly      ServiceActivationPreference = "persistent-only"
	ServiceActivationRestoreWorkingState ServiceActivationPreference = "restore-working-state"
	ServiceActivationReview              ServiceActivationPreference = "review"
)

// Services contains only selected, persistent user-service intent. Discovery
// candidates and runtime systemd state are not persisted here automatically.
type Services struct {
	Units []ServiceUnit `json:"units" toml:"unit"`
}

type ServiceUnit struct {
	Name                 string                      `json:"name" toml:"name"`
	Kind                 string                      `json:"kind" toml:"kind"`
	Management           ServiceManagementMode       `json:"management" toml:"management"`
	Presence             ServicePresence             `json:"presence" toml:"presence"`
	StartIntent          ServiceStartIntent          `json:"start_intent" toml:"start_intent"`
	ActivationPreference ServiceActivationPreference `json:"activation_preference,omitempty" toml:"activation_preference,omitempty"`
	ObservedActive       bool                        `json:"observed_active" toml:"observed_active"`
	// Definition names an exact authored file below services/units/. Empty
	// means the base is external: an overlay must never acquire that base.
	Definition     string `json:"definition,omitempty" toml:"definition,omitempty"`
	DefinitionHash string `json:"definition_hash,omitempty" toml:"definition_hash,omitempty"`
	// Mask is a selected user-level mask artifact. Explicit absence removes
	// only a previously managed mask, never the external base definition.
	Mask         *ServiceMask      `json:"mask,omitempty" toml:"mask,omitempty"`
	DropIns      []ServiceArtifact `json:"drop_ins,omitempty" toml:"drop_in,omitempty"`
	Instances    []ServiceInstance `json:"instances,omitempty" toml:"instance,omitempty"`
	LinkedSource string            `json:"linked_source,omitempty" toml:"linked_source,omitempty"`
}

// ServiceArtifact retains an exact managed drop-in's identity and provenance.
// An absent artifact is explicit desired absence, not an absent base unit.
type ServiceArtifact struct {
	Path     string          `json:"path" toml:"path"`
	Presence ServicePresence `json:"presence" toml:"presence"`
	Hash     string          `json:"hash,omitempty" toml:"hash,omitempty"`
	Mode     string          `json:"mode,omitempty" toml:"mode,omitempty"`
}

type ServiceMask struct {
	Presence ServicePresence `json:"presence" toml:"presence"`
}

// An instance has independent persistent intent but no duplicate template
// definition. Instance-specific drop-ins remain exact managed artifacts.
type ServiceInstance struct {
	Name                 string                      `json:"name" toml:"name"`
	Presence             ServicePresence             `json:"presence" toml:"presence"`
	StartIntent          ServiceStartIntent          `json:"start_intent" toml:"start_intent"`
	ActivationPreference ServiceActivationPreference `json:"activation_preference,omitempty" toml:"activation_preference,omitempty"`
	ObservedActive       bool                        `json:"observed_active" toml:"observed_active"`
	Mask                 *ServiceMask                `json:"mask,omitempty" toml:"mask,omitempty"`
	DropIns              []ServiceArtifact           `json:"drop_ins,omitempty" toml:"drop_in,omitempty"`
}

func validateServicesDesiredState(services Services) error {
	seen := map[string]bool{}
	for _, unit := range services.Units {
		if err := validateServiceName(unit.Name); err != nil {
			return err
		}
		if seen[unit.Name] {
			return fmt.Errorf("duplicate service unit %q", unit.Name)
		}
		seen[unit.Name] = true
	}
	for _, unit := range services.Units {
		if unit.Kind != "" && unit.Kind != strings.TrimPrefix(path.Ext(unit.Name), ".") {
			return fmt.Errorf("service %q kind %q does not match its name", unit.Name, unit.Kind)
		}
		if unit.Management != ServiceManagementDefinition && unit.Management != ServiceManagementCustomization {
			return fmt.Errorf("service %q has invalid management %q", unit.Name, unit.Management)
		}
		if err := validateServiceIntent(unit.Name, unit.Presence, unit.StartIntent, unit.ActivationPreference); err != nil {
			return err
		}
		if unit.Management == ServiceManagementCustomization && (unit.Definition != "" || unit.DefinitionHash != "") {
			return fmt.Errorf("external service %q cannot own its base definition", unit.Name)
		}
		if unit.Management == ServiceManagementCustomization && unit.Presence == ServiceAbsent {
			return fmt.Errorf("external service %q cannot be desired-absent; select an exact managed overlay artifact", unit.Name)
		}
		if err := validateServiceMask(unit.Name, unit.Mask, unit.StartIntent); err != nil {
			return err
		}
		if unit.Management == ServiceManagementDefinition && unit.Definition == "" {
			return fmt.Errorf("managed service %q has no authored definition", unit.Name)
		}
		if unit.Definition != "" && unit.Definition != "units/"+unit.Name {
			return fmt.Errorf("service %q has invalid definition path %q", unit.Name, unit.Definition)
		}
		if unit.Definition == "" && unit.DefinitionHash != "" {
			return fmt.Errorf("service %q has a definition hash without owned content", unit.Name)
		}
		if err := validateServiceDropIns(unit.Name, unit.DropIns); err != nil {
			return err
		}
		if len(unit.Instances) > 0 && !strings.Contains(unit.Name, "@.") {
			return fmt.Errorf("service %q cannot have instances without a template", unit.Name)
		}
		instances := map[string]bool{}
		for _, instance := range unit.Instances {
			if err := validateServiceName(instance.Name); err != nil {
				return err
			}
			prefix, suffix, _ := strings.Cut(unit.Name, "@.")
			if !strings.HasPrefix(instance.Name, prefix+"@") || !strings.HasSuffix(instance.Name, "."+suffix) || instance.Name == unit.Name {
				return fmt.Errorf("instance %q does not belong to template %q", instance.Name, unit.Name)
			}
			if instances[instance.Name] || seen[instance.Name] {
				return fmt.Errorf("duplicate service instance %q", instance.Name)
			}
			instances[instance.Name] = true
			if err := validateServiceIntent(instance.Name, instance.Presence, instance.StartIntent, instance.ActivationPreference); err != nil {
				return err
			}
			if err := validateServiceMask(instance.Name, instance.Mask, instance.StartIntent); err != nil {
				return err
			}
			if err := validateServiceDropIns(instance.Name, instance.DropIns); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateServiceMask(name string, mask *ServiceMask, start ServiceStartIntent) error {
	if mask == nil {
		return nil
	}
	if mask.Presence != ServicePresent && mask.Presence != ServiceAbsent {
		return fmt.Errorf("service %q has invalid managed mask presence %q", name, mask.Presence)
	}
	if mask.Presence == ServicePresent && start != ServiceStartMasked || mask.Presence == ServiceAbsent && start == ServiceStartMasked {
		return fmt.Errorf("service %q has conflicting managed mask and persistent start intent", name)
	}
	return nil
}

func validateServiceName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\ \t\n\r") || strings.IndexFunc(name, unicode.IsControl) >= 0 || strings.HasPrefix(name, ".") {
		return fmt.Errorf("invalid service unit name %q", name)
	}
	switch path.Ext(name) {
	case ".service", ".timer", ".socket", ".path", ".target", ".slice":
		return nil
	default:
		return fmt.Errorf("unsupported user service kind %q", name)
	}
}

func validateServiceIntent(name string, presence ServicePresence, start ServiceStartIntent, activation ServiceActivationPreference) error {
	if presence != ServicePresent && presence != ServiceAbsent {
		return fmt.Errorf("service %q has invalid presence %q", name, presence)
	}
	switch start {
	case "", ServiceStartEnabled, ServiceStartDisabled, ServiceStartMasked, ServiceStartIndirect, ServiceStartNotManaged:
	default:
		return fmt.Errorf("service %q has invalid persistent start intent %q", name, start)
	}
	switch activation {
	case "", ServiceActivationPersistentOnly, ServiceActivationRestoreWorkingState, ServiceActivationReview:
	default:
		return fmt.Errorf("service %q has invalid activation preference %q", name, activation)
	}
	if presence == ServiceAbsent && start != "" && start != ServiceStartNotManaged {
		return fmt.Errorf("service %q cannot be absent and request persistent start state", name)
	}
	return nil
}

func validateServiceDropIns(unit string, dropIns []ServiceArtifact) error {
	seen := map[string]bool{}
	for _, artifact := range dropIns {
		if artifact.Path == "" || path.Clean(artifact.Path) != artifact.Path || !strings.HasPrefix(artifact.Path, "units/"+unit+".d/") || strings.Contains(artifact.Path, "\\") || strings.Contains(strings.TrimPrefix(artifact.Path, "units/"+unit+".d/"), "/") || strings.HasPrefix(path.Base(artifact.Path), ".") || path.Ext(artifact.Path) != ".conf" {
			return fmt.Errorf("invalid drop-in path %q for %q", artifact.Path, unit)
		}
		if seen[artifact.Path] {
			return fmt.Errorf("duplicate service drop-in %q", artifact.Path)
		}
		seen[artifact.Path] = true
		if artifact.Presence != ServicePresent && artifact.Presence != ServiceAbsent {
			return fmt.Errorf("drop-in %q has invalid presence %q", artifact.Path, artifact.Presence)
		}
	}
	return nil
}

func sortServices(services *Services) {
	for i := range services.Units {
		unit := &services.Units[i]
		sort.Slice(unit.DropIns, func(a, b int) bool { return unit.DropIns[a].Path < unit.DropIns[b].Path })
		for j := range unit.Instances {
			instance := &unit.Instances[j]
			sort.Slice(instance.DropIns, func(a, b int) bool { return instance.DropIns[a].Path < instance.DropIns[b].Path })
		}
		sort.Slice(unit.Instances, func(a, b int) bool { return unit.Instances[a].Name < unit.Instances[b].Name })
	}
	sort.Slice(services.Units, func(i, j int) bool { return services.Units[i].Name < services.Units[j].Name })
}
