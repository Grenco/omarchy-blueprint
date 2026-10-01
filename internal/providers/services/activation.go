package services

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func planActivation(saved profile.ServiceUnit, current map[string]ObservedUnit, rc workflow.RestoreContext) ([]model.Operation, []model.ActivationCandidate, []model.Skipped) {
	var ops []model.Operation
	var candidates []model.ActivationCandidate
	var skipped []model.Skipped
	decision := rc.Targets[saved.Name]
	if !decision.Restore || !decision.Resolved || saved.Presence != profile.ServicePresent {
		return nil, nil, nil
	}
	add := func(name string, presence profile.ServicePresence, start profile.ServiceStartIntent, preference profile.ServiceActivationPreference, active bool) {
		if presence != profile.ServicePresent || !active || strings.Contains(name, "@.") {
			return
		}
		actual := current[name]
		reason := ""
		switch {
		case preference == "" || preference == profile.ServiceActivationPersistentOnly:
			reason = "Will not start during Restore: target preference is Persistent state only."
		case rc.Options.ActivationMode() == policy.ActivationPersistentOnly:
			reason = "Will not start during Restore: this run restores Persistent state only."
		case preference == profile.ServiceActivationReview && rc.Options.ActivationMode() == policy.ActivationRestoreWorkingState:
			reason = "Activation requires interactive activation review; working mode does not approve this target."
		case strings.HasSuffix(name, ".slice"):
			reason = "Activation withheld: slices are resource-control groups, not generic working-process entry points."
		case strings.HasSuffix(name, ".service") && (actual.Name == "" || !actual.TopologyKnown):
			reason = "Activation withheld: standalone service entry-point relationships are not established on this destination; restore persistent state, then inspect and replan."
		case start == profile.ServiceStartMasked || actual.StartIntent == profile.ServiceStartMasked:
			reason = "Activation withheld: unit is blocked from starting by a mask."
		case actual.ObservedActive:
			reason = "Already running; Blueprint will not restart or reload this process."
		case strings.HasSuffix(name, ".service") && len(actual.TriggeredBy) > 0:
			reason = "Will not invoke a triggered service directly; activate its timer/socket/path entry point instead."
		case actual.Name != "" && (!actual.TopologyKnown || actual.Generated || actual.Transient || actual.Runtime):
			reason = "Activation withheld: persistent effective source/readiness is not established."
		case actual.LoadState != "" && actual.LoadState != "loaded":
			reason = "Activation withheld: user manager load state needs a successful refresh and a new plan."
		}
		if reason == "" {
			for _, dependency := range append(append([]string(nil), current[saved.Name].RelatedUnits...), actual.RelatedUnits...) {
				dep, found := current[dependency]
				if !found || !dep.TopologyKnown || dep.Generated || dep.Transient || dep.Runtime || dep.StartIntent == profile.ServiceStartMasked || dep.LoadState != "" && dep.LoadState != "loaded" {
					reason = "Activation withheld because external dependency/readiness evidence is missing: " + dependency
					break
				}
			}
		}
		if reason != "" {
			skipped = append(skipped, model.Skipped{Provider: "services", Resource: name, Reason: reason})
			return
		}
		if rc.Options.ActivationMode() == policy.ActivationReview {
			approved := false
			if rc.Options.ReviewActivation != nil {
				for _, unit := range rc.Options.ReviewActivation.Units {
					approved = approved || unit == name
				}
			}
			candidates = append(candidates, model.ActivationCandidate{Provider: "services", Resource: saved.Name, Unit: name, Reason: "Was active when captured; eligible for individually approved activation after Restore.", Approved: approved})
			if !approved {
				return
			}
		}
		op := serviceCommand("start", name, saved.Name)
		op.Command = []string{"systemctl", "--user", "start", "--", name}
		op.Items = []string{name}
		op.Notice = "Will activate after Restore because it was active when captured and this run authorizes it."
		ops = append(ops, op)
	}
	add(saved.Name, saved.Presence, saved.StartIntent, saved.ActivationPreference, saved.ObservedActive)
	for _, instance := range saved.Instances {
		add(instance.Name, instance.Presence, instance.StartIntent, instance.ActivationPreference, instance.ObservedActive)
	}
	return ops, candidates, skipped
}

func verifyActivation(current map[string]ObservedUnit, rc workflow.RestoreContext) []string {
	var missing []string
	for _, op := range rc.PlannedOperations {
		if op.Provider != "services" || op.Action != "start" {
			continue
		}
		completed := false
		for _, receipt := range rc.CompletedOperations {
			if reflect.DeepEqual(receipt, op) {
				completed = true
				break
			}
		}
		if len(op.Items) != 1 {
			missing = append(missing, op.Resource)
			continue
		}
		name := op.Items[0]
		actual, found := current[name]
		ok := completed && found && actual.ActiveState != "failed" && (actual.ObservedActive || actual.ServiceType == "oneshot" && actual.ExecutionResult == "success")
		if !ok {
			missing = append(missing, fmt.Sprintf("%s (approved activation)", name))
		}
	}
	return missing
}
