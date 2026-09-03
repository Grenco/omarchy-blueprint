package model

type ChangeType string

const (
	ChangeAdd    ChangeType = "add"
	ChangeModify ChangeType = "modify"
	ChangeRemove ChangeType = "remove"
	ChangeWarn   ChangeType = "warn"
)

type Change struct {
	Type     ChangeType `json:"type"`
	Provider string     `json:"provider"`
	Kind     string     `json:"kind"`
	Name     string     `json:"name"`
	Summary  string     `json:"summary"`
}

type Risk string

const (
	RiskLow    Risk = "low"
	RiskMedium Risk = "medium"
	RiskHigh   Risk = "high"
)

type Operation struct {
	ID         string     `json:"id"`
	Provider   string     `json:"provider"`
	Action     string     `json:"action"`
	Resource   string     `json:"resource"`
	Items      []string   `json:"items,omitempty"`
	Command    []string   `json:"command"`
	Copy       *Copy      `json:"copy,omitempty"`
	File       *FileWrite `json:"file,omitempty"`
	DependsOn  []string   `json:"depends_on,omitempty"`
	Risk       Risk       `json:"risk"`
	Reversible bool       `json:"reversible"`
}

type Copy struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
}

type FileWrite struct {
	Source          string `json:"source"`
	Destination     string `json:"destination"`
	SourceHash      string `json:"source_hash"`
	ExpectedHash    string `json:"expected_hash,omitempty"`
	ExpectedMissing bool   `json:"expected_missing,omitempty"`
	Backup          bool   `json:"backup"`
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
