package restore

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/graeme/omarchy-blueprint/internal/model"
)

type delayedRunner struct{ delay time.Duration }

func (r delayedRunner) Run(context.Context, string, ...string) (string, error) {
	time.Sleep(r.delay)
	return "", nil
}

func TestExecuteReportsAndJournalsProgress(t *testing.T) {
	journal, err := NewJournal(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	plan := model.RestorePlan{Operations: []model.Operation{{
		ID: "packages.install.official", Provider: "packages", Action: "install",
		Items: []string{"git", "zoxide"}, Command: []string{"omarchy", "pkg", "add", "git", "zoxide"},
	}}}
	var events []ProgressType
	result, err := Execute(context.Background(), delayedRunner{delay: 15 * time.Millisecond}, plan, journal, time.Now, 2*time.Millisecond, func(event Progress) {
		events = append(events, event.Type)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) < 3 || events[0] != ProgressStarted || events[len(events)-1] != ProgressCompleted {
		t.Fatalf("events = %#v", events)
	}
	foundHeartbeat := false
	for _, event := range events {
		if event == ProgressHeartbeat {
			foundHeartbeat = true
		}
	}
	if !foundHeartbeat {
		t.Fatalf("no heartbeat in %#v", events)
	}
	if len(result.Completed) != 1 || len(result.Failed) != 0 {
		t.Fatalf("result = %#v", result)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(journal.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "OPERATION_PROGRESS") {
		t.Fatalf("journal missing progress: %s", b)
	}
}
