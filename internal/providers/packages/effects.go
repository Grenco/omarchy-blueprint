package packages

import (
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/omarchy"
)

// Effect descriptors for unattended authority (Auto Sync); see the
// capability audit §4.8. Package installs run install scripts and
// package-manager hooks; system packages elevate through sudo today, which
// may prompt.
func packagesEffect(family string, change model.EffectChange, payload model.EffectPayload, source model.EffectSource, privilege model.EffectPrivilege, interaction model.EffectInteraction, recovery model.EffectRecovery) *model.Effect {
	return &model.Effect{Family: "packages." + family, Change: change, Payload: payload, Source: source, Activation: model.ActivationNone, Privilege: privilege, Interaction: interaction, Recovery: recovery, Verifiable: true}
}

func installEffect(kind string) *model.Effect {
	switch kind {
	case "official":
		return packagesEffect("install-official", model.EffectAdd, model.PayloadExecNow, model.SourceSigned, model.PrivilegeRoot, model.InteractionPossible, model.RecoveryNone)
	case "aur":
		// Builds the current PKGBUILD; no revision is recorded yet.
		return packagesEffect("install-aur", model.EffectAdd, model.PayloadExecNow, model.SourceUnpinned, model.PrivilegeRoot, model.InteractionPossible, model.RecoveryNone)
	}
	return &model.Effect{} // unknown kind: undescribed
}

// Omarchy recipes run setup scripts whose sources and steps are not
// recorded; a recipe that waits for the person needs them present.
func recipeEffect(recipe omarchy.AppRecipe) *model.Effect {
	interaction := model.InteractionPossible
	if recipe.AwaitsYou != "" {
		interaction = model.InteractionRequired
	}
	return packagesEffect("install-recipe", model.EffectAdd, model.PayloadExecNow, model.SourceUnpinned, model.PrivilegeRoot, interaction, model.RecoveryNone)
}

func preinstallEffect(action string) *model.Effect {
	if action == "remove" {
		return packagesEffect("preinstall-remove", model.EffectRemove, model.PayloadExecNow, model.SourceNone, model.PrivilegeRoot, model.InteractionPossible, model.RecoveryReinstall)
	}
	return packagesEffect("preinstall-add", model.EffectAdd, model.PayloadExecNow, model.SourceSigned, model.PrivilegeRoot, model.InteractionPossible, model.RecoveryNone)
}

func removeEffect(kind string, recipe bool) *model.Effect {
	family := "remove-" + kind
	if recipe {
		family = "remove-recipe"
	}
	return packagesEffect(family, model.EffectRemove, model.PayloadExecNow, model.SourceNone, model.PrivilegeRoot, model.InteractionPossible, model.RecoveryReinstall)
}

// mise downloads tool executables into the user's home; tool versions are
// not yet enforced as exact pins.
func miseInstallEffect() *model.Effect {
	return packagesEffect("mise-install", model.EffectAdd, model.PayloadExecLater, model.SourceUnpinned, model.PrivilegeUser, model.InteractionNone, model.RecoveryNone)
}

func miseConfigureEffect(exists bool) *model.Effect {
	change, recovery := model.EffectAdd, model.RecoveryNone
	if exists {
		change, recovery = model.EffectUpdate, model.RecoveryBackup
	}
	return packagesEffect("mise-configure", change, model.PayloadExecLater, model.SourceArtifact, model.PrivilegeUser, model.InteractionNone, recovery)
}
