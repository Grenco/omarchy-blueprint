package syncengine

type TargetKey struct {
	Category string `json:"category"`
	Target   string `json:"target"`
}

type StateKind string

const (
	StateUnmanaged StateKind = "unmanaged"
	StatePresent   StateKind = "present"
	StateAbsent    StateKind = "absent"
)

// Fingerprint is provider-owned normalized semantic identity. Summary is safe
// presentation only and never participates in equality or mutation authority.
type State struct {
	Kind        StateKind `json:"kind"`
	Fingerprint string    `json:"fingerprint,omitempty"`
	Summary     string    `json:"summary,omitempty"`
}

func (s State) Equal(other State) bool {
	return s.Kind == other.Kind && s.Fingerprint == other.Fingerprint
}

type DesiredTarget struct {
	Key   TargetKey `json:"key"`
	State State     `json:"state"`
}
type HistoryClass string

const (
	HistoryUnchanged HistoryClass = "unchanged"
	HistoryLocal     HistoryClass = "local"
	HistoryRemote    HistoryClass = "remote"
	HistoryConverged HistoryClass = "converged"
	HistoryConflict  HistoryClass = "conflict"
)

type Resolution string

const (
	ResolutionNone         Resolution = "none"
	ResolutionKeepLocal    Resolution = "keep-local"
	ResolutionAcceptRemote Resolution = "accept-remote"
	ResolutionDefer        Resolution = "defer"
)

// MachineResolution is separate: choosing between L and R cannot resolve a
// third, different machine intent. Both reviews may be needed for one target.
type MachineResolution string

const (
	MachineResolutionNone        MachineResolution = "none"
	MachineResolutionKeepMachine MachineResolution = "keep-machine"
	MachineResolutionKeepProfile MachineResolution = "keep-profile"
	MachineResolutionDefer       MachineResolution = "defer"
)

type TargetResolution struct {
	Key               TargetKey         `json:"key"`
	Resolution        Resolution        `json:"resolution"`
	MachineResolution MachineResolution `json:"machine_resolution"`
}
type PlannedAction string

const (
	ActionNone    PlannedAction = "none"
	ActionCapture PlannedAction = "capture"
	ActionPublish PlannedAction = "publish"
	ActionRestore PlannedAction = "restore"
)

type ReviewKind string

const (
	ReviewNone    ReviewKind = "none"
	ReviewHistory ReviewKind = "history"
	ReviewMachine ReviewKind = "machine"
)

type EffectivePolicy struct {
	Capture bool `json:"capture"`
	Restore bool `json:"restore"`
}

type TargetInput struct {
	Key               TargetKey         `json:"key"`
	Base              State             `json:"base"`
	Local             State             `json:"local"`
	Remote            State             `json:"remote"`
	Machine           State             `json:"machine"`
	Policy            EffectivePolicy   `json:"policy"`
	Resolution        Resolution        `json:"resolution"`
	MachineResolution MachineResolution `json:"machine_resolution"`
}
type Item struct {
	TargetInput
	History HistoryClass `json:"history"`
	Desired State        `json:"desired"`
	// Actions are descriptive candidates, ordered capture -> publish -> restore.
	// Profile history stages and all authoritative replanning belong to workflow.
	Actions      []PlannedAction `json:"actions,omitempty"`
	Review       ReviewKind      `json:"review"`
	Choices      []string        `json:"choices,omitempty"`
	Deferred     bool            `json:"deferred"`
	Inapplicable bool            `json:"inapplicable"`
	// Settled describes this inspection only; never permission to advance a cursor.
	Settled bool `json:"settled"`
}
type Blocker struct {
	Code    string     `json:"code"`
	Message string     `json:"message"`
	Target  *TargetKey `json:"target,omitempty"`
}
type Identity struct {
	Base               string `json:"base"`
	Local              string `json:"local"`
	Remote             string `json:"remote"`
	Machine            string `json:"machine"`
	Branch             string `json:"branch"`
	Upstream           string `json:"upstream"`
	RemoteFingerprint  string `json:"remote_fingerprint"`
	ControlFingerprint string `json:"control_fingerprint"`
}
type GitState struct {
	Clean    bool `json:"clean"`
	Ahead    int  `json:"ahead"`
	Behind   int  `json:"behind"`
	Diverged bool `json:"diverged"`
}
type PlanRequest struct {
	Identity Identity
	Git      GitState
	Targets  []TargetInput
}

// Plan is a value-owned descriptive artifact. Reconcile copies its inputs; a
// consumer must not mutate a shared Plan or treat it as Apply/Verify authority.
type Plan struct {
	Identity          Identity  `json:"identity"`
	Git               GitState  `json:"git"`
	Items             []Item    `json:"items"`
	Blockers          []Blocker `json:"blockers,omitempty"`
	BootstrapRequired bool      `json:"bootstrap_required"`
}
