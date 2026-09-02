package restore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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

type failingRunner struct{ calls []string }

func (r *failingRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	r.calls = append(r.calls, name)
	if name == "validate" {
		return "", fmt.Errorf("invalid")
	}
	return "", nil
}

func TestExecuteBlocksDependentOperationsButContinuesIndependentOnes(t *testing.T) {
	j, err := NewJournal(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	runner := &failingRunner{}
	plan := model.RestorePlan{Operations: []model.Operation{{ID: "validate", Command: []string{"validate"}}, {ID: "copy", Command: []string{"copy"}, DependsOn: []string{"validate"}}, {ID: "independent", Command: []string{"other"}}}}
	result, err := Execute(context.Background(), runner, plan, j, time.Now, time.Second, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Failed) != 2 || len(result.Completed) != 1 {
		t.Fatalf("result=%#v", result)
	}
	if !reflect.DeepEqual(runner.calls, []string{"validate", "other"}) {
		t.Fatalf("calls=%#v", runner.calls)
	}
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

func TestExecuteCopiesThemeOnlyWhenDestinationIsMissing(t *testing.T) {
	source, destination := t.TempDir(), filepath.Join(t.TempDir(), "custom")
	if err := os.WriteFile(filepath.Join(source, "colors.toml"), []byte("accent = '#fff'\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	j, err := NewJournal(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	plan := model.RestorePlan{Operations: []model.Operation{{ID: "themes.copy.custom", Provider: "themes", Action: "copy", Copy: &model.Copy{Source: source, Destination: destination}}}}
	result, err := Execute(context.Background(), delayedRunner{}, plan, j, time.Now, time.Second, nil)
	if err != nil || len(result.Completed) != 1 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if got, err := os.ReadFile(filepath.Join(destination, "colors.toml")); err != nil || string(got) != "accent = '#fff'\n" {
		t.Fatalf("file=%q err=%v", got, err)
	}

	j2, err := NewJournal(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer j2.Close()
	result, err = Execute(context.Background(), delayedRunner{}, plan, j2, time.Now, time.Second, nil)
	if err != nil || len(result.Failed) != 1 {
		t.Fatalf("existing destination result=%#v err=%v", result, err)
	}
}
