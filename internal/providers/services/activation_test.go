package services

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/omarchy"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/restore"
)

func TestActivationAuthorityIntersection(t *testing.T) {
	for _, tc := range []struct {
		name                              string
		mode                              policy.ActivationMode
		pref                              profile.ServiceActivationPreference
		captured, running, reviewed, want bool
	}{
		{"default-headless", "", profile.ServiceActivationRestoreWorkingState, true, false, false, false},
		{"persistent", policy.ActivationPersistentOnly, profile.ServiceActivationRestoreWorkingState, true, false, false, false},
		{"working", policy.ActivationRestoreWorkingState, profile.ServiceActivationRestoreWorkingState, true, false, false, true},
		{"inactive-at-capture", policy.ActivationRestoreWorkingState, profile.ServiceActivationRestoreWorkingState, false, false, false, false},
		{"persistent-preference", policy.ActivationRestoreWorkingState, profile.ServiceActivationPersistentOnly, true, false, false, false},
		{"review-preference-in-working", policy.ActivationRestoreWorkingState, profile.ServiceActivationReview, true, false, false, false},
		{"review-not-approved", policy.ActivationReview, profile.ServiceActivationReview, true, false, false, false},
		{"review-approved", policy.ActivationReview, profile.ServiceActivationReview, true, false, true, true},
		{"persistent-preference-in-review", policy.ActivationReview, profile.ServiceActivationPersistentOnly, true, false, true, false},
		{"already-running", policy.ActivationRestoreWorkingState, profile.ServiceActivationRestoreWorkingState, true, true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, data, s, _ := persistentPlanFixture(t)
			data.Services.Units[0].ObservedActive, data.Services.Units[0].ActivationPreference = tc.captured, tc.pref
			s.units[0].ObservedActive = tc.running
			rc := planContext(*data, false, false)
			rc.Options.Activation = tc.mode
			if tc.reviewed {
				rc.Options.ReviewActivation = &policy.ActivationReviewSelection{Units: []string{"backup.service"}}
			}
			fragment, err := p.Plan(context.Background(), *data, omarchy.Info{}, rc)
			if err != nil || hasCommand(fragment, "start") != tc.want || hasCommand(fragment, "stop") || hasCommand(fragment, "restart") || hasCommand(fragment, "reload") {
				t.Fatalf("activation intersection violated: %+v err=%v", fragment, err)
			}
			if tc.name == "review-preference-in-working" && len(fragment.Skipped) == 0 {
				t.Fatal("review preference was silently swept into working mode")
			}
		})
	}
}

func TestCompatibilityWithholdsActivationWithoutBlockingPersistentRestore(t *testing.T) {
	p, data, s, _ := persistentPlanFixture(t)
	data.Services.Units[0].ObservedActive = true
	data.Services.Units[0].ActivationPreference = profile.ServiceActivationRestoreWorkingState
	s.units[0].RelatedUnits = []string{"missing.service"}
	rc := planContext(*data, false, false)
	rc.Options.Activation = policy.ActivationRestoreWorkingState
	fragment, err := p.Plan(context.Background(), *data, omarchy.Info{}, rc)
	if err != nil || hasCommand(fragment, "start") || fragment.Compatibility.Authority != model.CompatibilityUnchanged || len(fragment.Skipped) == 0 {
		t.Fatalf("dependency uncertainty granted activation or blocked persistent intent: %+v err=%v", fragment, err)
	}
}

func TestActivationStartsSemanticEntryPoint(t *testing.T) {
	for _, kind := range []string{"timer", "socket", "path"} {
		t.Run(kind, func(t *testing.T) {
			name := "backup." + kind
			saved := profile.ServiceUnit{Name: name, Presence: profile.ServicePresent, ObservedActive: true, ActivationPreference: profile.ServiceActivationRestoreWorkingState}
			data := profile.Data{Services: profile.Services{Units: []profile.ServiceUnit{saved}}}
			rc := planContext(data, false, false)
			rc.Options.Activation = policy.ActivationRestoreWorkingState
			ops, _, _ := planActivation(saved, map[string]ObservedUnit{}, rc)
			if len(ops) != 1 || ops[0].Command[4] != name {
				t.Fatalf("wrong entry point: %+v", ops)
			}
			service := saved
			service.Name = "backup.service"
			rc.Targets[service.Name] = rc.Targets[name]
			ops, _, _ = planActivation(service, map[string]ObservedUnit{service.Name: {Name: service.Name, TriggeredBy: []string{name}}}, rc)
			if len(ops) != 0 {
				t.Fatalf("triggered service invoked directly: %+v", ops)
			}
		})
	}
}

func TestActivationVerifyUsesApprovedOperationAndOneshotReceipt(t *testing.T) {
	p, data, s, _ := persistentPlanFixture(t)
	data.Services.Units[0].ObservedActive = true
	data.Services.Units[0].ActivationPreference = profile.ServiceActivationRestoreWorkingState
	rc := planContext(*data, false, false)
	rc.Options.Activation = policy.ActivationRestoreWorkingState
	fragment, err := p.Plan(context.Background(), *data, omarchy.Info{}, rc)
	if err != nil {
		t.Fatal(err)
	}
	rc.PlannedOperations = fragment.Operations
	s.units[0].ServiceType, s.units[0].ExecutionResult = "oneshot", "success"
	result, err := p.Verify(context.Background(), *data, rc)
	if err != nil || result.OK {
		t.Fatalf("missing execution receipt falsely verified: %+v %v", result, err)
	}
	rc.CompletedOperations = fragment.Operations
	result, err = p.Verify(context.Background(), *data, rc)
	if err != nil || !result.OK {
		t.Fatalf("successful inactive oneshot failed Verify: %+v %v", result, err)
	}
	s.units[0].ActiveState = "failed"
	result, err = p.Verify(context.Background(), *data, rc)
	if err != nil || result.OK {
		t.Fatalf("failed activation falsely converged: %+v %v", result, err)
	}
	rc.PlannedOperations = nil
	result, err = p.Verify(context.Background(), *data, rc)
	if err != nil || !result.OK {
		t.Fatalf("unplanned runtime intent was verified: %+v %v", result, err)
	}
}

func TestDefinitionFailureBlocksApprovedActivation(t *testing.T) {
	p, data, s, live := persistentPlanFixture(t)
	data.Services.Units[0].ObservedActive = true
	data.Services.Units[0].ActivationPreference = profile.ServiceActivationRestoreWorkingState
	if err := os.Remove(live); err != nil {
		t.Fatal(err)
	}
	s.units = nil
	rc := planContext(*data, false, false)
	rc.Options.Activation = policy.ActivationRestoreWorkingState
	fragment, err := p.Plan(context.Background(), *data, omarchy.Info{}, rc)
	if err != nil || !hasCommand(fragment, "start") {
		t.Fatalf("fixture lacks approved activation: %+v %v", fragment, err)
	}
	if err := os.WriteFile(live, []byte("unapproved destination race"), 0o644); err != nil {
		t.Fatal(err)
	}
	journal, err := restore.NewJournal(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	runner := &recordingRunner{}
	result, err := restore.Execute(context.Background(), runner, model.RestorePlan{Operations: fragment.Operations}, journal, time.Now, time.Second, nil)
	if err != nil || len(result.Failed) != 1 {
		t.Fatalf("definition race not rejected: %+v %v", result, err)
	}
	blocked := false
	for _, op := range result.Blocked {
		blocked = blocked || op.Operation.Action == "start"
	}
	if !blocked {
		t.Fatalf("failed definition retained start authority: %+v", result)
	}
}

func TestRejectedProposedDefinitionsDoNotOfferActivationReview(t *testing.T) {
	p, data, s, _ := persistentPlanFixture(t)
	data.Services.Units[0].ObservedActive = true
	data.Services.Units[0].ActivationPreference = profile.ServiceActivationReview
	s.verifyErr = errors.New("invalid unit")
	rc := planContext(*data, false, false)
	rc.Options.Activation = policy.ActivationReview
	fragment, err := p.Plan(context.Background(), *data, omarchy.Info{}, rc)
	if err != nil || len(fragment.ActivationReview) != 0 || hasCommand(fragment, "start") || fragment.Compatibility.Authority != model.CompatibilityBlocked {
		t.Fatalf("rejected definitions offered activation: %+v %v", fragment, err)
	}
}
