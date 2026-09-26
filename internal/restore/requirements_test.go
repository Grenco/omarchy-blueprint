package restore

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/model"
)

func TestValidatePlanRequiresRequirementsToNameRealOperations(t *testing.T) {
	plan := model.RestorePlan{
		Operations:   []model.Operation{{ID: "packages.install.official", Command: []string{"omarchy", "pkg", "add", "git"}}},
		Requirements: []model.Requirement{{ID: "packages.metadata", Kind: "package-metadata", Remediation: []string{"omarchy", "update"}, Operations: []string{"packages.install.official"}}},
	}
	if err := ValidatePlan(plan); err != nil {
		t.Fatal(err)
	}
	for _, broken := range []model.Requirement{
		{ID: "packages.metadata", Kind: "package-metadata", Remediation: []string{"omarchy", "update"}, Operations: []string{"missing"}},
		{ID: "", Kind: "package-metadata", Remediation: []string{"omarchy", "update"}, Operations: []string{"packages.install.official"}},
		{ID: "packages.metadata", Kind: "package-metadata", Operations: []string{"packages.install.official"}},
	} {
		plan.Requirements = []model.Requirement{broken}
		if err := ValidatePlan(plan); err == nil {
			t.Fatalf("invalid requirement accepted: %#v", broken)
		}
	}
}

type bufferedOnlyRunner struct{ calls int }

func (r *bufferedOnlyRunner) Run(context.Context, string, ...string) (string, error) {
	r.calls++
	return "", nil
}

func TestInteractiveOperationWithoutTerminalRunnerFailsAndIsJournalled(t *testing.T) {
	journal, err := NewJournal(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	runner := &bufferedOnlyRunner{}
	plan := model.RestorePlan{Operations: []model.Operation{{ID: "packages.install.official", Command: []string{"omarchy", "pkg", "add", "git"}, Interactive: true}}}
	result, err := Execute(context.Background(), runner, plan, journal, time.Now, time.Second, nil)
	if err != nil || len(result.Failed) != 1 || runner.calls != 0 {
		t.Fatalf("result=%#v err=%v buffered calls=%d", result, err, runner.calls)
	}
	data, err := os.ReadFile(journal.Path)
	if err != nil || !strings.Contains(string(data), "requires an interactive command runner") {
		t.Fatalf("failure not journalled: %s %v", data, err)
	}
}
