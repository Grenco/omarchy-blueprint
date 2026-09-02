package plugins

import (
	"context"
	"reflect"
	"testing"

	"github.com/graeme/omarchy-blueprint/internal/profile"
)

type runnerFunc func(context.Context, string, ...string) (string, error)

func (f runnerFunc) Run(ctx context.Context, name string, args ...string) (string, error) {
	return f(ctx, name, args...)
}

func TestDetectDiffPlanAndVerify(t *testing.T) {
	json := `[{"id":"omarchy.clock","enabled":true,"firstParty":true,"canDisable":true},{"id":"third.party","enabled":true,"firstParty":false,"canDisable":true}]`
	p := Provider{Runner: runnerFunc(func(context.Context, string, ...string) (string, error) { return json, nil })}
	current, err := p.Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(current.Items, []profile.Plugin{{ID: "omarchy.clock", Enabled: true}}) {
		t.Fatalf("plugins=%#v", current.Items)
	}
	saved := profile.Plugins{Items: []profile.Plugin{{ID: "omarchy.clock", Enabled: false}}}
	if len(Diff(saved, current)) != 1 {
		t.Fatal("expected drift")
	}
	plan := Plan(saved, current, 1, "4", "4")
	if got := plan.Operations[0].Command; !reflect.DeepEqual(got, []string{"omarchy", "plugin", "disable", "omarchy.clock"}) {
		t.Fatalf("command=%#v", got)
	}
	if Verify(saved, current).OK {
		t.Fatal("verification unexpectedly passed")
	}
}
