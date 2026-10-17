package plugins

import "github.com/Grenco/omarchy-blueprint/internal/model"

// Effect descriptors for unattended authority (Auto Sync); see the
// capability audit §4.6. Plugins are third-party code loaded by the shell.
func effect(family string, change model.EffectChange, payload model.EffectPayload, source model.EffectSource, activation model.EffectActivation, recovery model.EffectRecovery, verifiable bool) *model.Effect {
	return &model.Effect{Family: "plugins." + family, Change: change, Payload: payload, Source: source, Activation: activation, Privilege: model.PrivilegeUser, Interaction: model.InteractionNone, Recovery: recovery, Verifiable: verifiable}
}

var (
	// omarchy plugin add clones the repository tip before the pin runs.
	effectInstallGit = effect("install-git", model.EffectAdd, model.PayloadExecLater, model.SourceUnpinned, model.ActivationNone, model.RecoveryNone, true)
	effectPinGit     = effect("pin-git", model.EffectUpdate, model.PayloadExecLater, model.SourcePinned, model.ActivationNone, model.RecoveryNone, true)
	// Whether validation evaluates plugin code is not established, so it is
	// described as running it.
	effectValidate  = effect("validate", model.EffectNoChange, model.PayloadExecNow, model.SourceLocal, model.ActivationNone, model.RecoveryNone, false)
	effectCopyLocal = effect("copy-local", model.EffectAdd, model.PayloadExecLater, model.SourceArtifact, model.ActivationNone, model.RecoveryNone, true)
	effectRescan    = effect("rescan", model.EffectNoChange, model.PayloadExecNow, model.SourceLocal, model.ActivationActivate, model.RecoveryNone, false)
	effectEnable    = effect("enable", model.EffectUpdate, model.PayloadExecNow, model.SourceLocal, model.ActivationActivate, model.RecoveryNone, true)
	effectDisable   = effect("disable", model.EffectUpdate, model.PayloadConfig, model.SourceLocal, model.ActivationReload, model.RecoveryReinstall, true)
	effectRemove    = effect("remove", model.EffectRemove, model.PayloadExecLater, model.SourceNone, model.ActivationNone, model.RecoveryReinstall, true)
)

func withEffect(op model.Operation, e *model.Effect) model.Operation {
	copied := *e
	op.Effect = &copied
	return op
}
