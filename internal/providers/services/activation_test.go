package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
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
	p, data, _, live := persistentPlanFixture(t)
	data.Services.Units[0].ObservedActive = true
	data.Services.Units[0].ActivationPreference = profile.ServiceActivationRestoreWorkingState
	if err := os.Remove(live); err != nil {
		t.Fatal(err)
	}
	// The manager retains established source/relationship evidence even though
	// the definition vanished on disk; guarded reconstruction precedes its start.
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

func twoActivationsFixture(t *testing.T) (*Provider, *profile.Data, *planSystemd) {
	t.Helper()
	p, data, s, live := persistentPlanFixture(t)
	first := &data.Services.Units[0]
	first.ObservedActive, first.ActivationPreference, first.StartIntent = true, profile.ServiceActivationRestoreWorkingState, profile.ServiceStartEnabled
	second := *first
	second.Name, second.Definition = "beta.service", "units/beta.service"
	bytes, err := os.ReadFile(live)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.ProfileDir, "services", second.Definition), bytes, 0o644); err != nil {
		t.Fatal(err)
	}
	secondPath := filepath.Join(p.Roots.UserConfigDir, second.Name)
	if err := os.WriteFile(secondPath, bytes, 0o644); err != nil {
		t.Fatal(err)
	}
	data.Services.Units = append(data.Services.Units, second)
	s.units = append(s.units, observedService(second.Name, secondPath))
	return p, data, s
}

func TestIndependentActivationsDoNotDependOnEachOther(t *testing.T) {
	p, data, _ := twoActivationsFixture(t)
	rc := planContext(*data, false, false)
	rc.Options.Activation = policy.ActivationRestoreWorkingState
	fragment, err := p.Plan(context.Background(), *data, omarchy.Info{}, rc)
	if err != nil {
		t.Fatal(err)
	}
	var persistent []string
	var starts []model.Operation
	for _, op := range fragment.Operations {
		if op.Action == "start" {
			starts = append(starts, op)
		} else {
			persistent = append(persistent, op.ID)
		}
	}
	if len(starts) != 2 || len(persistent) == 0 {
		t.Fatalf("fixture lacks two starts and persistent work: %+v", fragment)
	}
	for _, start := range starts {
		if !reflect.DeepEqual(start.DependsOn, persistent) {
			t.Fatalf("start depends on sibling activation: %+v persistent=%v", start, persistent)
		}
	}
}

type independentActivationRunner struct{ starts []string }

func (r *independentActivationRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	if name == "systemctl" && len(args) > 1 && args[1] == "start" {
		unit := args[len(args)-1]
		r.starts = append(r.starts, unit)
		if unit == "backup.service" {
			return "", errors.New("first activation failed")
		}
	}
	return "", nil
}

func TestFailedActivationDoesNotBlockIndependentApprovedActivation(t *testing.T) {
	p, data, _ := twoActivationsFixture(t)
	for n := range data.Services.Units {
		data.Services.Units[n].ActivationPreference = profile.ServiceActivationReview
	}
	rc := planContext(*data, false, false)
	rc.Options.Activation = policy.ActivationReview
	rc.Options.ReviewActivation = &policy.ActivationReviewSelection{Units: []string{"backup.service", "beta.service"}}
	fragment, err := p.Plan(context.Background(), *data, omarchy.Info{}, rc)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := restore.NewJournal(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	runner := &independentActivationRunner{}
	result, err := restore.Execute(context.Background(), runner, model.RestorePlan{Operations: fragment.Operations}, journal, time.Now, time.Second, nil)
	if err != nil || len(result.Failed) != 1 || len(result.Blocked) != 0 || !reflect.DeepEqual(runner.starts, []string{"backup.service", "beta.service"}) {
		t.Fatalf("failed start blocked independent approved sibling: %+v starts=%v err=%v", result, runner.starts, err)
	}
}

func TestFreshRestoreDoesNotDirectlyStartTriggeredService(t *testing.T) {
	for _, mode := range []policy.ActivationMode{policy.ActivationRestoreWorkingState, policy.ActivationReview} {
		p, data, s, live := persistentPlanFixture(t)
		data.Services.Units[0].ObservedActive, data.Services.Units[0].ActivationPreference = true, profile.ServiceActivationRestoreWorkingState
		timer := profile.ServiceUnit{Name: "backup.timer", Kind: "timer", Management: profile.ServiceManagementDefinition, Presence: profile.ServicePresent, Definition: "units/backup.timer", StartIntent: profile.ServiceStartEnabled, ObservedActive: true, ActivationPreference: profile.ServiceActivationRestoreWorkingState}
		path := filepath.Join(p.ProfileDir, "services", timer.Definition)
		if err := os.WriteFile(path, []byte("[Timer]\nOnBootSec=1h\nUnit=backup.service\n[Install]\nWantedBy=timers.target\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		file, err := readServiceFile(path)
		if err != nil {
			t.Fatal(err)
		}
		timer.DefinitionHash = file.hash
		data.Services.Units = append(data.Services.Units, timer)
		if err := os.Remove(live); err != nil {
			t.Fatal(err)
		}
		s.units = nil
		rc := planContext(*data, false, false)
		rc.Options.Activation = mode
		if mode == policy.ActivationReview {
			rc.Options.ReviewActivation = &policy.ActivationReviewSelection{Units: []string{"backup.service", "backup.timer"}}
		}
		fragment, err := p.Plan(context.Background(), *data, omarchy.Info{}, rc)
		if err != nil {
			t.Fatal(err)
		}
		writes, starts := 0, 0
		for _, op := range fragment.Operations {
			if op.File != nil {
				writes++
			}
			if op.Action == "start" {
				starts++
				if op.Items[0] != timer.Name {
					t.Fatalf("fresh triggered service directly invoked: %+v", op)
				}
			}
		}
		if writes != 2 || starts != 1 || len(fragment.Skipped) == 0 {
			t.Fatalf("fresh restore lost persistent work or entry-point safety: %+v", fragment)
		}
		for _, candidate := range fragment.ActivationReview {
			if candidate.Unit == "backup.service" {
				t.Fatal("fresh service offered individual activation without relationship evidence")
			}
		}
	}
}

func TestSliceNeverReceivesGenericWorkingStateActivation(t *testing.T) {
	for _, mode := range []policy.ActivationMode{policy.ActivationRestoreWorkingState, policy.ActivationReview} {
		saved := profile.ServiceUnit{Name: "background.slice", Presence: profile.ServicePresent, ObservedActive: true, ActivationPreference: profile.ServiceActivationRestoreWorkingState}
		data := profile.Data{Services: profile.Services{Units: []profile.ServiceUnit{saved}}}
		rc := planContext(data, false, false)
		rc.Options.Activation = mode
		if mode == policy.ActivationReview {
			rc.Options.ReviewActivation = &policy.ActivationReviewSelection{Units: []string{saved.Name}}
		}
		ops, candidates, skipped := planActivation(saved, map[string]ObservedUnit{saved.Name: {Name: saved.Name, TopologyKnown: true}}, rc)
		if len(ops) != 0 || len(candidates) != 0 || len(skipped) == 0 {
			t.Fatalf("slice treated as ordinary captured-running process: ops=%v candidates=%v skips=%v", ops, candidates, skipped)
		}
	}
}
