package screens

import (
	"context"
	"fmt"
	"github.com/Grenco/omarchy-blueprint/internal/machine"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

// readPresentation retains only a result's descriptive desired-state snapshot.
// It never supplies Capture/Restore authority; those operations re-inspect.
type readPresentation struct{ readSnapshot *workflow.ReadSnapshot }

// A presented read snapshot is not authority for profile edits either. Reload
// at the action boundary, not while a read cycle is calculating its result.
func editProfileFresh(ctx context.Context, session *workflow.Session, edit func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := session.Reload(); err != nil {
		return fmt.Errorf("reload profile before edit: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return edit()
}

func (s *readPresentation) acceptSnapshot(snapshot workflow.ReadSnapshot) {
	snapshot = snapshot.Clone()
	s.readSnapshot = &snapshot
}

func (s readPresentation) desiredProfile(session *workflow.Session) profile.Data {
	if s.readSnapshot != nil {
		return s.readSnapshot.Profile
	}
	if session != nil {
		return session.Profile()
	}
	return profile.Data{}
}

func (s readPresentation) selectedMachine(session *workflow.Session) machine.Selection {
	if s.readSnapshot != nil {
		return s.readSnapshot.Machine
	}
	if session != nil {
		return session.Machine()
	}
	return machine.Selection{}
}

// HeaderIdentity lets the root header describe the accepted active read result
// without publishing its snapshot into the mutation Session.
func (s readPresentation) HeaderIdentity() (profileName, machineName, source string, known bool) {
	if s.readSnapshot == nil {
		return "", "", "", false
	}
	return s.readSnapshot.Profile.Manifest.Profile.Name, s.readSnapshot.Machine.Name, s.readSnapshot.Machine.Source, true
}
