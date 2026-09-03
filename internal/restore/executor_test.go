package restore

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/content"
	"github.com/Grenco/omarchy-blueprint/internal/model"
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

func TestExecuteFileWriteAtomicallyReplacesDestinationAndCreatesBackup(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "saved")
	destination := filepath.Join(dir, "config.lua")
	if err := os.WriteFile(source, []byte("saved configuration\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("default configuration\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old, err := os.Open(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	sourceHash := hashFile(t, source)
	destinationHash := hashFile(t, destination)
	journal, err := NewJournal(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()

	plan := model.RestorePlan{Operations: []model.Operation{{
		ID: "config.write.hypr.bindings", File: &model.FileWrite{Source: source, Destination: destination, SourceHash: sourceHash, ExpectedHash: destinationHash, Backup: true},
	}}}
	result, err := Execute(context.Background(), delayedRunner{}, plan, journal, time.Now, time.Second, nil)
	if err != nil || len(result.Completed) != 1 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if !result.Completed[0].Reversible {
		t.Fatal("backed-up file write was not marked reversible")
	}
	if got, err := os.ReadFile(destination); err != nil || string(got) != "saved configuration\n" {
		t.Fatalf("destination=%q err=%v", got, err)
	}
	if got, err := io.ReadAll(old); err != nil || string(got) != "default configuration\n" {
		t.Fatalf("open destination=%q err=%v", got, err)
	}
	if info, err := os.Stat(destination); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("destination mode=%v err=%v", info.Mode(), err)
	}
	backup := filepath.Join(strings.TrimSuffix(journal.Path, ".jsonl")+".backup", "config.write.hypr.bindings")
	if got, err := os.ReadFile(backup); err != nil || string(got) != "default configuration\n" {
		t.Fatalf("backup=%q err=%v", got, err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadFile(journal.Path)
	if err != nil || !strings.Contains(string(entries), "BACKUP_CREATED") || !strings.Contains(string(entries), backup) {
		t.Fatalf("journal=%q err=%v", entries, err)
	}
}

func TestExecuteFileWriteCreatesMissingDestination(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "saved")
	destination := filepath.Join(dir, "nested", "config.lua")
	if err := os.WriteFile(source, []byte("saved configuration\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	journal, err := NewJournal(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	plan := model.RestorePlan{Operations: []model.Operation{{ID: "config.write.new", File: &model.FileWrite{Source: source, Destination: destination, SourceHash: hashFile(t, source), ExpectedMissing: true}}}}
	result, err := Execute(context.Background(), delayedRunner{}, plan, journal, time.Now, time.Second, nil)
	if err != nil || len(result.Completed) != 1 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if got, err := os.ReadFile(destination); err != nil || string(got) != "saved configuration\n" {
		t.Fatalf("destination=%q err=%v", got, err)
	}
	if info, err := os.Stat(destination); err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("destination mode=%v err=%v", info.Mode(), err)
	}
}

func TestExecuteFileWriteRejectsCorruptionDriftAndSymlinks(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(t *testing.T, source, destination string) *model.FileWrite
		verify func(t *testing.T, destination string)
	}{
		{
			name: "source corruption", setup: func(t *testing.T, source, destination string) *model.FileWrite {
				hash := hashFile(t, source)
				if err := os.WriteFile(source, []byte("changed after planning"), 0o600); err != nil {
					t.Fatal(err)
				}
				return &model.FileWrite{Source: source, Destination: destination, SourceHash: hash, ExpectedMissing: true}
			},
			verify: func(t *testing.T, destination string) {
				if _, err := os.Lstat(destination); !os.IsNotExist(err) {
					t.Fatalf("destination was created: %v", err)
				}
			},
		},
		{
			name: "destination drift", setup: func(t *testing.T, source, destination string) *model.FileWrite {
				if err := os.WriteFile(destination, []byte("captured baseline"), 0o600); err != nil {
					t.Fatal(err)
				}
				hash := hashFile(t, destination)
				if err := os.WriteFile(destination, []byte("unknown work"), 0o600); err != nil {
					t.Fatal(err)
				}
				return &model.FileWrite{Source: source, Destination: destination, SourceHash: hashFile(t, source), ExpectedHash: hash, Backup: true}
			},
			verify: func(t *testing.T, destination string) {
				if got, err := os.ReadFile(destination); err != nil || string(got) != "unknown work" {
					t.Fatalf("destination=%q err=%v", got, err)
				}
			},
		},
		{
			name: "destination symlink", setup: func(t *testing.T, source, destination string) *model.FileWrite {
				target := filepath.Join(filepath.Dir(destination), "target")
				if err := os.WriteFile(target, []byte("default configuration\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, destination); err != nil {
					t.Fatal(err)
				}
				return &model.FileWrite{Source: source, Destination: destination, SourceHash: hashFile(t, source), ExpectedMissing: true}
			},
			verify: func(t *testing.T, destination string) {
				info, err := os.Lstat(destination)
				if err != nil || info.Mode()&os.ModeSymlink == 0 {
					t.Fatalf("destination mode=%v err=%v", info.Mode(), err)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			source, destination := filepath.Join(dir, "saved"), filepath.Join(dir, "config.lua")
			if err := os.WriteFile(source, []byte("saved configuration\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			journal, err := NewJournal(t.TempDir(), time.Now())
			if err != nil {
				t.Fatal(err)
			}
			defer journal.Close()
			plan := model.RestorePlan{Operations: []model.Operation{{ID: "config.write", File: tt.setup(t, source, destination)}}}
			result, err := Execute(context.Background(), delayedRunner{}, plan, journal, time.Now, time.Second, nil)
			if err != nil || len(result.Failed) != 1 {
				t.Fatalf("result=%#v err=%v", result, err)
			}
			tt.verify(t, destination)
		})
	}
}

func TestExecuteFileWriteFailureBlocksDependentsButNotIndependentOperations(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "saved")
	if err := os.WriteFile(source, []byte("saved configuration\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	journal, err := NewJournal(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	runner := &failingRunner{}
	plan := model.RestorePlan{Operations: []model.Operation{
		{ID: "config.write", File: &model.FileWrite{Source: source, Destination: filepath.Join(dir, "config.lua"), SourceHash: "invalid", ExpectedMissing: true}},
		{ID: "hypr.reload", Command: []string{"reload"}, DependsOn: []string{"config.write"}},
		{ID: "independent", Command: []string{"other"}},
	}}
	result, err := Execute(context.Background(), runner, plan, journal, time.Now, time.Second, nil)
	if err != nil || len(result.Failed) != 2 || len(result.Completed) != 1 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if !reflect.DeepEqual(runner.calls, []string{"other"}) {
		t.Fatalf("calls=%#v", runner.calls)
	}
}

func TestExecuteRejectsOperationsWithMultipleActions(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "copy")
	journal, err := NewJournal(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	runner := &failingRunner{}
	plan := model.RestorePlan{Operations: []model.Operation{{
		ID: "invalid", Command: []string{"other"}, Copy: &model.Copy{Source: t.TempDir(), Destination: destination},
	}}}
	result, err := Execute(context.Background(), runner, plan, journal, time.Now, time.Second, nil)
	if err != nil || len(result.Failed) != 1 || len(result.Completed) != 0 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner invoked for invalid operation: %#v", runner.calls)
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatalf("copy action ran for invalid operation: %v", err)
	}
}

func hashFile(t *testing.T, path string) string {
	t.Helper()
	hash, err := content.HashRegularFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

func TestExecutePreCanceledContextRunsNoOperations(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "saved")
	destination := filepath.Join(dir, "config.lua")
	if err := os.WriteFile(source, []byte("saved\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sourceHash := hashFile(t, source)
	journal, err := NewJournal(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	plan := model.RestorePlan{Operations: []model.Operation{{
		ID: "config.write.hypr.bindings", File: &model.FileWrite{Source: source, Destination: destination, SourceHash: sourceHash, ExpectedMissing: true},
	}}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = Execute(ctx, delayedRunner{}, plan, journal, time.Now, time.Second, nil)
	if err != context.Canceled {
		t.Fatalf("err = %v want context.Canceled", err)
	}
	if _, statErr := os.Lstat(destination); !os.IsNotExist(statErr) {
		t.Fatalf("destination must not exist after pre-cancel, statErr=%v", statErr)
	}
}

func TestExecuteWaitsForInFlightOperationBeforeReturning(t *testing.T) {
	journal, err := NewJournal(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	plan := model.RestorePlan{Operations: []model.Operation{{
		ID: "validate.slow", Command: []string{"slow"},
	}}}
	ctx, cancel := context.WithCancel(context.Background())
	started := time.Now()
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	_, err = Execute(ctx, delayedRunner{delay: 200 * time.Millisecond}, plan, journal, time.Now, 10*time.Millisecond, nil)
	elapsed := time.Since(started)
	if err != context.Canceled {
		t.Fatalf("err = %v want context.Canceled", err)
	}
	if elapsed < 190*time.Millisecond {
		t.Fatalf("Execute returned in %v; it must wait for the in-flight operation to finish", elapsed)
	}
}

type contextAwareRunner struct{}

func (contextAwareRunner) Run(ctx context.Context, _ string, _ ...string) (string, error) {
	<-ctx.Done()
	return "", ctx.Err()
}

type completeAfterCancelRunner struct{}

func (completeAfterCancelRunner) Run(ctx context.Context, _ string, _ ...string) (string, error) {
	<-ctx.Done()
	return "", nil
}

func journalEvents(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var events []string
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var event Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		events = append(events, event.Type)
	}
	return events
}

func TestExecuteRecordsCompletedOperationDespiteCancellation(t *testing.T) {
	journal, err := NewJournal(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	plan := model.RestorePlan{Operations: []model.Operation{{
		// Emulates a local mutation that is already past its safe
		// cancellation point when the context is cancelled: it finishes
		// successfully during the cancellation wait.
		ID: "config.write.hypr.bindings", Command: []string{"finish-after-cancel"},
	}}}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()
	result, err := Execute(ctx, completeAfterCancelRunner{}, plan, journal, time.Now, 10*time.Millisecond, nil)
	if err != context.Canceled {
		t.Fatalf("err = %v want context.Canceled", err)
	}
	if len(result.Completed) != 1 || result.Completed[0].ID != "config.write.hypr.bindings" {
		t.Fatalf("operation completed during cancellation must be recorded: %#v", result)
	}
	events := journalEvents(t, journal.Path)
	found := false
	for _, eventType := range events {
		if eventType == "OPERATION_COMPLETED" {
			found = true
		}
	}
	if !found {
		t.Fatalf("journal must record OPERATION_COMPLETED, got %v", events)
	}
}

func TestExecuteJournalsCancelledCommand(t *testing.T) {
	journal, err := NewJournal(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	plan := model.RestorePlan{Operations: []model.Operation{{
		ID: "slow.command", Command: []string{"slow"},
	}}}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	result, err := Execute(ctx, contextAwareRunner{}, plan, journal, time.Now, 10*time.Millisecond, nil)
	if err != context.Canceled {
		t.Fatalf("err = %v want context.Canceled", err)
	}
	if len(result.Completed) != 0 {
		t.Fatalf("aborted command must not be completed: %#v", result)
	}
	events := journalEvents(t, journal.Path)
	hasCancelled, hasCancelling := false, false
	for _, eventType := range events {
		if eventType == "OPERATION_CANCELLED" {
			hasCancelled = true
		}
		if eventType == "OPERATION_CANCELLING" {
			hasCancelling = true
		}
	}
	if !hasCancelled || !hasCancelling {
		t.Fatalf("journal must record OPERATION_CANCELLING then OPERATION_CANCELLED, got %v", events)
	}
}
