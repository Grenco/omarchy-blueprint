package model

// CompatibilityState describes what provider evidence establishes for selected
// Restore intent. Unknown is never implicit support.
type CompatibilityState string

const (
	CompatibilitySupported    CompatibilityState = "supported"
	CompatibilityUnknown      CompatibilityState = "unknown"
	CompatibilityIncompatible CompatibilityState = "incompatible"
)

// CompatibilityAuthority only retains or removes existing Restore authority;
// it never grants permission that policy/provider safety did not already give.
type CompatibilityAuthority string

const (
	CompatibilityUnchanged CompatibilityAuthority = "unchanged"
	CompatibilityReduced   CompatibilityAuthority = "reduced"
	CompatibilityBlocked   CompatibilityAuthority = "blocked"
)

type CompatibilityEnvironment struct {
	Known          bool   `json:"known"`
	OmarchyVersion string `json:"omarchy_version,omitempty"`
	OmarchyChannel string `json:"omarchy_channel,omitempty"`
}

type CompatibilityEvidence struct {
	Kind    string `json:"kind"`
	Summary string `json:"summary"`
}

type CompatibilityFinding struct {
	Code          string                  `json:"code"`
	Target        string                  `json:"target,omitempty"`
	State         CompatibilityState      `json:"state"`
	Authority     CompatibilityAuthority  `json:"authority"`
	Summary       string                  `json:"summary"`
	Evidence      []CompatibilityEvidence `json:"evidence,omitempty"`
	RequirementID string                  `json:"requirement_id,omitempty"`
}

type CompatibilityCategory struct {
	Category  string                  `json:"category"`
	Applies   bool                    `json:"applies"`
	State     CompatibilityState      `json:"state,omitempty"`
	Authority CompatibilityAuthority  `json:"authority"`
	Evidence  []CompatibilityEvidence `json:"evidence,omitempty"`
	Findings  []CompatibilityFinding  `json:"findings,omitempty"`
}

type CompatibilityReport struct {
	ProfileLastCapture CompatibilityEnvironment `json:"profile_last_capture"`
	Target             CompatibilityEnvironment `json:"target"`
	Categories         []CompatibilityCategory  `json:"categories"`
}
