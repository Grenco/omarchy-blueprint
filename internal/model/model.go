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
	ID          string           `json:"id"`
	Provider    string           `json:"provider"`
	Action      string           `json:"action"`
	Resource    string           `json:"resource"`
	Items       []string         `json:"items,omitempty"`
	Command     []string         `json:"command"`
	Copy        *Copy            `json:"copy,omitempty"`
	File        *FileWrite       `json:"file,omitempty"`
	Delete      *FileDelete      `json:"delete,omitempty"`
	Directory   *DirectoryCreate `json:"directory,omitempty"`
	Symlink     *SymlinkWrite    `json:"symlink,omitempty"`
	GitPatch    *GitPatchApply   `json:"git_patch,omitempty"`
	DependsOn   []string         `json:"depends_on,omitempty"`
	Risk        Risk             `json:"risk"`
	Reversible  bool             `json:"reversible"`
	Interactive bool             `json:"interactive,omitempty"`
	Notice      string           `json:"notice,omitempty"`
}

type Copy struct {
	Source               string `json:"source"`
	Destination          string `json:"destination"`
	SourceHash           string `json:"source_hash,omitempty"`
	RejectSymlinkParents bool   `json:"reject_symlink_parents,omitempty"`
}

type DirectoryCreate struct {
	Path                 string `json:"path"`
	Mode                 uint32 `json:"mode"`
	RejectSymlinkParents bool   `json:"reject_symlink_parents,omitempty"`
}

type SymlinkWrite struct {
	Destination          string                  `json:"destination"`
	Target               string                  `json:"target"`
	ExpectedMissing      bool                    `json:"expected_missing"`
	ReplaceExisting      bool                    `json:"replace_existing,omitempty"`
	ExpectedExisting     *FilesystemPrecondition `json:"expected_existing,omitempty"`
	Backup               bool                    `json:"backup,omitempty"`
	RejectSymlinkParents bool                    `json:"reject_symlink_parents,omitempty"`
}

type FilesystemPrecondition struct {
	Type   string `json:"type"`
	Hash   string `json:"hash,omitempty"`
	Mode   uint32 `json:"mode,omitempty"`
	Target string `json:"target,omitempty"`
}

type FileWrite struct {
	Source               string                  `json:"source,omitempty"`
	Generated            bool                    `json:"generated,omitempty"`
	Content              []byte                  `json:"-"`
	Destination          string                  `json:"destination"`
	SourceHash           string                  `json:"source_hash"`
	ExpectedHash         string                  `json:"expected_hash,omitempty"`
	ExpectedMissing      bool                    `json:"expected_missing,omitempty"`
	Backup               bool                    `json:"backup"`
	Mode                 *uint32                 `json:"mode,omitempty"`
	ExpectedMode         *uint32                 `json:"expected_mode,omitempty"`
	ReplaceExisting      bool                    `json:"replace_existing,omitempty"`
	ExpectedExisting     *FilesystemPrecondition `json:"expected_existing,omitempty"`
	RejectSymlinkParents bool                    `json:"reject_symlink_parents,omitempty"`
}

type FileDelete struct {
	Destination          string                  `json:"destination"`
	ExpectedExisting     *FilesystemPrecondition `json:"expected_existing,omitempty"`
	ExpectedMissing      bool                    `json:"expected_missing,omitempty"`
	Backup               bool                    `json:"backup,omitempty"`
	RejectSymlinkParents bool                    `json:"reject_symlink_parents,omitempty"`
}

type GitPatchApply struct {
	Repository string `json:"repository"`
	Source     string `json:"source"`
	SourceHash string `json:"source_hash"`
	ToIndex    bool   `json:"to_index"`
}

type RestorePlan struct {
	ProfileVersion int         `json:"profile_schema"`
	OmarchyFrom    string      `json:"omarchy_from"`
	OmarchyTo      string      `json:"omarchy_to"`
	Operations     []Operation `json:"operations"`
	Skipped        []Skipped   `json:"skipped,omitempty"`
	// Requirements must be satisfied by the user before the plan may be
	// applied; apply refuses while any remains (ADR 0022).
	Requirements []Requirement `json:"requirements,omitempty"`
}

// Requirement is a machine precondition a plan's operations need but that
// Blueprint deliberately does not perform itself, such as Omarchy's package
// metadata readiness. Remediation is the command the user runs.
type Requirement struct {
	ID          string   `json:"id"`
	Provider    string   `json:"provider"`
	Kind        string   `json:"kind"`
	Reason      string   `json:"reason"`
	Remediation []string `json:"remediation"`
	Operations  []string `json:"operations"`
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
