package config

import "github.com/Grenco/omarchy-blueprint/internal/model"

// Effect descriptors for unattended authority (Auto Sync); see the
// capability audit §4.3. Whether a configuration file can run code (a
// Hyprland exec line, a sourced shell file, Lua) is not established here, so
// every written file is described as exec-later until positive-evidence
// analysis proves otherwise.

func writeEffect(family string, write *model.FileWrite) *model.Effect {
	change, recovery := model.EffectUpdate, model.RecoveryNone
	if write.ExpectedMissing {
		change = model.EffectAdd
	}
	if write.Backup {
		recovery = model.RecoveryBackup
	}
	return &model.Effect{Family: "config." + family, Change: change, Payload: model.PayloadExecLater, Source: model.SourceArtifact, Activation: model.ActivationNone, Privilege: model.PrivilegeUser, Interaction: model.InteractionNone, Recovery: recovery, Verifiable: true}
}

func deleteEffect(family string) *model.Effect {
	return &model.Effect{Family: "config." + family, Change: model.EffectRemove, Payload: model.PayloadExecLater, Source: model.SourceNone, Activation: model.ActivationNone, Privilege: model.PrivilegeUser, Interaction: model.InteractionNone, Recovery: model.RecoveryBackup, Verifiable: true}
}

// hyprctl reload re-reads the whole sourced graph and re-runs every exec
// directive in it.
func reloadEffect() *model.Effect {
	return &model.Effect{Family: "config.reload", Change: model.EffectNoChange, Payload: model.PayloadExecNow, Source: model.SourceLocal, Activation: model.ActivationReload, Privilege: model.PrivilegeUser, Interaction: model.InteractionNone, Recovery: model.RecoveryNone, Verifiable: false}
}
