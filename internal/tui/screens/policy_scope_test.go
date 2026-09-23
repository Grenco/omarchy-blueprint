package screens

import (
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func TestPolicyScopeLabelOnlyShowsDistinctScope(t *testing.T) {
	for _, test := range []struct {
		name, active, want string
		scope              workflow.PolicyScope
	}{
		{name: "active machine is already in app header", active: "laptop", scope: workflow.PolicyScope{Machine: "laptop"}, want: ""},
		{name: "profile defaults remain explicit", active: "laptop", scope: workflow.PolicyScope{}, want: "Profile defaults"},
		{name: "different machine remains explicit", active: "laptop", scope: workflow.PolicyScope{Machine: "desktop"}, want: "Policy scope: desktop"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := policyScopeLabel(test.scope, test.active); got != test.want {
				t.Fatalf("label = %q, want %q", got, test.want)
			}
		})
	}
}
