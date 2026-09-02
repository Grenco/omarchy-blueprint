package model

type ChangeType string

const (
	ChangeAdd    ChangeType = "add"
	ChangeRemove ChangeType = "remove"
)

type Change struct {
	Type     ChangeType `json:"type"`
	Provider string     `json:"provider"`
	Kind     string     `json:"kind"`
	Name     string     `json:"name"`
	Summary  string     `json:"summary"`
}

type Risk string

const RiskLow Risk = "low"

type Operation struct {
	ID         string   `json:"id"`
	Provider   string   `json:"provider"`
	Action     string   `json:"action"`
	Resource   string   `json:"resource"`
	Items      []string `json:"items,omitempty"`
	Command    []string `json:"command"`
	Risk       Risk     `json:"risk"`
	Reversible bool     `json:"reversible"`
}

type RestorePlan struct {
	ProfileVersion int         `json:"profile_schema"`
	OmarchyFrom    string      `json:"omarchy_from"`
	OmarchyTo      string      `json:"omarchy_to"`
	Operations     []Operation `json:"operations"`
	Skipped        []Skipped   `json:"skipped,omitempty"`
}

type Skipped struct {
	Provider string `json:"provider"`
	Resource string `json:"resource"`
	Reason   string `json:"reason"`
}

type VerificationResult struct {
	OK      bool     `json:"ok"`
	Missing []string `json:"missing,omitempty"`
}
