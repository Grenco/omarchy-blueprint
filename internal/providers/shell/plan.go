package shell

import (
	"fmt"
	"path/filepath"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

// Plan applies semantic Shell intent where it is safe, preserving independent
// target state and reporting overlapping units as skips unless Force is set.
func (p Provider) Plan(saved profile.Shell, current State, schema int, from, to string, options MergeOptions) (model.RestorePlan, error) {
	plan := model.RestorePlan{ProfileVersion: schema, OmarchyFrom: from, OmarchyTo: to}
	if saved.Hash == "" {
		return plan, nil
	}
	if current.Status == StatusUnsupported || current.Version != SupportedVersion || current.Baseline.Version != SupportedVersion {
		plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "shell", Resource: "shell:config", Reason: fmt.Sprintf("Shell schema changed; migration required (current version %d, captured %d)", current.Version, saved.Version)})
		return plan, nil
	}
	intent, err := p.loadIntent(saved)
	if err != nil {
		return model.RestorePlan{}, err
	}
	analysis, err := p.Analyze(saved, current, options)
	if err != nil {
		return model.RestorePlan{}, err
	}
	for _, conflict := range analysis.Conflicts {
		name := pathName(conflict.Path)
		plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "shell", Resource: "shell:" + name, Reason: "changed independently on this machine; keeping the current value (use --force to apply captured intent)"})
	}
	if len(analysis.Applied) == 0 {
		return plan, nil
	}
	merged, err := EncodeDocument(analysis.Value)
	if err != nil {
		return model.RestorePlan{}, err
	}
	write := model.FileWrite{Destination: p.UserPath, SourceHash: merged.RawHash, Backup: current.UserExists}
	if current.UserExists {
		write.ExpectedHash = current.Current.RawHash
	} else {
		write.ExpectedMissing = true
	}
	if intent.Baseline.Hash == current.Baseline.Hash && current.Status == StatusDefault && merged.Hash == intent.Desired.Hash {
		write.Source = filepath.Join(p.ProfileDir, "shell", "shell.json")
		write.SourceHash = intent.Desired.RawHash
	} else {
		write.Generated = true
		write.Content = merged.Raw
	}
	risk := model.RiskMedium
	for _, applied := range analysis.Applied {
		if applied.Forced {
			risk = model.RiskHigh
			break
		}
	}
	plan.Operations = append(plan.Operations,
		model.Operation{ID: "shell.write", Provider: "shell", Action: "write", Resource: "shell:config", File: &write, Risk: risk, Reversible: write.Backup, Effect: shellWriteEffect(&write)},
		model.Operation{ID: "shell.restart", Provider: "shell", Action: "restart", Resource: "shell:runtime", Command: []string{"omarchy-restart-shell"}, DependsOn: []string{"shell.write"}, Risk: model.RiskMedium, Effect: shellRestartEffect()},
	)
	return plan, nil
}

// Effect descriptors for unattended authority (Auto Sync); see the
// capability audit §4.4. Shell configuration chooses which plugins load, so
// it is exec-later; content merged from verified profile intent and the live
// baseline is described as an artifact.
func shellWriteEffect(write *model.FileWrite) *model.Effect {
	change, recovery := model.EffectUpdate, model.RecoveryNone
	if write.ExpectedMissing {
		change = model.EffectAdd
	}
	if write.Backup {
		recovery = model.RecoveryBackup
	}
	return &model.Effect{Family: "shell.write", Change: change, Payload: model.PayloadExecLater, Source: model.SourceArtifact, Activation: model.ActivationNone, Privilege: model.PrivilegeUser, Interaction: model.InteractionNone, Recovery: recovery, Verifiable: true}
}

// Restarting the shell loads every enabled plugin.
func shellRestartEffect() *model.Effect {
	return &model.Effect{Family: "shell.restart", Change: model.EffectNoChange, Payload: model.PayloadExecNow, Source: model.SourceLocal, Activation: model.ActivationReload, Privilege: model.PrivilegeUser, Interaction: model.InteractionNone, Recovery: model.RecoveryNone, Verifiable: false}
}
