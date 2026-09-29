package services

import (
	"context"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

// ObservedUnit describes effective user-manager state without deciding
// Blueprint ownership. TopologyKnown distinguishes an unresolved catalogue
// template from a confirmed absent/runtime source. Persistent never grants
// permission to capture a discovered unit.
type ObservedUnit struct {
	Name             string
	Kind             string
	FragmentPath     string
	DropInPaths      []string
	TopologyKnown    bool
	RawUnitFileState string
	StartIntent      profile.ServiceStartIntent
	ObservedActive   bool
	Persistent       bool
	Generated        bool
	Transient        bool
	Runtime          bool
	Template         bool
	InstanceOf       string
	LinkedSource     string
	RelatedUnits     []string
}

// ProposedUnitSet will be built in a private temporary tree by Restore's
// validation task. The systemd boundary must never write to the live target
// or daemon-reload merely to inspect proposed definitions.
type ProposedUnitSet struct {
	Root  string
	Files []string
}

// Systemd separates read-only inspection/validation from explicit user-level
// effects. Stop, Restart, root operations and linger have no v1 entry point.
type Systemd interface {
	InspectUserUnits(context.Context) ([]ObservedUnit, error)
	VerifyUnitSet(context.Context, ProposedUnitSet) error
	DaemonReload(context.Context) error
	Enable(context.Context, ...string) error
	Disable(context.Context, ...string) error
	Mask(context.Context, ...string) error
	Unmask(context.Context, ...string) error
	Start(context.Context, ...string) error
}
