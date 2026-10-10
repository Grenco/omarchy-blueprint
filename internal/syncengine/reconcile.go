package syncengine

import (
	"errors"
	"sort"
	"strings"
)

func validateState(s State) error {
	switch s.Kind {
	case StateUnmanaged:
		if s.Fingerprint != "" {
			return errors.New("unmanaged state cannot have a fingerprint")
		}
	case StatePresent, StateAbsent:
		if strings.TrimSpace(s.Fingerprint) == "" {
			return errors.New("managed state requires a fingerprint")
		}
	default:
		return errors.New("invalid semantic state kind")
	}
	return nil
}

func ClassifyHistory(base, local, remote State) (HistoryClass, error) {
	for _, s := range []State{base, local, remote} {
		if err := validateState(s); err != nil {
			return "", err
		}
	}
	if local.Equal(base) {
		if remote.Equal(base) {
			return HistoryUnchanged, nil
		}
		return HistoryRemote, nil
	}
	if remote.Equal(base) {
		return HistoryLocal, nil
	}
	if local.Equal(remote) {
		return HistoryConverged, nil
	}
	return HistoryConflict, nil
}

func validateIdentity(i Identity) error {
	if !commitID.MatchString(i.Base) || !commitID.MatchString(i.Local) || !commitID.MatchString(i.Remote) {
		return errors.New("plan requires exact B/L/R commit identities")
	}
	c := Cursor{Version: CursorFormatVersion, Machine: i.Machine, Revision: i.Base, Branch: i.Branch, Upstream: i.Upstream, RemoteFingerprint: i.RemoteFingerprint}
	if err := c.validate(); err != nil {
		return err
	}
	if !remoteID.MatchString(i.ControlFingerprint) {
		return errors.New("plan requires a control-plane fingerprint")
	}
	return nil
}

// Reconcile classifies normalized facts only. It does not inspect, fetch,
// compose profile files, save a cursor, or execute any candidate action.
func Reconcile(request PlanRequest) (Plan, error) {
	if err := validateIdentity(request.Identity); err != nil {
		return Plan{}, err
	}
	if request.Git.Ahead < 0 || request.Git.Behind < 0 {
		return Plan{}, errors.New("invalid Git history counts")
	}
	result := Plan{Identity: request.Identity, Git: request.Git, Items: make([]Item, 0, len(request.Targets))}
	seen := map[TargetKey]bool{}
	for _, input := range request.Targets {
		if strings.TrimSpace(input.Key.Category) == "" || strings.TrimSpace(input.Key.Target) == "" || strings.ContainsAny(input.Key.Category+input.Key.Target, "\x00\r\n") {
			return Plan{}, errors.New("invalid semantic target key")
		}
		if seen[input.Key] {
			return Plan{}, errors.New("duplicate semantic target key")
		}
		seen[input.Key] = true
		item, err := reconcileTarget(input)
		if err != nil {
			return Plan{}, err
		}
		result.Items = append(result.Items, item)
	}
	sort.Slice(result.Items, func(i, j int) bool {
		a, b := result.Items[i].Key, result.Items[j].Key
		if a.Category == b.Category {
			return a.Target < b.Target
		}
		return a.Category < b.Category
	})
	return result, nil
}

func reconcileTarget(input TargetInput) (Item, error) {
	history, err := ClassifyHistory(input.Base, input.Local, input.Remote)
	if err != nil {
		return Item{}, err
	}
	if err := validateState(input.Machine); err != nil {
		return Item{}, err
	}
	if input.Resolution == "" {
		input.Resolution = ResolutionNone
	}
	if input.MachineResolution == "" {
		input.MachineResolution = MachineResolutionNone
	}
	item := Item{TargetInput: input, History: history, Desired: input.Local, Review: ReviewNone}
	publish := history == HistoryLocal
	if history == HistoryConflict {
		switch input.Resolution {
		case ResolutionNone:
			if input.MachineResolution != MachineResolutionNone {
				return Item{}, errors.New("machine choice cannot bypass unresolved history")
			}
			item.Review = ReviewHistory
			item.Choices = []string{string(ResolutionKeepLocal), string(ResolutionAcceptRemote), string(ResolutionDefer)}
			return item, nil
		case ResolutionDefer:
			if input.MachineResolution != MachineResolutionNone {
				return Item{}, errors.New("deferred history cannot select machine intent")
			}
			item.Deferred = true
			return item, nil
		case ResolutionKeepLocal:
			publish = true
		case ResolutionAcceptRemote:
			item.Desired = input.Remote
		default:
			return Item{}, errors.New("invalid history resolution")
		}
	} else {
		if input.Resolution != ResolutionNone {
			return Item{}, errors.New("history resolution requires a history conflict")
		}
		if history == HistoryRemote || history == HistoryConverged {
			item.Desired = input.Remote
		}
	}
	if input.Machine.Equal(item.Desired) {
		if input.MachineResolution != MachineResolutionNone {
			return Item{}, errors.New("machine resolution requires machine disagreement")
		}
		if publish {
			item.Actions = []PlannedAction{ActionPublish}
		} else {
			item.Settled = true
		}
		return item, nil
	}
	restoreAllowed := input.Policy.Restore && item.Desired.Kind != StateUnmanaged
	captureAllowed := input.Policy.Capture
	knownMachine := input.Machine.Equal(input.Base) || input.Machine.Equal(input.Local) || input.Machine.Equal(input.Remote)
	// Unchanged desired state has no competing committed intent. When both
	// directions are permitted, expose alternatives instead of choosing silently.
	needsReview := (history == HistoryUnchanged && captureAllowed && restoreAllowed) || (history != HistoryUnchanged && !knownMachine)
	if needsReview {
		switch input.MachineResolution {
		case MachineResolutionNone:
			item.Review = ReviewMachine
			if captureAllowed {
				item.Choices = append(item.Choices, string(MachineResolutionKeepMachine))
			}
			// Keep Profile may intentionally leave live state inapplicable by policy.
			item.Choices = append(item.Choices, string(MachineResolutionKeepProfile), string(MachineResolutionDefer))
			return item, nil
		case MachineResolutionDefer:
			item.Deferred = true
			return item, nil
		case MachineResolutionKeepMachine:
			if !captureAllowed {
				return Item{}, errors.New("Capture policy forbids keeping machine intent")
			}
			item.Desired = input.Machine
			item.Actions = []PlannedAction{ActionCapture, ActionPublish}
			return item, nil
		case MachineResolutionKeepProfile: // continue toward reviewed profile, never bypass Restore policy
		default:
			return Item{}, errors.New("invalid machine resolution")
		}
	} else if input.MachineResolution != MachineResolutionNone {
		return Item{}, errors.New("machine resolution requires a machine conflict")
	}
	if publish {
		item.Actions = append(item.Actions, ActionPublish)
	}
	if history == HistoryUnchanged && !needsReview && captureAllowed {
		item.Desired = input.Machine
		item.Actions = append(item.Actions, ActionCapture)
	} else if restoreAllowed {
		item.Actions = append(item.Actions, ActionRestore)
	} else {
		item.Inapplicable = true
	}
	item.Settled = len(item.Actions) == 0 && item.Inapplicable
	return item, nil
}
