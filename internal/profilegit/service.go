// Package profilegit provides safe Git operations rooted at a Blueprint profile.
package profilegit

import (
	"errors"

	"github.com/Grenco/omarchy-blueprint/internal/command"
	"github.com/Grenco/omarchy-blueprint/internal/inspection"
	"github.com/Grenco/omarchy-blueprint/internal/machine"
)

const maxDiffOutput = inspection.MaxPreviewBytes + 1

// Service owns profile-repository operations for one canonical profile root.
type Service struct {
	Runner command.Runner
	Root   string
}

// New constructs a service rooted exactly at profileRoot.
func New(runner command.Runner, profileRoot string) (Service, error) {
	if profileRoot == "" {
		return Service{}, errors.New("profile root is empty")
	}
	root, err := machine.CanonicalProfileRoot(profileRoot)
	if err != nil {
		return Service{}, err
	}
	return Service{Runner: runner, Root: root}, nil
}

// RootPath returns the canonical active profile root.
func (s Service) RootPath() string { return s.Root }
