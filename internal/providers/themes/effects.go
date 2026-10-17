package themes

import "github.com/Grenco/omarchy-blueprint/internal/model"

// Effect descriptors for unattended authority (Auto Sync). They state only
// what this provider knows; see docs/planning/audits/2026-10-17-auto-sync-restore-capabilities.md §4.1.
func effect(family string, change model.EffectChange, payload model.EffectPayload, source model.EffectSource, activation model.EffectActivation, recovery model.EffectRecovery) *model.Effect {
	return &model.Effect{Family: "themes." + family, Change: change, Payload: payload, Source: source, Activation: activation, Privilege: model.PrivilegeUser, Interaction: model.InteractionNone, Recovery: recovery, Verifiable: true}
}

var (
	// A local theme that is not active is only listed by name and its
	// preview images shown; it runs nothing until activated.
	effectCopyLocal = effect("copy-local", model.EffectAdd, model.PayloadData, model.SourceArtifact, model.ActivationNone, model.RecoveryNone)
	// An overlay shadows a built-in name, so a later activation of that name
	// loads this content.
	effectCopyOverlay = effect("copy-overlay", model.EffectAdd, model.PayloadExecLater, model.SourceArtifact, model.ActivationNone, model.RecoveryNone)
	// omarchy theme install clones the repository tip before the pin runs.
	effectInstallGit = effect("install-git", model.EffectAdd, model.PayloadExecLater, model.SourceUnpinned, model.ActivationNone, model.RecoveryNone)
	effectPinGit     = effect("pin-git", model.EffectUpdate, model.PayloadExecLater, model.SourcePinned, model.ActivationNone, model.RecoveryNone)
	// omarchy theme set runs post-theme commands and the user's theme-set
	// hook, and theme files such as neovim.lua become live code.
	effectActivate = effect("activate", model.EffectUpdate, model.PayloadExecNow, model.SourceLocal, model.ActivationActivate, model.RecoveryNone)
	effectRemove   = effect("remove", model.EffectRemove, model.PayloadData, model.SourceNone, model.ActivationNone, model.RecoveryReinstall)
)

func withEffect(op model.Operation, e *model.Effect) model.Operation {
	copied := *e
	op.Effect = &copied
	return op
}
