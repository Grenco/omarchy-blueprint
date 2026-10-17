package model

// Effect describes what an operation does to this computer, for deciding
// whether it may run without a person present (Auto Sync). The provider that
// plans an operation supplies it from facts it actually knows; a dimension it
// cannot establish is left empty, and an empty or unknown dimension always
// means "requires review". Risk and Reversible are interactive Restore
// labels and are never inputs to unattended authority.
type Effect struct {
	// Family is the provider-owned, stable operation family, prefixed with
	// the provider ID (for example "themes.copy-local"). It never contains a
	// target identifier.
	Family      string            `json:"family"`
	Change      EffectChange      `json:"change"`
	Payload     EffectPayload     `json:"payload"`
	Source      EffectSource      `json:"source"`
	Activation  EffectActivation  `json:"activation"`
	Privilege   EffectPrivilege   `json:"privilege"`
	Interaction EffectInteraction `json:"interaction"`
	Recovery    EffectRecovery    `json:"recovery"`
	// Verifiable says the provider's post-apply verification covers this
	// operation's outcome.
	Verifiable bool `json:"verifiable"`
}

type EffectChange string

const (
	EffectAdd      EffectChange = "add"    // destination absent before
	EffectUpdate   EffectChange = "update" // replaces existing state
	EffectRemove   EffectChange = "remove"
	EffectNoChange EffectChange = "none" // reload, rescan, verify: no stored state changes
)

type EffectPayload string

const (
	PayloadNone   EffectPayload = "none"
	PayloadData   EffectPayload = "data"
	PayloadConfig EffectPayload = "config"
	// PayloadExecLater is content that runs when it is later loaded, sourced,
	// enabled or activated. Content whose executability is not positively
	// established is described as ExecLater.
	PayloadExecLater EffectPayload = "exec-later"
	// PayloadExecNow means the operation itself runs third-party,
	// profile-supplied or user-controlled code.
	PayloadExecNow EffectPayload = "exec-now"
)

type EffectSource string

const (
	SourceNone      EffectSource = "none"       // no content input
	SourceArtifact  EffectSource = "artifact"   // profile content verified against a recorded hash
	SourcePinned    EffectSource = "pinned"     // exact remote revision or checksum
	SourceSigned    EffectSource = "signed"     // package-repository signature
	SourceClosedSet EffectSource = "closed-set" // value from a fixed vocabulary
	SourceLocal     EffectSource = "local"      // content already on this computer
	SourceUnpinned  EffectSource = "unpinned"   // floating or unverifiable
)

type EffectActivation string

const (
	ActivationNone     EffectActivation = "none"
	ActivationReload   EffectActivation = "reload"   // re-read configuration
	ActivationActivate EffectActivation = "activate" // enable, start, load, set active
)

type EffectPrivilege string

const (
	PrivilegeUser EffectPrivilege = "user"
	PrivilegeRoot EffectPrivilege = "root"
)

type EffectInteraction string

const (
	InteractionNone     EffectInteraction = "none"
	InteractionPossible EffectInteraction = "possible" // may prompt (for example sudo)
	InteractionRequired EffectInteraction = "required" // needs a person
)

type EffectRecovery string

const (
	RecoveryBackup    EffectRecovery = "backup"    // journalled backup restorable
	RecoveryReinstall EffectRecovery = "reinstall" // can be re-applied from recorded intent
	RecoveryNone      EffectRecovery = "none"
)

// Complete reports whether every dimension is set to a known value.
func (e Effect) Complete() bool {
	switch e.Change {
	case EffectAdd, EffectUpdate, EffectRemove, EffectNoChange:
	default:
		return false
	}
	switch e.Payload {
	case PayloadNone, PayloadData, PayloadConfig, PayloadExecLater, PayloadExecNow:
	default:
		return false
	}
	switch e.Source {
	case SourceNone, SourceArtifact, SourcePinned, SourceSigned, SourceClosedSet, SourceLocal, SourceUnpinned:
	default:
		return false
	}
	switch e.Activation {
	case ActivationNone, ActivationReload, ActivationActivate:
	default:
		return false
	}
	switch e.Privilege {
	case PrivilegeUser, PrivilegeRoot:
	default:
		return false
	}
	switch e.Interaction {
	case InteractionNone, InteractionPossible, InteractionRequired:
	default:
		return false
	}
	switch e.Recovery {
	case RecoveryBackup, RecoveryReinstall, RecoveryNone:
	default:
		return false
	}
	return e.Family != ""
}
