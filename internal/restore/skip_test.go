package restore

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/command"
	"github.com/Grenco/omarchy-blueprint/internal/model"
)

// skippingRunner reports the interactive step named skip as skipped by the
// person and every other command as succeeding.
type skippingRunner struct {
	skip string
	ran  []string
}

func (r *skippingRunner) Run(_ context.Context, name string, _ ...string) (string, error) {
	r.ran = append(r.ran, name)
	return "", nil
}

func (r *skippingRunner) RunInteractive(_ context.Context, name string, _ ...string) error {
	r.ran = append(r.ran, name)
	if name == r.skip {
		return &command.RunError{Name: name, ExitCode: 130, Err: fmt.Errorf("%w (exit status 130)", command.ErrSkippedByUser)}
	}
	return nil
}

func TestSkippedStepHoldsBackOnlyItsDependents(t *testing.T) {
	journal, err := NewJournal(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	plan := model.RestorePlan{Operations: []model.Operation{
		{ID: "a", Provider: "packages", Action: "install", Resource: "official:a", Command: []string{"install-a"}, Interactive: true},
		{ID: "tailscale", Provider: "packages", Action: "install", Resource: "official:tailscale", Command: []string{"setup-tailscale"}, Interactive: true},
		{ID: "after", Provider: "packages", Action: "install", Resource: "official:after", Command: []string{"after-tailscale"}, DependsOn: []string{"tailscale"}, Interactive: true},
		{ID: "b", Provider: "packages", Action: "install", Resource: "official:b", Command: []string{"install-b"}, Interactive: true},
	}}
	runner := &skippingRunner{skip: "setup-tailscale"}
	var skipped []string
	result, err := Execute(context.Background(), runner, plan, journal, time.Now, time.Hour, func(event Progress) {
		if event.Type == ProgressSkipped {
			skipped = append(skipped, event.Operation.ID)
		}
	})
	if err != nil {
		t.Fatalf("a skipped step stopped the restore: %v", err)
	}
	if !reflect.DeepEqual(runner.ran, []string{"install-a", "setup-tailscale", "install-b"}) {
		t.Fatalf("ran %v; want everything except the skipped step's dependent", runner.ran)
	}
	if len(result.Failed) != 0 || len(result.SkippedByYou) != 1 || result.SkippedByYou[0].ID != "tailscale" || len(result.Blocked) != 1 || result.Blocked[0].Operation.ID != "after" {
		t.Fatalf("result = %+v; want tailscale skipped (not failed) and only its dependent held back", result)
	}
	if !reflect.DeepEqual(skipped, []string{"tailscale"}) {
		t.Fatalf("skip progress = %v", skipped)
	}
}

func TestAwaitingStepsRunLastWithTheirDependents(t *testing.T) {
	ops := []model.Operation{
		{ID: "a"},
		{ID: "tailscale", AwaitsYou: "sign in"},
		{ID: "after", DependsOn: []string{"tailscale"}},
		{ID: "b"},
		{ID: "c", DependsOn: []string{"a"}},
	}
	var order []string
	for _, op := range AwaitingStepsLast(ops) {
		order = append(order, op.ID)
	}
	if !reflect.DeepEqual(order, []string{"a", "b", "c", "tailscale", "after"}) {
		t.Fatalf("order = %v", order)
	}
	unchanged := []model.Operation{{ID: "a"}, {ID: "b"}}
	if got := AwaitingStepsLast(unchanged); !reflect.DeepEqual(got, unchanged) {
		t.Fatalf("plan without waiting steps was reordered: %v", got)
	}
}
